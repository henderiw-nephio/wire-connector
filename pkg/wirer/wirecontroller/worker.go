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
	"log/slog"
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/client"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
)

type Worker interface {
	Start(ctx context.Context) error
	Stop()
	Write(e state.WorkerEvent)
	GetConfig() *client.Config
}

func NewWorker(ctx context.Context, wireCache WireCache, epCache NodeEpCache, cfg *client.Config) (Worker, error) {
	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	return &worker{
		cfg:       cfg,
		wireCache: wireCache,
		epCache:   epCache,
		ch:        make(chan state.WorkerEvent, 10),
		client:    c,
		l:         log.FromContext(ctx).With("address", cfg.Address),
	}, nil
}

type worker struct {
	cfg       *client.Config
	wireCache WireCache
	epCache   NodeEpCache
	ch        chan state.WorkerEvent
	client    client.Client
	cancel    context.CancelFunc
	//logger
	l *slog.Logger
}

func (r *worker) GetConfig() *client.Config {
	return r.cfg
}

func (r *worker) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("starting...")
	workerCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	if err := r.client.Start(workerCtx); err != nil {
		return err
	}
	log.Info("started...")
	go func() {
		for {
			select {
			case e, ok := <-r.ch:
				log.Info("event", "ok", ok, "e", e)
				if !ok {
					continue
				}
				switch e.Action {
				case state.WorkerActionCreate:
					switch req := e.Req.(type) {
					case *WireReq:
						nsn := req.GetNSN()
						log = log.With("nsn", nsn, "req", req.WireRequest)

						if os.Getenv("WIRER_INTERCLUSTER") == "true" {
							log.Info("get wire for create wire event")
							resp, err := r.client.WireGet(ctx, req.WireRequest)
							if err != nil {
								log.Error("get wire event failed", "error", err)
								eventCtx := e.EventCtx
								eventCtx.Message = err.Error()
								r.wireCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
								continue
							}
							if resp.StatusCode == wirepb.StatusCode_OK {
								if resp.EndpointsStatus[e.EventCtx.EpIdx].StatusCode == wirepb.StatusCode_OK &&
									resp.EndpointsStatus[e.EventCtx.EpIdx].Reason == "" {
									// success
									log.Info("create wire event success")
									r.wireCache.HandleEvent(ctx, nsn, state.CreatedEvent, &state.EventCtx{
										EpIdx: e.EventCtx.EpIdx,
									})
									continue
								}
							}
							// the wire exists, so we need to handle the delete
						}

						log.Info("create wire event")
						resp, err := r.client.WireCreate(ctx, req.WireRequest)
						if err != nil {
							log.Error("create wire event failed", "error", err)
							eventCtx := e.EventCtx
							eventCtx.Message = err.Error()
							r.wireCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						if resp.StatusCode == wirepb.StatusCode_NOK {
							log.Error("create wire event failed", "resp", resp)
							eventCtx := e.EventCtx
							eventCtx.Message = resp.GetReason()
							r.wireCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						// success
						log.Info("create wire event success")
						r.wireCache.HandleEvent(ctx, nsn, state.CreatedEvent, &state.EventCtx{
							EpIdx: e.EventCtx.EpIdx,
						})
					case *NodeEpReq:
						nsn := req.GetNSN()
						log = log.With("nsn", nsn, "req", req.EndpointRequest)
						log.Info("create endpoint event")
						resp, err := r.client.EndpointCreate(ctx, req.EndpointRequest)
						if err != nil {
							log.Error("create endpoint event failed", "error", err)
							eventCtx := e.EventCtx
							eventCtx.Message = err.Error()
							r.epCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						if resp.StatusCode == endpointpb.StatusCode_NOK {
							log.Error("create endpoint event failed", "resp", resp)
							eventCtx := e.EventCtx
							eventCtx.Message = resp.GetReason()
							r.epCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						// success
						log.Error("create endpoint event success")
						r.epCache.HandleEvent(ctx, nsn, state.CreatedEvent, &state.EventCtx{})
					}
				case state.WorkerActionDelete:
					switch req := e.Req.(type) {
					case *WireReq:
						nsn := req.GetNSN()
						log = log.With("nsn", nsn, "req", req.WireRequest)

						if os.Getenv("WIRER_INTERCLUSTER") == "true" {
							log.Info("get wire for delete wire event")
							resp, err := r.client.WireGet(ctx, req.WireRequest)
							if err != nil {
								log.Error("get wire for delete wire event failed", "error", err)
								eventCtx := e.EventCtx
								eventCtx.Message = err.Error()
								r.wireCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
								continue
							}
							if resp.StatusCode == wirepb.StatusCode_NotFound {
								// success
								log.Info("get wire not found -> wire delete success")
								r.wireCache.HandleEvent(ctx, nsn, state.DeletedEvent, &state.EventCtx{
									EpIdx: e.EventCtx.EpIdx,
								})
								continue
							}
							// the wire exists, so we need to handle the delete
						}

						log.Info("create wire event")
						resp, err := r.client.WireDelete(ctx, req.WireRequest)
						if err != nil {
							log.Error("delete wire event failed", "error", err)
							eventCtx := e.EventCtx
							eventCtx.Message = err.Error()
							r.wireCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						if resp.StatusCode == wirepb.StatusCode_NOK {
							log.Error("delete wire event failed", "resp", resp)
							eventCtx := e.EventCtx
							eventCtx.Message = resp.GetReason()
							r.wireCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						// success
						log.Info("delete wire event success")
						r.wireCache.HandleEvent(ctx, nsn, state.DeletedEvent, &state.EventCtx{
							EpIdx: e.EventCtx.EpIdx,
						})
					case *NodeEpReq:
						nsn := req.GetNSN()
						log = log.With("nsn", nsn, "req", req.EndpointRequest)
						log.Info("delete endpoint event")
						resp, err := r.client.EndpointCreate(ctx, req.EndpointRequest)
						if err != nil {
							log.Error("delete endpoint event failed", "err", err)
							eventCtx := e.EventCtx
							eventCtx.Message = err.Error()
							r.epCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						if resp.StatusCode == endpointpb.StatusCode_NOK {
							log.Error("delete endpoint event failed", "resp", resp)
							eventCtx := e.EventCtx
							eventCtx.Message = resp.GetReason()
							r.epCache.HandleEvent(ctx, nsn, state.FailedEvent, eventCtx)
							continue
						}
						// success
						log.Info("delete endpoint event success")
						r.epCache.HandleEvent(ctx, nsn, state.DeletedEvent, &state.EventCtx{})
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
