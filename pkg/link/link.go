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

	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	log "github.com/sirupsen/logrus"
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

// getEndpoint returns an endpoint which provides context wrt
// cluster connectiivty: local or remote or unknown
// host conneciticity: local or remote or unknown; if local also the nspath of the container is initialized
func (r *Link) getEndpoint(epSpec invv1alpha1.LinkEndpointSpec) *Endpoint {
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

// IsReady returns true if cluster and host information of both endpoints are known
func (r *Link) IsReady() bool {
	return !(r.endpointA.clusterConnectivity == invv1alpha1.ClusterConnectivityUnknown ||
		r.endpointB.clusterConnectivity == invv1alpha1.ClusterConnectivityUnknown ||
		r.endpointA.hostConnectivity == invv1alpha1.HostConnectivityUnknown ||
		r.endpointB.hostConnectivity == invv1alpha1.HostConnectivityUnknown)
}

// IsCrossCluster returns true if the link is connected accross clusters
func (r *Link) IsCrossCluster() bool {
	return r.endpointA.clusterConnectivity == invv1alpha1.ClusterConnectivityRemote ||
		r.endpointB.clusterConnectivity == invv1alpha1.ClusterConnectivityRemote
}

// IsHostLocal returns true if both endpoints are on the same host
// the wiring in this case is all local to the host
func (r *Link) IsHostLocal() bool {
	return r.endpointA.hostConnectivity == invv1alpha1.HostConnectivityLocal &&
		r.endpointB.hostConnectivity == invv1alpha1.HostConnectivityLocal
}

// HasLocal returns true is one endpoint of the link is local to the host
// this indicated that a wiring activity is needed
func (r *Link) HasLocal() bool {
	return r.endpointA.hostConnectivity == invv1alpha1.HostConnectivityLocal ||
		r.endpointB.hostConnectivity == invv1alpha1.HostConnectivityLocal
}

// GetConn is a debugging facility
func (r *Link) GetConn() string {
	return fmt.Sprintf("epA: cluster: %s, host: %s, epB: cluster: %s, host: %s",
		r.endpointA.clusterConnectivity, r.endpointA.hostConnectivity,
		r.endpointB.clusterConnectivity, r.endpointB.hostConnectivity,
	)
}

// SetMtu sets the mtu on the link
func (r *Link) SetMtu(mtu int) {
	r.mtu = mtu
}

// Deploy deploys the link on the host
// Creates a veth pair
// Per endpoint deploys either a veth itfce in the container namespace
// or a remote tunnel for which a veth pair gets cross connected with BPF XDP
func (r *Link) Deploy() error {
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

func (r *Link) Destroy() error {
	if err := r.endpointA.Destroy(); err != nil {
		return err
	}
	if err := r.endpointB.Destroy(); err != nil {
		return err
	}
	return nil
}

// Exists returns true if the link exists. Since we have 2 endpoints
// it might be some part does not exist. if only a part exists 
// we return true
func (r *Link) Exists() bool {
	// we need to recover the id of the veth pair
	// we know the veth interface in the container ns.
	// if it exists we return the index of the peer veth pair on the host
	// otherwise the veth pair does not exist
	// the peerIndex is used later to retrieve the peerID random name
	// that is used for both the veth/vxlan interface
	r.endpointA.InitPeerVethIndex(r.endpointB)
	r.endpointB.InitPeerVethIndex(r.endpointA)

	log.Infof("endpointA: %v, endpointB: %v", r.endpointA, r.endpointB)

	epAexists := r.endpointA.Exists()
	epBExists := r.endpointB.Exists()

	log.Infof("exists epAexists %t, epBexists %t", epAexists, epBExists)
	return epAexists || epBExists
}
