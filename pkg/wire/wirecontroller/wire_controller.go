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
	"sync"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type Config struct {
	DaemonCache wire.Cache[daemon.Daemon]
	PodCache    wire.Cache[pod.Pod]
	NodeCache   wire.Cache[node.Node]
}

func New(cfg *Config) wire.Wire {
	r := &wc{
		daemonCache: cfg.DaemonCache,
		podCache:    cfg.PodCache,
		nodeCache:   cfg.NodeCache,
		wireCache:   NewWireCache(),
	}
	r.daemonCache.AddWatch(r.daemonCallback)
	r.podCache.AddWatch(r.podCallback)
	r.nodeCache.AddWatch((r.nodeCallback))
	return r
}

type wc struct {
	daemonCache wire.Cache[daemon.Daemon]
	podCache    wire.Cache[pod.Pod]
	nodeCache   wire.Cache[node.Node]

	wireCache WireCache

	geventCh chan event.GenericEvent
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
func (r *wc) DeleteWatch()

func (r *wc) Get(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	if err := r.validate(req); err != nil {
		return &wirepb.WireResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	_, err := r.wireCache.Get(types.NamespacedName{Namespace: req.Namespace, Name: req.Name})
	if err != nil {
		return &wirepb.WireResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	// TODO -> ELABORATE STATUS
	return &wirepb.WireResponse{
		StatusCode: wirepb.StatusCode_OK,
	}, nil
}

func (r *wc) UpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO allocate VPN
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wirereq := *req
	wireNSN := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	if _, err := r.wireCache.Get(wireNSN); err != nil {
		// not found -> create a new link
		r.wireCache.Upsert(wireNSN, NewLink(&wirereq, 200))
	} else {
		r.wireCache.SetDesiredAction(wireNSN, DesiredActionCreate)
	}
	r.wireCreate(ctx, &wirereq)

	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) Delete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO deallocate VPN
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wirereq := *req
	wireNSN := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	r.wireCache.SetDesiredAction(wireNSN, DesiredActionDelete)

	r.wireDelete(ctx, &wirereq)

	return &wirepb.EmptyResponse{}, nil
}

// resolveEndpoint finds the service endpoint and daemon nodeName based on the network pod (namespace/name)
// - check if the pod exists in the cache and if it is ready
// -> if ready we get the nodeName the network pod is running on
// - via the nodeName we can find the serviceendpoint in the daemon cache if the daemon is ready
func (r *wc) resolveEndpoint(ctx context.Context, req *wirepb.WireRequest, epIdx int) (*ResolvedData, error) {
	wireNSN := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	epPoDNodeNSN := types.NamespacedName{Namespace: req.Endpoints[epIdx].Topology, Name: req.Endpoints[epIdx].NodeName}
	pod, err := r.podCache.Get(epPoDNodeNSN)
	if err != nil {
		if err := r.wireCache.HandleEvent(ResolutionFailedEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        epIdx,
			ResolvedData: nil,
			Message:      fmt.Sprintf("pod not found: %s", epPoDNodeNSN.String()),
		}); err != nil {
			return nil, err
		}
	}
	if !pod.IsReady {
		if err := r.wireCache.HandleEvent(ResolutionFailedEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        epIdx,
			ResolvedData: nil,
			Message:      fmt.Sprintf("pod not ready: %s", epPoDNodeNSN.String()),
		}); err != nil {
			return nil, err
		}
	}
	daemonHostNodeNSN := types.NamespacedName{
		Namespace: os.Getenv("POD_NAMESPACE"),
		Name:      pod.HostNodeName}
	d, err := r.daemonCache.Get(daemonHostNodeNSN)
	if err != nil {
		if err := r.wireCache.HandleEvent(ResolutionFailedEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        epIdx,
			ResolvedData: nil,
			Message:      fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String()),
		}); err != nil {
			return nil, err
		}
	}
	if !d.IsReady {
		if err := r.wireCache.HandleEvent(ResolutionFailedEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        epIdx,
			ResolvedData: nil,
			Message:      fmt.Sprintf("wireDaemon not ready: %s", daemonHostNodeNSN.String()),
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

func (r *wc) wireCreate(ctx context.Context, req *wirepb.WireRequest) {
	wireNSN := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	// we want to resolve first to see if both endpoints resolve
	// if not we dont create the endpoint event
	resolvedDataA, _ := r.resolveEndpoint(ctx, req, 0)
	resolvedDataB, _ := r.resolveEndpoint(ctx, req, 1)
	if resolvedDataA != nil && resolvedDataB != nil {
		// resolution worked for both epA and epB
		r.wireCache.HandleEvent(CreateEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        0,
			ResolvedData: resolvedDataA,
		})
		// both endpoints resolve to the same host -> through dependency we indicate
		// this (the state machine handles the dependency)
		if resolvedDataA.HostIP == resolvedDataB.HostIP {
			r.wireCache.HandleEvent(CreateEvent, &EventCtx{
				WireNSN:      wireNSN,
				EpIdx:        1,
				ResolvedData: resolvedDataB,
				SameHost:     true,
			})
		} else {
			r.wireCache.HandleEvent(CreateEvent, &EventCtx{
				WireNSN:      wireNSN,
				EpIdx:        1,
				ResolvedData: resolvedDataB,
			})
		}
	}
}

func (r *wc) wireDelete(ctx context.Context, req *wirepb.WireRequest) {
	wireNSN := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}

	resolvedDataA, _ := r.resolveEndpoint(ctx, req, 0)
	resolvedDataB, _ := r.resolveEndpoint(ctx, req, 1)
	if resolvedDataA != nil {
		r.wireCache.HandleEvent(DeleteEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        0,
			ResolvedData: resolvedDataA,
		})
	}
	if resolvedDataB != nil {
		if resolvedDataA != nil && resolvedDataA.HostIP == resolvedDataB.HostIP {
			// both endpoints resolve to the same host -> through dependency we indicate
			// this (the state machine handles the dependency)
			r.wireCache.HandleEvent(DeleteEvent, &EventCtx{
				WireNSN:      wireNSN,
				EpIdx:        0,
				ResolvedData: resolvedDataA,
				SameHost:     true,
			})
			return
		}
		r.wireCache.HandleEvent(DeleteEvent, &EventCtx{
			WireNSN:      wireNSN,
			EpIdx:        0,
			ResolvedData: resolvedDataA,
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
	r.commonCallback(ctx, nsn, d, &CallbackCtx{
		Message:          "daemon failed",
		Hold:             true,
		EvalHostNodeName: true,
	})
}

func (r *wc) podCallback(ctx context.Context, nsn types.NamespacedName, d any) {
	r.commonCallback(ctx, nsn, d, &CallbackCtx{
		Message:          "pod failed",
		Hold:             false,
		EvalHostNodeName: false,
	})
}

func (r *wc) nodeCallback(ctx context.Context, nsn types.NamespacedName, d any) {
	r.commonCallback(ctx, nsn, d, &CallbackCtx{
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
			for epIdx, ep := range wire.Endpoints {
				epIdx := epIdx
				if ep.ResolvedData != nil && ep.ResolvedData.CompareName(cbctx.EvalHostNodeName, nsn.Name) {
					wg.Add(1)
					go func() {
						(&w).HandleEvent(ResolutionFailedEvent, &EventCtx{
							WireNSN:      wireNSN,
							Wire:         &w,
							EpIdx:        epIdx,
							ResolvedData: nil,
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
			w := *wire
			wg.Add(1)

			go func() {
				if (&w).DesiredAction == DesiredActionCreate {
					r.wireCreate(ctx, w.WireReq)
				} else {
					r.wireDelete(ctx, w.WireReq)
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
