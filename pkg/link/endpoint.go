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
	"strings"

	//"github.com/containernetworking/plugins/pkg/ns"

	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

type Endpoint struct {
	ifName string
	//containerID string
	clusterConnectivity invv1alpha1.ClusterConnectivity
	hostConnectivity    invv1alpha1.HostConnectivity
	nsPath              string
	hostIP              string
	peerID              string // ID used for tun and vtep random name
	peerIndex           int    // peerIndex of the container veth pair on the host ns

	veth netlink.Link // temporary stored when veth pair gets created
	tun  netlink.Link
	mac  net.HardwareAddr

	xdp xdp.XDP
}

type EndpointCtx struct {
	IfName              string
	HostConnectivity    invv1alpha1.HostConnectivity
	ClusterConnectivity invv1alpha1.ClusterConnectivity
	NsPath              string
	HostIP              string
	XDP                 xdp.XDP
}

func NewEndpoint(epctx *EndpointCtx) *Endpoint {
	return &Endpoint{
		ifName:              epctx.IfName,
		clusterConnectivity: epctx.ClusterConnectivity,
		hostConnectivity:    epctx.HostConnectivity,
		nsPath:              epctx.NsPath,
		hostIP:              epctx.HostIP,
		xdp:                 epctx.XDP,
	}
}

// SetMAC sets the mac on the endpoint
func (r *Endpoint) SetMAC(mac net.HardwareAddr) {
	r.mac = mac
}

// Destroy destroys the endpoint
// for local endpoints it deletes the veth itfce from the container ns
// for remote endpoints it deletes the tun interface and xdp
func (r *Endpoint) Destroy() error {
	log.Infof("destroy endpoint %s in ns %s", r.ifName, r.nsPath)
	switch r.hostConnectivity {
	case invv1alpha1.HostConnectivityLocal:
		// delete itfce in container namespace
		return deleteIfInNS(r.nsPath, r.ifName)
	case invv1alpha1.HostConnectivityRemote:
		l, err := getLinkByName(getVethName(r.peerID))
		if err != nil {
			return err
		}
		if l != nil {
			log.Infof("destroy xdp: from %s", (*l).Attrs().Name)
			if err := r.xdp.DeleteXConnectBPFMap(l); err != nil {
				return err
			}
		}
		if err := deleteItfce(getVethName(r.peerID)); err != nil {
			return err
		}
		// delete tunnel
		return deleteItfce(getTunnelName(r.peerID))
		// TODO add the XDP binding
	default:
		return fmt.Errorf("deploying a link on an endpoint for which the host connectivity is not local or remote is not allowed")
	}
}

// Exists validates if the endpoint exists
// Local endpoints check interface in container namespace
// Remote endpoints get peerID random name from peerIndex
func (r *Endpoint) Exists() bool {
	log.Infof("validate endpoint %s in ns %s", r.ifName, r.nsPath)
	if r.hostConnectivity == invv1alpha1.HostConnectivityLocal {
		// check ifName in Container
		return validateIfInNSExists(r.nsPath, r.ifName)
	}
	if r.hostConnectivity == invv1alpha1.HostConnectivityRemote {
		if r.peerIndex == 0 {
			return false // does not exist
		}
		r.peerID = getPeerIDFromIndex(r.peerIndex)
		//log.Infof("exists peerID %s ", r.peerID)
		if r.peerID == "" {
			return false // does not exist
		}
		exists := validateIfItfceExists(getVethName(r.peerID))
		if !exists {
			return false
		}
		return validateIfItfceExists(getTunnelName(r.peerID))

	}
	log.Debug("endpoint exists should only allow for local or remote host connectivity, readiness check should be performed before")
	return false
}

func (r *Endpoint) InitPeerVethIndex(peerEp *Endpoint) {
	if r.hostConnectivity == invv1alpha1.HostConnectivityLocal {
		idx, exists := getPeerVethIndexFrimIfInNS(r.nsPath, r.ifName)
		if exists {
			peerEp.peerIndex = idx
		}
		log.Infof("peerIndex: idx: %d, exists: %t", idx, exists)
	}
}

func (r *Endpoint) Deploy(peerEp *Endpoint) error {
	log.Infof("deploy endpoint %s in ns %s", r.ifName, r.nsPath)
	switch r.hostConnectivity {
	case invv1alpha1.HostConnectivityLocal:
		// attach veth to Namespace and rename to requested name
		if err := addIfInNS(r.nsPath, r.ifName, r.veth); err != nil {
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				log.Debugf("delete vethA %s failed, err: %v", r.veth.Attrs().Name, err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				log.Debugf("delete vethB %s failed, err: %s", r.veth.Attrs().Name, err)
			}
			return err
		}
	case invv1alpha1.HostConnectivityRemote:
		r.peerIndex = peerEp.veth.Attrs().ParentIndex
		r.peerID = strings.TrimPrefix(r.veth.Attrs().Name, vethPrefix)
		log.Infof("deploy peer veth and tunnel index %d, id %s, peerVeth: %s, localVeth: %s", r.peerIndex, r.peerID, r.veth.Attrs().Name, peerEp.veth.Attrs().Name)
		if err := setIfUp(r.veth); err != nil {
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				log.Debugf("delete vethA %s failed, err: %v", r.veth.Attrs().Name, err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				log.Debugf("delete vethB %s failed, err: %s", r.veth.Attrs().Name, err)
			}
			return err
		}
		// create tunnel
		tun, err := createTunnel(getTunnelName(r.peerID), r.hostIP, peerEp.hostIP, 200)
		if err != nil {
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				log.Debugf("delete vethA %s failed, err: %v", r.veth.Attrs().Name, err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				log.Debugf("delete vethB %s failed, err: %s", r.veth.Attrs().Name, err)
			}
			return err
		}
		log.Infof("deploy xdp: from/to %s/%s", r.veth.Attrs().Name, (*tun).Attrs().Name)
		if err := r.xdp.UpsertXConnectBPFMap(&r.veth, tun); err != nil {
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				log.Debugf("delete vethA %s failed, err: %v", r.veth.Attrs().Name, err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				log.Debugf("delete vethB %s failed, err: %s", r.veth.Attrs().Name, err)
			}
			if err := deleteItfce(getTunnelName(r.peerID)); err != nil {
				log.Debugf("delete tunn %s failed, err: %s", getTunnelName(r.peerID), err)
			}
			return err
		}

	default:
		return fmt.Errorf("deploying a link on an endpoint for which the host connectivity is not local or remote is not allowed")
	}
	return nil
}
