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

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type EpCache interface {
	Get(types.NamespacedName) (*Endpoint, error)
	Upsert(context.Context, types.NamespacedName, *Endpoint)
	Delete(context.Context, types.NamespacedName)
	List() map[types.NamespacedName]*Endpoint
	Resolve(nsn types.NamespacedName, resolvedData *resolve.Data)
	UnResolve(nsn types.NamespacedName)
	HandleEvent(types.NamespacedName, state.Event, *state.EventCtx) error
}

func NewEpCache(c wire.Cache[*Endpoint]) EpCache {
	l := ctrl.Log.WithName("ep-cache")
	return &epcache{
		c: c,
		l: l,
	}
}

type epcache struct {
	c wire.Cache[*Endpoint]
	l logr.Logger
}

// Get return the type
func (r *epcache) Get(nsn types.NamespacedName) (*Endpoint, error) {
	return r.c.Get(nsn)
}

// Upsert creates or updates the entry in the cache
func (r *epcache) Upsert(ctx context.Context, nsn types.NamespacedName, ep *Endpoint) {
	r.c.Upsert(ctx, nsn, ep)
}

// Delete deletes the entry in the cache
func (r *epcache) Delete(ctx context.Context, nsn types.NamespacedName) {
	r.c.Delete(ctx, nsn)
}

func (r *epcache) List() map[types.NamespacedName]*Endpoint {
	return r.c.List()
}

func (r *epcache) Resolve(nsn types.NamespacedName, resolvedData *resolve.Data) {
	ep, err := r.c.Get(nsn)
	if err == nil {
		ep.EpReq.Resolve(resolvedData)
		r.c.Upsert(context.Background(), nsn, ep)
	}
}

func (r *epcache) UnResolve(nsn types.NamespacedName) {
	ep, err := r.c.Get(nsn)
	if err == nil {
		ep.EpReq.Unresolve()
		r.c.Upsert(context.Background(), nsn, ep)
	}
}

func (r *epcache) HandleEvent(nsn types.NamespacedName, event state.Event, eventCtx *state.EventCtx) error {
	ep, err := r.c.Get(nsn)
	if err != nil {
		return fmt.Errorf("cannot handleEvent, nsn not found %s", nsn.String())
	}
	log := r.l.WithValues("event", event, "nsn", nsn, "evenCtx", eventCtx, "state", ep.State.String())
	log.Info("handleEvent")

	ep.State.HandleEvent(event, eventCtx, ep)

	// update the wire status
	if ep.State.String() == "Deleted" {
		r.c.Delete(context.Background(), nsn)
	} else {
		r.Upsert(context.Background(), nsn, ep)
	}
	return nil
}
