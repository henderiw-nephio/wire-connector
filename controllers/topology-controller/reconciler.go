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
	"os"
	"reflect"

	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resources"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	reconcilerinterface.Register("topologycontroller", &reconciler{})
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
	r.APIPatchingApplicator = resource.NewAPIPatchingApplicator(mgr.GetClient())
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("TopologyController").
			For(&invv1alpha1.Topology{}).
			Owns(&corev1.ConfigMap{}).
			Watches(&corev1.ConfigMap{}, &cmEventHandler{client: mgr.GetClient()}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	resource.APIPatchingApplicator
	finalizer *resource.APIFinalizer

}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile")

	res := resources.New(r.APIPatchingApplicator, resources.Config{
		Owns: []schema.GroupVersionKind{
			{Group: "", Version: "v1", Kind: reflect.TypeOf(corev1.ConfigMap{}).Name()},
			{Group: "", Version: "v1", Kind: reflect.TypeOf(corev1.Namespace{}).Name()},
		},
	})
	res.Init(client.MatchingLabels{})

	cr := &invv1alpha1.Topology{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return reconcile.Result{}, nil
	}

	if meta.WasDeleted(cr) {
		if err := res.APIDelete(ctx, cr); err != nil {
			log.Error(err, "cannot delete resources")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("topology destroyed...")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		cr.SetConditions(resourcev1alpha1.Failed("cannot add finalizer"))
		return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	

	// for a default namespace we add a label to indicate this ns is used for network topologies
	if cr.GetName() == "default" {
		ns := &corev1.Namespace{}
		if err := r.Get(ctx, types.NamespacedName{Name: "default"}, ns); err != nil {
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		if len(ns.Labels) == 0 {
			ns.Labels = map[string]string{}
		}
		ns.Labels[invv1alpha1.NephioTopologyKey] = cr.GetName()
		if err := r.Apply(ctx, ns); err != nil {
			log.Error(err, "cannot set label to namespace")
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	} else {
		if err := res.AddNewResource(cr, buildNamespace(cr)); err != nil {
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}

	// list all the configmaps with the topologyket set to network-system
	cms := &corev1.ConfigMapList{}
	if err := r.List(ctx, cms, []client.ListOption{
		client.InNamespace(os.Getenv("POD_NAMESPACE")),
		client.MatchingLabels{
			invv1alpha1.NephioTopologyKey: os.Getenv("POD_NAMESPACE"),
		},
	}...); err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	log.Info("namespace", "ns", os.Getenv("POD_NAMESPACE"))

	for _, cm := range cms.Items {
		log.Info("configmap", "name", cm.GetName(), "namespace", cm.GetNamespace())
		if err := res.AddNewResource(cr, buildConfigMap(cr, &cm)); err != nil {
			cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
			return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}

	for ref := range res.GetNewResources() {
		log.Info("new resources", "ref", ref.String())
	}

	if err := res.APIApply(ctx, cr); err != nil {
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)

	}

	cr.SetConditions(resourcev1alpha1.Ready())
	return reconcile.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func buildNamespace(cr *invv1alpha1.Topology) *corev1.Namespace {
	labels := map[string]string{}
	labels[invv1alpha1.NephioTopologyKey] = cr.Name

	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       reflect.TypeOf(corev1.Namespace{}).Name(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   cr.GetName(),
			Labels: labels,
			//OwnerReferences: []metav1.OwnerReference{{APIVersion: cr.APIVersion, Kind: cr.Kind, Name: cr.Name, UID: cr.UID, Controller: pointer.Bool(true)}},
		},
		Spec:   corev1.NamespaceSpec{},
		Status: corev1.NamespaceStatus{},
	}
}

func buildConfigMap(cr *invv1alpha1.Topology, cm *corev1.ConfigMap) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       reflect.TypeOf(corev1.ConfigMap{}).Name(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.GetName(),
			Namespace: cr.GetName(),
			//OwnerReferences: []metav1.OwnerReference{{APIVersion: cr.APIVersion, Kind: cr.Kind, Name: cr.Name, UID: cr.UID, Controller: pointer.Bool(true)}},
		},
		Data: cm.Data,
	}
}
