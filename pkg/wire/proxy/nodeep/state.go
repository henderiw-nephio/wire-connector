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

package nodeepproxy

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"google.golang.org/grpc/peer"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type stateConfig struct {
	be wire.Ep2NodeWirer
}

type clientContext struct {
	stream wirepb.Wire_WireWatchServer
	cancel context.CancelFunc
}

type s struct {
	m sync.RWMutex
	// key is clientName
	clients map[string]*clientContext
	be      wire.Ep2NodeWirer
	l       logr.Logger
}

func NewProxyState(cfg *stateConfig) *s {
	l := ctrl.Log.WithName("proxy-state")
	return &s{
		clients: map[string]*clientContext{},
		be:      cfg.be,
		l:       l,
	}
}

func (r *s) AddWireCallBackFn(req *wirepb.WatchRequest, stream wirepb.Wire_WireWatchServer) {
	p, _ := peer.FromContext(stream.Context())

	r.m.Lock()
	// cancelFn if a client adss another entry the client is misbehaving
	if clientCtx, ok := r.clients[p.Addr.String()]; ok {
		clientCtx.cancel()
	}
	ctx, cancel := context.WithCancel(stream.Context())

	r.clients[p.Addr.String()] = &clientContext{
		stream: stream,
		cancel: cancel,
	}
	r.m.Unlock()

	// we already validated the existance of the backend before calling this function
	r.be.AddEndpointWatch(r.CreateCallBackFn(stream))

	for range ctx.Done() {
		r.DeleteWireCallBackFn(p.Addr.String())
		r.l.Info("watch stopped", "address", p.Addr.String())
		return

	}
}

func (r *s) DeleteWireCallBackFn(clientName string) {
	r.m.Lock()
	defer r.m.Unlock()
	r.be.DeleteEndpointWatch()
	delete(r.clients, clientName)
}

func (r *s) CreateCallBackFn(stream wirepb.Wire_WireWatchServer) wire.CallbackFn {
	// TODO
	return func([]types.NamespacedName, string) {}
}
