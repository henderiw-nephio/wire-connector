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

package nodeepcontroller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/henderiw-nephio/network-node-operator/pkg/node"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	wclient "github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resources"
	perrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("nodeepcontroller", &reconciler{})
}

const (
	finalizer = "wire.nephio.org/finalizer"
	// error
	errGetCr        = "cannot get resource"
	errUpdateStatus = "cannot update status"
)

//+kubebuilder:rbac:groups=inv.nephio.org,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.nephio.org,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=inv.nephio.org,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.nephio.org,resources=endpoints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=inv.nephio.org,resources=targets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.nephio.org,resources=targets/status,verbs=get;update;patch

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
	r.APIPatchingApplicator = resource.NewAPIPatchingApplicator(mgr.GetClient())
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.resources = resources.New(r.APIPatchingApplicator, resources.Config{
		Owns: []schema.GroupVersionKind{
			invv1alpha1.EndpointGroupVersionKind,
			invv1alpha1.TargetGroupVersionKind,
		},
	})
	r.nodeRegistry = cfg.Noderegistry
	r.scheme = mgr.GetScheme()

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
			Named("NodeEndpointController").
			For(&invv1alpha1.Node{}).
			Owns(&invv1alpha1.Endpoint{}).
			Owns(&invv1alpha1.Target{}).
			Watches(&invv1alpha1.NodeConfig{}, &nodeConfigEventHandler{client: mgr.GetClient()}).
			Watches(&invv1alpha1.NodeModel{}, &nodeModelEventHandler{client: mgr.GetClient()}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	resource.APIPatchingApplicator
	finalizer *resource.APIFinalizer

	resources    resources.Resources
	scheme       *runtime.Scheme
	nodeRegistry node.NodeRegistry

	wireclient wclient.Client
}

func getEndpointReq(n *invv1alpha1.Node, nm *invv1alpha1.NodeModel) *endpointpb.EndpointRequest {
	eps := make([]*endpointpb.Endpoint, 0, len(nm.Spec.Interfaces))
	for _, itfce := range nm.Spec.Interfaces {
		eps = append(eps, &endpointpb.Endpoint{IfName: itfce.Name})
	}

	req := &endpointpb.EndpointRequest{
		NodeKey: &endpointpb.NodeKey{
			Topology: n.Namespace,
			NodeName: n.Name,
		},
		Endpoints: eps,
	}
	return req
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile")

	cr := &invv1alpha1.Node{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, errGetCr)
			return ctrl.Result{}, perrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return reconcile.Result{}, nil
	}

	// we assume for now that the cleanup of the endpoints on the node happens automatically
	// if the pod is deleted, the veth pairs will be cleaned up
	// xdp entries are useless and will be overwritten to the real once
	if meta.WasDeleted(cr) {
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

	// TODO handle change of the network model config

	nm, err := r.getNodeModel(ctx, cr)
	if err != nil {
		// TODO delete interfaces
		if errd := r.resources.APIDelete(ctx, cr); errd != nil {
			err = errors.Join(err, errd)
			log.Error(err, "cannot populate and delete existingresources")
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Error(err, "cannot populate resources")
		cr.SetConditions(resourcev1alpha1.Failed(err.Error()))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	epReq := getEndpointReq(cr, nm)
	epResp, err := r.wireclient.EndpointGet(ctx, epReq)
	if err != nil {
		// we should not recive an error since this indicates aan issue with the communication
		log.Error(err, "cannot get endpoint")
		cr.SetConditions(resourcev1alpha1.Failed("cannot get endpoint"))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	exists := true
	if epResp.StatusCode == endpointpb.StatusCode_NotFound {
		exists = false
	}
	log.Info("endpoint get", "nodeKey", epResp.NodeKey.String(), "exists", exists, "status", epResp.StatusCode.String())


	// if everything is ok we dont have to deploy things
	if epResp.StatusCode != endpointpb.StatusCode_OK {
		_, err := r.wireclient.EndpointCreate(ctx, epReq)
		if err != nil {
			log.Error(err, "cannot create endpoint")
			cr.SetConditions(resourcev1alpha1.Failed("cannot create endpoint"))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("endpoint deploying...")
		cr.SetConditions(resourcev1alpha1.Action("creating"))
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// TODO create endpoint resources in k8s api

	log.Info("endpoint deployed...")
	cr.SetConditions(resourcev1alpha1.Ready())
	return reconcile.Result{}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)

}

func (r *reconciler) getNodeModel(ctx context.Context, cr *invv1alpha1.Node) (*invv1alpha1.NodeModel, error) {
	// get the specific provider implementation of the network device
	node, err := r.nodeRegistry.NewNodeOfProvider(cr.Spec.Provider, r.Client, r.scheme)
	if err != nil {
		return nil, err
	}
	// get the node config associated to the node
	nc, err := node.GetNodeConfig(ctx, cr)
	if err != nil {
		return nil, err
	}
	cr.Status.UsedNodeConfigRef = &corev1.ObjectReference{
		APIVersion: nc.APIVersion,
		Kind:       nc.Kind,
		Name:       nc.Name,
		Namespace:  nc.Namespace,
	}
	cr.Status.UsedNodeModelRef = node.GetNodeModelConfig(ctx, nc)
	// get interfaces
	return node.GetInterfaces(ctx, nc)
}
