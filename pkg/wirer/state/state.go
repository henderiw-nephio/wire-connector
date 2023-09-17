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

package state

import "context"

type WorkerEvent struct {
	Action   WorkerAction
	Req      any
	EventCtx *EventCtx
}

type EventCtx struct {
	EpIdx    int
	Message  string // used to indicate failures
	Hold     bool
	SameHost bool // inidcated this ep is on the same host as the peer ep
}

type Event string

const (
	CreateEvent           Event = "create"
	DeleteEvent           Event = "delete"
	ResolutionFailedEvent Event = "resolutionFailed"
	CreatedEvent          Event = "created"
	DeletedEvent          Event = "deleted"
	FailedEvent           Event = "failed"
)

type WorkerAction string

const (
	WorkerActionCreate WorkerAction = "create"
	WorkerActionDelete WorkerAction = "delete"
)

type StateCtx struct {
	State
	EventCtx
}

type StateTransition interface {
	Transition(ctx context.Context, newState State, eventCtx *EventCtx, generatedEvents ...WorkerAction)
	GetAdditionalState(eventCtx *EventCtx) []StateCtx
}

type State interface {
	String() string
	HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, w StateTransition)
}

type Deleting struct{}

func (s *Deleting) String() string { return "Deleting" }

func (s *Deleting) HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, o StateTransition) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			// do nothing
			o.Transition(ctx, &Created{}, eventCtx)
		} else {
			// action -> trigger create to the daemon
			o.Transition(ctx, &Creating{}, eventCtx, "create")
		}

	case DeleteEvent:
		// action -> do nothing as deleting is ongoing
	case ResolutionFailedEvent:
		o.Transition(ctx, &ResolutionFailed{}, eventCtx)
		// action -> based on hold trigger delete event on the other end
		if !eventCtx.Hold {
			sctxs := o.GetAdditionalState(eventCtx)
			for _, sctx := range sctxs {
				sctx.State.HandleEvent(ctx, DeleteEvent, &sctx.EventCtx, o)
			}
		}
	case DeletedEvent:
		o.Transition(ctx, &Deleted{}, eventCtx)
		// action -> do nothing since the delete was successfull, trigger genericEvent
	case FailedEvent:
		o.Transition(ctx, &Failed{}, eventCtx)
		// action -> do nothing, trigger genericEvent
	default:
		// these events should not happen
		// CreatedEvent
	}
}

type Deleted struct{}

func (s *Deleted) String() string { return "Deleted" }

func (s *Deleted) HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, o StateTransition) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			o.Transition(ctx, &Created{}, eventCtx)
		} else {
			o.Transition(ctx, &Creating{}, eventCtx, "create")
		}
		// action -> trigger create to the daemon
	case DeleteEvent:
		// action -> do nothing since we are already deleted
	case ResolutionFailedEvent:
		o.Transition(ctx, &ResolutionFailed{}, eventCtx)		
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}
}

type Failed struct{} // here we trigger an action on the other end

func (s *Failed) String() string { return "Failed" }

func (s *Failed) HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, o StateTransition) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			o.Transition(ctx, &Created{}, eventCtx)
		} else {
			o.Transition(ctx, &Creating{}, eventCtx, "create")
		}
		// action -> trigger create to the daemon
	case DeleteEvent:
		if eventCtx.SameHost {
			o.Transition(ctx, &Deleted{}, eventCtx)
		} else {
			o.Transition(ctx, &Deleting{}, eventCtx, "delete")
		}
		// action -> trigger create to the daemon
	case ResolutionFailedEvent:
		o.Transition(ctx, &ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			sctxs := o.GetAdditionalState(eventCtx)
			for _, sctx := range sctxs {
				sctx.State.HandleEvent(ctx, DeleteEvent, &sctx.EventCtx, o)
			}
		}
	default:
		// these events should not happen
		// CreatedEvent, DeletedEvent, FailedEvent
	}

}

type Creating struct{}

func (s *Creating) String() string { return "Creating" }

func (s *Creating) HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, o StateTransition) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			o.Transition(ctx, &Created{}, eventCtx)
		}
		// action -> do nothing as creating is ongoing
	case DeleteEvent:
		if eventCtx.SameHost {
			o.Transition(ctx, &Deleted{}, eventCtx)
		} else {
			o.Transition(ctx, &Deleting{}, eventCtx, "delete")
		}
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		o.Transition(ctx, &ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			sctxs := o.GetAdditionalState(eventCtx)
			for _, sctx := range sctxs {
				sctx.State.HandleEvent(ctx, DeleteEvent, &sctx.EventCtx, o)
			}
		}
	case CreatedEvent:
		o.Transition(ctx, &Created{}, eventCtx)
		// done
	case FailedEvent:
		o.Transition(ctx, &Failed{}, eventCtx)
		// done
	default:
		// these events should not happen: DeletedEvent
	}
}

type Created struct{}

func (s *Created) String() string { return "Created" }

func (s *Created) HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, o StateTransition) {
	switch event {
	case CreateEvent:
		// do nothing
	case DeleteEvent:
		if eventCtx.SameHost {
			o.Transition(ctx, &Deleted{}, eventCtx)
		} else {
			o.Transition(ctx, &Deleting{}, eventCtx, "delete")
		}
		// action -> trigger delete to the daemon
	case ResolutionFailedEvent:
		o.Transition(ctx, &ResolutionFailed{}, eventCtx)
		if !eventCtx.Hold {
			sctxs := o.GetAdditionalState(eventCtx)
			for _, sctx := range sctxs {
				sctx.State.HandleEvent(ctx, DeleteEvent, &sctx.EventCtx, o)
			}
			/*
				otherEpIdx := (eventCtx.EpIdx + 1) % 2
				o.EndpointsState[otherEpIdx].HandleEvent(DeleteEvent, &EventCtx{
					EpIdx: otherEpIdx,
				}, o)
			*/
		}
	default:
		// these events should not happen: CreatedEvent, DeletedEvent, FailedEvent
	}
}

type ResolutionFailed struct{} // here we can either hold or trigger an action on the other end

func (s *ResolutionFailed) String() string { return "ResolutionFailed" }

func (s *ResolutionFailed) HandleEvent(ctx context.Context, event Event, eventCtx *EventCtx, w StateTransition) {
	switch event {
	case CreateEvent:
		if eventCtx.SameHost {
			w.Transition(ctx, &Created{}, eventCtx)
		} else {
			w.Transition(ctx, &Creating{}, eventCtx, "create")
		}
	case DeleteEvent:
		w.Transition(ctx, &Deleting{}, eventCtx, "delete")
	case ResolutionFailedEvent:
		// do nothing since we are already in resolution failed
	default:
		// these events should not happen: CreatedEvent, DeletedEvent, FailedEvent
	}
}
