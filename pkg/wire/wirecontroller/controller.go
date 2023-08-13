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
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	DaemonCache wire.Cache[wiredaemon.Daemon]
	PodCache    wire.Cache[wirepod.Pod]
	NodeCache   wire.Cache[wirenode.Node]
}

func New(ctx context.Context, cfg *Config) wire.Wire {
	l := ctrl.Log.WithName("wire-controller")

	workerCache := wire.NewCache[Worker]()
	r := &wc{
		daemonCache: cfg.DaemonCache,
		podCache:    cfg.PodCache,
		nodeCache:   cfg.NodeCache,
		wireCache:   NewWireCache(workerCache),
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
				for nsn, w := range r.wireCache.List() {
					r.l.Info("wire", "nsn", nsn, "wire resp", w.WireResp,
						"status0", reflect.TypeOf(w.EndpointsState[0]).Name(),
						"status1", reflect.TypeOf(w.EndpointsState[1]).Name())
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
	workerCache wire.Cache[Worker]

	wireCache WireCache

	//geventCh chan event.GenericEvent

	l logr.Logger
}

//type ResourceCallbackFn[T1 any] func(types.NamespacedName, T1)

func (r *wc) validate(req *wirepb.WireRequest) error {
	if r.wireCache == nil {
		return fmt.Errorf("cache not initialized")
	}
	if req == nil {
		return fmt.Errorf("invalid argument provided nil object")
	}
	if len(req.Endpoints) != 2 {
		return fmt.Errorf("invalid argument provided emdpoints should have exectly 2 element, got: %d", len(req.Endpoints))
	}
	return nil
}

func (r *wc) AddWatch(fn wire.CallbackFn) {}
func (r *wc) DeleteWatch()                {}

func (r *wc) Get(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	r.l.Info("get...")
	if err := r.validate(req); err != nil {
		return &wirepb.WireResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{req}
	r.l.Info("get", "nsn", wreq.GetNSN())
	w, err := r.wireCache.Get(wreq.GetNSN())
	if err != nil {
		return &wirepb.WireResponse{StatusCode: wirepb.StatusCode_NotFound, Reason: err.Error()}, nil
	}
	return w.GetWireResponse(), nil
}

func (r *wc) UpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO allocate VPN
	r.l.Info("upsert...")
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{req}
	r.l.Info("upsert", "nsn", wreq.GetNSN())
	if _, err := r.wireCache.Get(wreq.GetNSN()); err != nil {
		// not found -> create a new link
		r.l.Info("upsert cache", "nsn", wreq.GetNSN())
		r.wireCache.Upsert(wreq.GetNSN(), NewWire(r.workerCache, wreq, 200))
	} else {
		r.l.Info("upsert cache", "nsn", wreq.GetNSN(), "desired action", DesiredActionCreate)
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionCreate)
	}
	r.wireCreate(wreq, "api")
	r.l.Info("creating...", "nsn", wreq.GetNSN())
	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) Delete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO deallocate VPN
	r.l.Info("delete...")
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{req}
	if _, err := r.wireCache.Get(wreq.GetNSN()); err == nil {
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionDelete)
		r.l.Info("delete", "nsn", wreq.GetNSN())
		r.wireDelete(wreq, "api")
	}
	r.l.Info("deleting...", "nsn", wreq.GetNSN())
	return &wirepb.EmptyResponse{}, nil
}

// resolveEndpoint finds the service endpoint and daemon nodeName based on the network pod (namespace/name)
// - check if the pod exists in the cache and if it is ready
// -> if ready we get the nodeName the network pod is running on
// - via the nodeName we can find the serviceendpoint in the daemon cache if the daemon is ready
func (r *wc) resolveEndpoint(wreq *WireReq, epIdx int) *ResolvedData {
	pod, err := r.podCache.Get(wreq.GetEndpointNodeNSN(epIdx))
	if err != nil {
		return &ResolvedData{Message: fmt.Sprintf("pod not found: %s", wreq.GetEndpointNodeNSN(epIdx).String())}
	}
	if !pod.IsReady {
		return &ResolvedData{Message: fmt.Sprintf("pod not ready: %s", wreq.GetEndpointNodeNSN(epIdx).String())}
	}
	daemonHostNodeNSN := types.NamespacedName{
		Namespace: "default",
		Name:      pod.HostNodeName}
	d, err := r.daemonCache.Get(daemonHostNodeNSN)
	if err != nil {
		return &ResolvedData{Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}
	}
	if !d.IsReady {
		return &ResolvedData{Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}
	}
	if d.GRPCAddress == "" || d.GRPCPort == "" {
		return &ResolvedData{Message: fmt.Sprintf("wireDaemon no grpc address/port: %s", daemonHostNodeNSN.String())}
	}
	return &ResolvedData{
		Success:         true,
		PodNodeName:     pod.HostNodeName,
		ServiceEndpoint: fmt.Sprintf("%s:%s", d.GRPCAddress, d.GRPCPort),
		HostIP:          d.HostIP,
		HostNodeName:    pod.HostNodeName,
	}
}

func (r *wc) resolve(wreq *WireReq) []*ResolvedData {
	resolvedData := make([]*ResolvedData, 2)
	for epIdx := range wreq.Endpoints {
		resolvedData[epIdx] = r.resolveEndpoint(wreq, epIdx)
	}
	return resolvedData
}

func (r *wc) wireCreate(wreq *WireReq, origin string) {
	r.l.Info("wireCreate ...start...", "nsn", wreq.GetNSN(), "origin", origin)
	// we want to resolve first to see if both endpoints resolve
	// if not we dont create the endpoint event
	resolvedData := r.resolve(wreq)
	r.l.Info("wireCreate", "nsn", wreq.GetNSN(), "origin", origin, "resolvedData0", resolvedData[0], "resolvedData1", resolvedData[1])
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	if resolvedData[0].Success && resolvedData[1].Success {
		r.l.Info("wireCreate", "nsn", wreq.GetNSN(), "origin", origin, "resolution", "succeeded")
		// resolution worked for both epA and epB
		r.wireCache.HandleEvent(wreq.GetNSN(), CreateEvent, &EventCtx{
			EpIdx: 0,
		})
		// both endpoints resolve to the same host -> through dependency we indicate
		// this (the state machine handles the dependency)
		if resolvedData[0].HostIP == resolvedData[1].HostIP {
			r.wireCache.HandleEvent(wreq.GetNSN(), CreateEvent, &EventCtx{
				EpIdx:    1,
				SameHost: true,
			})
		} else {
			r.wireCache.HandleEvent(wreq.GetNSN(), CreateEvent, &EventCtx{
				EpIdx: 1,
			})
		}
	} else {
		// handle event is done by the resolution with more specific info
		r.l.Info("wireCreate", "nsn", wreq.GetNSN(), "origin", origin, "resolution", "failed")
		// from a callback we try to only deal
		if origin != "callback" {
			r.wireCache.HandleEvent(wreq.GetNSN(), ResolutionFailedEvent, &EventCtx{
				EpIdx:   0,
				Message: resolvedData[0].Message,
				Hold:    true,
			})
			r.wireCache.HandleEvent(wreq.GetNSN(), ResolutionFailedEvent, &EventCtx{
				EpIdx:   1,
				Message: resolvedData[0].Message,
				Hold:    true,
			})
		}
	}
	r.l.Info("wireCreate ...end...", "nsn", wreq.GetNSN(), "origin", origin)
}

func (r *wc) wireDelete(wreq *WireReq, origin string) {
	r.l.Info("wireDelete ...start...", "nsn", wreq.GetNSN(), "origin", origin)
	// we want to resolve first to see if the endpoints resolve
	// depending on this we generate delete events if the resolution was ok
	resolvedData := r.resolve(wreq)
	r.l.Info("wireDelete", "nsn", wreq.GetNSN(), "origin", origin, "resolvedData0", resolvedData[0], "resolvedData1", resolvedData[1])
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	if resolvedData[0].Success {
		r.wireCache.HandleEvent(wreq.GetNSN(), DeleteEvent, &EventCtx{
			EpIdx: 0,
		})
	}
	if resolvedData[1].Success {
		if resolvedData[0].Success && resolvedData[0].HostIP == resolvedData[1].HostIP {
			// both endpoints resolve to the same host -> through dependency we indicate
			// this (the state machine handles the dependency)
			r.wireCache.HandleEvent(wreq.GetNSN(), DeleteEvent, &EventCtx{
				EpIdx:    0,
				SameHost: true,
			})
			return
		}
		r.wireCache.HandleEvent(wreq.GetNSN(), DeleteEvent, &EventCtx{
			EpIdx: 1,
		})
	}
	r.l.Info("wireDelete ...end...", "nsn", wreq.GetNSN(), "origin", origin)
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
			// worker found
			c.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
	}
	r.l.Info("daemonCallback ...call common callback...", "nsn", nsn, "data", d)

	r.commonCallback(ctx, nsn, newd, &CallbackCtx{
		Message:          "daemon failed",
		Hold:             true,
		EvalHostNodeName: true,
	})
	r.l.Info("daemonCallback ...end...", "nsn", nsn, "data", d)
}

func (r *wc) podCallback(ctx context.Context, nsn types.NamespacedName, d any) {
	r.l.Info("podCallback ...start...", "nsn", nsn, "data", d)
	p, ok := d.(wirepod.Pod)
	if !ok {
		r.l.Info("expect Pod", "got", reflect.TypeOf(d).Name())
		return
	}
	var newd any
	newd = nil
	if d != nil && p.IsReady {
		newd = p
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
	n, ok := d.(wirenode.Node)
	if !ok {
		r.l.Info("expect Node", "got", reflect.TypeOf(d).Name())
		return
	}
	var newd any
	newd = nil
	if d != nil && n.IsReady {
		newd = n
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
						r.wireCache.HandleEvent(wireNSN, ResolutionFailedEvent, &EventCtx{
							EpIdx:   epIdx,
							Message: cbctx.Message,
							Hold:    cbctx.Hold, // we do not want this event to eb replciated to the other endpoint
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
					r.wireCreate(wire.WireReq, "callback")
				} else {
					r.wireDelete(wire.WireReq, "callback")
				}
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
