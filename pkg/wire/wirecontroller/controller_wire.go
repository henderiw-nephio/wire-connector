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

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

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

func (r *wc) AddWireWatch(fn wire.CallbackFn) {}
func (r *wc) DeleteWireWatch()                {}

func (r *wc) WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	r.l.Info("get...")
	if err := r.validate(req); err != nil {
		return &wirepb.WireResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{WireRequest: req}
	r.l.Info("get", "nsn", wreq.GetNSN())
	w, err := r.wireCache.Get(wreq.GetNSN())
	if err != nil {
		return &wirepb.WireResponse{StatusCode: wirepb.StatusCode_NotFound, Reason: err.Error()}, nil
	}
	return w.GetWireResponse(), nil
}

func (r *wc) WireUpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO allocate VPN
	r.l.Info("upsert...")
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{WireRequest: req}
	r.l.Info("upsert", "nsn", wreq.GetNSN())
	if _, err := r.wireCache.Get(wreq.GetNSN()); err != nil {
		// not found -> create a new link
		r.l.Info("upsert cache", "nsn", wreq.GetNSN())
		r.wireCache.Upsert(ctx, wreq.GetNSN(), NewWire(r.dispatcher, wreq, 200))
	} else {
		r.l.Info("upsert cache", "nsn", wreq.GetNSN(), "desired action", DesiredActionCreate)
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionCreate)
	}
	r.wireCreate(wreq, "api")
	r.l.Info("creating...", "nsn", wreq.GetNSN())
	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	// TODO deallocate VPN
	r.l.Info("delete...")
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{WireRequest: req}
	if _, err := r.wireCache.Get(wreq.GetNSN()); err == nil {
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionDelete)
		r.l.Info("delete", "nsn", wreq.GetNSN())
		r.wireDelete(wreq, "api")
	}
	r.l.Info("deleting...", "nsn", wreq.GetNSN())
	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) resolveWire(wreq *WireReq) []*resolve.Data {
	resolvedData := make([]*resolve.Data, 2)
	for epIdx := range wreq.Endpoints {
		resolvedData[epIdx] = r.resolveEndpoint(wreq.GetEndpointNodeNSN(epIdx))
	}
	return resolvedData
}

func (r *wc) wireCreate(wreq *WireReq, origin string) {
	r.l.Info("wireCreate ...start...", "nsn", wreq.GetNSN(), "origin", origin)
	// we want to resolve first to see if both endpoints resolve
	// if not we dont create the endpoint event
	resolvedData := r.resolveWire(wreq)
	r.l.Info("wireCreate", "nsn", wreq.GetNSN(), "origin", origin, "resolvedData0", resolvedData[0], "resolvedData1", resolvedData[1])
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	if resolvedData[0].Success && resolvedData[1].Success {
		r.l.Info("wireCreate", "nsn", wreq.GetNSN(), "origin", origin, "resolution", "succeeded")
		// resolution worked for both epA and epB
		r.wireCache.HandleEvent(wreq.GetNSN(), state.CreateEvent, &state.EventCtx{
			EpIdx: 0,
		})
		// both endpoints resolve to the same host -> through dependency we indicate
		// this (the state machine handles the dependency)
		if resolvedData[0].HostIP == resolvedData[1].HostIP {
			r.wireCache.HandleEvent(wreq.GetNSN(), state.CreateEvent, &state.EventCtx{
				EpIdx:    1,
				SameHost: true,
			})
		} else {
			r.wireCache.HandleEvent(wreq.GetNSN(), state.CreateEvent, &state.EventCtx{
				EpIdx: 1,
			})
		}
	} else {
		// handle event is done by the resolution with more specific info
		r.l.Info("wireCreate", "nsn", wreq.GetNSN(), "origin", origin, "resolution", "failed")
		// from a callback we try to only deal
		if origin != "callback" {
			r.wireCache.HandleEvent(wreq.GetNSN(), state.ResolutionFailedEvent, &state.EventCtx{
				EpIdx:   0,
				Message: resolvedData[0].Message,
				Hold:    true,
			})
			r.wireCache.HandleEvent(wreq.GetNSN(), state.ResolutionFailedEvent, &state.EventCtx{
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
	resolvedData := r.resolveWire(wreq)
	r.l.Info("wireDelete", "nsn", wreq.GetNSN(), "origin", origin, "resolvedData0", resolvedData[0], "resolvedData1", resolvedData[1])
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	if resolvedData[0].Success {
		r.wireCache.HandleEvent(wreq.GetNSN(), state.DeleteEvent, &state.EventCtx{
			EpIdx: 0,
		})
	}
	if resolvedData[1].Success {
		if resolvedData[0].Success && resolvedData[0].HostIP == resolvedData[1].HostIP {
			// both endpoints resolve to the same host -> through dependency we indicate
			// this (the state machine handles the dependency)
			r.wireCache.HandleEvent(wreq.GetNSN(), state.DeleteEvent, &state.EventCtx{
				EpIdx:    1,
				SameHost: true,
			})
			return
		}
		r.wireCache.HandleEvent(wreq.GetNSN(), state.DeleteEvent, &state.EventCtx{
			EpIdx: 1,
		})
	}
	r.l.Info("wireDelete ...end...", "nsn", wreq.GetNSN(), "origin", origin)
}
