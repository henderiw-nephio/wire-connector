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
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/types"
)

type DesiredAction string

const (
	DesiredActionCreate DesiredAction = "create"
	DesiredActionDelete DesiredAction = "delete"
)

type Wire struct {
	dispatcher     Dispatcher
	DesiredAction  DesiredAction
	WireReq        *WireReq
	WireResp       *WireResp
	EndpointsState []state.State
	l              *slog.Logger
}

// NewWire is like create link/wire, once the object exists, this is no longer required
func NewWire(ctx context.Context, d Dispatcher, wreq *WireReq, vpnID uint32) *Wire {
	wreq.AddVPN(vpnID)
	return &Wire{
		dispatcher:     d,
		DesiredAction:  DesiredActionCreate,
		WireReq:        wreq,
		WireResp:       newWireResp(wreq),
		EndpointsState: []state.State{&state.Deleted{}, &state.Deleted{}},
		l:              log.FromContext(ctx).WithGroup("wire").With("nsn", wreq.GetNSN()),
	}
}

func (r *Wire) GetWireResponse() *wirepb.WireResponse {
	return r.WireResp.WireResponse
}

func (r *Wire) SetDesiredAction(a DesiredAction) {
	r.DesiredAction = a
}

// GetAdditionalState returns the other endpoint state on the wire
// + its associated event context to handle further events on the adjacent endpoint
func (r *Wire) GetAdditionalState(eventCtx *state.EventCtx) []state.StateCtx {
	otherEpIdx := (eventCtx.EpIdx + 1) % 2

	return []state.StateCtx{
		{
			State:    r.EndpointsState[otherEpIdx],
			EventCtx: state.EventCtx{EpIdx: otherEpIdx},
		},
	}
}

func (r *Wire) Transition(ctx context.Context, newState state.State, eventCtx *state.EventCtx, generatedEvents ...state.WorkerAction) {
	log := log.FromContext(ctx).With("from/to", fmt.Sprintf("%s/%s", r.EndpointsState[eventCtx.EpIdx], newState), "eventCtx", eventCtx, "wireResp", r.WireResp, "generated events", generatedEvents)
	log.Info("transition")
	r.EndpointsState[eventCtx.EpIdx] = newState
	r.WireResp.UpdateStatus(newState, eventCtx)
	log.Info("transition", "link status", fmt.Sprintf("%s/%s", r.WireResp.StatusCode.String(), r.WireResp.Reason),
		"ep0 status", fmt.Sprintf("%s/%s", r.WireResp.EndpointsStatus[0].StatusCode.String(), r.WireResp.EndpointsStatus[0].Reason),
		"ep1 status", fmt.Sprintf("%s/%s", r.WireResp.EndpointsStatus[1].StatusCode.String(), r.WireResp.EndpointsStatus[1].Reason),
	)

	for _, ge := range generatedEvents {
		log = log.With("from/to", fmt.Sprintf("%s/%s", r.EndpointsState[eventCtx.EpIdx], newState), "ge", ge)
		log.Info("transition generated event")
		if r.WireReq.IsResolved(eventCtx.EpIdx) {
			// should always resolve
			workerNsn := types.NamespacedName{
				Name: r.WireReq.GetHostNodeName(eventCtx.EpIdx),
			}
			if os.Getenv("WIRER_INTERCLUSTER") == "true" {
				workerNsn = types.NamespacedName{
					Name: r.WireReq.GetClusterName(eventCtx.EpIdx),
				}
			}
			log.Info("transition generated event", "workerNsn", workerNsn)

			if err := r.dispatcher.Write(workerNsn, state.WorkerEvent{Action: ge, Req: r.WireReq, EventCtx: eventCtx}); err != nil {
				// should never happen, as it means the worker does not exist
				newEventCtx := *eventCtx
				newEventCtx.Message = err.Error()
				r.HandleEvent(ctx, state.FailedEvent, &newEventCtx)
				continue
			}
		}
	}
}

func (r *Wire) HandleEvent(ctx context.Context, event state.Event, eventCtx *state.EventCtx) {
	r.EndpointsState[eventCtx.EpIdx].HandleEvent(ctx, event, eventCtx, r)
}

type WireReq struct {
	*wirepb.WireRequest
}

func (r *WireReq) GetNSN() types.NamespacedName {
	return types.NamespacedName{
		Namespace: r.WireKey.Namespace,
		Name:      r.WireKey.Name,
	}
}

func (r *WireReq) Act(epIdx int) bool {
	return !r.Endpoints[epIdx].NoAction
}

func (r *WireReq) IsResolved(epIdx int) bool {
	return r.Endpoints[epIdx].ServiceEndpoint != ""
}

func (r *WireReq) Unresolve(epIdx int) {
	r.Endpoints[epIdx].HostIP = ""
	r.Endpoints[epIdx].HostNodeName = ""
	r.Endpoints[epIdx].ServiceEndpoint = ""
	r.Endpoints[epIdx].NoAction = false
}

func (r *WireReq) Resolve(resolvedData []*resolve.Data) {
	for epIdx, res := range resolvedData {
		if res != nil {
			r.Endpoints[epIdx].HostIP = res.HostIP
			r.Endpoints[epIdx].HostNodeName = res.HostNodeName
			r.Endpoints[epIdx].ServiceEndpoint = res.ServiceEndpoint
			r.Endpoints[epIdx].NoAction = res.NoAction
		} else {
			r.Unresolve(epIdx)
		}
	}
}

func (r *WireReq) AddVPN(vpnID uint32) {
	r.VpnID = vpnID
}

func (r *WireReq) GetEndpointNodeNSN(epIdx int) types.NamespacedName {
	return types.NamespacedName{
		Namespace: r.Endpoints[epIdx].Topology,
		Name:      r.Endpoints[epIdx].NodeName,
	}
}

func (r *WireReq) GetEndpoint(epIdx int) *wirepb.Endpoint {
	return r.Endpoints[epIdx]
}

func (r *WireReq) GetHostNodeName(epIdx int) string {
	return r.Endpoints[epIdx].HostNodeName
}

func (r *WireReq) GetClusterName(epIdx int) string {
	return r.Endpoints[epIdx].ClusterName
}

// TODO add the fn for the service lookup
func (r *WireReq) CompareName(epIdx int, evaluate EvaluateName, name string) bool {
	switch evaluate {
	case EvaluateClusterName:
		return r.Endpoints[epIdx].ClusterName == name
	case EvaluateHostNodeName:
		return r.Endpoints[epIdx].HostNodeName == name
	case EvaluateNodeName:
		return r.Endpoints[epIdx].NodeName == name
	default:
		return false
	}
}

func newWireResp(req *WireReq) *WireResp {
	// initialize the endpoint status -> for endpoints w/o an action we resolve to success
	epStatus := make([]*wirepb.EndpointStatus, 0, len(req.Endpoints))
	for _, epReq := range req.Endpoints {
		if epReq.NoAction {
			epStatus = append(epStatus, &wirepb.EndpointStatus{StatusCode: wirepb.StatusCode_OK, Reason: ""})
		} else {
			epStatus = append(epStatus, &wirepb.EndpointStatus{StatusCode: wirepb.StatusCode_NOK, Reason: "to be created"})
		}
	}

	return &WireResp{
		WireResponse: &wirepb.WireResponse{
			WireKey:         req.GetWireKey(),
			StatusCode:      wirepb.StatusCode_NOK,
			Reason:          "to be created",
			EndpointsStatus: epStatus,
		},
	}
}

type WireResp struct {
	*wirepb.WireResponse
}

func (r *WireResp) UpdateStatus(newState state.State, eventCtx *state.EventCtx) {
	if r.EndpointsStatus == nil || len(r.EndpointsStatus) == 0 {
		r.EndpointsStatus = []*wirepb.EndpointStatus{{Reason: ""}, {Reason: ""}}
	}
	// if the eventCtx massage is empty it means the transition was successfull
	// only when we transition to Created or Deleted we put the status to OK
	// when message is empty but the newState is not
	if eventCtx.Message == "" {
		if newState.String() == "Created" || newState.String() == "Deleted" {
			r.EndpointsStatus[eventCtx.EpIdx] = &wirepb.EndpointStatus{
				Reason:     "",
				StatusCode: wirepb.StatusCode_OK,
			}
		} else {
			r.EndpointsStatus[eventCtx.EpIdx] = &wirepb.EndpointStatus{
				Reason:     newState.String(),
				StatusCode: wirepb.StatusCode_NOK,
			}
		}
	} else {
		r.EndpointsStatus[eventCtx.EpIdx] = &wirepb.EndpointStatus{
			Reason:     eventCtx.Message,
			StatusCode: wirepb.StatusCode_NOK,
		}
	}

	// update the overall status
	ok := true
	for _, eps := range r.EndpointsStatus {
		if eps.StatusCode == wirepb.StatusCode_NOK {
			ok = false
		}
	}
	if ok {
		r.StatusCode = wirepb.StatusCode_OK
		r.Reason = ""
	} else {
		r.StatusCode = wirepb.StatusCode_NOK
	}
}
