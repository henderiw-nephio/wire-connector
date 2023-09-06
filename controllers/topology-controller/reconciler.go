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
	"reflect"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/topology"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("topologycontroller", &reconciler{})
}

const (
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// New Reconciler
func New(ctx context.Context, cfg *ctrlconfig.Config) reconcile.Reconciler {
	return &reconciler{
		Client:      cfg.Client,
		topoCache:   cfg.TopologyCache,
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
	r.topoCache = cfg.TopologyCache
	r.clusterName = cfg.ClusterName

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("PodController").
			For(&corev1.Pod{}).
			Complete(r)
}

// reconciler reconciles a KRM resource
type reconciler struct {
	client.Client

	clusterName string
	topoCache   wire.Cache[wiretopology.Topology]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("cluster", r.clusterName)
	log.Info("reconcile topology/namespace")

	cr := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		r.topoCache.Delete(ctx, types.NamespacedName{Namespace: r.clusterName, Name: req.Name})
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		r.topoCache.Delete(ctx, types.NamespacedName{Namespace: r.clusterName, Name: req.Name})
		return ctrl.Result{}, nil
	}

	// update (add/update) node to cache
	if cr.GetAnnotations()["wirer-key"] == "true" {
		// validate if namespace is not already used in another cluster
		t, err := r.topoCache.Get(types.NamespacedName{Name: req.Name})
		if err == nil {
			if t.ClusterName != r.clusterName {
				log.Error(fmt.Errorf("overlapping namespace"), "overlapping namespace", "cluster", t.ClusterName, "cluster", r.clusterName)
				return ctrl.Result{}, nil
			}
		}

		r.topoCache.Upsert(ctx, types.NamespacedName{Name: req.Name}, wiretopology.Topology{
			Object:      wire.Object{IsReady: true},
			ClusterName: r.clusterName,
		})
	} else {
		r.topoCache.Delete(ctx, types.NamespacedName{Name: req.Name})
	}

	return ctrl.Result{}, nil
}
