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

package podcontroller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("nodecontroller", &reconciler{})
}

const (
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	// register scheme
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.nodeCache = cfg.NodeCache

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("NodeController").
			For(&corev1.Node{}).
			Complete(r)
}

// reconciler reconciles a KRM resource
type reconciler struct {
	client.Client

	nodeCache wire.Cache[wirenode.Node]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	cr := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		r.nodeCache.Delete(ctx, req.NamespacedName)
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		r.nodeCache.Delete(ctx, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// update (add/update) node to cache
	r.nodeCache.Upsert(ctx, req.NamespacedName, r.getNode(cr))

	return ctrl.Result{}, nil
}

// getNode retrieves specific data from the CR.
func (r *reconciler) getNode(n *corev1.Node) wirenode.Node {
	node := wirenode.Node{}

	for _, addr := range n.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			node.IsReady = true
			node.HostIP = addr.Address
		}
	}
	return node
}
