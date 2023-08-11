package wirecontroller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func NewWriter(nodeNSN types.NamespacedName) *writer {
	return &writer{
		//nodeNSN: nodeNSN,
		ch: make(chan Event, 10),
	}
}

type writer struct {
	nodeNSN types.NamespacedName
	ch      chan Event
}

func (r *writer) GetCh() chan Event {
	return r.ch
}

func (r *writer) Write(e Event) {
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

type worker struct {
	ch chan Event
	// grpcClient
}

func NewWorker(ch chan Event) *worker {
	return &worker{
		ch: ch,
	}
}

func (r *worker) Run(ctx context.Context) {
	for {
		select {
		case e, _ := <-r.ch:
			switch e {
			case "creaate":
			}
			/*
			switch event {
			case create:
			case delete:
			}
			*/
		case <-ctx.Done():

		}
	}
}
