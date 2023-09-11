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

package wireproxy

import (
	"context"
	"log/slog"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw/logger/log"
	"google.golang.org/grpc/peer"
)

type Proxy interface {
	WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error)
	WireCreate(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
	WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
	WireWatch(req *wirepb.WatchRequest, stream wirepb.Wire_WireWatchServer) error
}

type Config struct {
	Backend wire.Node2NodeWirer
}

func New(ctx context.Context, cfg *Config) Proxy {
	return &p{
		be:    cfg.Backend,
		state: NewProxyState(&stateConfig{be: cfg.Backend}),
		l:     log.FromContext(ctx).WithGroup("node2node grpc proxy"),
	}
}

type p struct {
	be    wire.Node2NodeWirer
	state *s
	//logger
	l *slog.Logger
}

func (r *p) WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	ctx = log.IntoContext(ctx, r.l.With("nsn", req.WireKey.String(), "epA", req.Endpoints[0], "epB", req.Endpoints[1], "intercluster", req.Intercluster))
	return r.be.WireGet(ctx, req)
}

func (r *p) WireCreate(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	ctx = log.IntoContext(ctx, r.l.With("nsn", req.WireKey.String(), "epA", req.Endpoints[0], "epB", req.Endpoints[1], "intercluster", req.Intercluster))
	return r.be.WireUpSert(ctx, req)
}

func (r *p) WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	ctx = log.IntoContext(ctx, r.l.With("nsn", req.WireKey.String(), "epA", req.Endpoints[0], "epB", req.Endpoints[1], "intercluster", req.Intercluster))
	return r.be.WireDelete(ctx, req)
}

func (r *p) WireWatch(req *wirepb.WatchRequest, stream wirepb.Wire_WireWatchServer) error {
	ctx := stream.Context()
	p, _ := peer.FromContext(ctx)
	addr := "unknown"
	if p != nil {
		addr = p.Addr.String()
	}
	r.l.Info("watch", "address", addr, "req", req)

	r.state.AddWireCallBackFn(req, stream)
	return nil
}
