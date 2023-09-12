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

package leasecontroller

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/daemon"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("leasecontroller", &reconciler{})
}

const (
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	// register scheme
	cfg, ok := c.(*ctrlconfig.Config)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.daemonCache = cfg.DaemonCache

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("LeaseController").
			For(&coordinationv1.Lease{}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	client.Client

	daemonCache wirer.Cache[wiredaemon.Daemon]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	cr := &coordinationv1.Lease{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		r.daemonCache.Delete(ctx, req.NamespacedName)
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		// check if this pod was used for a link wire
		// if so clean up the link wire
		// delete the pod from the manager
		r.daemonCache.Delete(ctx, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// update (add/update) pod to inventory
	if cr.Namespace == os.Getenv("POD_NAMESPACE") {
		r.daemonCache.Upsert(ctx, req.NamespacedName, r.getDaemon(cr))
	}

	return ctrl.Result{}, nil
}

func (r *reconciler) getDaemon(l *coordinationv1.Lease) wiredaemon.Daemon {
	d := wiredaemon.Daemon{}

	now := metav1.NowMicro()

	if l.Spec.RenewTime != nil {
		expectedRenewTime := l.Spec.RenewTime.Add(time.Duration(*l.Spec.LeaseDurationSeconds) * time.Second)
		if !expectedRenewTime.Before(now.Time) {
			if len(l.Labels) != 0 {
				nodeAddress, ok := l.Labels[invv1alpha1.NephioWireNodeAddress]
				if !ok {
					return d
				}
				grpcAddress, ok := l.Labels[invv1alpha1.NephioWireGRPCAddress]
				if !ok {
					return d
				}
				grpcPort, ok := l.Labels[invv1alpha1.NephioWireGRPCPort]
				if !ok {
					return d
				}
				d.IsReady = true
				d.GRPCAddress = grpcAddress
				d.HostIP = nodeAddress
				d.GRPCPort = grpcPort
				return d
			}

		}
	}
	return d
}
