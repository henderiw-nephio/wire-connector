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

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *GrpcServer) WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	if s.wireGetHandler == nil {
		return &wirepb.WireResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.wireGetHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *GrpcServer) WireCreate(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	if s.wireCreateHandler == nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.wireCreateHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *GrpcServer) WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	if s.wireDeleteHandler == nil {
		return &wirepb.EmptyResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.wireDeleteHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *GrpcServer) WireWatch(req *wirepb.WatchRequest, stream wirepb.Wire_WireWatchServer) error {
	if s.wireWatchHandler == nil {
		return status.Error(codes.Unimplemented, "not implemented")
	}
	err := s.acquireSem(stream.Context())
	if err != nil {
		return err
	}
	defer s.sem.Release(1)

	if s.watchHandler != nil {
		return s.wireWatchHandler(req, stream)
	}
	return status.Error(codes.Unimplemented, "")
}
