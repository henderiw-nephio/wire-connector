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
	"reflect"
	"strings"
	"time"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resources"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("nodenodepoolcontroller", &reconciler{})
}

const (
	finalizer = "wire.nephio.org/finalizer"
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// New Reconciler -> used for intercluster controller
func New(ctx context.Context, cfg *ctrlconfig.Config) reconcile.Reconciler {
	c := resource.NewAPIPatchingApplicator(cfg.Client)
	return &reconciler{
		APIPatchingApplicator: c,
		finalizer:             resource.NewAPIFinalizer(cfg.Client, finalizer),
		nodePoolCache:         cfg.NodePoolCache,
		clusterName:           cfg.ClusterName,
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
	r.APIPatchingApplicator = resource.NewAPIPatchingApplicator(mgr.GetClient())
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.nodePoolCache = cfg.NodePoolCache

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("NodeNodePoolController").
			For(&corev1.Node{}).
			Owns(&invv1alpha1.Node{}).
			Watches(&invv1alpha1.NodePool{}, &nodePoolEventHandler{client: mgr.GetClient()}).
			Complete(r)
}

// reconciler reconciles a KRM resource
type reconciler struct {
	resource.APIPatchingApplicator
	finalizer *resource.APIFinalizer

	clusterName   string
	nodePoolCache wire.Cache[invv1alpha1.NodePool]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("cluster", r.clusterName)
	log.Info("reconcile node")

	res := resources.New(r.APIPatchingApplicator, resources.Config{
		Owns: []schema.GroupVersionKind{
			invv1alpha1.NodeGroupVersionKind,
		},
	})

	cr := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return reconcile.Result{}, nil
	}

	res.Init(client.MatchingLabels{})

	if meta.WasDeleted(cr) {
		// TODO delete resources
		if err := res.APIDelete(ctx, cr); err != nil {
			log.Error(err, "cannot remove resources")
			return reconcile.Result{Requeue: true}, err
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			return reconcile.Result{Requeue: true}, err
		}
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		return reconcile.Result{Requeue: true}, err
	}

	found := false
	for labelKey, labelValue := range cr.GetLabels() {
		if strings.Contains(labelKey, "nodepool") {
			np, err := r.nodePoolCache.Get(types.NamespacedName{
				Namespace: r.clusterName,
				Name:      labelValue,
			})
			if err != nil {
				// not found -> wait till nodepool is found
				// we could optimize this and fetch the nodepool from the cache
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			}

			res.Init(client.MatchingLabels{})
			for _, addr := range cr.Status.Addresses {
				// there is only 1 internal ip
				if addr.Type == corev1.NodeInternalIP {
					if err := res.AddNewResource(cr, invv1alpha1.BuildNode(
						metav1.ObjectMeta{
							Name:      cr.Name,
							Namespace: r.clusterName,
							//OwnerReferences: []metav1.OwnerReference{{APIVersion: cr.APIVersion, Kind: cr.Kind, Name: cr.Name, UID: cr.UID, Controller: pointer.Bool(true)}},
						},
						invv1alpha1.NodeSpec{
							Provider:          np.Spec.Provider,
							Address:           &addr.Address,
							Location:          np.Spec.Location,
							NodeConfig:        np.Spec.NodeConfig,
							UserDefinedLabels: np.Spec.UserDefinedLabels,
						},
						invv1alpha1.NodeStatus{},
					).DeepCopy()); err != nil {
						return ctrl.Result{}, err
					}
					found = true
					break
				}
			}
		}
	}
	if found {
		if err := res.APIApply(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := res.APIDelete(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
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
