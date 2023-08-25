/*
Copyright 2023 The Nephio Authors.

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

package ctrlconfig

import (
	"time"

	noder "github.com/henderiw-nephio/network-node-operator/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type ControllerConfig struct {
	Poll  time.Duration
	Copts controller.Options

	PodManager    pod.Manager
	NodeManager   node.Manager
	CRI           cri.CRI
	XDP           xdp.XDP
	PodCache      wire.Cache[wirepod.Pod]
	DaemonCache   wire.Cache[wiredaemon.Daemon]
	NodeCache     wire.Cache[wirenode.Node]
	ClusterCache  wire.Cache[wirecluster.Cluster]
	ServiceCache  wire.Cache[wireservice.Service]
	TopologyCache wire.Cache[struct{}]
	Noderegistry  noder.NodeRegistry
}
