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
	"net"

	"github.com/containernetworking/plugins/pkg/ns"
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

	mac net.HardwareAddr
}

type EndpointCtx struct {
	IfName              string
	HostConnectivity    invv1alpha1.HostConnectivity
	ClusterConnectivity invv1alpha1.ClusterConnectivity
	NsPath              string
	HostIP              string
}

func NewEndpoint(epctx *EndpointCtx) *Endpoint {
	return &Endpoint{
		ifName:              epctx.IfName,
		clusterConnectivity: epctx.ClusterConnectivity,
		hostConnectivity:    epctx.HostConnectivity,
		nsPath:              epctx.NsPath,
		hostIP:              epctx.HostIP,
	}
}

func (r *Endpoint) SetMAC(mac net.HardwareAddr) {
	r.mac = mac
}

func (r *Endpoint) Destroy() error {
	return deleteFromNS(r.ifName, r.nsPath)
}

func (r *Endpoint) Exists() bool {
	var NNS ns.NetNS
	var err error
	log.Info("ep exists", "nsPath", r.nsPath)

	if NNS, err = ns.GetNS(r.nsPath); err != nil {
		log.Info("ep exists get ns:", "err", err)
		return false
	}

	err = NNS.Do(func(_ ns.NetNS) error {
		// try to get Link by Name
		_, err = netlink.LinkByName(r.ifName)
		return err
	})
	if err != nil {
		log.Info("ep exists get link by Name", "err", err)
		return true
	}

	return true
}
