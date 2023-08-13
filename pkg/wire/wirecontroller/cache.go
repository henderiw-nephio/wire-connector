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
	"fmt"
	"sync"

	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// errors
	NotFound = "not found"
)

type WireCache interface {
	Get(types.NamespacedName) (*Wire, error)
	Upsert(types.NamespacedName, *Wire)
	Delete(types.NamespacedName)
	List() map[types.NamespacedName]*Wire
	SetDesiredAction(types.NamespacedName, DesiredAction)
	Resolve(nsn types.NamespacedName, resolvedData []*ResolvedData)
	UnResolve(nsn types.NamespacedName, epIdx int)
	HandleEvent(types.NamespacedName, Event, *EventCtx) error
}

func NewWireCache(workerCache wire.Cache[Worker]) WireCache {
	return &cache{
		db:          map[types.NamespacedName]*Wire{},
		workerCache: workerCache,
	}
}

type cache struct {
	m           sync.RWMutex
	db          map[types.NamespacedName]*Wire
	workerCache wire.Cache[Worker]
}

// Get return the type
func (r *cache) Get(nsn types.NamespacedName) (*Wire, error) {
	r.m.RLock()
	defer r.m.RUnlock()

	w, ok := r.db[nsn]
	if !ok {
		return nil, fmt.Errorf(NotFound)
	}
	return w, nil
}

// Upsert creates or updates the entry in the cache
func (r *cache) Upsert(nsn types.NamespacedName, newd *Wire) {
	r.m.Lock()
	defer r.m.Unlock()

	r.db[nsn] = newd
}

// Delete deletes the entry in the cache
func (r *cache) Delete(nsn types.NamespacedName) {
	r.m.Lock()
	defer r.m.Unlock()

	delete(r.db, nsn)
}

func (r *cache) List() map[types.NamespacedName]*Wire {
	r.m.RLock()
	defer r.m.RUnlock()

	wires := map[types.NamespacedName]*Wire{}
	for nsn, x := range r.db {
		wires[nsn] = x
	}
	return wires
}

func (r *cache) SetDesiredAction(nsn types.NamespacedName, desiredAction DesiredAction) {
	r.m.Lock()
	defer r.m.Unlock()
	w, ok := r.db[nsn]
	if ok {
		w.SetDesiredAction(desiredAction)
	}
	return
}

func (r *cache) Resolve(nsn types.NamespacedName, resolvedData []*ResolvedData) {
	r.m.Lock()
	defer r.m.Unlock()
	w, ok := r.db[nsn]
	if ok {
		w.WireReq.Resolve(resolvedData)
	}
	return
}

func (r *cache) UnResolve(nsn types.NamespacedName, epIdx int) {
	r.m.Lock()
	defer r.m.Unlock()
	w, ok := r.db[nsn]
	if ok {
		w.WireReq.Unresolve(epIdx)
	}
	return
}

func (r *cache) HandleEvent(nsn types.NamespacedName, event Event, eventCtx *EventCtx) error {
	r.m.Lock()
	defer r.m.Unlock()

	if eventCtx.EpIdx < 0 || eventCtx.EpIdx > 1 {
		return fmt.Errorf("cannot handleEvent, invalid endpoint index %d", eventCtx.EpIdx)
	}

	w, ok := r.db[nsn]
	if !ok {
		return fmt.Errorf("cannot handleEvent, nsn not found %s", nsn.String())
	}

	w.EndpointsState[eventCtx.EpIdx].HandleEvent(event, eventCtx, w)
	// update the wire status

	r.db[nsn] = w
	return nil
}
