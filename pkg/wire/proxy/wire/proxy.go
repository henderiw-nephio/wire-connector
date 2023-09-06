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

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"google.golang.org/grpc/peer"
	ctrl "sigs.k8s.io/controller-runtime"
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

func New(cfg *Config) Proxy {
	l := ctrl.Log.WithName("wire-proxy")
	return &p{
		be:    cfg.Backend,
		state: NewProxyState(&stateConfig{be: cfg.Backend}),
		l:     l,
	}
}

type p struct {
	be    wire.Node2NodeWirer
	state *s
	//logger
	l logr.Logger
}

func (r *p) WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	return r.be.WireGet(ctx, req)
}

func (r *p) WireCreate(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	return r.be.WireUpSert(ctx, req)
}

func (r *p) WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	return r.be.WireDelete(ctx, req)
}

func (r *p) WireWatch(req *wirepb.WatchRequest, stream wirepb.Wire_WireWatchServer) error {
	ctx := stream.Context()
	p, _ := peer.FromContext(ctx)
	addr := "unknown"
	if p != nil {
		addr = p.Addr.String()
	}
	r.l.Info("waatch", "address", addr, "req", req)

	r.state.AddWireCallBackFn(req, stream)
	return nil
}