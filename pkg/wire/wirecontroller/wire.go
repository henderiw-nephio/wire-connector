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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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
	l              logr.Logger
}

// NewWire is like create link/wire, once the object exists, this is no longer required
func NewWire(d Dispatcher, wreq *WireReq, vpnID uint32) *Wire {
	l := ctrl.Log.WithName("wire").WithValues("nsn", wreq.GetNSN())

	wreq.AddVPN(vpnID)
	return &Wire{
		dispatcher:     d,
		DesiredAction:  DesiredActionCreate,
		WireReq:        wreq,
		WireResp:       newWireResp(wreq),
		EndpointsState: []state.State{&state.Deleted{}, &state.Deleted{}},
		l:              l,
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

func (r *Wire) Transition(newState state.State, eventCtx *state.EventCtx, generatedEvents ...state.WorkerAction) {
	r.l.Info("transition", "from/to", fmt.Sprintf("%s/%s", r.EndpointsState[eventCtx.EpIdx], newState), "eventCtx", eventCtx, "wireResp", r.WireResp, "generated events", generatedEvents)
	r.EndpointsState[eventCtx.EpIdx] = newState
	r.WireResp.UpdateStatus(newState, eventCtx)
	r.l.Info("transition", "link status", fmt.Sprintf("%s/%s", r.WireResp.StatusCode.String(), r.WireResp.Reason),
		"ep0 status", fmt.Sprintf("%s/%s", r.WireResp.EndpointsStatus[0].StatusCode.String(), r.WireResp.EndpointsStatus[0].Reason),
		"ep1 status", fmt.Sprintf("%s/%s", r.WireResp.EndpointsStatus[1].StatusCode.String(), r.WireResp.EndpointsStatus[1].Reason),
	)

	for _, ge := range generatedEvents {
		r.l.Info("transition generated event", "from/to", fmt.Sprintf("%s/%s", r.EndpointsState[eventCtx.EpIdx], newState), "ge", ge)
		if r.WireReq.IsResolved(eventCtx.EpIdx) {
			// should always resolve
			workerNsn := types.NamespacedName{
				Namespace: "default",
				Name:      r.WireReq.GetHostNodeName(eventCtx.EpIdx),
			}

			if err := r.dispatcher.Write(workerNsn, state.WorkerEvent{Action: ge, Req: r.WireReq, EventCtx: eventCtx}); err != nil {
				// should never happen, as it means the worker does not exist
				r.HandleEvent(state.FailedEvent, eventCtx)
				continue
			}
		}
	}
}

func (r *Wire) HandleEvent(event state.Event, eventCtx *state.EventCtx) {
	r.EndpointsState[eventCtx.EpIdx].HandleEvent(event, eventCtx, r)
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

func (r *WireReq) HasLocalAction(epIdx int) bool {
	return r.Endpoints[epIdx].LocalAction
}

func (r *WireReq) IsResolved(epIdx int) bool {
	return r.Endpoints[epIdx].ServiceEndpoint != ""
}

func (r *WireReq) Unresolve(epIdx int) {
	r.Endpoints[epIdx].HostIP = ""
	r.Endpoints[epIdx].HostNodeName = ""
	r.Endpoints[epIdx].ServiceEndpoint = ""
}

func (r *WireReq) Resolve(resolvedData []*resolve.Data) {
	for epIdx, res := range resolvedData {
		if res != nil {
			r.Endpoints[epIdx].HostIP = res.HostIP
			r.Endpoints[epIdx].HostNodeName = res.HostNodeName
			r.Endpoints[epIdx].ServiceEndpoint = res.ServiceEndpoint
			r.Endpoints[epIdx].LocalAction = res.LocalAction
			r.Endpoints[epIdx].ClusterName = res.ClusterName
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

func (r *WireReq) GetHostNodeName(epIdx int) string {
	return r.Endpoints[epIdx].HostNodeName
}

func (r *WireReq) CompareName(epIdx int, hostNodeName bool, name string) bool {
	if hostNodeName {
		return r.Endpoints[epIdx].HostNodeName == name
	} else {
		return r.Endpoints[epIdx].NodeName == name
	}
}

func newWireResp(req *WireReq) *WireResp {
	return &WireResp{
		WireResponse: &wirepb.WireResponse{
			WireKey:         req.GetWireKey(),
			EndpointsStatus: []*wirepb.EndpointStatus{{Reason: ""}, {Reason: ""}},
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
