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
	go func() {
		for {
			select {
			case e, ok := <-r.ch:
				if !ok {
					r.l.Info("event", "nok", e)
					continue
				}
				r.l.Info("event", "ok", e)
				switch e.Action {
				case WorkerActionCreate:
					r.l.Info("create event", "nsn", e.WireReq.GetNSN(), "data", e.WireReq.WireRequest)
					var eventCtx *EventCtx
					resp, err := r.client.Create(ctx, e.WireReq.WireRequest)
					r.l.Info("create event", "nsn", e.WireReq.GetNSN(), "resp", resp, "err", err)
					if err != nil {
						//failed
						eventCtx = e.EventCtx
						eventCtx.Message = err.Error()
						r.wireCache.HandleEvent(e.WireReq.GetNSN(), FailedEvent, eventCtx)
						continue
					}
					if resp.StatusCode == wirepb.StatusCode_NOK {
						// event failed
						eventCtx = e.EventCtx
						eventCtx.Message = resp.GetReason()
						r.wireCache.HandleEvent(e.WireReq.GetNSN(), FailedEvent, eventCtx)
						continue
					}
					r.wireCache.HandleEvent(e.WireReq.GetNSN(), CreatedEvent, eventCtx)
				case WorkerActionDelete:
					r.l.Info("delete event", "event", e.WireReq)
					var eventCtx *EventCtx
					resp, err := r.client.Delete(ctx, e.WireReq.WireRequest)
					if err != nil {
						//failed
						eventCtx = e.EventCtx
						eventCtx.Message = err.Error()
						r.wireCache.HandleEvent(e.WireReq.GetNSN(), FailedEvent, eventCtx)
						continue
					}
					if resp.StatusCode == wirepb.StatusCode_NOK {
						// event failed
						eventCtx = e.EventCtx
						eventCtx.Message = resp.GetReason()
						r.wireCache.HandleEvent(e.WireReq.GetNSN(), FailedEvent, eventCtx)
						continue
					}
					r.wireCache.HandleEvent(e.WireReq.GetNSN(), DeletedEvent, eventCtx)
				}
			case <-ctx.Done():
				// cancelled
			}
		}
	}()
	r.l.Info("started...")
	return nil
}

func (r *worker) Stop() {
	r.l.Info("stopping...")
	r.cancel()
}

func (r *worker) Write(e WorkerEvent) {
	r.ch <- e
	/*
	for {
		select {
		case r.ch <- e:
			// all good
		case <-time.After(5 * time.Second):
			// not able to write for 5 sec worker 1 exhausted
			// set state to exhausted
		}
	}
	*/
}
