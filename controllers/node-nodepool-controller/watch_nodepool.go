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

package nodenodepoolcontroller

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
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
)

func NewNodePoolEventHandler(cfg *ctrlconfig.Config) handler.EventHandler {
	return &nodePoolEventHandler{
		client: cfg.Client,
		l:      ctrl.Log.WithName("nodepool eventhandler"),
	}
}

type nodePoolEventHandler struct {
	client client.Client
	l      logr.Logger
}

// Create enqueues a request for all ip allocation within the ipam
func (r *nodePoolEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *nodePoolEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *nodePoolEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *nodePoolEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

func (r *nodePoolEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	cr, ok := obj.(*invv1alpha1.NodePool)
	if !ok {
		return
	}
	r.l = log.FromContext(ctx).WithValues("gvk", fmt.Sprintf("%s.%s", cr.APIVersion, cr.Kind), "name", cr.GetName())
	r.l.Info("event")

	nodes := &corev1.NodeList{}
	if err := r.client.List(ctx, nodes, []client.ListOption{}...); err != nil {
		r.l.Error(err, "cannot list nodes")
		return
	}

	for _, node := range nodes.Items {
		// only enqueue if the nodepool matches
		r.l.Info("event", "node labels", node.GetLabels())
		for labelKey, labelValue := range node.GetLabels() {
			if strings.Contains(labelKey, "nodepool") {
				if labelValue == cr.Name {
					queue.Add(reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: node.GetNamespace(),
						Name:      node.GetName(),
					}})
				}
			}
		}
	}
}
