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

package wirecontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	wclient "github.com/henderiw-nephio/wire-connector/pkg/wire/client"
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
	reconcilerinterface.Register("wirecontroller", &reconciler{})
}

const (
	finalizer = "wire.nephio.org/finalizer"
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	// register scheme
	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)

	wireClient, err := wclient.New(&wclient.Config{
		Address:  fmt.Sprintf("%s:%s", "127.0.0.1", "9999"),
		Insecure: true,
	})
	if err != nil {
		return nil, err
	}
	if err := wireClient.Start(ctx); err != nil {
		return nil, err
	}
	r.wireclient = wireClient

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("WireController").
			For(&invv1alpha1.Link{}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	wireclient wclient.Client
}

func getWireReq(l *invv1alpha1.Link) *wirepb.WireRequest {
	req := &wirepb.WireRequest{
		Namespace: l.Namespace,
		Name:      l.Name,
		Endpoints: make([]*wirepb.Endpoint, len(l.Spec.Endpoints), len(l.Spec.Endpoints)),
	}
	for epIdx, ep := range l.Spec.Endpoints {
		req.Endpoints[epIdx].Topology = ep.Topology
		req.Endpoints[epIdx].NodeName = ep.NodeName
		req.Endpoints[epIdx].IfName = ep.InterfaceName
	}
	return req
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

	wreq := getWireReq(cr)
	wresp, err := r.wireclient.Get(ctx, wreq)
	if err != nil {
		log.Error(err, "cannot get wire")
		cr.SetConditions(resourcev1alpha1.Failed("cannot get wire"))
		return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	exists := true
	if wresp.StatusCode == wirepb.StatusCode_NotFound {
		exists = false
	}

	if meta.WasDeleted(cr) {
		if exists {
			if _, err := r.wireclient.Delete(ctx, wreq); err != nil {
				log.Error(err, "cannot remove wire")
				cr.SetConditions(resourcev1alpha1.Failed("cannot remove wire"))
				return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			// TODO -> for now we poll, to be changed to event driven
			return reconcile.Result{RequeueAfter: 1 * time.Second}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Info("wire destroyed...")
		return ctrl.Result{}, nil
	}

	// we should only add a finalizer when we act
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		cr.SetConditions(resourcev1alpha1.Failed("cannot add finalizer"))
		return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if wresp.StatusCode == wirepb.StatusCode_NOK {
		_, err := r.wireclient.Create(ctx, wreq)
		if err != nil {
			log.Error(err, "cannot create wire")
			cr.SetConditions(resourcev1alpha1.Failed("cannot create wire"))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Error(err, "wire deploying...")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	log.Info("wire deployed...")

	// we assume when the link becomes ready we get a new reconcile trigger
	return ctrl.Result{}, nil
}
