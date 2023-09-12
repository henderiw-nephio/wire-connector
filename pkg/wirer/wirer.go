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

package wirer

import (
	"context"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	//"github.com/henderiw-nephio/wire-connector/pkg/proto/resolverpb"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
)

type Wirer interface {
	Node2NodeWirer
	Ep2NodeWirer
}

type DaemonWirer interface {
	Node2NodeWirer
	Ep2NodeWirer
}

type InClusterWirer interface {
	Node2NodeWirer
	Ep2NodeWirer
	//Resolver
}

type InterClusterWirer interface {
	Node2NodeWirer
}

/*
type Resolver interface {
	Resolve(ctx context.Context, req *resolverpb.ResolveRequest) (*resolverpb.ResolveResponse, error)
}
*/

type Node2NodeWirer interface {
	WireGet(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error)
	WireUpSert(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
	WireDelete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
	AddWireWatch(fn CallbackFn)
	DeleteWireWatch()
}

type Ep2NodeWirer interface {
	EndpointGet(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EndpointResponse, error)
	EndpointUpSert(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error)
	EndpointDelete(ctx context.Context, req *endpointpb.EndpointRequest) (*endpointpb.EmptyResponse, error)
	AddEndpointWatch(fn CallbackFn)
	DeleteEndpointWatch()
}
