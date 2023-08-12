package wirecontroller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	ctrl "sigs.k8s.io/controller-runtime"
)

type WorkerAction string

const (
	WorkerActionCreate WorkerAction = "create"
	WorkerActionDelete WorkerAction = "delete"
)

type WorkerEvent struct {
	Action   WorkerAction
	WireReq  *WireReq
	EventCtx *EventCtx
}

type Worker interface {
	Start(ctx context.Context) error
	Stop()
	Write(e WorkerEvent)
}

func NewWorker(ctx context.Context, wireCache WireCache, cfg *client.Config) (Worker, error) {
	l := ctrl.Log.WithName("worker").WithValues("address", cfg.Address)
	
	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	return &worker{
		wireCache: wireCache,
		ch:        make(chan WorkerEvent, 10),
		client:    c,
		l:         l,
	}, nil
}

type worker struct {
	wireCache WireCache
	ch        chan WorkerEvent
	client    client.Client
	cancel    context.CancelFunc
	//logger
	l logr.Logger
}

func (r *worker) Start(ctx context.Context) error {
	r.l.Info("starting...")
	workerCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	if err := r.client.Start(workerCtx); err != nil {
		return err
	}
	r.l.Info("started...")
	for {
		select {
		case e, _ := <-r.ch:
			switch e.Action {
			case WorkerActionCreate:
				var event Event
				var eventCtx *EventCtx
				resp, err := r.client.Create(ctx, e.WireReq.WireRequest)
				if err != nil {
					//failed
					event = FailedEvent
					eventCtx = e.EventCtx
					eventCtx.Message = err.Error()
				}
				if resp.StatusCode == wirepb.StatusCode_NOK {
					// event failed
					event = FailedEvent
					eventCtx = e.EventCtx
					eventCtx.Message = resp.GetReason()
				}
				r.wireCache.HandleEvent(e.WireReq.GetNSN(), event, eventCtx)
			case WorkerActionDelete:
				var event Event
				var eventCtx *EventCtx
				resp, err := r.client.Delete(ctx, e.WireReq.WireRequest)
				if err != nil {
					//failed
					event = FailedEvent
					eventCtx = e.EventCtx
					eventCtx.Message = err.Error()
				}
				if resp.StatusCode == wirepb.StatusCode_NOK {
					// event failed
					event = FailedEvent
					eventCtx = e.EventCtx
					eventCtx.Message = resp.GetReason()
				}
				r.wireCache.HandleEvent(e.WireReq.GetNSN(), event, eventCtx)
			}
		case <-ctx.Done():
			// cancelled
		}
	}
}

func (r *worker) Stop() {
	r.l.Info("stopping...")
	r.cancel()
}

func (r *worker) Write(e WorkerEvent) {
	for {
		select {
		case r.ch <- e:
			// all good
		case <-time.After(5 * time.Second):
			// not able to write for 5 sec worker 1 exhausted
			// set state to exhausted
		}
	}
}
