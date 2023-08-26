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
	clusterctrlconfig "github.com/henderiw-nephio/wire-connector/controllers/cluster-controller/ctrlconfig"
	servicecontroller "github.com/henderiw-nephio/wire-connector/controllers/cluster-controller/service-controller"
	topologycontroller "github.com/henderiw-nephio/wire-connector/controllers/cluster-controller/topology-controller"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	"github.com/henderiw-nephio/wire-connector/pkg/cluster"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	reconcilerinterface "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"github.com/nokia/k8s-ipam/pkg/meta"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
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
	r.clusterCache = cfg.ClusterCache
	r.serviceCache = cfg.ServiceCache
	r.topoCache = cfg.TopologyCache
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
	mgr manager.Manager

	clusterCache wire.Cache[wirecluster.Cluster]
	serviceCache wire.Cache[wireservice.Service]
	topoCache    wire.Cache[struct{}]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

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

	if meta.WasDeleted(cr) {
		r.clusterCache.Delete(ctx, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	clusterClient := cluster.Cluster{Client: r.Client}.GetClusterClient(cr)
	if clusterClient != nil {
		config, err := clusterClient.GetRESTConfig(ctx)
		if err != nil {
			r.clusterCache.Delete(ctx, req.NamespacedName)
		} else {
			// update (add/update) node to cache
			cl, err := client.New(config, client.Options{})
			if err != nil {
				log.Error(err, "cannot get cluster client")
				return ctrl.Result{}, errors.Wrap(err, "cannot get cluster client")
			}
			cc := &clusterctrlconfig.Config{
				ClusterName:   clusterClient.GetName(),
				Client:        cl,
				ServiceCache:  r.serviceCache,
				TopologyCache: r.topoCache,
			}

			r.clusterCache.Upsert(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: clusterClient.GetName()}, wirecluster.Cluster{
				Object: wire.Object{
					IsReady: true,
				},
				// add the controller with the service and topology/namespace reconcilers
				Controller: clusterwatchcontroller.New(r.mgr, &clusterwatchcontroller.Config{
					Name: clusterClient.GetName(),
					Reconcilers: []clusterwatchcontroller.Reconciler{
						{Object: &corev1.Namespace{}, Reconciler: topologycontroller.New(ctx, cc)},
						{Object: &corev1.Service{}, Reconciler: servicecontroller.New(ctx, cc)},
					},
					RESTConfig: config,
					RESTmapper: cl.RESTMapper(),
					Scheme:     scheme.Scheme, // this is the default kubernetes scheme
				}),
			})
		}
	} else {
		r.clusterCache.Delete(ctx, req.NamespacedName)
	}
	return ctrl.Result{}, nil
}
