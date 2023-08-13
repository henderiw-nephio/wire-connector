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
	"fmt"
	"net"
	"strings"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"github.com/vishvananda/netlink"
	ctrl "sigs.k8s.io/controller-runtime"
)

type EndpointConfig struct {
	IfName  string
	IsReady bool // the container nsPath is available to communicate with
	IsLocal bool // the endpoint is local to the host
	NsPath  string
	HostIP  string
	XDP     xdp.XDP
}

func NewEndpoint(cfg *EndpointConfig) *Endpoint {
	l := ctrl.Log.WithName("endpoint")
	return &Endpoint{
		ifName:  cfg.IfName,
		isReady: cfg.IsReady,
		isLocal: cfg.IsLocal,
		nsPath:  cfg.NsPath,
		hostIP:  cfg.HostIP,
		xdp:     cfg.XDP,
		l:       l,
	}
}

type Endpoint struct {
	ifName    string
	isLocal   bool
	isReady   bool
	nsPath    string
	hostIP    string
	peerID    string // ID used for tun and vtep random name
	peerIndex int    // peerIndex of the container veth pair on the host ns

	veth netlink.Link // temporary stored when veth pair gets created
	tun  netlink.Link
	mac  net.HardwareAddr

	xdp xdp.XDP
	//logger
	l logr.Logger
}

func (r *Endpoint) SetMAC(mac net.HardwareAddr) {
	r.mac = mac
}

// Destroy destroys the endpoint
// for local endpoints it deletes the veth itfce from the container ns
// for remote endpoints it deletes the tun interface and xdp
func (r *Endpoint) Destroy() error {
	r.l.Info("destroy endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
	if r.isLocal {
		// delete itfce in container namespace
		return deleteIfInNS(r.nsPath, r.ifName)
	} else {
		l, err := getLinkByName(getVethName(r.peerID))
		if err != nil {
			return err
		}
		if l != nil {
			r.l.Info("destroy xdp", "from", (*l).Attrs().Name)
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
	}
}

// Exists validates if the endpoint exists
// Local endpoints check interface in container namespace
// Remote endpoints get peerID random name from peerIndex
func (r *Endpoint) Exists() bool {
	r.l.Info("validate endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
	if r.isLocal {
		// check ifName in Container
		return validateIfInNSExists(r.nsPath, r.ifName)
	} else {
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
}

func (r *Endpoint) InitPeerVethIndex(peerEp *Endpoint) {
	if r.isLocal {
		idx, exists := getPeerVethIndexFrimIfInNS(r.nsPath, r.ifName)
		if exists {
			peerEp.peerIndex = idx
		}
		r.l.Info("peerIndex", "idx", idx, "exists", exists)
	}
}

func (r *Endpoint) Deploy(peerEp *Endpoint) error {
	r.l.Info("deploy endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
	if r.isLocal {
		// attach veth to Namespace and rename to requested name
		if err := addIfInNS(r.nsPath, r.ifName, r.veth); err != nil {
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				r.l.Info("delete vethA failed", "name", r.veth.Attrs().Name, "err", err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				r.l.Info("delete vethB failed", "name", r.veth.Attrs().Name, "err", err)
			}
			return err
		}
	} else {
		r.peerIndex = peerEp.veth.Attrs().ParentIndex
		r.peerID = strings.TrimPrefix(r.veth.Attrs().Name, vethPrefix)
		r.l.Info("deploy peer veth and tunnel", "index", r.peerIndex, "id", r.peerID, "peerVeth", r.veth.Attrs().Name, "localVeth", peerEp.veth.Attrs().Name)
		if err := setIfUp(r.veth); err != nil {
			r.l.Info("set veth up failed", "err", err)
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				r.l.Info("delete vethA failed", "name", r.veth.Attrs().Name, "err", err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				r.l.Info("delete vethB failed", "name", r.veth.Attrs().Name, "err", err)
			}
			return err
		}
		// create tunnel
		r.l.Info("create tunnel", "peerID", r.peerID, "tunnelName", getTunnelName(r.peerID))
		tun, err := createTunnel(getTunnelName(r.peerID), r.hostIP, peerEp.hostIP, 200)
		if err != nil {
			r.l.Info("create tunnel failed", "err", err)
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				r.l.Info("delete vethA failed", "name", r.veth.Attrs().Name, "err", err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				r.l.Info("delete vethB failed", "name", r.veth.Attrs().Name, "err", err)
			}
			return err
		}
		r.l.Info("deploy xdp", "from/to", fmt.Sprintf("%s/%s", r.veth.Attrs().Name, (*tun).Attrs().Name))
		if err := r.xdp.UpsertXConnextBPFMap(&r.veth, tun); err != nil {
			r.l.Info("deploy xdp failed", "err", err)
			// delete the links to ensure we dont keep these resources hanging
			if err := netlink.LinkDel(r.veth); err != nil {
				r.l.Info("delete vethA failed", "name", r.veth.Attrs().Name, "err", err)
			}
			if err := netlink.LinkDel(peerEp.veth); err != nil {
				r.l.Info("delete vethB failed", "name", r.veth.Attrs().Name, "err", err)
			}
			if err := deleteItfce(getTunnelName(r.peerID)); err != nil {
				r.l.Info("delete tunn failed", "tunnel", getTunnelName(r.peerID), "err", err)
			}
			return err
		}
	}
	return nil
}
