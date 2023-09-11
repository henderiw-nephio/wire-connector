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

package nodeepproxy

import (
	"context"
	"log/slog"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw/logger/log"
	"google.golang.org/grpc/peer"
)

type Proxy interface {
	EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error)
	EndpointCreate(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error)
	EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error)
	EndpointWatch(req *endpointpb.WatchRequest, stream endpointpb.NodeEndpoint_EndpointWatchServer) error
}

type Config struct {
	Backend wire.Ep2NodeWirer
}

func New(ctx context.Context, cfg *Config) Proxy {
	return &p{
		be:    cfg.Backend,
		state: NewProxyState(&stateConfig{be: cfg.Backend}),
		l:     log.FromContext(ctx).WithGroup("nodeep grpc proxy"),
	}
}

type p struct {
	be    wire.Ep2NodeWirer
	state *s
	//logger
	l *slog.Logger
}

func (r *p) EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error) {
	ctx = log.IntoContext(ctx, r.l.With("nsn", req.NodeKey.String(), "server", req.ServerType))
	return r.be.EndpointGet(ctx, req)
}

func (r *p) EndpointCreate(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	ctx = log.IntoContext(ctx, r.l.With("nsn", req.NodeKey.String(), "server", req.ServerType))
	return r.be.EndpointUpSert(ctx, req)
}

func (r *p) EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	ctx = log.IntoContext(ctx, r.l.With("nsn", req.NodeKey.String(), "server", req.ServerType))
	return r.be.EndpointDelete(ctx, req)
}

func (r *p) EndpointWatch(req *endpointpb.WatchRequest, stream endpointpb.NodeEndpoint_EndpointWatchServer) error {
	ctx := stream.Context()
	p, _ := peer.FromContext(ctx)
	addr := "unknown"
	if p != nil {
		addr = p.Addr.String()
	}
	r.l.Info("watch", "address", addr, "req", req)

	// TODO add watch
	//r.state.AddCallBackFn(req, stream)
	return nil
}
