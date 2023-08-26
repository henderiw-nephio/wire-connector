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

	"github.com/henderiw-nephio/wire-connector/controllers/cluster-controller/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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

// reconciler reconciles a KRM resource
type reconciler struct {
	client.Client

	clusterName string
	topoCache   wire.Cache[struct{}]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("cluster", r.clusterName)
	log.Info("reconcile cluster topology/namespace")

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
		r.topoCache.Upsert(ctx, types.NamespacedName{Namespace: r.clusterName, Name: req.Name}, struct{}{})
	} else {
		r.topoCache.Delete(ctx, types.NamespacedName{Namespace: r.clusterName, Name: req.Name})
	}

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
