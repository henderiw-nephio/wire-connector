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
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/resolve"

	//"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/worker"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	//"github.com/henderiw-nephio/wire-connector/pkg/wire/dispatcher"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	DaemonCache wire.Cache[wiredaemon.Daemon]
	PodCache    wire.Cache[wirepod.Pod]
	NodeCache   wire.Cache[wirenode.Node]
	//EndpointCache wire.Cache[*wireep.Endpoint]
}

func New(ctx context.Context, cfg *Config) wire.Wirer {
	l := ctrl.Log.WithName("wire-controller")

	workerCache := wire.NewCache[Worker]()
	dispatcher := NewDispatcher(workerCache)

	r := &wc{
		daemonCache: cfg.DaemonCache,
		podCache:    cfg.PodCache,
		nodeCache:   cfg.NodeCache,
		nodeepCache: NewEpCache(wire.NewCache[*NodeEndpoint]()),
		wireCache:   NewWireCache(wire.NewCache[*Wire]()),
		dispatcher:  dispatcher,
		workerCache: workerCache,
		l:           l,
	}
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
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return r
}

type wc struct {
	daemonCache wire.Cache[wiredaemon.Daemon]
	podCache    wire.Cache[wirepod.Pod]
	nodeCache   wire.Cache[wirenode.Node]
	nodeepCache NodeEpCache
	wireCache   WireCache
	workerCache wire.Cache[Worker]
	dispatcher  Dispatcher

	//geventCh chan event.GenericEvent

	l logr.Logger
}

// resolveEndpoint finds the service endpoint and daemon nodeName based on the network pod (namespace/name)
// - check if the pod exists in the cache and if it is ready
// -> if ready we get the nodeName the network pod is running on
// - via the nodeName we can find the serviceendpoint in the daemon cache if the daemon is ready
func (r *wc) resolveEndpoint(nsn types.NamespacedName) *resolve.Data {
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
	if d.GRPCAddress == "" || d.GRPCPort == "" {
		return &resolve.Data{Message: fmt.Sprintf("wireDaemon no grpc address/port: %s", daemonHostNodeNSN.String())}
	}
	return &resolve.Data{
		Success:         true,
		PodNodeName:     pod.HostNodeName,
		ServiceEndpoint: fmt.Sprintf("%s:%s", d.GRPCAddress, d.GRPCPort),
		HostIP:          d.HostIP,
		HostNodeName:    pod.HostNodeName,
	}
}

type CallbackCtx struct {
	Message          string
	Hold             bool
	EvalHostNodeName bool
}

// daemonCallback notifies the wire controller about the fact
// that the daemon status changed and should reconcile the object
func (r *wc) daemonCallback(ctx context.Context, nsn types.NamespacedName, d any) {
	r.l.Info("daemonCallback ...start...", "nsn", nsn, "data", d)
	daemon, ok := d.(wiredaemon.Daemon)
	if !ok {
		r.l.Info("expect Daemon", "got", reflect.TypeOf(d).Name())
		return
	}
	var newd any
	newd = nil
	if d != nil && daemon.IsReady {
		newd = daemon
		address := fmt.Sprintf("%s:%s", daemon.GRPCAddress, daemon.GRPCPort)

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
			// worker found
			c.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
	}
	r.l.Info("daemonCallback ...call common callback...", "nsn", nsn, "data", d)

	r.commonCallback(ctx, nsn, newd, &CallbackCtx{
		Message:          "daemon failed",
		Hold:             true, // when the daemon fails this most likely mean a daemon upgrade or restart, so we dont want to delete the other end of the wire
		EvalHostNodeName: true,
	})
	r.l.Info("daemonCallback ...end...", "nsn", nsn, "data", d)
}

func (r *wc) podCallback(ctx context.Context, nsn types.NamespacedName, d any) {
	r.l.Info("podCallback ...start...", "nsn", nsn, "data", d)

	var newd any
	newd = nil
	if d != nil {
		p, ok := d.(wirepod.Pod)
		if !ok {
			r.l.Info("expect Pod", "got", reflect.TypeOf(d).Name())
			return
		}
		if p.IsReady {
			newd = p
		}
	}
	r.commonCallback(ctx, nsn, newd, &CallbackCtx{
		Message:          "pod failed",
		Hold:             false,
		EvalHostNodeName: false,
	})
	r.l.Info("podCallback ...end...", "nsn", nsn, "data", d)
}

func (r *wc) nodeCallback(ctx context.Context, nsn types.NamespacedName, d any) {
	r.l.Info("nodeCallback ...start...", "nsn", nsn, "data", d)

	var newd any
	newd = nil
	if d != nil {
		n, ok := d.(wirenode.Node)
		if !ok {
			r.l.Info("expect Node", "got", reflect.TypeOf(d).Name())
			return
		}
		if n.IsReady {
			newd = n
		}
	}

	r.commonCallback(ctx, nsn, newd, &CallbackCtx{
		Message:          "node failed",
		Hold:             false,
		EvalHostNodeName: true,
	})
	r.l.Info("nodeCallback ...end...", "nsn", nsn, "data", d)
}

func (r *wc) commonCallback(ctx context.Context, nsn types.NamespacedName, d any, cbctx *CallbackCtx) {
	//log := log.FromContext(ctx)
	r.l.Info("commonCallback ...start...", "nsn", nsn, "data", d)
	var wg sync.WaitGroup
	if d == nil {
		// delete
		for wireNSN, wire := range r.wireCache.List() {
			wireNSN := wireNSN
			w := *wire
			for epIdx := range wire.WireReq.Endpoints {
				epIdx := epIdx
				if w.WireReq.IsResolved(epIdx) && w.WireReq.CompareName(epIdx, cbctx.EvalHostNodeName, nsn.Name) {
					wg.Add(1)
					go func() {
						defer wg.Done()
						r.wireCache.UnResolve(wireNSN, epIdx)
						r.wireCache.HandleEvent(wireNSN, state.ResolutionFailedEvent, &state.EventCtx{
							EpIdx:   epIdx,
							Message: cbctx.Message,
							Hold:    cbctx.Hold, // we do not want this event to be replciated to the other endpoint
						})
					}()
				}
			}
		}
		for nodeepNSN, nodeep := range r.nodeepCache.List() {
			nodeepNSN := nodeepNSN
			if nodeep.NodeEpReq.IsResolved() && nodeep.NodeEpReq.CompareName(cbctx.EvalHostNodeName, nsn.Name) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r.nodeepCache.UnResolve(nodeepNSN)
					r.nodeepCache.HandleEvent(nodeepNSN, state.ResolutionFailedEvent, &state.EventCtx{
						Message: cbctx.Message,
					})
				}()
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
					r.wireCreate(wire.WireReq, "callback")
				} else {
					r.wireDelete(wire.WireReq, "callback")
				}
			}()
		}
		for _, nodeep := range r.nodeepCache.List() {
			nodeep := nodeep
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.nodeepCreate(nodeep.NodeEpReq, "callback")
			}()
		}
	}
	wg.Wait()
	r.l.Info("commonCallback ...end...", "nsn", nsn, "data", d)
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
