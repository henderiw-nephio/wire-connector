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
	"os"
	"reflect"
	"sync"

	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/daemon"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/service"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/client"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/types"
)

type CallbackCtx struct {
	Message  string
	Hold     bool
	Evaluate EvaluateName
}

type EvaluateName string

const (
	EvaluateClusterName  EvaluateName = "clusterName"
	EvaluateHostNodeName EvaluateName = "hostNodeName"
	EvaluateNodeName     EvaluateName = "nodeName"
)

// clusterCallback notifies the wire controller about the fact
// that the cluster is no longer available
// used only for intercluster wirer controllers
func (r *wc) clusterCallback(ctx context.Context, a wirer.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).With("nsn", nsn, "action", a)
	log.Info("clusterCallback ...start...")

	if a == wirer.UpsertAction {
		p, ok := d.(wirecluster.Cluster)
		if !ok {
			log.Info("expect Cluster", "got", reflect.TypeOf(d).Name())
			return
		}
		if p.IsReady {
			if err := p.Start(ctx); err != nil {
				log.Error("cannot start cluster controller watchers", "error", err)
			}
		}
	} else {
		p, ok := d.(wirecluster.Cluster)
		if !ok {
			log.Info("expect Cluster", "got", reflect.TypeOf(d).Name())
			return
		}
		// delete the cluster controller watches
		p.Stop()
		// delete the respective caches
		r.serviceCache.Delete(ctx, nsn)
		// for topologies we store the following data
		// namespace = cluster, name = namespace which is the topology
		// we check if the Namespace of the topology matches the clusterName
		for topoNSN, topo := range r.topologyCache.List() {
			if topo.ClusterName == nsn.Name {
				r.topologyCache.Delete(ctx, topoNSN)
			}
		}
	}
	log.Info("clusterCallback ...end...")
}

// serviceCallback notifies the wire controller about the fact
// that the service status changed and should reconcile the object
// used only for intercluster wirer controllers
func (r *wc) serviceCallback(ctx context.Context, a wirer.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).With("nsn", nsn, "action", a)
	log.Info("serviceCallback ...start...")

	service, ok := d.(wireservice.Service)
	if !ok {
		log.Info("expect Service", "got", reflect.TypeOf(d).Name())
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
		w, err := NewWorker(ctx, r.wireCache, r.nodeepCache, &client.Config{
			Address:  address,
			Insecure: true,
		})
		if err != nil {
			log.Error("cannot get workercache", "error", err)
			return
		}
		if err := w.Start(ctx); err != nil {
			log.Error("cannot start worker", "error", err)
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
		Message:  "service/cluster failed",
		Hold:     false,               // to be checked what it means to the client if the grpc service restores
		Evaluate: EvaluateClusterName, // TODO get all topologies of this cluster
	})
	log.Info("serviceCallback ...end...")
}

// daemonCallback notifies the wire controller about the fact
// that the daemon status changed and should reconcile the object
func (r *wc) daemonCallback(ctx context.Context, a wirer.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).With("nsn", nsn, "action", a, "data", d)
	log.Info("daemonCallback ...start...")

	daemon, ok := d.(wiredaemon.Daemon)
	if !ok {
		log.Info("expect Daemon", "got", reflect.TypeOf(d).Name())
		return
	}
	var newd any
	newd = nil
	if a == wirer.UpsertAction && daemon.IsReady {
		newd = daemon
		address := fmt.Sprintf("%s:%s", daemon.GRPCAddress, daemon.GRPCPort)

		log.Info("daemonCallback upsert")

		// this is a safety
		oldw, err := r.workerCache.Get(nsn)
		if err == nil {
			oldw.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
		// create a new client
		w, err := NewWorker(ctx, r.wireCache, r.nodeepCache, &client.Config{
			Address:  address,
			Insecure: true,
		})
		if err != nil {
			log.Error("cannot get workercache", "error", err)
			return
		}
		if err := w.Start(ctx); err != nil {
			log.Error("cannot start worker", "error", err)
			return
		}
		r.workerCache.Upsert(ctx, nsn, w)
	} else {
		// delete
		c, err := r.workerCache.Get(nsn)
		if err == nil {
			// worker found
			c.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
	}
	log.Info("daemonCallback ...call common callback...")

	r.commonCallback(ctx, a, nsn, newd, &CallbackCtx{
		Message:  "daemon failed",
		Hold:     true, // when the daemon fails this most likely mean a daemon upgrade or restart, so we dont want to delete the other end of the wire
		Evaluate: EvaluateHostNodeName,
	})
	log.Info("daemonCallback ...end...")
}

func (r *wc) podCallback(ctx context.Context, a wirer.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).With("nsn", nsn, "action", a, "data", d)
	log.Info("podCallback ...start...")

	r.commonCallback(ctx, a, nsn, d, &CallbackCtx{
		Message:  "pod failed",
		Hold:     false,
		Evaluate: EvaluateNodeName,
	})
	log.Info("podCallback ...end...")
}

func (r *wc) nodeCallback(ctx context.Context, a wirer.Action, nsn types.NamespacedName, d any) {
	log := log.FromContext(ctx).With("nsn", nsn, "action", a, "data", d)
	log.Info("nodeCallback ...start...")

	r.commonCallback(ctx, a, nsn, d, &CallbackCtx{
		Message:  "node failed",
		Hold:     false,
		Evaluate: EvaluateHostNodeName,
	})
	log.Info("nodeCallback ...end...")
}

func (r *wc) commonCallback(ctx context.Context, a wirer.Action, nsn types.NamespacedName, d any, cbctx *CallbackCtx) {
	log := log.FromContext(ctx).With("nsn", nsn, "action", a, "data", d)
	log.Info("commonCallback ...start...")
	var wg sync.WaitGroup
	if a == wirer.DeleteAction {
		// delete
		for wireNSN, wire := range r.wireCache.List() {
			wireNSN := wireNSN
			w := *wire
			for epIdx := range wire.WireReq.Endpoints {
				epIdx := epIdx
				if w.WireReq.IsResolved(epIdx) && w.WireReq.HasLocalAction(epIdx) && w.WireReq.CompareName(epIdx, cbctx.Evaluate, nsn.Name) {
					wg.Add(1)
					go func() {
						defer wg.Done()
						r.wireCache.UnResolve(wireNSN, epIdx)
						r.wireCache.HandleEvent(ctx, wireNSN, state.ResolutionFailedEvent, &state.EventCtx{
							EpIdx:   epIdx,
							Message: cbctx.Message,
							Hold:    cbctx.Hold, // we do not want this event to be replciated to the other endpoint
						})
					}()
				}
			}
		}
		// only needed for the local node
		if os.Getenv("WIRER_INTERCLUSTER") != "true" {
			for nodeepNSN, nodeep := range r.nodeepCache.List() {
				nodeepNSN := nodeepNSN
				if nodeep.NodeEpReq.IsResolved() && nodeep.NodeEpReq.CompareName(cbctx.Evaluate, nsn.Name) {
					wg.Add(1)
					go func() {
						defer wg.Done()
						r.nodeepCache.UnResolve(nodeepNSN)
						r.nodeepCache.HandleEvent(ctx, nodeepNSN, state.ResolutionFailedEvent, &state.EventCtx{
							Message: cbctx.Message,
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
		for _, nodeep := range r.nodeepCache.List() {
			nodeep := nodeep
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.nodeepCreate(ctx, nodeep.NodeEpReq, "callback")
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
