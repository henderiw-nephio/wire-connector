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

package grpcserver

import (
	"context"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/resolverpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *GrpcServer) Resolve(ctx context.Context, req *resolverpb.ResolveRequest) (*resolverpb.ResolveResponse, error) {
	if s.resolverHandler == nil {
		return &resolverpb.ResolveResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.resolverHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
