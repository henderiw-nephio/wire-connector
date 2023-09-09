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

	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	noder "github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/nodemgr"
	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/topology"
	vxlanclient "github.com/henderiw-nephio/wire-connector/pkg/wire/vxlan/client"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type Config struct {
	Poll  time.Duration
	Copts controller.Options

	Client      client.Client
	VXLANClient vxlanclient.Client
	Scheme      *runtime.Scheme
	ClusterName string
	PodManager  pod.Manager     // used in distributed approach
	NodeManager nodemgr.Manager // used in distributed approach
	CRI         cri.CRI
	XDP         xdp.XDP
	PodCache    wire.Cache[wirepod.Pod]
	DaemonCache wire.Cache[wiredaemon.Daemon]
	NodeCache   wire.Cache[wirenode.Node]
	//NodePoolCache wire.Cache[invv1alpha1.NodePool]
	ClusterCache  wire.Cache[wirecluster.Cluster]
	ServiceCache  wire.Cache[wireservice.Service]
	TopologyCache wire.Cache[wiretopology.Topology]
	NodeRegistry  noder.NodeRegistry
}
