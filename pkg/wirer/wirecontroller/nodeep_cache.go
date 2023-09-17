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
	"fmt"
	"log/slog"

	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/types"
)

type NodeEpCache interface {
	Get(types.NamespacedName) (*NodeEndpoint, error)
	Upsert(context.Context, types.NamespacedName, *NodeEndpoint)
	Delete(context.Context, types.NamespacedName)
	List() map[types.NamespacedName]*NodeEndpoint
	Resolve(nsn types.NamespacedName, resolvedData *resolve.Data)
	UnResolve(nsn types.NamespacedName)
	HandleEvent(context.Context, types.NamespacedName, state.Event, *state.EventCtx) error
}

func NewEpCache(ctx context.Context, c wirer.Cache[*NodeEndpoint]) NodeEpCache {
	return &nodeepCache{
		c: c,
		l: log.FromContext(ctx).WithGroup("ep-cache"),
	}
}

type nodeepCache struct {
	c wirer.Cache[*NodeEndpoint]
	l *slog.Logger
}

// Get return the type
func (r *nodeepCache) Get(nsn types.NamespacedName) (*NodeEndpoint, error) {
	return r.c.Get(nsn)
}

// Upsert creates or updates the entry in the cache
func (r *nodeepCache) Upsert(ctx context.Context, nsn types.NamespacedName, ep *NodeEndpoint) {
	r.c.Upsert(ctx, nsn, ep)
}

// Delete deletes the entry in the cache
func (r *nodeepCache) Delete(ctx context.Context, nsn types.NamespacedName) {
	r.c.Delete(ctx, nsn)
}

func (r *nodeepCache) List() map[types.NamespacedName]*NodeEndpoint {
	return r.c.List()
}

func (r *nodeepCache) Resolve(nsn types.NamespacedName, resolvedData *resolve.Data) {
	ep, err := r.c.Get(nsn)
	if err == nil {
		ep.NodeEpReq.Resolve(resolvedData)
		r.c.Upsert(context.Background(), nsn, ep)
	}
}

func (r *nodeepCache) UnResolve(nsn types.NamespacedName) {
	ep, err := r.c.Get(nsn)
	if err == nil {
		ep.NodeEpReq.Unresolve()
		r.c.Upsert(context.Background(), nsn, ep)
	}
}

func (r *nodeepCache) HandleEvent(ctx context.Context, nsn types.NamespacedName, event state.Event, eventCtx *state.EventCtx) error {
	ep, err := r.c.Get(nsn)
	if err != nil {
		return fmt.Errorf("cannot handleEvent, nsn not found %s", nsn.String())
	}
	log := log.FromContext(ctx).With("event", event, "nsn", nsn, "evenCtx", eventCtx, "state", ep.State.String())
	log.Info("handleEvent")

	ep.State.HandleEvent(ctx, event, eventCtx, ep)

	// update the wire status
	if ep.State.String() == "Deleted" {
		r.c.Delete(ctx, nsn)
	} else {
		r.Upsert(ctx, nsn, ep)
	}
	return nil
}
