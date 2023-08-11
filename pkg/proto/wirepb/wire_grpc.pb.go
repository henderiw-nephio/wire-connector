//
//Copyright 2022 Nokia.
//
//Licensed under the Apache License, Version 2.0 (the "License");
//you may not use this file except in compliance with the License.
//You may obtain a copy of the License at
//
//http://www.apache.org/licenses/LICENSE-2.0
//
//Unless required by applicable law or agreed to in writing, software
//distributed under the License is distributed on an "AS IS" BASIS,
//WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//See the License for the specific language governing permissions and
//limitations under the License.

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v3.6.1
// source: pkg/proto/wirepb/wire.proto

//import "google/protobuf/any.proto";

package wirepb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	Wire_Get_FullMethodName       = "/wire.Wire/Get"
	Wire_Create_FullMethodName    = "/wire.Wire/Create"
	Wire_Delete_FullMethodName    = "/wire.Wire/Delete"
	Wire_WireWatch_FullMethodName = "/wire.Wire/WireWatch"
)

// WireClient is the client API for Wire service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type WireClient interface {
	Get(ctx context.Context, in *WireRequest, opts ...grpc.CallOption) (*WireResponse, error)
	Create(ctx context.Context, in *WireRequest, opts ...grpc.CallOption) (*EmptyResponse, error)
	Delete(ctx context.Context, in *WireRequest, opts ...grpc.CallOption) (*EmptyResponse, error)
	WireWatch(ctx context.Context, in *WatchRequest, opts ...grpc.CallOption) (Wire_WireWatchClient, error)
}

type wireClient struct {
	cc grpc.ClientConnInterface
}

func NewWireClient(cc grpc.ClientConnInterface) WireClient {
	return &wireClient{cc}
}

func (c *wireClient) Get(ctx context.Context, in *WireRequest, opts ...grpc.CallOption) (*WireResponse, error) {
	out := new(WireResponse)
	err := c.cc.Invoke(ctx, Wire_Get_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *wireClient) Create(ctx context.Context, in *WireRequest, opts ...grpc.CallOption) (*EmptyResponse, error) {
	out := new(EmptyResponse)
	err := c.cc.Invoke(ctx, Wire_Create_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *wireClient) Delete(ctx context.Context, in *WireRequest, opts ...grpc.CallOption) (*EmptyResponse, error) {
	out := new(EmptyResponse)
	err := c.cc.Invoke(ctx, Wire_Delete_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *wireClient) WireWatch(ctx context.Context, in *WatchRequest, opts ...grpc.CallOption) (Wire_WireWatchClient, error) {
	stream, err := c.cc.NewStream(ctx, &Wire_ServiceDesc.Streams[0], Wire_WireWatch_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &wireWireWatchClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Wire_WireWatchClient interface {
	Recv() (*WatchResponse, error)
	grpc.ClientStream
}

type wireWireWatchClient struct {
	grpc.ClientStream
}

func (x *wireWireWatchClient) Recv() (*WatchResponse, error) {
	m := new(WatchResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// WireServer is the server API for Wire service.
// All implementations must embed UnimplementedWireServer
// for forward compatibility
type WireServer interface {
	Get(context.Context, *WireRequest) (*WireResponse, error)
	Create(context.Context, *WireRequest) (*EmptyResponse, error)
	Delete(context.Context, *WireRequest) (*EmptyResponse, error)
	WireWatch(*WatchRequest, Wire_WireWatchServer) error
	mustEmbedUnimplementedWireServer()
}

// UnimplementedWireServer must be embedded to have forward compatible implementations.
type UnimplementedWireServer struct {
}

func (UnimplementedWireServer) Get(context.Context, *WireRequest) (*WireResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Get not implemented")
}
func (UnimplementedWireServer) Create(context.Context, *WireRequest) (*EmptyResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Create not implemented")
}
func (UnimplementedWireServer) Delete(context.Context, *WireRequest) (*EmptyResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Delete not implemented")
}
func (UnimplementedWireServer) WireWatch(*WatchRequest, Wire_WireWatchServer) error {
	return status.Errorf(codes.Unimplemented, "method WireWatch not implemented")
}
func (UnimplementedWireServer) mustEmbedUnimplementedWireServer() {}

// UnsafeWireServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to WireServer will
// result in compilation errors.
type UnsafeWireServer interface {
	mustEmbedUnimplementedWireServer()
}

func RegisterWireServer(s grpc.ServiceRegistrar, srv WireServer) {
	s.RegisterService(&Wire_ServiceDesc, srv)
}

func _Wire_Get_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WireRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WireServer).Get(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Wire_Get_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WireServer).Get(ctx, req.(*WireRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Wire_Create_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WireRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WireServer).Create(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Wire_Create_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WireServer).Create(ctx, req.(*WireRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Wire_Delete_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WireRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WireServer).Delete(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Wire_Delete_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WireServer).Delete(ctx, req.(*WireRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Wire_WireWatch_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(WatchRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(WireServer).WireWatch(m, &wireWireWatchServer{stream})
}

type Wire_WireWatchServer interface {
	Send(*WatchResponse) error
	grpc.ServerStream
}

type wireWireWatchServer struct {
	grpc.ServerStream
}

func (x *wireWireWatchServer) Send(m *WatchResponse) error {
	return x.ServerStream.SendMsg(m)
}

// Wire_ServiceDesc is the grpc.ServiceDesc for Wire service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Wire_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "wire.Wire",
	HandlerType: (*WireServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Get",
			Handler:    _Wire_Get_Handler,
		},
		{
			MethodName: "Create",
			Handler:    _Wire_Create_Handler,
		},
		{
			MethodName: "Delete",
			Handler:    _Wire_Delete_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "WireWatch",
			Handler:       _Wire_WireWatch_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "pkg/proto/wirepb/wire.proto",
}
