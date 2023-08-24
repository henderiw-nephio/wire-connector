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

package wiredaemon

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/endpointpb"
	ctrl "sigs.k8s.io/controller-runtime"
)

type WireEp2Node interface {
	IsReady() bool
	Deploy(req *endpointpb.EndpointRequest) error
	Destroy(req *endpointpb.EndpointRequest) error
}

type WireEp2NodeConfig struct {
	// TODO container Cache -> keep a local cache evertime
	// we query to avoid going to CRI all the time
	CRI cri.CRI
}

func NewWireEp2Node(ctx context.Context, nsPath string, cfg *WireEp2NodeConfig) WireEp2Node {
	l := ctrl.Log.WithName("wire-ep-2-node")
	r := &wep2node{
		cri: cfg.CRI,
		l:   l,
	}
	r.nsPath = nsPath
	r.isReady = true
	return r
}

type wep2node struct {
	nsPath  string
	isReady bool

	mtu int

	cri cri.CRI

	l logr.Logger
}

// IsReady returns true if both ep are ready
func (r *wep2node) IsReady() bool {
	return r.isReady
}

// Deploy deploys the link on the host
// Creates a veth pair
// Per endpoint deploys either a veth itfce in the container namespace
// or a remote tunnel for which a veth pair gets cross connected with BPF XDP
func (r *wep2node) Deploy(req *endpointpb.EndpointRequest) error {
	for _, ep := range req.Endpoints {
		// check veth pair existance
		epA := Endpoint{
			ifName:  ep.IfName,
			isLocal: true,
			nsPath:  r.nsPath,
		}
		epB := Endpoint{
			ifName:  getVethName(getHashValue(getNsIfName(req.NodeKey.Topology, req.NodeKey.NodeName, ep.IfName))),
			isLocal: true,
			nsPath:  "", // this is explicit since this is the host namespace on which this req is send
		}
		// since a veth pair delates both ends we assume that if 1 ens exists the ep2node veth pait exists
		if doesItfceExistsInNS(epA.ifName, epA.nsPath) || doesItfceExists(epB.ifName) {
			r.l.Info("veth pair already exists", "epA", ep.IfName, "epB", epB.ifName)
			continue
		}
		// the ep2node veth-pair does not exist -> create it

		// get random names for veth sides as they will be created in root netns first
		var err error
		epA.veth, epB.veth, err = createVethPair()
		if err != nil {
			return err
		}
		if err := epA.DeployEp2Node(); err != nil {
			return err
		}
		if err := epB.DeployEp2Node(); err != nil {
			return err
		}
	}
	return nil
}

func (r *wep2node) Destroy(req *endpointpb.EndpointRequest) error {
	for _, ep := range req.Endpoints {
		// get random names for veth sides as they will be created in root netns first
		epA := Endpoint{
			ifName:  ep.IfName,
			isLocal: true,
			nsPath:  r.nsPath,
		}
		epB := Endpoint{
			ifName:  getHashValue(getNsIfName(req.NodeKey.Topology, req.NodeKey.NodeName, ep.IfName)),
			isLocal: true,
		}
		if err := epA.DestroyEp2Node(); err != nil {
			return err
		}
		if err := epB.DestroyEp2Node(); err != nil {
			return err
		}
	}
	return nil
}
