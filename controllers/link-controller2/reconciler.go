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

package link2controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	topov1alpha1 "github.com/nokia/k8s-ipam/apis/topo/v1alpha1"

	//"github.com/nokia/k8s-ipam/pkg/lease"
	"github.com/nokia/k8s-ipam/pkg/meta"
	//"github.com/nokia/k8s-ipam/pkg/objects/endpoint"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/endpoint"
	"github.com/henderiw-nephio/wire-connector/pkg/lease"
	perrors "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("linkcontroller", &reconciler{})
}

const (
	finalizer = "link.topo.nephio.org/finalizer"
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {

	cfg, ok := c.(*ctrlconfig.Config)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}
	// register scheme
	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	// initialize reconciler
	//r.APIPatchingApplicator = resource.NewAPIPatchingApplicator(mgr.GetClient())
	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)

	r.epCache = endpoint.New(&endpoint.Config{
		TopologyCache: cfg.TopologyCache,
		ClusterCache:  cfg.ClusterCache,
		EndpointCache: cfg.EndpointCache,
	})
	r.epLease = lease.New(types.NamespacedName{
		Namespace: os.Getenv("POD_NAMESPACE"),
		Name:      "endpoint"}, &lease.Config{
		TopologyCache: cfg.TopologyCache,
		ClusterCache:  cfg.ClusterCache,
	})

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("LinkController").
			For(&invv1alpha1.Link{}).
			//Watches(&invv1alpha1.Endpoint{}, &endpointEventHandler{client: mgr.GetClient()}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	epCache endpoint.EpCache
	epLease lease.Lease
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile", "req", req)

	cr := &invv1alpha1.Link{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, perrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return reconcile.Result{}, nil
	}

	// for links owned by the logical interconnect link we dont do anything
	// because th endpoint allocation is already done
	for _, ownRef := range cr.OwnerReferences {
		if ownRef.APIVersion == topov1alpha1.GroupVersion.String() &&
			ownRef.Kind == topov1alpha1.LogicalInterconnectKind {
			return reconcile.Result{}, nil
		}
	}

	if meta.WasDeleted(cr) {
		// delete usedRef from endpoint status
		if err := r.epCache.DeleteClaim(ctx, cr); err != nil {
			log.Error(err, "cannot delete endpoint claim")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("Successfully deleted resource")
		return reconcile.Result{Requeue: false}, nil
	}
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		// If this is the first time we encounter this issue we'll be requeued
		// implicitly when we update our status with the new error condition. If
		// not, we requeue explicitly, which will trigger backoff.
		log.Error(err, "cannot add finalizer")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	cr = cr.DeepCopy()
	// acquire endpoint lease to update the resource
	if err := r.acquireLease(ctx, cr); err != nil {
		log.Error(err, "cannot acquire endpoint lease")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true, RequeueAfter: lease.RequeueInterval}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := r.claimResources(ctx, cr); err != nil {
		// claim resources failed
		if errd := r.epCache.DeleteClaim(ctx, cr); errd != nil {
			err = errors.Join(err, errd)
			log.Error(err, "cannot claim resource and delete endpoint claims")
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Error(err, "cannot claim resources")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	cr.SetConditions(resourcev1alpha1.Ready())
	return ctrl.Result{}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) acquireLease(ctx context.Context, cr *invv1alpha1.Link) error {
	log := log.FromContext(ctx)
	log.Info("acquire lease")

	// acquire lease per endpoint since topology could belong to a different cluster
	// which has a different client
	for _, ep := range cr.Spec.Endpoints {
		if err := r.epLease.AcquireLease(ctx, ep.Topology, cr); err != nil {
			return err
		}
	}
	return nil
}

func (r *reconciler) claimResources(ctx context.Context, cr *invv1alpha1.Link) error {
	log := log.FromContext(ctx)
	log.Info("claim resources")

	// transform the link to a epReqs
	epReqs := []endpoint.Endpoint{}
	for _, ep := range cr.Spec.Endpoints {
		epReqs = append(epReqs, endpoint.Endpoint{Topology: ep.Topology, NodeName: ep.NodeName, IfName: ep.InterfaceName})
	}

	return r.epCache.Claim(ctx, cr, epReqs)
}
