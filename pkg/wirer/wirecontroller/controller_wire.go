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
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	//"sigs.k8s.io/controller-runtime/pkg/event"
)

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

func (r *wc) AddWireWatch(fn wirer.CallbackFn) {}
func (r *wc) DeleteWireWatch()                 {}

func (r *wc) WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	log := log.FromContext(ctx)
	log.Info("get...")
	if err := r.validate(req); err != nil {
		return &wirepb.WireResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{WireRequest: req}
	log.Info("get", "nsn", wreq.GetNSN())
	w, err := r.wireCache.Get(wreq.GetNSN())
	if err != nil {
		return &wirepb.WireResponse{StatusCode: wirepb.StatusCode_NotFound, Reason: err.Error()}, nil
	}
	return w.GetWireResponse(), nil
}

func (r *wc) WireUpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("upsert...")
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	wreq := &WireReq{WireRequest: req}
	log = log.With("nsn", wreq.GetNSN())
	log.Info("upsert", )
	if _, err := r.wireCache.Get(wreq.GetNSN()); err != nil {
		// allocate vxlanID
		vxlanID, err := r.vxlanclient.Claim(ctx, req)
		if err != nil {
			log.Error("vxlan claim failed", "errot", err.Error())
			return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
		}
		log.Info("upsert cache")
		r.wireCache.Upsert(ctx, wreq.GetNSN(), NewWire(r.dispatcher, wreq, *vxlanID))
	} else {
		r.l.Info("upsert cache", "desired action", DesiredActionCreate)
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionCreate)
	}
	r.wireCreate(ctx, wreq, "api")
	log.Info("creating...")
	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("delete...")
	if err := r.validate(req); err != nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	
	wreq := &WireReq{WireRequest: req}
	if _, err := r.wireCache.Get(wreq.GetNSN()); err == nil {
		r.wireCache.SetDesiredAction(wreq.GetNSN(), DesiredActionDelete)
		log.Info("delete", "nsn", wreq.GetNSN())
		r.wireDelete(ctx, wreq, "api")
	}
	log = log.With("nsn", wreq.GetNSN())
	log.Info("deleting...")
	return &wirepb.EmptyResponse{}, nil
}

func (r *wc) resolveWire(ctx context.Context, wreq *WireReq) []*resolve.Data {
	log := log.FromContext(ctx)
	log.Info("resolve wire...")
	resolvedData := make([]*resolve.Data, 2)
	for epIdx := range wreq.Endpoints {
		resolvedData[epIdx] = r.resolveEndpoint(wreq.GetEndpointNodeNSN(epIdx), wreq.Intercluster)
	}
	if resolvedData[0].Action == false && resolvedData[1].Action == false {
		return []*resolve.Data{
			{
				Success: false,
				Message: "a wire cannot have both endpoints in a remote cluster",
			},
			{
				Success: false,
				Message: "a wire cannot have both endpoints in a remote cluster",
			},
		}
	}
	return resolvedData
}

func (r *wc) wireCreate(ctx context.Context, wreq *WireReq, origin string) {
	log := log.FromContext(ctx).With("nsn", wreq.GetNSN(), "origin", origin)
	log.Info("wireCreate ...start...")
	// we want to resolve first to see if both endpoints resolve
	// if not we dont create the endpoint event
	resolvedData := r.resolveWire(ctx, wreq)
	log.Info("wireCreate", "resolvedData0", resolvedData[0], "resolvedData1", resolvedData[1])
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	// TODO check if at least 1 ep is local for intercluster wiring
	if resolvedData[0].Success && resolvedData[1].Success {
		log.Info("wireCreate resolution succeeded")
		// resolution worked for both epA and epB
		if resolvedData[0].Action {
			r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.CreateEvent, &state.EventCtx{
				EpIdx: 0,
			})
		}
		// both endpoints resolve to the same host -> through dependency we indicate
		// this (the state machine handles the dependency)
		if resolvedData[1].Action {
			if resolvedData[0].HostIP == resolvedData[1].HostIP {

				r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.CreateEvent, &state.EventCtx{
					EpIdx:    1,
					SameHost: true,
				})
			} else {
				r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.CreateEvent, &state.EventCtx{
					EpIdx: 1,
				})
			}
		}
	} else {
		// handle event is done by the resolution with more specific info
		log.Info("wireCreate resolution faialed")
		// from a callback we try to only deal
		if origin != "callback" {
			r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.ResolutionFailedEvent, &state.EventCtx{
				EpIdx:   0,
				Message: resolvedData[0].Message,
				Hold:    true,
			})
			r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.ResolutionFailedEvent, &state.EventCtx{
				EpIdx:   1,
				Message: resolvedData[0].Message,
				Hold:    true,
			})
		}
	}
	log.Info("wireCreate ...end...")
}

func (r *wc) wireDelete(ctx context.Context, wreq *WireReq, origin string) {
	log := log.FromContext(ctx).With("nsn", wreq.GetNSN(), "origin", origin)
	log.Info("wireDelete ...start...")

	// we want to resolve first to see if the endpoints resolve
	// depending on this we generate delete events if the resolution was ok
	resolvedData := r.resolveWire(ctx, wreq)
	log.Info("wireDelete", "resolvedData0", resolvedData[0], "resolvedData1", resolvedData[1])
	r.wireCache.Resolve(wreq.GetNSN(), resolvedData)
	if resolvedData[0].Success && resolvedData[0].Action {
		r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.DeleteEvent, &state.EventCtx{
			EpIdx: 0,
		})
	}
	if resolvedData[1].Success && resolvedData[1].Action {
		if resolvedData[0].Success && resolvedData[0].HostIP == resolvedData[1].HostIP {
			// both endpoints resolve to the same host -> through dependency we indicate
			// this (the state machine handles the dependency)
			r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.DeleteEvent, &state.EventCtx{
				EpIdx:    1,
				SameHost: true,
			})
			return
		}
		r.wireCache.HandleEvent(ctx, wreq.GetNSN(), state.DeleteEvent, &state.EventCtx{
			EpIdx: 1,
		})
	}
	log.Info("wireDelete ...end...")
}
