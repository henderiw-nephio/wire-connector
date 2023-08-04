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

package link

import (
	"fmt"
	"net"

	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/types"
)

type Link struct {
	podManager pod.Manager
	topologies map[string]struct{}

	endpointA *Endpoint
	endpointB *Endpoint

	mtu int
}

type LinkCtx struct {
	PodManager pod.Manager
	Topologies map[string]struct{}
}

func NewLink(cr *invv1alpha1.Link, lctx *LinkCtx) *Link {
	r := &Link{
		podManager: lctx.PodManager,
	}

	return &Link{
		endpointA: r.getEndpoint(cr.Spec.Endpoints[0]),
		endpointB: r.getEndpoint(cr.Spec.Endpoints[1]),
	}
}

func (r *Link) getEndpoint(epSpec invv1alpha1.EndpointSpec) *Endpoint {
	log.Info("getEndpoint", "epSpec", epSpec)
	epCtx := &EndpointCtx{
		IfName:              epSpec.InterfaceName,
		ClusterConnectivity: invv1alpha1.GetClusterConnectivity(epSpec.Topology, r.topologies),
	}
	log.Info("getEndpoint", "clusterConn", invv1alpha1.GetClusterConnectivity(epSpec.Topology, r.topologies))
	if epCtx.ClusterConnectivity != invv1alpha1.ClusterConnectivityLocal {
		// this ep is on a remote cluster
		epCtx.HostConnectivity = invv1alpha1.HostConnectivityRemote
		return NewEndpoint(epCtx)
	}
	// this ep is on a local cluster
	// for now namespace is default
	podCtx, err := r.podManager.GetPod(types.NamespacedName{Namespace: "default", Name: epSpec.NodeName})
	if err != nil {
		log.Info("getEndpoint pod does not exist")
		// pod does not exist
		epCtx.HostConnectivity = invv1alpha1.HostConnectivityUnknown
	} else {
		epCtx.HostConnectivity = podCtx.HostConnectivity
		epCtx.HostIP = podCtx.HostIP
		if podCtx.HostConnectivity == invv1alpha1.HostConnectivityLocal {
			epCtx.NsPath = podCtx.Containers[epSpec.NodeName].NSPath
		}
	}
	log.Info("getEndpoint", "epCtx", epCtx)
	return NewEndpoint(epCtx)
}

func (r *Link) IsReady() bool {
	if r.endpointA.clusterConnectivity == invv1alpha1.ClusterConnectivityUnknown ||
		r.endpointB.clusterConnectivity == invv1alpha1.ClusterConnectivityUnknown ||
		r.endpointA.hostConnectivity == invv1alpha1.HostConnectivityUnknown ||
		r.endpointB.hostConnectivity == invv1alpha1.HostConnectivityUnknown {
		return false
	}
	return true
}

func (r *Link) IsCrossCluster() bool {
	if r.endpointA.clusterConnectivity == invv1alpha1.ClusterConnectivityRemote ||
		r.endpointB.clusterConnectivity == invv1alpha1.ClusterConnectivityRemote {
		return true
	}
	return false
}

func (r *Link) IsHostLocal() bool {
	if r.endpointA.hostConnectivity == invv1alpha1.HostConnectivityLocal &&
		r.endpointB.hostConnectivity == invv1alpha1.HostConnectivityLocal {
		return true
	}
	return false
}

func (r *Link) GetConn() string {
	return fmt.Sprintf("epA: cluster: %s, host: %s, epB: cluster: %s, host: %s",
		r.endpointA.clusterConnectivity, r.endpointA.hostConnectivity,
		r.endpointB.clusterConnectivity, r.endpointB.hostConnectivity,
	)
}

func (r *Link) SetMtu(mtu int) {
	r.mtu = mtu
}

func (r *Link) Deploy() error {
	// get random names for veth sides as they will be created in root netns first
	linkA, linkB, err := r.createVethIfacePair()
	if err != nil {
		return err
	}

	// attach linkA to Namespace and rename to requested name
	err = linkToNS(linkA, r.endpointA.ifName, r.endpointA.nsPath)
	if err != nil {
		return err
	}

	// attach linkB to Namespace and rename to requested name
	err = linkToNS(linkB, r.endpointB.ifName, r.endpointB.nsPath)
	if err != nil {
		return err
	}

	return nil
}

func (r *Link) Destroy() error {
	for _, ep := range []*Endpoint{r.endpointA, r.endpointB} {
		ep.Destroy()
	}
	return nil
}

func (r *Link) Exists() bool {
	return r.endpointA.Exists() && r.endpointB.Exists()
}

func (r *Link) createVethIfacePair() (netlink.Link, netlink.Link, error) {
	var err error
	var linkA *netlink.Veth
	var linkB netlink.Link

	interfaceARandName := fmt.Sprintf("wire-%s", genIfName())
	interfaceBRandName := fmt.Sprintf("wire-%s", genIfName())

	log.Info("createVethIfacePair", "ifa", interfaceARandName, "ifb", interfaceBRandName)

	linkA = &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  interfaceARandName,
			Flags: net.FlagUp,
		},
		PeerName: interfaceBRandName,
	}

	// set mtu if present
	if r.mtu > 0 {
		linkA.MTU = r.mtu
	}

	// set Mac if present
	if len(r.endpointA.mac) > 0 {
		linkA.LinkAttrs.HardwareAddr = r.endpointA.mac
	}
	// set peer Mac if present
	if len(r.endpointB.mac) > 0 {
		linkA.PeerHardwareAddr = r.endpointB.mac
	}

	// add the link
	if err := netlink.LinkAdd(linkA); err != nil {
		log.Info("createVethIfacePair", "err", err)
		return nil, nil, err
	}

	// retrieve netlink.Link for the peer interface
	if linkB, err = netlink.LinkByName(interfaceBRandName); err != nil {
		err = fmt.Errorf("failed to lookup %q: %v", interfaceBRandName, err)
		log.Info("createVethIfacePair", "err", err)
		return nil, nil, err
	}

	return linkA, linkB, nil
}
