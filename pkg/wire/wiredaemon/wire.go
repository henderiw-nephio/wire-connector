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
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Wire interface {
	IsReady() bool
	Exists() bool
	Deploy() error
	Destroy() error
}

type WireConfig struct {
	// TODO container Cache -> keep a local cache evertime
	// we query to avoid going to CRI all the time
	XDP xdp.XDP
	CRI cri.CRI
}

func NewWire(ctx context.Context, req *wirepb.WireRequest, cfg *WireConfig) Wire {
	l := ctrl.Log.WithName("wire")
	r := &w{
		xdp: cfg.XDP,
		cri: cfg.CRI,
		l:   l,
	}

	return &w{
		endpointA: r.getEndpoint(ctx, req, 0),
		endpointB: r.getEndpoint(ctx, req, 1),
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
		IfName:  epReq.IfName,
		IsLocal: epReq.HostIP == os.Getenv("NODE_IP"),
		XDP:     r.xdp,
	}

	if epCfg.IsLocal {
		nsPath, err := r.getContainerNsPath(ctx, req, epIdx)
		if err != nil {
			// not ready
			r.l.Info("getEndpoint", "epCfg", epCfg)
			return NewEndpoint(epCfg)
		}
		epCfg.NsPath = nsPath
	}
	epCfg.IsReady = true

	r.l.Info("getEndpoint", "epCfg", epCfg)
	return NewEndpoint(epCfg)
}

// TODO optimize this by using a cache to avoid querying all the time
func (r *w) getContainerNsPath(ctx context.Context, req *wirepb.WireRequest, epIdx int) (string, error) {
	epReq := req.Endpoints[epIdx]
	containers, err := r.cri.ListContainers(ctx, nil)
	if err != nil {
		r.l.Error(err, "cannot get containers from cri")
		return "", err
	}

	for _, c := range containers {
		containerName := ""
		if c.GetMetadata() != nil {
			containerName = c.GetMetadata().GetName()
		}
		info, err := r.cri.GetContainerInfo(ctx, c.GetId())
		if err != nil {
			r.l.Error(err, "cannot get container info", "name", containerName, "id", c.GetId())
			continue
		}
		r.l.Info("container", "name", containerName, "name", fmt.Sprintf("%s=%s", epReq.NodeName, info.PodName), "namespace", fmt.Sprintf("%s=%s", req.Namespace, info.Namespace))
		if info.PodName == epReq.NodeName && info.Namespace == req.Namespace {
			return info.NsPath, nil
		}
	}
	return "", fmt.Errorf("not found")
}

// IsReady returns true if both ep are ready
func (r *w) IsReady() bool {
	return r.endpointA.isReady && r.endpointB.isReady
}

// Exists returns true if the link exists. Since we have 2 endpoints
// it might be some part does not exist. if only a part exists
// we return true
func (r *w) Exists() bool {
	// we need to recover the id of the veth pair
	// we know the veth interface in the container ns.
	// if it exists we return the index of the peer veth pair on the host
	// otherwise the veth pair does not exist
	// the peerIndex is used later to retrieve the peerID random name
	// that is used for both the veth/tunn interface
	r.endpointA.InitPeerVethIndex(r.endpointB)
	r.endpointB.InitPeerVethIndex(r.endpointA)

	r.l.Info("link exists", "endpointA", r.endpointA, "endpointB", r.endpointB)

	epAexists := r.endpointA.Exists()
	epBExists := r.endpointB.Exists()

	r.l.Info("exists", "epAexists", epAexists, "epAexists", epBExists)
	return epAexists || epBExists
}

// Deploy deploys the link on the host
// Creates a veth pair
// Per endpoint deploys either a veth itfce in the container namespace
// or a remote tunnel for which a veth pair gets cross connected with BPF XDP
func (r *w) Deploy() error {
	// get random names for veth sides as they will be created in root netns first
	vethA, vethB, err := createVethPair()
	if err != nil {
		return err
	}
	r.endpointA.veth = vethA
	r.endpointB.veth = vethB

	if err := r.endpointA.Deploy(r.endpointB); err != nil {
		return err
	}
	if err := r.endpointB.Deploy(r.endpointA); err != nil {
		return err
	}
	return nil
}

func (r *w) Destroy() error {
	if err := r.endpointA.Destroy(); err != nil {
		return err
	}
	if err := r.endpointB.Destroy(); err != nil {
		return err
	}
	return nil
}
