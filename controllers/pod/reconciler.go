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

package pod

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
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
	reconcilerinterface.Register("pods", &reconciler{})
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
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.podManager = cfg.PodManager
	r.cri = cfg.CRI

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("PodController").
			For(&corev1.Pod{}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	podManager pod.Manager
	cri        cri.CRI

	l logr.Logger
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.l = log.FromContext(ctx)

	cr := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			r.l.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		// check if this pod was used for a link wire
		// if so clean up the link wire
		// delete the pod from the manager
		r.podManager.DeletePod(req.NamespacedName)
		r.l.Info("cr deleted")
		return ctrl.Result{}, nil
	}

	// annotations indicate if this pod is relevant for wiring
	if len(cr.Annotations) == 0 || cr.Annotations[invv1alpha1.NephioWiringKey] != "true" {
		// we are only interested pods that we have to wire to
		return ctrl.Result{}, nil
	}

	r.l.Info("reconcile")
	// update (add/update) pod to inventory
	r.podManager.UpsertPod(req.NamespacedName, cr)

	// if the host IP does not match the host we do not need to track the pod
	if cr.Status.HostIP == "" || cr.Status.HostIP != os.Getenv("NODE_IP") {
		// we dont need to lookup containers if we know this is not a pod locally on the node
		return ctrl.Result{}, nil
	}

	containers, err := r.cri.ListContainers(ctx, nil)
	if err != nil {
		r.l.Error(err, "cannot get containers from cri")
		return ctrl.Result{}, err
	}

	for _, c := range containers {
		containerName := ""
		if c.GetMetadata() != nil {
			containerName = c.GetMetadata().GetName()
		}
		info, err := r.cri.GetContainerInfo(ctx, c.GetId())
		if err != nil {
			r.l.Error(err, "cannot get container info name: %s, id: %s", containerName, c.GetId())
			return ctrl.Result{}, err
		}
		r.l.Info("container", "name", containerName, "name", fmt.Sprintf("%s=%s", cr.GetName(), info.PodName), "namespace", fmt.Sprintf("%s=%s", cr.GetNamespace(), info.Namespace))
		if info.PodName == cr.GetName() && info.Namespace == cr.GetNamespace() {
			r.podManager.UpsertContainer(req.NamespacedName, containerName, &pod.ContainerCtx{
				ID:     c.GetId(),
				Pid:    info.PiD,
				NSPath: fmt.Sprintf("/proc/%s/ns/net", info.PiD),
				State:  c.GetState(),
			})
		}
	}

	return ctrl.Result{}, nil
}
