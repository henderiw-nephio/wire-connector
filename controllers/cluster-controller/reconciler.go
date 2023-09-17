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

package clustercontroller

import (
	"context"
	"fmt"
	"reflect"

	clusterwatchcontroller "github.com/henderiw-nephio/wire-connector/controllers/cluster-controller/clusterwatch-controller"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	epcachecontroller "github.com/henderiw-nephio/wire-connector/controllers/endpoint-cache-controller"
	nodecachecontroller "github.com/henderiw-nephio/wire-connector/controllers/node-cache-controller"

	//nodenodepoolcontroller "github.com/henderiw-nephio/wire-connector/controllers/node-nodepool-controller"
	//nodepoolcachecontroller "github.com/henderiw-nephio/wire-connector/controllers/nodepool-cache-controller"
	podcachecontroller "github.com/henderiw-nephio/wire-connector/controllers/pod-cache-controller"
	servicecachecontroller "github.com/henderiw-nephio/wire-connector/controllers/service-cache-controller"
	topologycachecontroller "github.com/henderiw-nephio/wire-connector/controllers/topology-cache-controller"
	"github.com/henderiw-nephio/wire-connector/pkg/cluster"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/daemon"
	wireendpoint "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/endpoint"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/topology"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilerinterface.Register("clustercontroller", &reconciler{})
}

const (
	finalizer = "cluster.topo.nephio.org/finalizer"
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

	/*
		if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
			return nil, err
		}
	*/
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)

	// initialize reconciler
	r.Client = mgr.GetClient()
	r.clusterCache = cfg.ClusterCache
	r.serviceCache = cfg.ServiceCache
	r.topoCache = cfg.TopologyCache
	r.podCache = cfg.PodCache
	r.daemonCache = cfg.DaemonCache
	r.nodeCache = cfg.NodeCache
	r.epCache = cfg.EndpointCache
	r.mgr = mgr

	return nil,
		ctrl.NewControllerManagedBy(mgr).
			Named("ClusterController").
			For(&corev1.Secret{}).
			Complete(r)
}

// reconciler reconciles a KRM resource
type reconciler struct {
	client.Client
	mgr       manager.Manager
	finalizer *resource.APIFinalizer

	//scheme       *runtime.Scheme
	clusterCache wirer.Cache[wirecluster.Cluster]
	serviceCache wirer.Cache[wireservice.Service]
	topoCache    wirer.Cache[wiretopology.Topology]
	podCache     wirer.Cache[wirepod.Pod]
	daemonCache  wirer.Cache[wiredaemon.Daemon]
	nodeCache    wirer.Cache[wirenode.Node]
	epCache      wirer.Cache[wireendpoint.Endpoint]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile")

	cr := &corev1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		r.clusterCache.Delete(ctx, req.NamespacedName)
		return reconcile.Result{}, nil
	}

	// get clusterclient will return a client if the name of the secret contains kubeconfig
	clusterClient := cluster.Cluster{Client: r.Client}.GetClusterClient(cr)

	if meta.WasDeleted(cr) {
		if clusterClient != nil {
			// we only act on secrets that are clusterclients
			r.clusterCache.Delete(ctx, types.NamespacedName{Name: clusterClient.GetName()})

			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				log.Error(err, "cannot remove finalizer")
				return reconcile.Result{Requeue: true}, err
			}
		}

		return ctrl.Result{}, nil
	}

	if clusterClient != nil {
		if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot add finalizer")
			return reconcile.Result{Requeue: true}, err
		}

		config, err := clusterClient.GetRESTConfig(ctx)
		if err != nil {
			log.Info("cluster client cannot get restconfig", "err", err)
			r.clusterCache.Delete(ctx, types.NamespacedName{Name: clusterClient.GetName()})
		} else {
			log.Info("cluster ready")
			runScheme := runtime.NewScheme()
			if err := scheme.AddToScheme(runScheme); err != nil {
				log.Error(err, "cannot initialize core schema")
			}
			if err := invv1alpha1.AddToScheme(runScheme); err != nil {
				log.Error(err, "cannot initialize invv1alpha1 schema")
			}
			// update (add/update) node to cache
			cl, err := client.New(config, client.Options{
				Scheme: runScheme,
			})
			if err != nil {
				log.Error(err, "cannot get cluster client")
				return ctrl.Result{}, errors.Wrap(err, "cannot get cluster client")
			}
			cc := &ctrlconfig.Config{
				Scheme:        runScheme,
				ClusterName:   clusterClient.GetName(),
				Client:        cl,
				ServiceCache:  r.serviceCache,
				TopologyCache: r.topoCache,
				PodCache:      r.podCache,
				NodeCache:     r.nodeCache,
				DaemonCache:   r.daemonCache,
				EndpointCache: r.epCache,
			}

			r.clusterCache.Upsert(ctx, types.NamespacedName{Name: clusterClient.GetName()}, wirecluster.Cluster{
				Object: wirer.Object{
					IsReady: true,
				},
				Client: cl,
				// add the controller with the service and topology/namespace reconcilers
				Controller: clusterwatchcontroller.New(r.mgr, &clusterwatchcontroller.Config{
					Name: clusterClient.GetName(),
					Reconcilers: []clusterwatchcontroller.Reconciler{
						{Object: &corev1.Namespace{}, Reconciler: topologycachecontroller.New(ctx, cc)},
						{Object: &corev1.Service{}, Reconciler: servicecachecontroller.New(ctx, cc)},
						{Object: &corev1.Pod{}, Reconciler: podcachecontroller.New(ctx, cc)},
						{Object: &corev1.Node{}, Reconciler: nodecachecontroller.New(ctx, cc)},
						{Object: &invv1alpha1.Endpoint{}, Reconciler: epcachecontroller.New(ctx, cc)},
						/*
							{Object: &invv1alpha1.NodePool{}, Reconciler: nodepoolcachecontroller.New(ctx, cc)},
							{Object: &corev1.Node{}, Reconciler: nodenodepoolcontroller.New(ctx, cc),
								Owns: []client.Object{&invv1alpha1.Node{}},
								Watches: []clusterwatchcontroller.Watch{
									{Object: &invv1alpha1.NodePool{}, EventHandler: nodenodepoolcontroller.NewNodePoolEventHandler(cc)},
								},
							},
						*/
					},
					RESTConfig: config,
					RESTmapper: cl.RESTMapper(),
					Scheme:     runScheme, // this is the default kubernetes scheme
				}),
			})
		}
	}
	return ctrl.Result{}, nil
}
