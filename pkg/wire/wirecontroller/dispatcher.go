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
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/state"
	"k8s.io/apimachinery/pkg/types"
)

type Dispatcher interface {
	Write(workerNsn types.NamespacedName, e state.WorkerEvent) error
}

func NewDispatcher(wc wire.Cache[Worker]) Dispatcher {
	return &dispatcher{
		workerCache: wc,
	}
}

type dispatcher struct {
	workerCache wire.Cache[Worker]
}

func (r *dispatcher) Write(workerNsn types.NamespacedName, e state.WorkerEvent) error {
	worker, err := r.workerCache.Get(workerNsn)
	if err != nil {
		return err
	}
	worker.Write(e)
	return nil
}
