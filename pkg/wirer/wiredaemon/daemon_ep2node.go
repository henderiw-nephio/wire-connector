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
	"fmt"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw/logger/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
)

func (r *daemon) EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error) {
	log := log.FromContext(ctx)
	log.Info("ep2node get...")
	return &endpointpb.EndpointResponse{}, status.Error(codes.Unimplemented, "not implemented")
}

func (r *daemon) EndpointUpSert(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("ep2node upsert...")

	// this validates the container exists and returns a valid nsPath if it exists
	// if the nsPath does not exist the container is not ready and we return a NOK response
	nsPath := ""
	if !req.ServerType {
		var err error
		nsPath, err = r.getContainerNsPath(ctx, types.NamespacedName{Namespace: req.NodeKey.Topology, Name: req.NodeKey.NodeName})
		if err != nil {
			return &endpointpb.EmptyResponse{StatusCode: endpointpb.StatusCode_NOK, Reason: fmt.Sprintf("container not ready, err: %s", err.Error())}, nil
		}
	}

	// container is ready
	w := NewWireEp2Node(ctx, nsPath, &WireEp2NodeConfig{CRI: r.cri})
	if err := w.Deploy(ctx, req); err != nil {
		return &endpointpb.EmptyResponse{StatusCode: endpointpb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return &endpointpb.EmptyResponse{StatusCode: endpointpb.StatusCode_OK}, nil
}

func (r *daemon) EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error) {
	log := log.FromContext(ctx)
	log.Info("ep2node delete...")

	// this validates the container exists and returns a valid nsPath if it exists
	nsPath := ""
	if !req.ServerType {
		var err error
		nsPath, err = r.getContainerNsPath(ctx, types.NamespacedName{Namespace: req.NodeKey.Topology, Name: req.NodeKey.NodeName})
		if err != nil {
			return &endpointpb.EmptyResponse{StatusCode: endpointpb.StatusCode_NOK, Reason: fmt.Sprintf("container not ready, err: %s", err.Error())}, nil
		}
	}

	// after this check we determine we are ready to wire
	w := NewWireEp2Node(ctx, nsPath, &WireEp2NodeConfig{CRI: r.cri})
	if err := w.Destroy(ctx, req); err != nil {
		return &endpointpb.EmptyResponse{StatusCode: endpointpb.StatusCode_NOK, Reason: err.Error()}, nil
	}
	return &endpointpb.EmptyResponse{StatusCode: endpointpb.StatusCode_OK}, nil
}

func (r *daemon) AddEndpointWatch(fn wirer.CallbackFn) {}
func (r *daemon) DeleteEndpointWatch()                {}
