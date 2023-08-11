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
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"k8s.io/apimachinery/pkg/types"
)

type DesiredAction string

const (
	DesiredActionCreate DesiredAction = "create"
	DesiredActionDelete DesiredAction = "delete"
)

type Wire struct {
	DesiredAction DesiredAction
	WireReq       *wirepb.WireRequest
	VpnID         uint32
	Endpoints     []*Endpoint
}

type Endpoint struct {
	State   State
	Message string
	*ResolvedData
}

type ResolvedData struct {
	PodNodeName     string // name of the pod
	ServiceEndpoint string // ip address or dns name + port
	HostIP          string // ip address
	HostNodeName    string // name of the host node
}

func (r *ResolvedData) CompareName(hostNodeName bool, name string) bool {
	if hostNodeName {
		return r.HostNodeName == name
	} else {
		return r.PodNodeName == name
	}
}

// NewLink is like create link, once the object exists, this is no longer required
func NewLink(wire *wirepb.WireRequest, vpnID uint32) *Wire {
	l := *wire
	return &Wire{
		DesiredAction: DesiredActionCreate,
		WireReq:       &l,
		VpnID:         vpnID,
		Endpoints: []*Endpoint{
			{
				State:        &Deleted{},
				Message:      "",
				ResolvedData: nil,
			},
			{
				State:        &Deleted{},
				Message:      "",
				ResolvedData: nil,
			},
		},
	}
}

func (r *Wire) Transition(newState State, eventCtx *EventCtx) {
	r.Endpoints[eventCtx.EpIdx].State = newState
	r.Endpoints[eventCtx.EpIdx].Message = eventCtx.Message
	r.Endpoints[eventCtx.EpIdx].ResolvedData = eventCtx.ResolvedData
}

func (r *Wire) HandleEvent(event Event, eventCtx *EventCtx) {
	eventCtx.Wire = r
	r.Endpoints[eventCtx.EpIdx].State.HandleEvent(event, eventCtx)
}

type EventCtx struct {
	WireNSN      types.NamespacedName
	Wire         *Wire
	EpIdx        int
	ResolvedData *ResolvedData
	Message      string
	Hold         bool
	SameHost     bool // inidcated this ep is on the same host as the peer ep
}

type Event string

const (
	CreateEvent           Event = "create"
	DeleteEvent           Event = "delete"
	ResolutionFailedEvent Event = "resolutionFailed"
	CreatedEvent          Event = "created"
	DeletedEvent          Event = "deleted"
	FailedEvent           Event = "failed"
	//CreateFailedEvent Event = "createFailed"
	//DeleteFailedEvent Event = "deleteFailed"
)

type State interface {
	HandleEvent(event Event, eventCtx *EventCtx)
}

type Deleting struct{}

func (s *Deleting) HandleEvent(event Event, eventCtx *EventCtx) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Created{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Creating{}, eventCtx)

		}
		// action -> trigger create to the daemon
	case DeleteEvent:
		// action -> do nothing as deleting is ongoing
	case ResolutionFailedEvent:
		eventCtx.Wire.Transition(&ResolutionFailed{}, eventCtx)
		// action -> based on hold trigger delete event on the other end
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			eventCtx.Wire.Endpoints[otherEpIdx].State.HandleEvent(DeleteEvent, &EventCtx{
				Wire:         eventCtx.Wire,
				EpIdx:        otherEpIdx,
				ResolvedData: eventCtx.Wire.Endpoints[otherEpIdx].ResolvedData,
			})
		}
	case DeletedEvent:
		eventCtx.Wire.Transition(&Deleted{}, eventCtx)
		// action -> do nothing since the delete was successfull, trigger genericEvent
	case FailedEvent:
		eventCtx.Wire.Transition(&Failed{}, eventCtx)
		// action -> do nothing, trigger genericEvent
	default:
		// these events should not happen
		// CreatedEvent
	}
}

type Deleted struct{}

func (s *Deleted) HandleEvent(event Event, eventCtx *EventCtx) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Created{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Creating{}, eventCtx)
		}
		// action -> trigger create to the daemon
	case DeleteEvent:
		// action -> do nothing since we are already deleted
	case ResolutionFailedEvent:
		// action -> do nothing since we are already deleted
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}
}

type Failed struct{} // here we trigger an action on the other end

func (s *Failed) HandleEvent(event Event, eventCtx *EventCtx) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Created{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Creating{}, eventCtx)
		}
		// action -> trigger create to the daemon
	case DeleteEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Deleted{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Deleting{}, eventCtx)
		}
		// action -> trigger create to the daemon
	case ResolutionFailedEvent:
		eventCtx.Wire.Transition(&ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			eventCtx.Wire.Endpoints[otherEpIdx].State.HandleEvent(DeleteEvent, &EventCtx{
				Wire:         eventCtx.Wire,
				EpIdx:        otherEpIdx,
				ResolvedData: eventCtx.Wire.Endpoints[otherEpIdx].ResolvedData,
			})
		}
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}

}

type Creating struct{}

func (s *Creating) HandleEvent(event Event, eventCtx *EventCtx) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Created{}, eventCtx)
		}
		// action -> do nothing as creating is ongoing
	case DeleteEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Deleted{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Deleting{}, eventCtx)
		}
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		eventCtx.Wire.Transition(&ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			eventCtx.Wire.Endpoints[otherEpIdx].State.HandleEvent(DeleteEvent, &EventCtx{
				Wire:         eventCtx.Wire,
				EpIdx:        otherEpIdx,
				ResolvedData: eventCtx.Wire.Endpoints[otherEpIdx].ResolvedData,
			})
		}
	case CreatedEvent:
		eventCtx.Wire.Transition(&Created{}, eventCtx)
	case FailedEvent:
		eventCtx.Wire.Transition(&Failed{}, eventCtx)
	default:
		// these events should not happen
		// DeletedEvent
	}
}

type Created struct{}

func (s *Created) HandleEvent(event Event, eventCtx *EventCtx) {
	switch event {
	case CreateEvent:
		// do nothing
	case DeleteEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Deleted{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Deleting{}, eventCtx)
		}
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		eventCtx.Wire.Transition(&ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			eventCtx.Wire.Endpoints[otherEpIdx].State.HandleEvent(DeleteEvent, &EventCtx{
				Wire:         eventCtx.Wire,
				EpIdx:        otherEpIdx,
				ResolvedData: eventCtx.Wire.Endpoints[otherEpIdx].ResolvedData,
			})
		}
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}
}

type ResolutionFailed struct{} // here we can either hold or trigger an action on the other end

func (s *ResolutionFailed) HandleEvent(event Event, eventCtx *EventCtx) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			eventCtx.Wire.Transition(&Created{}, eventCtx)
		} else {
			eventCtx.Wire.Transition(&Creating{}, eventCtx)
		}
		// action -> trigger delete to the daemon
	case DeleteEvent:
		eventCtx.Wire.Transition(&Deleting{}, eventCtx)
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		// do nothing
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}
}

type Queued struct{}