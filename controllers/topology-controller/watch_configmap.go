/*
Copyright 2023 The Nephio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package topologycontroller

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
)

func NewConfigMapEventHandler(cfg *ctrlconfig.Config) handler.EventHandler {
	return &cmEventHandler{
		client: cfg.Client,
		l:      ctrl.Log.WithName("configmap eventhandler"),
	}
}

type cmEventHandler struct {
	client client.Client
	l      logr.Logger
}

// Create enqueues a request for all ip allocation within the ipam
func (r *cmEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *cmEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *cmEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *cmEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

func (r *cmEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	cr, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return
	}
	r.l = log.FromContext(ctx)
	r.l.Info("event",
		"gvk", fmt.Sprintf("%s.%s",
			obj.GetObjectKind().GroupVersionKind().GroupVersion().String(),
			obj.GetObjectKind().GroupVersionKind().Kind),
		"name", cr.GetName())

	topos := &invv1alpha1.TopologyList{}
	if err := r.client.List(ctx, topos, []client.ListOption{}...); err != nil {
		r.l.Error(err, "cannot list nodes")
		return
	}

	// for any change in the configmaps in the network-system namespace
	// we retrigger the topology reconcile
	if cr.Namespace == os.Getenv("POD_NAMESPACE") && cr.Labels["app.kubernetes.io/name"] == os.Getenv("POD_NAMESPACE") {
		for _, topo := range topos.Items {
			queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: topo.GetNamespace(),
				Name:      topo.GetName(),
			}})
		}
	}
}
