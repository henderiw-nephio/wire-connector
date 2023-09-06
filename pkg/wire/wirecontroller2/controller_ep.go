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
	r.l.Info("node-epa get...")
	if req.NodeKey == nil {
		return &endpointpb.EndpointResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &NodeEpReq{EndpointRequest: req}
	r.l.Info("get", "nsn", epreq.GetNSN())
	ep, err := r.nodeepCache.Get(epreq.GetNSN())
	if err != nil {
		return &endpointpb.EndpointResponse{StatusCode: endpointpb.StatusCode_NotFound, Reason: err.Error()}, nil
	}
	return ep.GetResponse(), nil
}

func (r *wc) EndpointUpSert(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	// TODO allocate VPN
	r.l.Info("node-epa upsert...")
	if req.NodeKey == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &NodeEpReq{EndpointRequest: req}
	r.l.Info("node-epa upsert", "nsn", epreq.GetNSN())
	if _, err := r.nodeepCache.Get(epreq.GetNSN()); err != nil {
		// not found -> create a new link
		r.l.Info("node-epa upsert cache", "nsn", epreq.GetNSN())
		r.nodeepCache.Upsert(ctx, epreq.GetNSN(), NewNodeEndpoint(r.dispatcher, epreq))
	}
	r.nodeepCreate(epreq, "api")
	r.l.Info("node-epa creating...", "nsn", epreq.GetNSN())
	return &endpointpb.EmptyResponse{}, nil
}

func (r *wc) EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	// TODO deallocate VPN
	r.l.Info("node-epa delete...")
	if req.NodeKey == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &NodeEpReq{EndpointRequest: req}
	if _, err := r.nodeepCache.Get(epreq.GetNSN()); err == nil {
		r.l.Info("node-epa delete", "nsn", epreq.GetNSN())
		r.nodeepDelete(epreq, "api")
	}
	r.l.Info("node-epa deleting...", "nsn", epreq.GetNSN())
	return &endpointpb.EmptyResponse{}, nil
}

func (r *wc) nodeepCreate(req *NodeEpReq, origin string) {
	nsn := req.GetNSN()
	log := r.l.WithValues("fn", "nodeepCreate", "nsn", nsn, "origin", origin)
	log.Info("nodeepCreate ...start...")
	// we want to resolve first to see if the daemon is available
	resolvedData := r.resolveEndpoint(nsn, false)
	r.l.Info("nodeepCreate resolution", "resolvedData", *resolvedData)
	r.nodeepCache.Resolve(nsn, resolvedData)
	if resolvedData.Success {
		log.Info("nodeepCreate resolution succeeded")
		// resolution worked for both epA and epB
		r.nodeepCache.HandleEvent(nsn, state.CreateEvent, &state.EventCtx{})

	} else {
		// handle event is done by the resolution with more specific info
		log.Info("nodeepCreate resolution failed")
		// from a callback we try to only deal
		if origin != "callback" {
			r.nodeepCache.HandleEvent(nsn, state.ResolutionFailedEvent, &state.EventCtx{
				Message: resolvedData.Message,
			})
		}
	}
	log.Info("nodeepCreate ...end...")
}

func (r *wc) nodeepDelete(req *NodeEpReq, origin string) {
	nsn := req.GetNSN()
	log := r.l.WithValues("fn", "nodeepDelete", "nsn", nsn, "origin", origin)
	log.Info("nodeepDelete ...start...")
	// we want to resolve first to see if the daemon is available
	resolvedData := r.resolveEndpoint(nsn, false)
	r.l.Info("resolution", "resolvedData", *resolvedData)
	r.nodeepCache.Resolve(nsn, resolvedData)
	if resolvedData.Success {
		log.Info("resolution succeeded")
		r.nodeepCache.HandleEvent(nsn, state.DeleteEvent, &state.EventCtx{})
	}
	log.Info("nodeepDelete ...end...")
}
