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

package wirecontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/topology"
	vxlanclient "github.com/henderiw-nephio/wire-connector/pkg/wire/vxlan/client"

	ctrl "sigs.k8s.io/controller-runtime"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	//Client      client.Client
	VXLANClient vxlanclient.Client
	// used only for intercluster controller but dont do any harm
	ClusterCache wire.Cache[wirecluster.Cluster]
	ServiceCache wire.Cache[wireservice.Service]
	// used for both intercluster and incluster controller
	TopologyCache wire.Cache[wiretopology.Topology]
	DaemonCache   wire.Cache[wiredaemon.Daemon]
	PodCache      wire.Cache[wirepod.Pod]
	NodeCache     wire.Cache[wirenode.Node]
}

func New(ctx context.Context, cfg *Config) (wire.Wirer, error) {
	l := ctrl.Log.WithName("wire-controller")

	workerCache := wire.NewCache[Worker]()
	dispatcher := NewDispatcher(workerCache)

	r := &wc{
		vxlanclient: cfg.VXLANClient,
		// used only for inter-cluster controller but dont do any harm
		clusterCache: cfg.ClusterCache,
		serviceCache: cfg.ServiceCache,
		// used for both intercluster and incluster controller
		topologyCache: cfg.TopologyCache,
		daemonCache:   cfg.DaemonCache,
		podCache:      cfg.PodCache,
		nodeCache:     cfg.NodeCache,
		nodeepCache:   NewEpCache(wire.NewCache[*NodeEndpoint]()),
		wireCache:     NewWireCache(wire.NewCache[*Wire](), cfg.VXLANClient),
		dispatcher:    dispatcher,
		workerCache:   workerCache,
		l:             l,
	}

	// used only for inter-cluster controller but dont do any harm
	r.clusterCache.AddWatch(r.clusterCallback)
	r.serviceCache.AddWatch(r.serviceCallback)
	// used for both intercluster and incluster controller
	// r.topologyCache.AddWatch(r.topologyCallback) -> not needed since the pod will go down
	r.daemonCache.AddWatch(r.daemonCallback)
	r.podCache.AddWatch(r.podCallback)
	r.nodeCache.AddWatch((r.nodeCallback))

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
				r.l.Info("nodeeps...")
				for nsn, o := range r.nodeepCache.List() {
					r.l.Info("nodeep", "nsn", nsn, "nodeep resp", o.NodeEpResp, "nodeep status", o.NodeEpResp.StatusCode)
				}
				r.l.Info("worker...")
				for nsn, o := range r.workerCache.List() {
					r.l.Info("worker", "nsn", nsn, "addess", o.GetConfig().Address)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return r, nil
}

type wc struct {
	vxlanclient   vxlanclient.Client
	clusterCache  wire.Cache[wirecluster.Cluster]
	serviceCache  wire.Cache[wireservice.Service]
	topologyCache wire.Cache[wiretopology.Topology]
	daemonCache   wire.Cache[wiredaemon.Daemon]
	podCache      wire.Cache[wirepod.Pod]
	nodeCache     wire.Cache[wirenode.Node]
	nodeepCache   NodeEpCache
	wireCache     WireCache
	workerCache   wire.Cache[Worker]
	dispatcher    Dispatcher

	//geventCh chan event.GenericEvent

	l logr.Logger
}
