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

package endpointcachecontroller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wireendpoint "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/endpoint"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("endpointcachecontroller", &reconciler{})
}

const (
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// New Reconciler -> used for intercluster controller
func New(ctx context.Context, cfg *ctrlconfig.Config) reconcile.Reconciler {
	return &reconciler{
		Client:      cfg.Client,
		epCache:     cfg.EndpointCache,
		clusterName: cfg.ClusterName,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	// register scheme
	cfg, ok := c.(*ctrlconfig.Config)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.epCache = cfg.EndpointCache

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("EndpointCacheController").
			For(&invv1alpha1.Endpoint{}).
			Complete(r)
}

// reconciler reconciles a KRM resource
type reconciler struct {
	client.Client

	clusterName string
	epCache     wirer.Cache[wireendpoint.Endpoint]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("cluster", r.clusterName)
	log.Info("reconcile endpoint")

	clusterNamespace := r.clusterName
	if clusterNamespace == "" {
		clusterNamespace = "default"
	}

	cr := &invv1alpha1.Endpoint{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		r.epCache.Delete(ctx, req.NamespacedName)
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		r.epCache.Delete(ctx, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// update (add/update) node to cache
	cr = cr.DeepCopy()
	r.epCache.Upsert(ctx, req.NamespacedName, wireendpoint.Endpoint{
		Object:   wirer.Object{IsReady: true},
		Endpoint: *cr,
	})

	return ctrl.Result{}, nil
}
