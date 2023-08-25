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

package wireclustercontroller

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/client"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TODO WORKER IS TO BE ADOPTED TO THE SPECIFIC IMPLEMENTATION

type Worker interface {
	Start(ctx context.Context) error
	Stop()
	Write(e state.WorkerEvent)
}

func NewWorker(ctx context.Context, wireCache WireCache, cfg *client.Config) (Worker, error) {
	l := ctrl.Log.WithName("worker").WithValues("address", cfg.Address)

	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	return &worker{
		wireCache: wireCache,
		ch:        make(chan state.WorkerEvent, 10),
		client:    c,
		l:         l,
	}, nil
}

type worker struct {
	wireCache WireCache
	//epCache   NodeEpCache
	ch     chan state.WorkerEvent
	client client.Client
	cancel context.CancelFunc
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
				r.l.Info("event", "ok", ok, "e", e)
				if !ok {
					continue
				}
				switch e.Action {
				case state.WorkerActionCreate:
					switch req := e.Req.(type) {
					case *WireReq:
						nsn := req.GetNSN()
						r.l.Info("create wire event", "nsn", nsn, "data", req.WireRequest)
						resp, err := r.client.WireCreate(ctx, req.WireRequest)
						r.l.Info("create wire event", "nsn", nsn, "resp", resp, "err", err)
						if err != nil {
							eventCtx := e.EventCtx
							eventCtx.Message = err.Error()
							r.wireCache.HandleEvent(nsn, state.FailedEvent, eventCtx)
							continue
						}
						if resp.StatusCode == wirepb.StatusCode_NOK {
							eventCtx := e.EventCtx
							eventCtx.Message = resp.GetReason()
							r.wireCache.HandleEvent(nsn, state.FailedEvent, eventCtx)
						}
						// success
						r.wireCache.HandleEvent(nsn, state.CreatedEvent, &state.EventCtx{
							EpIdx: e.EventCtx.EpIdx,
						})
					}
				case state.WorkerActionDelete:
					switch req := e.Req.(type) {
					case *WireReq:
						nsn := req.GetNSN()
						r.l.Info("delete wire event", "nsn", nsn, "data", req.WireRequest)
						resp, err := r.client.WireDelete(ctx, req.WireRequest)
						r.l.Info("delete wire event", "nsn", nsn, "resp", resp, "err", err)
						if err != nil {
							eventCtx := e.EventCtx
							eventCtx.Message = err.Error()
							r.wireCache.HandleEvent(nsn, state.FailedEvent, eventCtx)
							continue
						}
						if resp.StatusCode == wirepb.StatusCode_NOK {
							eventCtx := e.EventCtx
							eventCtx.Message = resp.GetReason()
							r.wireCache.HandleEvent(nsn, state.FailedEvent, eventCtx)
						}
						// success
						r.wireCache.HandleEvent(nsn, state.DeletedEvent, &state.EventCtx{
							EpIdx: e.EventCtx.EpIdx,
						})
					}
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

func (r *worker) Write(e state.WorkerEvent) {
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
