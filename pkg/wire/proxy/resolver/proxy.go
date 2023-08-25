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

package resolverproxy

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/resolverpb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Proxy interface {
	Resolve(ctx context.Context, req *resolverpb.ResolveRequest) (*resolverpb.ResolveResponse, error)
}

type Config struct {
	Backend wire.Resolver
}

func New(cfg *Config) Proxy {
	l := ctrl.Log.WithName("resolver-proxy")
	return &p{
		be: cfg.Backend,
		l:  l,
	}
}

type p struct {
	be wire.Resolver
	//logger
	l logr.Logger
}

func (r *p) Resolve(ctx context.Context, req *resolverpb.ResolveRequest) (*resolverpb.ResolveResponse, error) {
	return r.be.Resolve(ctx, req)
}
