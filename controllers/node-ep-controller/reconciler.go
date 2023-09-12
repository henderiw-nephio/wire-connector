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
	"strconv"
	"time"

	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	wclient "github.com/henderiw-nephio/wire-connector/pkg/wirer/client"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resources"
	perrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("nodeepcontroller", &reconciler{})
}

const (
	finalizer = "nodeep.wirer.nephio.org/finalizer"
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
	cfg, ok := c.(*ctrlconfig.Config)
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
	r.nodeRegistry = cfg.NodeRegistry
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
			Watches(&invv1alpha1.NodeConfig{}, &nodeConfigEventHandler{client: mgr.GetClient()}).
			Watches(&invv1alpha1.NodeModel{}, &nodeModelEventHandler{client: mgr.GetClient()}).
			Complete(r)
}

// reconciler reconciles a IPPrefix object
type reconciler struct {
	resource.APIPatchingApplicator
	finalizer *resource.APIFinalizer

	//resources    resources.Resources
	scheme       *runtime.Scheme
	nodeRegistry node.NodeRegistry

	wireclient wclient.Client
}

func getDeleteEndpointReq(cr *invv1alpha1.Node) *endpointpb.EndpointRequest {
	req := &endpointpb.EndpointRequest{
		NodeKey: &endpointpb.NodeKey{
			Topology: cr.Namespace,
			NodeName: cr.Name,
		},
	}
	return req
}

func getEndpointReq(cr *invv1alpha1.Node, nm *invv1alpha1.NodeModel, providerType node.ProviderType) *endpointpb.EndpointRequest {
	eps := make([]*endpointpb.Endpoint, 0, len(nm.Spec.Interfaces))
	for _, itfce := range nm.Spec.Interfaces {
		eps = append(eps, &endpointpb.Endpoint{IfName: itfce.Name})
	}

	req := &endpointpb.EndpointRequest{
		NodeKey: &endpointpb.NodeKey{
			Topology: cr.Namespace,
			NodeName: cr.Name,
		},
		Endpoints:  eps,
		ServerType: providerType == node.ProviderTypeServer,
	}
	return req
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("resource", "nodeep")
	log.Info("reconcile")

	res := resources.New(r.APIPatchingApplicator, resources.Config{
		OwnerRef: true,
		Owns: []schema.GroupVersionKind{
			invv1alpha1.EndpointGroupVersionKind,
		},
	})

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
		if cr.GetCondition(resourcev1alpha1.ConditionTypeEPReady).Status == metav1.ConditionTrue {
			// while the epReq is not fully initialized the deletion of the pod associated with the node will detroy
			// the ep and veth pairs
			if _, err := r.wireclient.EndpointDelete(ctx, getDeleteEndpointReq(cr)); err != nil {
				log.Error(err, "cannot remove wire")
				cr.SetConditions(resourcev1alpha1.EPFailed("cannot remove endpoint from controller"))
				return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			// TODO -> for now we poll, to be changed to event driven
			cr.SetConditions(resourcev1alpha1.EPUnknown())
			return reconcile.Result{RequeueAfter: 1 * time.Second}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(resourcev1alpha1.EPFailed(err.Error()))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("Successfully deleted resource")
		return reconcile.Result{Requeue: false}, nil
	}

	// check if the node is ready for further processing -> the ready condition tells if the node is deployed
	// and configured
	if cr.GetCondition(resourcev1alpha1.ConditionTypeReady).Status != metav1.ConditionTrue {
		cr.SetConditions(resourcev1alpha1.EPNotReady("node deployment not ready"))
		return reconcile.Result{}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// TODO handle change of the network model config
	nm, providerType, err := r.getNodeModel(ctx, cr)
	if err != nil {
		// TODO The scenario to handle is update the system when a model is not resovable
		if errd := res.APIDelete(ctx, cr); errd != nil {
			err = errors.Join(err, errd)
			cr.SetConditions(resourcev1alpha1.EPFailed("cannot delete resources"))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Error(err, "cannot populate resources")
		cr.SetConditions(resourcev1alpha1.EPFailed("cannot get node model"))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// the model is not yet known but we can validate if the node exists in the topology
	epReq := getEndpointReq(cr, nm, providerType)
	epResp, err := r.wireclient.EndpointGet(ctx, epReq)
	if err != nil {
		// we should not recive an error since this indicates aan issue with the communication
		log.Error(err, "cannot get endpoint")
		cr.SetConditions(resourcev1alpha1.EPFailed("cannot get endpoint from controller"))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	exists := true
	if epResp.StatusCode == endpointpb.StatusCode_NotFound {
		exists = false
	}
	log.Info("endpoint get", "nodeKey", epResp.NodeKey.String(), "exists", exists, "status", epResp.StatusCode.String())

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		// If this is the first time we encounter this issue we'll be requeued
		// implicitly when we update our status with the new error condition. If
		// not, we requeue explicitly, which will trigger backoff.
		log.Error(err, "cannot add finalizer")
		cr.SetConditions(resourcev1alpha1.EPFailed(err.Error()))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// initialize the resource list + provide the topology key
	res.Init(client.MatchingLabels{
		invv1alpha1.NephioTopologyKey: cr.Namespace,
	})

	// if everything is ok we dont have to deploy things
	if epResp.StatusCode != endpointpb.StatusCode_OK {
		_, err := r.wireclient.EndpointCreate(ctx, epReq)
		if err != nil {
			log.Error(err, "cannot create endpoint")
			cr.SetConditions(resourcev1alpha1.EPFailed("cannot create endpoint"))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("deploying...")
		cr.SetConditions(resourcev1alpha1.Action("creating"))
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// build endpoints based on the node model
	for epIdx, itfce := range nm.Spec.Interfaces {
		if err := res.AddNewResource(cr, buildEndpoint(cr, itfce, epIdx).DeepCopy()); err != nil {
			cr.SetConditions(resourcev1alpha1.Failed("cannot add resource"))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}

	// apply the resources to the api (endpoints)
	if err := res.APIApply(ctx, cr); err != nil {
		if errd := res.APIDelete(ctx, cr); errd != nil {
			err = errors.Join(err, errd)
			log.Error(err, "cannot populate and delete existingresources")
			cr.SetConditions(resourcev1alpha1.EPFailed(err.Error()))
			return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Error(err, "cannot populate resources")
		cr.SetConditions(resourcev1alpha1.EPFailed(err.Error()))
		return reconcile.Result{Requeue: true}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	log.Info("deployed...")
	cr.SetConditions(resourcev1alpha1.EPReady())
	return reconcile.Result{}, perrors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) getNodeModel(ctx context.Context, cr *invv1alpha1.Node) (*invv1alpha1.NodeModel, node.ProviderType, error) {
	// get the specific provider implementation of the network device
	node, err := r.nodeRegistry.NewNodeOfProvider(cr.Spec.Provider, r.Client, r.scheme)
	if err != nil {
		return nil, "", err
	}
	// get the node config associated to the node
	nc, err := node.GetNodeConfig(ctx, cr)
	if err != nil {
		return nil, "", err
	}
	cr.Status.UsedNodeConfigRef = &corev1.ObjectReference{
		APIVersion: nc.APIVersion,
		Kind:       nc.Kind,
		Name:       nc.Name,
		Namespace:  nc.Namespace,
	}
	cr.Status.UsedNodeModelRef = node.GetNodeModelConfig(ctx, nc)
	// get interfaces
	nm, err := node.GetNodeModel(ctx, nc)
	if err != nil {
		return nil, "", err
	}
	return nm, node.GetProviderType(ctx), nil
}

func buildEndpoint(cr *invv1alpha1.Node, itfce invv1alpha1.NodeModelInterface, epIdx int) *invv1alpha1.Endpoint {
	labels := map[string]string{}
	labels[invv1alpha1.NephioTopologyKey] = cr.Namespace
	labels[invv1alpha1.NephioProviderKey] = cr.Spec.Provider
	labels[invv1alpha1.NephioInventoryNodeNameKey] = cr.Name
	labels[invv1alpha1.NephioInventoryInterfaceNameKey] = itfce.Name
	labels[invv1alpha1.NephioInventoryEndpointIndex] = strconv.Itoa(epIdx)
	for k, v := range cr.Spec.GetUserDefinedLabels() {
		labels[k] = v
	}
	for k, v := range cr.GetLabels() {
		labels[k] = v
	}
	epSpec := invv1alpha1.EndpointSpec{
		NodeName:      cr.GetName(),
		InterfaceName: itfce.Name,
	}

	ep := invv1alpha1.BuildEndpoint(
		metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cr.GetName(), itfce.Name),
			Namespace: cr.GetNamespace(),
			Labels:    labels,
			//OwnerReferences: []metav1.OwnerReference{{APIVersion: cr.APIVersion, Kind: cr.Kind, Name: cr.Name, UID: cr.UID, Controller: pointer.Bool(true)}},
		},
		epSpec,
		invv1alpha1.EndpointStatus{},
	)
	ep.SetConditions(resourcev1alpha1.Ready())
	return ep
}
