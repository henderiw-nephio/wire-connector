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

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *GrpcServer) EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error) {
	if s.epGetHandler == nil {
		return &endpointpb.EndpointResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.epGetHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *GrpcServer) EndpointCreate(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	if s.epCreateHandler == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.epCreateHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *GrpcServer) EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	if s.epDeleteHandler == nil {
		return &endpointpb.EmptyResponse{}, status.Error(codes.Unimplemented, "not implemented")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	err := s.acquireSem(ctx)
	if err != nil {
		return nil, err
	}
	defer s.sem.Release(1)
	resp, err := s.epDeleteHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *GrpcServer) EndpointWatch(req *endpointpb.WatchRequest, stream endpointpb.NodeEndpoint_EndpointWatchServer) error {
	if s.epWatchHandler == nil {
		return status.Error(codes.Unimplemented, "not implemented")
	}
	err := s.acquireSem(stream.Context())
	if err != nil {
		return err
	}
	defer s.sem.Release(1)

	if s.watchHandler != nil {
		return s.epWatchHandler(req, stream)
	}
	return status.Error(codes.Unimplemented, "")
}
