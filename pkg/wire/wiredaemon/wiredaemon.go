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

package wiredaemon

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Config struct {
	//PodCache wire.Cache[pod.Pod]
	XDP xdp.XDP
	CRI cri.CRI
}

func New(cfg *Config) wire.Wire {
	l := ctrl.Log.WithName("wiredaemon")
	return &daemon{
		//wireCache: wire.NewCache[daemonwire.Wire](),
		cri: cfg.CRI,
		xdp: cfg.XDP,
		l:   l,
	}
}

type daemon struct {
	//wireCache wire.Cache[daemonwire.Wire]

	xdp xdp.XDP
	cri cri.CRI
	//logger
	l logr.Logger
}

func (r *daemon) Get(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	return &wirepb.WireResponse{}, status.Error(codes.Unimplemented, "not implemented")
}

func (r *daemon) UpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	w := NewWire(ctx, req, &WireConfig{XDP: r.xdp, CRI: r.cri})
	if !w.IsReady() {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: "endpoint not ready"}, nil
	}
	if w.Exists() {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_OK}, nil
	}
	if err := w.Deploy(); err != nil {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_OK}, nil
}

func (r *daemon) Delete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	w := NewWire(ctx, req, &WireConfig{XDP: r.xdp, CRI: r.cri})
	if !w.IsReady() {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: "endpoint not ready"}, nil
	}
	if err := w.Destroy(); err != nil {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_OK}, nil
}

func (r *daemon) AddWatch(fn wire.CallbackFn) {}
func (r *daemon) DeleteWatch()                {}
