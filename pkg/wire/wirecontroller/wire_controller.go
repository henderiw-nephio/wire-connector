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

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	"google.golang.org/appengine/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	DaemonCache wire.Cache[wiredaemon.Daemon]
	PodCache    wire.Cache[wirepod.Pod]
	NodeCache   wire.Cache[wirenode.Node]
}

func New(cfg *Config) wire.Wire {
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
	return r
}

type wc struct {
	daemonCache wire.Cache[wiredaemon.Daemon]
	podCache    wire.Cache[wirepod.Pod]
	nodeCache   wire.Cache[wirenode.Node]
	workerCache wire.Cache[Worker]

	wireCache WireCache

	geventCh chan event.GenericEvent

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
	if err := r.validate(req); err != nil {
		return &wirepb.WireResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{req}
	w, err := r.wireCache.Get(wreq.GetNSN())
	if err != nil {
		return &wirepb.WireResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return w.GetWireResponse(), nil
}

func (r *wc) UpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO allocate VPN
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{req}
	if _, err := r.wireCache.Get(wreq.GetNSN()); err != nil {
		// not found -> create a new link
		r.wireCache.Upsert(wreq.GetNSN(), NewWire(wreq, 200))
	} else {
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionCreate)
	}
	r.wireCreate(wreq)

	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) Delete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO deallocate VPN
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{req}
	if _, err := r.wireCache.Get(wreq.GetNSN()); err == nil {
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionDelete)
		r.wireDelete(wreq)
	}

	return &wirepb.EmptyResponse{}, nil
}

// resolveEndpoint finds the service endpoint and daemon nodeName based on the network pod (namespace/name)
// - check if the pod exists in the cache and if it is ready
// -> if ready we get the nodeName the network pod is running on
// - via the nodeName we can find the serviceendpoint in the daemon cache if the daemon is ready
func (r *wc) resolveEndpoint(wreq *WireReq, epIdx int) (*ResolvedData, error) {
	pod, err := r.podCache.Get(wreq.GetEndpointNodeNSN(epIdx))
	if err != nil {
		if err := r.wireCache.HandleEvent(wreq.GetNSN(), ResolutionFailedEvent, &EventCtx{
			EpIdx:   epIdx,
			Message: fmt.Sprintf("pod not found: %s", wreq.GetEndpointNodeNSN(epIdx).String()),
		}); err != nil {
			return nil, err
		}
	}
	if !pod.IsReady {
		if err := r.wireCache.HandleEvent(wreq.GetNSN(), ResolutionFailedEvent, &EventCtx{
			EpIdx:   epIdx,
			Message: fmt.Sprintf("pod not ready: %s", wreq.GetEndpointNodeNSN(epIdx).String()),
		}); err != nil {
			return nil, err
		}
	}
	daemonHostNodeNSN := types.NamespacedName{
		Namespace: os.Getenv("POD_NAMESPACE"),
		Name:      pod.HostNodeName}
	d, err := r.daemonCache.Get(daemonHostNodeNSN)
	if err != nil {
		if err := r.wireCache.HandleEvent(wreq.GetNSN(), ResolutionFailedEvent, &EventCtx{
			EpIdx:   epIdx,
			Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String()),
		}); err != nil {
			return nil, err
		}
	}
	if !d.IsReady {
		if err := r.wireCache.HandleEvent(wreq.GetNSN(), ResolutionFailedEvent, &EventCtx{
			EpIdx:   epIdx,
			Message: fmt.Sprintf("wireDaemon not ready: %s", daemonHostNodeNSN.String()),
		}); err != nil {
			return nil, err
		}
	}
	return &ResolvedData{
		PodNodeName:     pod.HostNodeName,
		ServiceEndpoint: fmt.Sprintf("%s:%s", d.GRPCAddress, d.GRPCPort),
		HostIP:          d.HostIP,
		HostNodeName:    pod.HostNodeName,
	}, nil
}

func (r *wc) resolve(wreq *WireReq) []*ResolvedData {
	resolvedData := make([]*ResolvedData, 2, 2)
	for epIdx := range wreq.Endpoints {
		resolvedData[epIdx], _ = r.resolveEndpoint(wreq, epIdx)
	}
	return resolvedData
}

func (r *wc) wireCreate(wreq *WireReq) {
	// we want to resolve first to see if both endpoints resolve
	// if not we dont create the endpoint event
	resolvedData := r.resolve(wreq)
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	if resolvedData[0] != nil && resolvedData[1] != nil {
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
	}
}

func (r *wc) wireDelete(wreq *WireReq) {
	// we want to resolve first to see if the endpoints resolve
	// depending on this we generate delete events if the resolution was ok
	resolvedData := r.resolve(wreq)
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)

	if resolvedData[0] != nil {
		r.wireCache.HandleEvent(wreq.GetNSN(), DeleteEvent, &EventCtx{
			EpIdx: 0,
		})
	}
	if resolvedData[1] != nil {
		if resolvedData[0] != nil && resolvedData[0].HostIP == resolvedData[1].HostIP {
			// both endpoints resolve to the same host -> through dependency we indicate
			// this (the state machine handles the dependency)
			r.wireCache.HandleEvent(wreq.GetNSN(), DeleteEvent, &EventCtx{
				EpIdx:    0,
				SameHost: true,
			})
			return
		}
		r.wireCache.HandleEvent(wreq.GetNSN(), DeleteEvent, &EventCtx{
			EpIdx: 0,
		})
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
		if err != nil {
			log.Errorf(ctx, "err: %s", err.Error())
		} else {
			oldw.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
		// create a new client
		w, err := NewWorker(ctx, r.wireCache, &client.Config{
			Address:  address,
			Insecure: true,
		})
		if err != nil {
			log.Errorf(ctx, "err: %s", err.Error())
			return
		}
		if err := w.Start(ctx); err != nil {
			log.Errorf(ctx, "err: %s", err.Error())
			return
		}
		r.workerCache.Upsert(ctx, nsn, w)
	} else {
		c, err := r.workerCache.Get(nsn)
		if err != nil {
			log.Errorf(ctx, "err: %s", err.Error())
		} else {
			c.Stop()
			r.workerCache.Delete(ctx, nsn)
		}
	}

	r.commonCallback(ctx, nsn, newd, &CallbackCtx{
		Message:          "daemon failed",
		Hold:             true,
		EvalHostNodeName: true,
	})
}

func (r *wc) podCallback(ctx context.Context, nsn types.NamespacedName, d any) {
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
}

func (r *wc) nodeCallback(ctx context.Context, nsn types.NamespacedName, d any) {
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
}

func (r *wc) commonCallback(ctx context.Context, nsn types.NamespacedName, d any, cbctx *CallbackCtx) {
	//log := log.FromContext(ctx)
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
						r.wireCache.UnResolve(wireNSN, epIdx)
						r.wireCache.HandleEvent(wireNSN, ResolutionFailedEvent, &EventCtx{
							EpIdx:        epIdx,
							Message:      cbctx.Message,
							Hold:         cbctx.Hold, // we do not want this event to eb replciated to the other endpoint
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
				if wire.DesiredAction == DesiredActionCreate {
					r.wireCreate(wire.WireReq)
				} else {
					r.wireDelete(wire.WireReq)
				}
			}()
		}
	}
	wg.Wait()
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
