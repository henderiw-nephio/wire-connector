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
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *wc) AddEndpointWatch(fn wirer.CallbackFn) {}
func (r *wc) DeleteEndpointWatch()                 {}

func (r *wc) EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error) {
	log := log.FromContext(ctx)
	log.Info("node2ep get...")
	if req.NodeKey == nil {
		return &endpointpb.EndpointResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &NodeEpReq{EndpointRequest: req}
	log.Info("node2ep get", "nsn", epreq.GetNSN())
	ep, err := r.nodeepCache.Get(epreq.GetNSN())
	if err != nil {
		return &endpointpb.EndpointResponse{StatusCode: endpointpb.StatusCode_NotFound, Reason: err.Error()}, nil
	}
	return ep.GetResponse(), nil
}

func (r *wc) EndpointUpSert(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("node2ep upsert...")
	if req.NodeKey == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &NodeEpReq{EndpointRequest: req}
	log = log.With("nsn", epreq.GetNSN())
	log.Info("node2ep upsert")
	if _, err := r.nodeepCache.Get(epreq.GetNSN()); err != nil {
		// not found -> create a new link
		r.l.Info("node2ep upsert cache")
		r.nodeepCache.Upsert(ctx, epreq.GetNSN(), NewNodeEndpoint(ctx, r.dispatcher, epreq))
	}
	r.nodeepCreate(ctx, epreq, "api")
	log.Info("node2ep creating...")
	return &endpointpb.EmptyResponse{}, nil
}

func (r *wc) EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("node2ep delete...")
	if req.NodeKey == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	epreq := &NodeEpReq{EndpointRequest: req}
	log = log.With("nsn", epreq.GetNSN())
	if _, err := r.nodeepCache.Get(epreq.GetNSN()); err == nil {
		log.Info("node2ep delete")
		r.nodeepDelete(ctx, epreq, "api")
	}
	r.l.Info("node-epa deleting...")
	return &endpointpb.EmptyResponse{}, nil
}

func (r *wc) nodeepCreate(ctx context.Context, req *NodeEpReq, origin string) {
	nsn := req.GetNSN()
	log := log.FromContext(ctx).With("nsn", nsn, "origin", origin)
	log.Info("node2ep create ...start...")

	// we want to resolve first to see if the daemon is available
	resolvedData := r.resolveEndpoint(nsn, &wirepb.Endpoint{}, false)
	r.l.Info("node2ep create resolution", "resolvedData", *resolvedData)
	r.nodeepCache.Resolve(nsn, resolvedData)
	if resolvedData.Success {
		log.Info("node2ep create resolution succeeded")
		// resolution worked for both epA and epB
		r.nodeepCache.HandleEvent(ctx, nsn, state.CreateEvent, &state.EventCtx{})

	} else {
		// handle event is done by the resolution with more specific info
		log.Info("node2ep create resolution failed")
		// from a callback we try to only deal
		if origin != "callback" {
			r.nodeepCache.HandleEvent(ctx, nsn, state.ResolutionFailedEvent, &state.EventCtx{
				Message: resolvedData.Message,
			})
		}
	}
	log.Info("node2ep create ...end...")
}

func (r *wc) nodeepDelete(ctx context.Context, req *NodeEpReq, origin string) {
	nsn := req.GetNSN()
	log := log.FromContext(ctx).With("nsn", nsn, "origin", origin)
	log.Info("node2ep delete ...start...")
	// we want to resolve first to see if the daemon is available
	resolvedData := r.resolveEndpoint(nsn, &wirepb.Endpoint{}, false)
	log.Info("resolution", "resolvedData", *resolvedData)
	r.nodeepCache.Resolve(nsn, resolvedData)
	if resolvedData.Success {
		log.Info("resolution succeeded")
		r.nodeepCache.HandleEvent(ctx, nsn, state.DeleteEvent, &state.EventCtx{})
	}
	log.Info("node2ep delete ...end...")
}
