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

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *wc) AddEndpointWatch(fn wire.CallbackFn) {}
func (r *wc) DeleteEndpointWatch()                {}

func (r *wc) EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error) {
	r.l.Info("ep get...")
	if req.NodeKey == nil {
		return &endpointpb.EndpointResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &EpReq{EndpointRequest: req}
	r.l.Info("get", "nsn", epreq.GetNSN())
	ep, err := r.epCache.Get(epreq.GetNSN())
	if err != nil {
		return &endpointpb.EndpointResponse{StatusCode: endpointpb.StatusCode_NotFound, Reason: err.Error()}, nil
	}
	return ep.GetResponse(), nil
}

func (r *wc) EndpointUpSert(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	// TODO allocate VPN
	r.l.Info("ep upsert...")
	if req.NodeKey == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &EpReq{EndpointRequest: req}
	r.l.Info("ep upsert", "nsn", epreq.GetNSN())
	if _, err := r.epCache.Get(epreq.GetNSN()); err != nil {
		// not found -> create a new link
		r.l.Info("upsert ep cache", "nsn", epreq.GetNSN())
		r.epCache.Upsert(ctx, epreq.GetNSN(), NewEndpoint(r.dispatcher, epreq))
	}
	r.epCreate(epreq, "api")
	r.l.Info("creating...", "nsn", epreq.GetNSN())
	return &endpointpb.EmptyResponse{}, nil
}

func (r *wc) EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	// TODO deallocate VPN
	r.l.Info("delete...")
	if req.NodeKey == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &EpReq{EndpointRequest: req}
	if _, err := r.epCache.Get(epreq.GetNSN()); err == nil {
		r.l.Info("delete", "nsn", epreq.GetNSN())
		r.epDelete(epreq, "api")
	}
	r.l.Info("deleting...", "nsn", epreq.GetNSN())
	return &endpointpb.EmptyResponse{}, nil
}

func (r *wc) epCreate(req *EpReq, origin string) {
	nsn := req.GetNSN()
	log := r.l.WithValues("fn", "epCreate", "nsn", nsn, "origin", origin)
	log.Info("epCreate ...start...")
	// we want to resolve first to see if the daemon is available
	resolvedData := r.resolveEndpoint(nsn)
	r.l.Info("resolution", "resolvedData", *resolvedData)
	r.epCache.Resolve(nsn, resolvedData)
	if resolvedData.Success {
		log.Info("resolution succeeded")
		// resolution worked for both epA and epB
		r.epCache.HandleEvent(nsn, state.CreateEvent, &state.EventCtx{})

	} else {
		// handle event is done by the resolution with more specific info
		log.Info("resolution failed")
		// from a callback we try to only deal
		if origin != "callback" {
			r.epCache.HandleEvent(nsn, state.ResolutionFailedEvent, &state.EventCtx{
				Message: resolvedData.Message,
			})
		}
	}
	log.Info("epCreate ...end...")
}

func (r *wc) epDelete(req *EpReq, origin string) {
	nsn := req.GetNSN()
	log := r.l.WithValues("fn", "epDelete", "nsn", nsn, "origin", origin)
	log.Info("epDelete ...start...")
	// we want to resolve first to see if the daemon is available
	resolvedData := r.resolveEndpoint(nsn)
	r.l.Info("resolution", "resolvedData", *resolvedData)
	r.epCache.Resolve(nsn, resolvedData)
	if resolvedData.Success {
		log.Info("resolution succeeded")
		r.epCache.HandleEvent(nsn, state.DeleteEvent, &state.EventCtx{})
	}
	log.Info("epDelete ...end...")
}
