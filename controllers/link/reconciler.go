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

package link

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/link"
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("links", &reconciler{})
}

const (
	finalizer = "wire.nephio.org/finalizer"
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}
	// register scheme
	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.nodeManager = cfg.NodeManager
	r.podManager = cfg.PodManager

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("LinkController").
			For(&invv1alpha1.Link{}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	nodeManager node.Manager
	podManager  pod.Manager
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile")

	cr := &invv1alpha1.Link{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return reconcile.Result{}, nil
	}

	link := link.NewLink(cr, &link.LinkCtx{
		PodManager: r.podManager,
		Topologies: map[string]struct{}{},
	})

	if meta.WasDeleted(cr) {
		if link.Exists() {
			if err := link.Destroy(); err != nil {
				log.Error(err, "cannot remove link")
				cr.SetConditions(resourcev1alpha1.Failed("cannot remove link"))
				return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		}
		
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Info("link destroyed...")
		return ctrl.Result{}, nil
	}

	if !link.IsReady() {
		log.Info("cannot wire, endpoints not ready", "connectivity", link.GetConn())
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if link.IsCrossCluster() {
		log.Info("cannot wire, crosscluster wiring not supported", "connectivity", link.GetConn())
		return ctrl.Result{}, nil
	}
	if !link.HasLocal() {
		log.Info("nothing todo, no local endpoints", "connectivity", link.GetConn())
		return ctrl.Result{}, nil
	}

	// we should only add a finalizer when we act
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if !link.Exists() {
		if err := link.Deploy(); err != nil {
			log.Error(err, "cannot deploy link")
			// the issue is that error always changes and this causes continuous reconciliation
			cr.SetConditions(resourcev1alpha1.Failed("cannot deploy link"))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("link deployed...")
	} else {
		log.Info("link exists...")
	}

	return ctrl.Result{}, nil
}
