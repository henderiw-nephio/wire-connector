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

package wireclustercontroller

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"

	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/resolve"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/topology"

	"github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	ClusterCache  wire.Cache[wirecluster.Cluster]
	ServiceCache  wire.Cache[wireservice.Service]
	TopologyCache wire.Cache[wiretopology.Topology]
	DaemonCache   wire.Cache[wiredaemon.Daemon]
	PodCache      wire.Cache[wirepod.Pod]
	NodeCache     wire.Cache[wirenode.Node]
}

func New(ctx context.Context, cfg *Config) wire.InterClusterWirer {
	l := ctrl.Log.WithName("wire-cluster-controller")

	workerCache := wire.NewCache[Worker]()
	dispatcher := NewDispatcher(workerCache)

	r := &wc{
		clusterCache:  cfg.ClusterCache,
		serviceCache:  cfg.ServiceCache,
		topologyCache: cfg.TopologyCache,
		daemonCache:   cfg.DaemonCache,
		podCache:      cfg.PodCache,
		nodeCache:     cfg.NodeCache,
		wireCache:     NewWireCache(wire.NewCache[*Wire]()),
		dispatcher:    dispatcher,
		workerCache:   workerCache,
		l:             l,
	}
	r.clusterCache.AddWatch(r.clusterCallback)
	r.serviceCache.AddWatch(r.serviceCallback)
	r.topologyCache.AddWatch(r.topologyCallback)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				r.l.Info("wires...")
				for nsn, o := range r.wireCache.List() {
					r.l.Info("wire", "nsn", nsn, "wire resp", o.WireResp, "wire status", o.WireResp.StatusCode,
						"ep0", fmt.Sprintf("%s/%s", o.WireResp.EndpointsStatus[0].StatusCode.String(), o.WireResp.EndpointsStatus[0].Reason),
						"ep1", fmt.Sprintf("%s/%s", o.WireResp.EndpointsStatus[1].StatusCode.String(), o.WireResp.EndpointsStatus[1].Reason),
					)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return r
}

type wc struct {
	clusterCache  wire.Cache[wirecluster.Cluster]
	serviceCache  wire.Cache[wireservice.Service]
	topologyCache wire.Cache[wiretopology.Topology]
	daemonCache   wire.Cache[wiredaemon.Daemon]
	podCache      wire.Cache[wirepod.Pod]
	nodeCache     wire.Cache[wirenode.Node]
	wireCache     WireCache
	workerCache   wire.Cache[Worker]
	dispatcher    Dispatcher

	//geventCh chan event.GenericEvent

	l logr.Logger
}

// resolveEndpoint resolve the endpoints hostIP to be able to communicte to the remote cluster
// if checks if the topology/service exists and ready and the resolution via grpc is ok
func (r *wc) resolveEndpoint(nsn types.NamespacedName, intercluster bool) *resolve.Data {
	// find the topology -> provides the clusterName or validates the name exists within the cluster
	t, err := r.topologyCache.Get(types.NamespacedName{Name: nsn.Namespace})
	if err != nil {
		// for intercluster wires we allow the resolution to topology resolution to fail
		// since one ep can reside in the local cluster and the other ep can reside in a remote cluster
		if !intercluster {
			return &resolve.Data{
				Success: true,
				Action:  false,
			}
		}
		return &resolve.Data{Message: fmt.Sprintf("topology not found: %s", nsn.String())}
	}
	if !t.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("topology not ready: %s", nsn.String())}
	}
	// only needed for intercluster controller
	s, err := r.serviceCache.Get(types.NamespacedName{Name: t.ClusterName})
	if err != nil {
		return &resolve.Data{Message: fmt.Sprintf("service not found: %s", nsn.String())}
	}
	if !s.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("service not ready: %s", nsn.String())}
	}

	pod, err := r.podCache.Get(nsn)
	if err != nil {
		return &resolve.Data{Message: fmt.Sprintf("pod not found: %s", nsn.String())}
	}
	if !pod.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("pod not ready: %s", nsn.String())}
	}
	daemonHostNodeNSN := types.NamespacedName{
		Namespace: "default",
		Name:      pod.HostNodeName}
	d, err := r.daemonCache.Get(daemonHostNodeNSN)
	if err != nil {
		return &resolve.Data{Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}
	}
	if !d.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}
	}
	/*
		if d.GRPCAddress == "" || d.GRPCPort == "" {
			return &resolve.Data{Message: fmt.Sprintf("wireDaemon no grpc address/port: %s", daemonHostNodeNSN.String())}
		}
	*/
	return &resolve.Data{
		Success:     true,
		PodNodeName: pod.HostNodeName,
		//ServiceEndpoint: fmt.Sprintf("%s:%s", d.GRPCAddress, d.GRPCPort),
		HostIP:       d.HostIP,
		HostNodeName: pod.HostNodeName,
		ClusterName:  t.ClusterName,
	}

	/*
		w, err := r.workerCache.Get(types.NamespacedName{Name: t.ClusterName})
		if err != nil {
			return &resolve.Data{Message: fmt.Sprintf("worker not found: %s", nsn.String())}
		}
		resolvedData := w.Resolve(ctx, &resolverpb.ResolveRequest{NodeKey: &resolverpb.NodeKey{
			Topology: nsn.Namespace,
			NodeName: nsn.Name,
		}})
		resolvedData.ClusterName = t.ClusterName
		return resolvedData
	*/
}

type CallbackCtx struct {
	Message      string
	Hold         bool
	EvalTopology bool
}

// clusterCallback notifies the wire controller about the fact
// that the cluster is no longer available
func (r *wc) clusterCallback(ctx context.Context, a wire.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).WithValues("nsn", nsn, "data", d)
	log.Info("clusterCallback ...start...")

	if a == wire.UpsertAction {
		p, ok := d.(wirecluster.Cluster)
		if !ok {
			r.l.Info("expect Cluster", "got", reflect.TypeOf(d).Name())
			return
		}
		if p.IsReady {
			if err := p.Start(ctx); err != nil {
				log.Error(err, "cannot start cluster controller watchers")
			}
		}
	} else {
		p, ok := d.(wirecluster.Cluster)
		if !ok {
			r.l.Info("expect Cluster", "got", reflect.TypeOf(d).Name())
			return
		}
		// delete the cluster controller watches
		p.Stop()
		// delete the respective caches
		r.serviceCache.Delete(ctx, nsn)
		// for topologies we store the following data
		// namespace = cluster, name = namespace which is the topology
		// we check if the Namespace of the topology matches the clusterName
		for topoNSN := range r.topologyCache.List() {
			if topoNSN.Namespace == nsn.Name {
				r.topologyCache.Delete(ctx, topoNSN)
			}
		}
	}
	log.Info("clusterCallback ...end...")
}

// serviceCallback notifies the wire controller about the fact
// that the service status changed and should reconcile the object
func (r *wc) serviceCallback(ctx context.Context, a wire.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).WithValues("nsn", nsn, "data", d)
	log.Info("serviceCallback ...start...")

	service, ok := d.(wireservice.Service)
	if !ok {
		r.l.Info("expect Service", "got", reflect.TypeOf(d).Name())
		return
	}
	if d != nil && service.IsReady {
		address := fmt.Sprintf("%s:%s", service.GRPCAddress, service.GRPCPort)

		// this is a safety
		oldw, err := r.workerCache.Get(nsn)
		if err == nil {
			oldw.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
		// create a new client
		w, err := NewWorker(ctx, r.wireCache, &client.Config{
			Address:  address,
			Insecure: true,
		})
		if err != nil {
			r.l.Error(err, "cannot get workercache")
			return
		}
		if err := w.Start(ctx); err != nil {
			r.l.Error(err, "cannot start worker")
			return
		}
		r.workerCache.Upsert(ctx, nsn, w)
	} else {
		c, err := r.workerCache.Get(nsn)
		if err == nil {
			// worker found -> first stop and afterwards delete
			c.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
	}
	log.Info("serviceCallback ...call common callback...")

	r.commonCallback(ctx, a, nsn, d, &CallbackCtx{
		Message:      "service/cluster failed",
		Hold:         false, // to be checked what it means to the client if the grpc service restores
		EvalTopology: false, // Here we evaluate the cluster
	})
	log.Info("serviceCallback ...end...")
}

func (r *wc) topologyCallback(ctx context.Context, a wire.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).WithValues("nsn", nsn, "data", d)
	log.Info("topologyCallback ...start...")

	r.commonCallback(ctx, a, nsn, d, &CallbackCtx{
		Message:      "topology/namespace failed",
		Hold:         false,
		EvalTopology: true, // Here we evaluate the cluster
	})
	log.Info("topologyCallback ...start...")
}

func (r *wc) commonCallback(ctx context.Context, a wire.Action, nsn types.NamespacedName, d any, cbctx *CallbackCtx) {
	log := log.FromContext(ctx).WithValues("nsn", nsn, "data", d)
	log.Info("commonCallback ...start...")
	var wg sync.WaitGroup
	if a == wire.DeleteAction {
		// delete
		for wireNSN, wire := range r.wireCache.List() {
			wireNSN := wireNSN
			w := *wire
			for epIdx := range wire.WireReq.Endpoints {
				epIdx := epIdx
				if w.WireReq.IsResolved(epIdx) && w.WireReq.CompareName(epIdx, cbctx.EvalTopology, nsn.Name) {
					wg.Add(1)
					go func() {
						defer wg.Done()
						r.wireCache.UnResolve(wireNSN, epIdx)
						r.wireCache.HandleEvent(wireNSN, state.ResolutionFailedEvent, &state.EventCtx{
							EpIdx:   epIdx,
							Message: cbctx.Message,
							Hold:    cbctx.Hold, // we do not want this event to be replicated to the other endpoint
						})
					}()
				}
			}
		}
	} else {
		// create or update -> we treat these also as adjacent ep create/delete
		for _, wire := range r.wireCache.List() {
			wire := wire
			wg.Add(1)
			go func() {
				defer wg.Done()
				if wire.DesiredAction == DesiredActionCreate {
					r.wireCreate(ctx, wire.WireReq, "callback")
				} else {
					r.wireDelete(ctx, wire.WireReq, "callback")
				}
			}()
		}
	}
	wg.Wait()
	log.Info("commonCallback ...end...")
}

/* generic event update

// since we can have 2 endpoints in a link that can be
	// connected on the same host we need to avoid sending
	// the event on the link twice
	notifyList := map[types.NamespacedName]struct{}{}
	// for each link in the cache notify the reconciler
	for linkNsn := range r.wireCache.List() {
		// build the link object for the generic event
		l := invv1alpha1.BuildLink(metav1.ObjectMeta{
			Name:      linkNsn.Name,
			Namespace: linkNsn.Namespace,
		}, invv1alpha1.LinkSpec{}, invv1alpha1.LinkStatus{})

		if _, ok := notifyList[types.NamespacedName{
			Name:      linkNsn.Name,
			Namespace: linkNsn.Namespace,
		}]; !ok {
			// send the event
			r.geventCh <- event.GenericEvent{
				Object: l,
			}
			// add the link nsn to the notify list to avoid double eventing
			notifyList[types.NamespacedName{
				Name:      linkNsn.Name,
				Namespace: linkNsn.Namespace,
			}] = struct{}{}

			// TODO delete endpoint

			// when the data is nil, this is a delete of the daemon and we can also remove
			// the entries from the daemon2link cache as the daemon no longer exists
			if d == nil {
				r.wireCache.Delete(nsn)
			}
		}
	}
*/
