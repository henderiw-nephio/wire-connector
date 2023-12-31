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

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/types"
)

// Endoint provides the
type NodeEndpoint struct {
	//state.StateTransition
	dispatcher Dispatcher
	wirer.Object
	NodeEpReq  *NodeEpReq
	NodeEpResp *EpResp
	State      state.State
	l          *slog.Logger
}

type NodeEpReq struct {
	*endpointpb.EndpointRequest
}

func NewNodeEndpoint(ctx context.Context, d Dispatcher, req *NodeEpReq) *NodeEndpoint {
	return &NodeEndpoint{
		dispatcher: d,
		NodeEpReq:  req,
		NodeEpResp: newNodeEndpointResp(req),
		State:      &state.Deleted{},
		l:          log.FromContext(ctx).WithGroup("nodeep").With("nsn", req.GetNSN()),
	}
}

func newNodeEndpointResp(req *NodeEpReq) *EpResp {
	return &EpResp{
		EndpointResponse: &endpointpb.EndpointResponse{
			NodeKey:    req.GetNodeKey(),
			StatusCode: endpointpb.StatusCode_NOK,
			Reason:     "deleted",
		},
	}
}

// GetAdditionalState returns no additional state, since there is only 1 endpoint
func (r *NodeEndpoint) GetAdditionalState(eventCtx *state.EventCtx) []state.StateCtx {
	return []state.StateCtx{}
}

func (r *NodeEndpoint) Transition(ctx context.Context, newState state.State, eventCtx *state.EventCtx, generatedEvents ...state.WorkerAction) {
	log := log.FromContext(ctx).With("from/to", fmt.Sprintf("%s/%s", r.State, newState), "eventCtx", eventCtx)
	log.Info("transition", "wireResp", r.NodeEpResp, "generated events", generatedEvents)
	r.State = newState
	r.NodeEpResp.UpdateStatus(newState, eventCtx)

	for _, ge := range generatedEvents {
		log.Info("transition generated event", "ge", ge, "resolved", r.NodeEpReq.IsResolved())
		if r.NodeEpReq.IsResolved() {
			// should always resolve
			workerNsn := types.NamespacedName{
				Name:      r.NodeEpReq.HostNodeName,
			}

			if err := r.dispatcher.Write(workerNsn, state.WorkerEvent{Action: ge, Req: r.NodeEpReq, EventCtx: eventCtx}); err != nil {
				// should never happen, as it means the worker does not exist
				r.HandleEvent(ctx, state.FailedEvent, eventCtx)
				continue
			}
		}
	}
}

func (r *NodeEndpoint) HandleEvent(ctx context.Context, event state.Event, eventCtx *state.EventCtx) {
	r.State.HandleEvent(ctx, event, eventCtx, r)
}

func (r *NodeEndpoint) GetResponse() *endpointpb.EndpointResponse {
	return r.NodeEpResp.EndpointResponse
}

func (r *NodeEpReq) GetNSN() types.NamespacedName {
	return types.NamespacedName{
		Namespace: r.NodeKey.Topology,
		Name:      r.NodeKey.NodeName,
	}
}

func (r *NodeEpReq) IsResolved() bool {
	return r.ServiceEndpoint != ""
}

func (r *NodeEpReq) Unresolve() {
	r.HostIP = ""
	r.HostNodeName = ""
	r.ServiceEndpoint = ""
}

func (r *NodeEpReq) Resolve(res *resolve.Data) {
	if res != nil {
		//r.Endpoints[epIdx].NodeName = res.PodNodeName
		r.HostIP = res.HostIP
		r.HostNodeName = res.HostNodeName
		r.ServiceEndpoint = res.ServiceEndpoint
	} else {
		r.Unresolve()
	}
}

func (r *NodeEpReq) GetHostNodeName() string {
	return r.HostNodeName
}

func (r *NodeEpReq) CompareName(evaluate EvaluateName, name string) bool {
	switch evaluate {
	case EvaluateHostNodeName:
		return r.HostNodeName == name
	case EvaluateNodeName:
		return r.NodeKey.NodeName == name
	default:
		return false
	}
}

type EpResp struct {
	*endpointpb.EndpointResponse
}

func (r *EpResp) UpdateStatus(newState state.State, eventCtx *state.EventCtx) {

	// if the eventCtx massage is empty it means the transition was successfull
	// only when we transition to Created or Deleted we put the status to OK
	// when message is empty but the newState is not
	if eventCtx.Message == "" {
		if newState.String() == "Created" || newState.String() == "Deleted" {
			r.StatusCode = endpointpb.StatusCode_OK
			r.Reason = ""
		} else {
			// the state machine is still transition, we put the reason to the state
			r.StatusCode = endpointpb.StatusCode_NOK
			r.Reason = newState.String()

		}
	} else {
		r.StatusCode = endpointpb.StatusCode_NOK
		r.Reason = eventCtx.Message
	}
}
