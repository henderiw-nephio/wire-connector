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
	"log/slog"
	"time"

	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/topology"
	vxlanclient "github.com/henderiw-nephio/wire-connector/pkg/wirer/vxlan/client"
	"github.com/henderiw/logger/log"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	//Client      client.Client
	VXLANClient vxlanclient.Client
	// used only for intercluster controller but dont do any harm
	ClusterCache wirer.Cache[wirecluster.Cluster]
	ServiceCache wirer.Cache[wireservice.Service]
	// used for both intercluster and incluster controller
	TopologyCache wirer.Cache[wiretopology.Topology]
	DaemonCache   wirer.Cache[wiredaemon.Daemon]
	PodCache      wirer.Cache[wirepod.Pod]
	NodeCache     wirer.Cache[wirenode.Node]
}

func New(ctx context.Context, cfg *Config) (wirer.Wirer, error) {
	workerCache := wirer.NewCache[Worker]()
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
		nodeepCache:   NewEpCache(ctx, wirer.NewCache[*NodeEndpoint]()),
		wireCache:     NewWireCache(ctx, wirer.NewCache[*Wire](), cfg.VXLANClient),
		dispatcher:    dispatcher,
		workerCache:   workerCache,
		l:             log.FromContext(ctx).WithGroup("wirer-daemon"),
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
				log := r.l
				log.Info("wires...")
				for nsn, o := range r.wireCache.List() {
					log.Info("wire", "nsn", nsn, "wire resp", o.WireResp, "wire status", o.WireResp.StatusCode,
						"ep0", fmt.Sprintf("%s/%s", o.WireResp.EndpointsStatus[0].StatusCode.String(), o.WireResp.EndpointsStatus[0].Reason),
						"ep1", fmt.Sprintf("%s/%s", o.WireResp.EndpointsStatus[1].StatusCode.String(), o.WireResp.EndpointsStatus[1].Reason),
					)
				}
				log.Info("nodeeps...")
				for nsn, o := range r.nodeepCache.List() {
					log.Info("nodeep", "nsn", nsn, "nodeep resp", o.NodeEpResp, "nodeep status", o.NodeEpResp.StatusCode)
				}
				log.Info("worker...")
				for nsn, o := range r.workerCache.List() {
					log.Info("worker", "nsn", nsn, "addess", o.GetConfig().Address)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return r, nil
}

type wc struct {
	vxlanclient   vxlanclient.Client
	clusterCache  wirer.Cache[wirecluster.Cluster]
	serviceCache  wirer.Cache[wireservice.Service]
	topologyCache wirer.Cache[wiretopology.Topology]
	daemonCache   wirer.Cache[wiredaemon.Daemon]
	podCache      wirer.Cache[wirepod.Pod]
	nodeCache     wirer.Cache[wirenode.Node]
	nodeepCache   NodeEpCache
	wireCache     WireCache
	workerCache   wirer.Cache[Worker]
	dispatcher    Dispatcher

	//geventCh chan event.GenericEvent

	l *slog.Logger
}
