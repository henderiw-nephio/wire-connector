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

package wirecontroller

/*
import (
	"context"
	"fmt"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/resolverpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
)

func (r *wc) Resolve(ctx context.Context, req *resolverpb.ResolveRequest) (*resolverpb.ResolveResponse, error) {
	r.l.Info("resolve...")
	if req.NodeKey == nil {
		return &resolverpb.ResolveResponse{}, status.Error(codes.InvalidArgument, "Invalid argument provided nil object")
	}
	pod, err := r.podCache.Get(types.NamespacedName{Namespace: req.NodeKey.Topology, Name: req.NodeKey.NodeName})
	if err != nil {
		return &resolverpb.ResolveResponse{
			StatusCode: resolverpb.StatusCode_NOK,
			Reason:     err.Error()}, nil
	}
	if !pod.IsReady {
		return &resolverpb.ResolveResponse{
			StatusCode: resolverpb.StatusCode_NOK,
			Reason:     fmt.Sprintf("pod not ready: %s", req.NodeKey.String())}, nil
	}
	daemonHostNodeNSN := types.NamespacedName{
		Namespace: "default",
		Name:      pod.HostNodeName}
	d, err := r.daemonCache.Get(daemonHostNodeNSN)
	if err != nil {
		return &resolverpb.ResolveResponse{
			StatusCode: resolverpb.StatusCode_NOK,
			Reason:     fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}, nil
	}
	if !d.IsReady {
		return &resolverpb.ResolveResponse{
			StatusCode: resolverpb.StatusCode_NOK,
			Reason:     fmt.Sprintf("wireDaemon not ready: %s", daemonHostNodeNSN.String())}, nil
	}
	if d.GRPCAddress == "" || d.GRPCPort == "" {
		return &resolverpb.ResolveResponse{
			StatusCode: resolverpb.StatusCode_NOK,
			Reason:     fmt.Sprintf("wireDaemon no grpc address/port: %s", daemonHostNodeNSN.String())}, nil

	}
	return &resolverpb.ResolveResponse{
		StatusCode: resolverpb.StatusCode_OK,
		HostIP:     d.HostIP,
	}, nil
}
*/