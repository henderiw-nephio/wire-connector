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

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw/logger/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *daemon) WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	log := log.FromContext(ctx)
	log.Info("node2node get...")
	return &wirepb.WireResponse{}, status.Error(codes.Unimplemented, "not implemented")
}

func (r *daemon) WireUpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("node2node upsert...")

	w := NewWireNode2Node(ctx, req, &WireNode2NodeConfig{XDP: r.xdp, CRI: r.cri})
	if err := w.Deploy(); err != nil {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_OK}, nil
}

func (r *daemon) WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("node2node delete...")

	w := NewWireNode2Node(ctx, req, &WireNode2NodeConfig{XDP: r.xdp, CRI: r.cri})
	if err := w.Destroy(); err != nil {
		return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return &wirepb.EmptyResponse{StatusCode: wirepb.StatusCode_OK}, nil
}

func (r *daemon) AddWireWatch(fn wirer.CallbackFn) {}
func (r *daemon) DeleteWireWatch()                {}
