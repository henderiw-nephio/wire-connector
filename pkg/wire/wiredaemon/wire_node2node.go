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
	"os"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"github.com/vishvananda/netlink"
	ctrl "sigs.k8s.io/controller-runtime"
)

type WireNode2Node interface {
	Deploy() error
	Destroy() error
}

type WireNode2NodeConfig struct {
	// TODO container Cache -> keep a local cache evertime
	// we query to avoid going to CRI all the time
	XDP xdp.XDP
	CRI cri.CRI
}

func NewWireNode2Node(ctx context.Context, req *wirepb.WireRequest, cfg *WireNode2NodeConfig) WireNode2Node {
	l := ctrl.Log.WithName("wire")
	r := &w{
		xdp: cfg.XDP,
		cri: cfg.CRI,
		l:   l,
	}

	return &w{
		endpointA: r.getEndpoint(ctx, req, 0),
		endpointB: r.getEndpoint(ctx, req, 1),
		l:         l,
	}
}

type w struct {
	podCache wire.Cache[wirepod.Pod]

	endpointA *Endpoint
	endpointB *Endpoint

	mtu int

	xdp xdp.XDP
	cri cri.CRI

	l logr.Logger
}

// getEndpoint returns an endpoint which provides context wrt
// IsLocal and IsReady, if isLocal the nsPath is returned if found
func (r *w) getEndpoint(ctx context.Context, req *wirepb.WireRequest, epIdx int) *Endpoint {
	epReq := req.Endpoints[epIdx]
	r.l.Info("getEndpoint", "epReq", epReq)
	epCfg := &EndpointConfig{
		IsLocal: epReq.HostIP == os.Getenv("NODE_IP"),
		HostIP:  epReq.HostIP,
		NsPath:  "", // this is just to indicate the endpoint is set on purpose to true
	}

	epHash := getHashValue(getNsIfName(epReq.Topology, epReq.NodeName, epReq.IfName))

	if epCfg.IsLocal {
		epCfg.IfName = getVethName(epHash)
	} else {
		epCfg.IfName = getTunnelName(epHash)
	}

	r.l.Info("getEndpoint", "epCfg", epCfg)
	return NewEndpoint(epCfg)
}

// Deploy deploys the link on the host
// Creates a veth pair
// Per endpoint deploys either a veth itfce in the container namespace
// or a remote tunnel for which a veth pair gets cross connected with BPF XDP
func (r *w) Deploy() error {
	if err := r.endpointA.DeployNode2Node(r.endpointB); err != nil {
		return err
	}
	if err := r.endpointB.DeployNode2Node(r.endpointA); err != nil {
		return err
	}
	la, err := netlink.LinkByName(r.endpointA.ifName)
	if err != nil {
		return err
	}
	lb, err := netlink.LinkByName(r.endpointA.ifName)
	if err != nil {
		return err
	}

	if err := r.connect(la, lb); err != nil {
		return err
	}
	return nil
}

func (r *w) Destroy() error {
	la, err := netlink.LinkByName(r.endpointA.ifName)
	if err != nil {
		return err
	}
	if err := r.disconnect(la); err != nil {
		return err
	}
	lb, err := netlink.LinkByName(r.endpointA.ifName)
	if err != nil {
		return err
	}
	if err := r.disconnect(lb); err != nil {
		return err
	}
	if err := r.endpointA.DestroyNode2Node(); err != nil {
		return err
	}
	if err := r.endpointB.DestroyNode2Node(); err != nil {
		return err
	}
	return nil
}

func (r *w) connect(la, lb netlink.Link) error {
	if err := r.xdp.UpsertXConnectBPFMap(la, lb); err != nil {
		return err
	}
	if err := r.xdp.UpsertXConnectBPFMap(lb, la); err != nil {
		return err
	}
	return nil
}

func (r *w) disconnect(l netlink.Link) error {
	if err := r.xdp.DeleteXConnectBPFMap(l); err != nil {
		return err
	}
	return nil
}
