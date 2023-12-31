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
	"log/slog"
	"net"
	"sync"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type GrpcServer struct {
	config Config
	wirepb.UnimplementedWireServer
	endpointpb.UnimplementedNodeEndpointServer

	sem *semaphore.Weighted

	// logger
	l *slog.Logger

	//wire Handlers
	wireGetHandler    WireGetHandler
	wireCreateHandler WireCreateHandler
	wireDeleteHandler WireDeleteHandler
	wireWatchHandler  WireWatchHandler

	//ep Handlers
	epGetHandler    EndpointGetHandler
	epCreateHandler EndpointCreateHandler
	epDeleteHandler EndpointDeleteHandler
	epWatchHandler  EndpointWatchHandler

	//health handlers
	checkHandler CheckHandler
	watchHandler WatchHandler
	// cached certificate
	cm *sync.Mutex
}

// Health Handlers
type CheckHandler func(context.Context, *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error)
type WatchHandler func(*healthpb.HealthCheckRequest, healthpb.Health_WatchServer) error

// Wire Handlers
type WireGetHandler func(context.Context, *wirepb.WireRequest) (*wirepb.WireResponse, error)
type WireCreateHandler func(context.Context, *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
type WireDeleteHandler func(context.Context, *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
type WireWatchHandler func(*wirepb.WatchRequest, wirepb.Wire_WireWatchServer) error

// Endpoint Handlers
type EndpointGetHandler func(context.Context, *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error)
type EndpointCreateHandler func(context.Context, *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error)
type EndpointDeleteHandler func(context.Context, *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error)
type EndpointWatchHandler func(*endpointpb.WatchRequest, endpointpb.NodeEndpoint_EndpointWatchServer) error

type Option func(*GrpcServer)

func New(ctx context.Context, c Config, opts ...Option) *GrpcServer {
	c.setDefaults()
	s := &GrpcServer{
		config: c,
		sem:    semaphore.NewWeighted(c.MaxRPC),
		cm:     &sync.Mutex{},
		l:      log.FromContext(ctx).WithGroup("grpcserver"),
	}

	for _, o := range opts {
		o(s)
	}

	return s
}

func (s *GrpcServer) Start(ctx context.Context) error {
	log := s.l.With("address", s.config.Address,
		"certDir", s.config.CertDir,
		"certName", s.config.CertName,
		"keyName", s.config.KeyName,
		"caName", s.config.CaName)
	log.Info("start...")
	
	l, err := net.Listen("tcp", s.config.Address)
	if err != nil {
		return errors.Wrap(err, "cannot listen")
	}
	opts, err := s.serverOpts(ctx)
	if err != nil {
		return err
	}
	// create a gRPC server object
	grpcServer := grpc.NewServer(opts...)

	wirepb.RegisterWireServer(grpcServer, s)
	log.Info("grpc server with wire service...")

	endpointpb.RegisterNodeEndpointServer(grpcServer, s)
	log.Info("grpc server with endpoint service...")

	healthpb.RegisterHealthServer(grpcServer, s)
	log.Info("grpc server with health service...")

	reflection.Register(grpcServer)
	log.Info("grpc server with reflection service...")

	log.Info("starting grpc server...")
	err = grpcServer.Serve(l)
	if err != nil {
		log.Info("gRPC serve failed", "error", err)
		return err
	}
	return nil
}

func WithCheckHandler(h CheckHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.checkHandler = h
	}
}

func WithWatchHandler(h WatchHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.watchHandler = h
	}
}

func WithWireGetHandler(h WireGetHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.wireGetHandler = h
	}
}

func WithWireCreateHandler(h WireCreateHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.wireCreateHandler = h
	}
}

func WithWireDeleteHandler(h WireDeleteHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.wireDeleteHandler = h
	}
}

func WithWireWatchHandler(h WireWatchHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.wireWatchHandler = h
	}
}

func WithEndpointGetHandler(h EndpointGetHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.epGetHandler = h
	}
}

func WithEndpointCreateHandler(h EndpointCreateHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.epCreateHandler = h
	}
}

func WithEndpointDeleteHandler(h EndpointDeleteHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.epDeleteHandler = h
	}
}

func WithEndpointWatchHandler(h EndpointWatchHandler) func(*GrpcServer) {
	return func(s *GrpcServer) {
		s.epWatchHandler = h
	}
}

func (s *GrpcServer) acquireSem(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return s.sem.Acquire(ctx, 1)
	}
}
