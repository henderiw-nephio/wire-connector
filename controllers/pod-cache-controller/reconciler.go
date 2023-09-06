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

package podcachecontroller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
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
	reconcilerinterface.Register("podcachecontroller", &reconciler{})
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
		podCache:    cfg.PodCache,
		daemonCache: cfg.DaemonCache,
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
	r.podCache = cfg.PodCache
	r.daemonCache = cfg.DaemonCache

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("PodController").
			For(&corev1.Pod{}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	client.Client

	clusterName string
	podCache    wire.Cache[wirepod.Pod]
	daemonCache wire.Cache[wiredaemon.Daemon]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("cluster", r.clusterName)
	log.Info("reconcile pod")

	cr := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		r.podCache.Delete(ctx, req.NamespacedName)
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		// check if this pod was used for a link wire
		// if so clean up the link wire
		// delete the pod from the manager
		r.podCache.Delete(ctx, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// annotations indicate if this pod is relevant for wiring
	if len(cr.Annotations) != 0 &&
		cr.Annotations[invv1alpha1.NephioWiringKey] == "true" { // this is a wiring node
		// update (add/update) pod to inventory

		r.podCache.Upsert(ctx, req.NamespacedName, r.getPod(cr))
		return ctrl.Result{}, nil
	}
	if len(cr.Labels) != 0 &&
		cr.Labels["fn.kptgen.dev/controller"] == "wire-connector-daemon" {
		// TODO -> TBD add namespace ???

		hostNodeName, d := r.getLeaseInfo(cr)
		if hostNodeName != "" {
			r.daemonCache.Upsert(ctx, types.NamespacedName{Namespace: "default", Name: hostNodeName}, d)
			return ctrl.Result{}, nil
		}
	}
	return ctrl.Result{}, nil
}

func (r *reconciler) getPod(p *corev1.Pod) wirepod.Pod {
	pod := wirepod.Pod{}
	if !wirepod.IsPodReady(p) {
		return pod
	}
	pod.IsReady = true
	pod.HostIP = p.Status.HostIP
	pod.HostNodeName = p.Spec.NodeName
	return pod

}

func (r *reconciler) getLeaseInfo(p *corev1.Pod) (string, wiredaemon.Daemon) {
	d := wiredaemon.Daemon{}
	if !wirepod.IsPodReady((p)) {
		return p.Spec.NodeName, d
	}
	d.IsReady = true
	d.HostIP = p.Status.HostIP
	d.GRPCAddress = p.Status.PodIP
	d.GRPCPort = "9999"
	return p.Spec.NodeName, d
}
