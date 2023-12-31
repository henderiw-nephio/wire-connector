/*
Copyright 2022 Nokia.

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

package nodedeployer

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/resources"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func init() {
	reconcilerinterface.Register("nodedeployercontroller", &reconciler{})
}

const (
	finalizer = "nodedeployer.wirer.nephio.org/finalizer"
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.Config)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	if os.Getenv("ENABLE_NAD") == "true" {
		if err := nadv1.AddToScheme(mgr.GetScheme()); err != nil {
			return nil, err
		}
	}

	r.APIPatchingApplicator = resource.NewAPIPatchingApplicator(mgr.GetClient())
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.scheme = mgr.GetScheme()
	r.nodeRegistry = cfg.NodeRegistry

	if os.Getenv("ENABLE_NAD") == "true" {
		return nil, ctrl.NewControllerManagedBy(mgr).
			Named("NodeDeployerController").
			For(&invv1alpha1.Node{}).
			Owns(&corev1.Pod{}).
			Owns(&nadv1.NetworkAttachmentDefinition{}).
			Owns(&invv1alpha1.Target{}).
			Complete(r)
	}
	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("NodeDeployerController").
		For(&invv1alpha1.Node{}).
		Owns(&corev1.Pod{}).
		Owns(&invv1alpha1.Target{}).
		Complete(r)

}

// reconciler reconciles a srlinux node object
type reconciler struct {
	resource.APIPatchingApplicator
	scheme       *runtime.Scheme
	finalizer    *resource.APIFinalizer
	nodeRegistry node.NodeRegistry
	//resources    resources.Resources

}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile", "req", req)

	res := resources.New(r.APIPatchingApplicator, resources.Config{
		OwnerRef: true,
		Owns: []schema.GroupVersionKind{
			nadv1.SchemeGroupVersion.WithKind(reflect.TypeOf(nadv1.NetworkAttachmentDefinition{}).Name()),
		},
	})

	cr := &invv1alpha1.Node{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	cr = cr.DeepCopy()

	if resource.WasDeleted(cr) {
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	node, err := r.nodeRegistry.NewNodeOfProvider(cr.Spec.Provider, r.Client, r.scheme)
	if err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// some providers dont need to be deployed so we can finish here
	// example server -> deployed as part of the cluster (bare metal or VM or container (kind cluster))
	if !node.ToBeDeployed(ctx) {
		cr.SetConditions(resourcev1alpha1.Ready())
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	nc, err := node.GetNodeConfig(ctx, cr)
	if err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	nads, err := node.GetNetworkAttachmentDefinitions(ctx, cr, nc)
	if err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	res.Init(client.MatchingLabels{})

	if os.Getenv("ENABLE_NAD") == "true" {
		for _, nad := range nads {
			log.Info("nad info", "name", nad.GetName())
			if err := res.AddNewResource(cr, nad); err != nil {
				cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
				return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		}
		if err := res.APIApply(ctx, cr); err != nil {
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}

	newPod, err := node.GetPodSpec(ctx, cr, nc, nads)
	if err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	if err := r.handlePodUpdate(ctx, cr, newPod); err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// at this stage the pod should exist
	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	podIPs, msg, ready := getPodStatus(pod)
	if !ready {
		cr.SetConditions(resourcev1alpha1.NotReady(msg))
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	log.Info("pod ips", "ips", podIPs)
	if err := node.SetInitialConfig(ctx, cr, podIPs); err != nil {
		log.Error(err, "cannot set initial config")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// add a target to the deployment using ownerReferences
	targetRes := resources.New(r.APIPatchingApplicator, resources.Config{
		OwnerRef: true,
		Owns: []schema.GroupVersionKind{
			invv1alpha1.TargetGroupVersionKind,
		},
	})
	/*
		if err := targetRes.AddNewResource(cr, buildTarget(cr, podIPs)); err != nil {
			r.l.Error(err, "cannot add target resource")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	*/
	if err := targetRes.APIApply(ctx, cr); err != nil {
		log.Error(err, "cannot apply target resource")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	log.Info("ready", "req", req)
	cr.SetConditions(resourcev1alpha1.Ready())
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) handlePodUpdate(ctx context.Context, cr *invv1alpha1.Node, newPod *corev1.Pod) error {
	log := log.FromContext(ctx)
	var create bool
	existingPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      cr.GetName(),
		Namespace: cr.GetNamespace(),
	}, existingPod); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		// pod does not exist -> indicate to create it
		create = true
	} else {
		log.Info("pod exists",
			"oldHash", existingPod.GetAnnotations()[invv1alpha1.RevisionHash],
			"newHash", newPod.GetAnnotations()[invv1alpha1.RevisionHash],
		)
		if newPod.GetAnnotations()[invv1alpha1.RevisionHash] != existingPod.GetAnnotations()[invv1alpha1.RevisionHash] {
			// pod spec changed, since pods are immutable we delete and create the pod
			log.Info("pod spec changed")
			if err := r.Delete(ctx, existingPod); err != nil {
				return err
			}
			create = true
		}
	}

	if create {
		if err := r.Create(ctx, newPod); err != nil {
			return err
		}
	}
	return nil
}

func getPodStatus(pod *corev1.Pod) ([]corev1.PodIP, string, bool) {
	if len(pod.Status.ContainerStatuses) == 0 {
		return nil, "pod conditions empty", false
	}
	if !pod.Status.ContainerStatuses[0].Ready {
		return nil, "pod not ready empty", false
	}
	if len(pod.Status.PodIPs) == 0 {
		return nil, "no ip provided", false
	}
	return pod.Status.PodIPs, "", true
}

func buildTarget(cr *invv1alpha1.Node, ips []corev1.PodIP) *invv1alpha1.Target {
	labels := cr.GetLabels()
	labels[invv1alpha1.NephioTopologyKey] = cr.Namespace
	if len(labels) == 0 {
		labels = map[string]string{}
	}
	for k, v := range cr.Spec.GetUserDefinedLabels() {
		labels[k] = v
	}
	targetSpec := invv1alpha1.TargetSpec{
		Provider:   cr.Spec.Provider,
		SecretName: cr.Spec.Provider,
		Address:    &ips[0].IP,
	}
	return invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Name:      cr.GetName(),
			Namespace: cr.GetNamespace(),
			Labels:    labels,
		},
		targetSpec,
		invv1alpha1.TargetStatus{},
	)
}
