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
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"github.com/vishvananda/netlink"
	"golang.org/x/exp/slog"
)

type EndpointConfig struct {
	IfName string
	//IsReady bool // the container nsPath is available to communicate with
	IsLocal bool // the endpoint is local to the host
	NsPath  string
	HostIP  string
}

func NewEndpoint(cfg *EndpointConfig) Endpoint {
	l := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: new(slog.LevelVar),
		//AddSource: true,
	})).WithGroup("wirer-endpoint")
	slog.SetDefault(l)
	return Endpoint{
		ifName: cfg.IfName,
		//isReady: cfg.IsReady,
		isLocal: cfg.IsLocal,
		nsPath:  cfg.NsPath,
		hostIP:  cfg.HostIP,
		l:       l,
	}
}

type Endpoint struct {
	ifName  string
	isLocal bool
	isReady bool
	nsPath  string
	hostIP  string

	veth netlink.Link // temporary stored when veth pair gets created
	tun  netlink.Link
	mac  net.HardwareAddr

	xdp xdp.XDP
	//logger
	l *slog.Logger
}

func (r Endpoint) SetMAC(mac net.HardwareAddr) {
	r.mac = mac
}

func (r Endpoint) DeployEp2Node() error {
	r.l.Info("deploy ep2node endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
	if r.nsPath != "" {
		// if this does not exist we add the interface
		if !doesItfceExistsInNS(r.ifName, r.nsPath) {
			// attach veth to Namespace and rename to requested name
			if err := addIfInNS(r.nsPath, r.ifName, r.veth); err != nil {
				// delete the links to ensure we dont keep these resources hanging
				if err := netlink.LinkDel(r.veth); err != nil {
					r.l.Error("delete vethA failed", "name", r.veth.Attrs().Name, "err", err)
				}
				return err
			}
		}
	} else {
		r.l.Info("deploy ep2node endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
		if doesItfceExists(r.ifName) {
			if err := setItfceUp(r.veth); err != nil {
				return err
			}
		}
		/*
			if !doesItfceExists(r.ifName) {
				r.l.Info("deploy ep2node endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
				if err := setIfNameAndUp(r.ifName, r.veth); err != nil {
					return err
				}
			}
		*/
	}
	return nil
}

func (r Endpoint) DestroyEp2Node() error {
	r.l.Info("destroy ep2node endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
	if r.nsPath != "" {
		// if this does not exist we add the interface
		// the delete is safe and validate the existance
		if err := deleteIfInNS(r.ifName, r.nsPath); err != nil {
			return err
		}
	} else {
		if err := deleteItfce(r.ifName); err != nil {
			return err
		}
	}
	return nil
}

func (r Endpoint) DeployNode2Node(peerEp Endpoint) error {
	r.l.Info("deploy wire endpoint", "ifName", r.ifName, "nsPath", r.nsPath)
	if r.isLocal {
		// a local interface endpoint should exist
		if _, err := netlink.LinkByName(r.ifName); err != nil {
			// we assume the interface does not exist
			return fmt.Errorf("cannot find veth ifname %s in host namespace", r.ifName)
		}
	} else {
		if _, err := netlink.LinkByName(r.ifName); err != nil {
			// deploy tunnel
			if _, err := createTunnel(r.ifName, peerEp.hostIP, r.hostIP, 200); err != nil {
				return err
			}
		}
	}
	return nil
}

// Destroy destroys the endpoint
// for local endpoints it deletes the veth itfce from the container ns
// for remote endpoints it deletes the tun interface and xdp
func (r Endpoint) DestroyNode2Node() error {
	r.l.Info("destroy wire endpoint", "ifName", r.ifName, "local", r.isLocal)
	// we only need to destroy non local ep of a wire since the other end is managed by node2ep
	if !r.isLocal {
		if err := deleteItfce(r.ifName); err != nil {
			r.l.Error("deleteItfce failed", "itfce", r.ifName, "err", err.Error())
			return err
		}
	}
	return nil
}
