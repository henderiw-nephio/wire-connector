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

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/resolve"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/state"
	vxlanclient "github.com/henderiw-nephio/wire-connector/pkg/wirer/vxlan/client"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// errors
	NotFound = "not found"
)

type WireCache interface {
	Get(types.NamespacedName) (*Wire, error)
	Upsert(context.Context, types.NamespacedName, *Wire)
	Delete(context.Context, types.NamespacedName)
	List() map[types.NamespacedName]*Wire
	SetDesiredAction(types.NamespacedName, DesiredAction)
	Resolve(ctx context.Context, nsn types.NamespacedName, resolvedData []*resolve.Data)
	UnResolve(ctx context.Context, nsn types.NamespacedName, epIdx int)
	HandleEvent(context.Context, types.NamespacedName, state.Event, *state.EventCtx) error
}

func NewWireCache(ctx context.Context, c wirer.Cache[*Wire], vxlanclient vxlanclient.Client) WireCache {
	return &wcache{
		c:           c,
		vxlanclient: vxlanclient,
		l:           log.FromContext(ctx).WithGroup("wire-cache"),
	}
}

type wcache struct {
	vxlanclient vxlanclient.Client
	c           wirer.Cache[*Wire]
	l           *slog.Logger
}

// Get return the type
func (r *wcache) Get(nsn types.NamespacedName) (*Wire, error) {
	return r.c.Get(nsn)
}

// Upsert creates or updates the entry in the cache
func (r *wcache) Upsert(ctx context.Context, nsn types.NamespacedName, w *Wire) {
	r.c.Upsert(ctx, nsn, w)
}

// Delete deletes the entry in the cache
func (r *wcache) Delete(ctx context.Context, nsn types.NamespacedName) {
	r.c.Delete(ctx, nsn)
}

func (r *wcache) List() map[types.NamespacedName]*Wire {
	return r.c.List()
}

func (r *wcache) SetDesiredAction(nsn types.NamespacedName, desiredAction DesiredAction) {
	w, err := r.Get(nsn)
	if err == nil {
		w.SetDesiredAction(desiredAction)
		r.c.Upsert(context.Background(), nsn, w)
	}
}

func (r *wcache) Resolve(ctx context.Context, nsn types.NamespacedName, resolvedData []*resolve.Data) {
	w, err := r.Get(nsn)
	if err == nil {
		w.WireReq.Resolve(resolvedData)
		r.c.Upsert(ctx, nsn, w)
	}
}

func (r *wcache) UnResolve(ctx context.Context, nsn types.NamespacedName, epIdx int) {
	w, err := r.Get(nsn)
	if err == nil {
		w.WireReq.Unresolve(epIdx)
		r.c.Upsert(ctx, nsn, w)
	}
}

func (r *wcache) HandleEvent(ctx context.Context, nsn types.NamespacedName, event state.Event, eventCtx *state.EventCtx) error {
	log := log.FromContext(ctx).With("event", event, "nsn", nsn, "eventCtx", eventCtx)
	if eventCtx.EpIdx < 0 || eventCtx.EpIdx > 1 {
		return fmt.Errorf("cannot handleEvent, invalid endpoint index %d", eventCtx.EpIdx)
	}

	w, err := r.Get(nsn)
	if err != nil {
		return fmt.Errorf("cannot handleEvent, nsn not found %s", nsn.String())
	}
	log = log.With("state", w.EndpointsState[eventCtx.EpIdx].String())
	log.Info("handleEvent")

	w.EndpointsState[eventCtx.EpIdx].HandleEvent(ctx, event, eventCtx, w)

	// update the wire status
	if w.DesiredAction == DesiredActionDelete && w.WireResp.StatusCode == wirepb.StatusCode_OK {
		if err := r.vxlanclient.DeleteClaim(ctx, w.WireReq.WireRequest); err != nil {
			return err
		}
		r.c.Delete(ctx, nsn)

	} else {
		r.Upsert(ctx, nsn, w)
	}
	return nil
}
