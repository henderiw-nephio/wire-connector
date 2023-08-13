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
type Endpoint struct {
	State   State
	//Message string
	//*ResolvedData
}
*/

type ResolvedData struct {
	Success         bool   // inidctaes if the resolution was successfull or not
	Message         string // indicates why the resolution failed
	PodNodeName     string // name of the pod
	ServiceEndpoint string // ip address or dns name + port
	HostIP          string // ip address
	HostNodeName    string // name of the host node
}

type EventCtx struct {
	//WireNSN types.NamespacedName
	//Wire    *Wire
	EpIdx int
	//ResolvedData *ResolvedData
	Message  string // used to indicate failures
	Hold     bool
	SameHost bool // inidcated this ep is on the same host as the peer ep
	//Reason   string // used for failed events
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
	String() string
	HandleEvent(event Event, eventCtx *EventCtx, w *Wire)
}

type Deleting struct{}

func (s *Deleting) String() string {return "Deleting"}

func (s *Deleting) HandleEvent(event Event, eventCtx *EventCtx, w *Wire) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			// do nothing
			w.Transition(&Created{}, eventCtx)
		} else {
			// action -> trigger create to the daemon
			w.Transition(&Creating{}, eventCtx, "create")
		}

	case DeleteEvent:
		// action -> do nothing as deleting is ongoing
	case ResolutionFailedEvent:
		w.Transition(&ResolutionFailed{}, eventCtx)
		// action -> based on hold trigger delete event on the other end
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			w.EndpointsState[otherEpIdx].HandleEvent(DeleteEvent, &EventCtx{
				EpIdx: otherEpIdx,
			}, w)
		}
	case DeletedEvent:
		w.Transition(&Deleted{}, eventCtx)
		// action -> do nothing since the delete was successfull, trigger genericEvent
	case FailedEvent:
		w.Transition(&Failed{}, eventCtx)
		// action -> do nothing, trigger genericEvent
	default:
		// these events should not happen
		// CreatedEvent
	}
}

type Deleted struct{}

func (s *Deleted) String() string {return "Deleted"}

func (s *Deleted) HandleEvent(event Event, eventCtx *EventCtx, w *Wire) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			w.Transition(&Created{}, eventCtx)
		} else {
			w.Transition(&Creating{}, eventCtx, "create")
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

func (s *Failed) String() string {return "Failed"}

func (s *Failed) HandleEvent(event Event, eventCtx *EventCtx, w *Wire) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			w.Transition(&Created{}, eventCtx)
		} else {
			w.Transition(&Creating{}, eventCtx, "create")
		}
		// action -> trigger create to the daemon
	case DeleteEvent:
		if eventCtx.SameHost {
			w.Transition(&Deleted{}, eventCtx)
		} else {
			w.Transition(&Deleting{}, eventCtx, "delete")
		}
		// action -> trigger create to the daemon
	case ResolutionFailedEvent:
		w.Transition(&ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			w.EndpointsState[otherEpIdx].HandleEvent(DeleteEvent, &EventCtx{
				EpIdx: otherEpIdx,
			}, w)
		}
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}

}

type Creating struct{}

func (s *Creating) String() string {return "Creating"}

func (s *Creating) HandleEvent(event Event, eventCtx *EventCtx, w *Wire) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			w.Transition(&Created{}, eventCtx)
		}
		// action -> do nothing as creating is ongoing
	case DeleteEvent:
		if eventCtx.SameHost {
			w.Transition(&Deleted{}, eventCtx)
		} else {
			w.Transition(&Deleting{}, eventCtx, "delete")
		}
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		w.Transition(&ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			w.EndpointsState[otherEpIdx].HandleEvent(DeleteEvent, &EventCtx{
				EpIdx: otherEpIdx,
			}, w)
		}
	case CreatedEvent:
		w.Transition(&Created{}, eventCtx)
		// done
	case FailedEvent:
		w.Transition(&Failed{}, eventCtx)
		// done
	default:
		// these events should not happen
		// DeletedEvent
	}
}

type Created struct{}

func (s *Created) String() string {return "Created"}

func (s *Created) HandleEvent(event Event, eventCtx *EventCtx, w *Wire) {
	switch event {
	case CreateEvent:
		// do nothing
	case DeleteEvent:
		if eventCtx.SameHost {
			w.Transition(&Deleted{}, eventCtx)
		} else {
			w.Transition(&Deleting{}, eventCtx, "delete")
		}
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		w.Transition(&ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			otherEpIdx := (eventCtx.EpIdx + 1) % 2
			w.EndpointsState[otherEpIdx].HandleEvent(DeleteEvent, &EventCtx{
				EpIdx: otherEpIdx,
			}, w)
		}
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}
}

type ResolutionFailed struct{} // here we can either hold or trigger an action on the other end

func (s *ResolutionFailed) String() string {return "ResolutionFailed"}

func (s *ResolutionFailed) HandleEvent(event Event, eventCtx *EventCtx, w *Wire) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			w.Transition(&Created{}, eventCtx)
		} else {
			w.Transition(&Creating{}, eventCtx, "delete")
		}
		// action -> trigger delete to the daemon
	case DeleteEvent:
		w.Transition(&Deleting{}, eventCtx, "delete")
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		// do nothing
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}
}
