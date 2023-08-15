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

	"github.com/google/uuid"
	"github.com/henderiw-nephio/wire-connector/pkg/ns"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	vethPrefix   = "wire-"
	tunnelPrefix = "wtun-"
)

func genIfName() string {
	s, _ := uuid.New().MarshalText() // .MarshalText() always return a nil error
	return string(s[:8])
}

func getVethName(name string) string {
	return fmt.Sprintf("%s%s", vethPrefix, name)
}

func getTunnelName(name string) string {
	return fmt.Sprintf("%s%s", tunnelPrefix, name)
}

func createVethPair() (netlink.Link, netlink.Link, error) {
	ifNameRandA := getVethName(genIfName())
	ifNameRandB := getVethName(genIfName())

	log.Infof("createVethIfacePair, itfce A: %s, B: %s", ifNameRandA, ifNameRandB)

	vethA := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  ifNameRandA,
			Flags: net.FlagUp,
		},
		PeerName: ifNameRandB,
	}

	// add the link
	if err := netlink.LinkAdd(vethA); err != nil {
		return nil, nil, err
	}

	// retrieve netlink.Link for the peer interface
	vethB, err := netlink.LinkByName(ifNameRandB)
	if err != nil {
		return nil, nil, err
	}
	return vethA, vethB, nil
}

func createTunnel(tunName, localIP, remoteIP string, vni int) (*netlink.Link, error) {

	tun := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:   tunName,
			Flags:  net.FlagUp,
			TxQLen: 1000,
		},
		VxlanId:  200,
		//VtepDevIndex: parentIf.Attrs().Index,
		SrcAddr:  net.IP(localIP),
		Group:    net.IP(remoteIP),
		Port:     4789,
		Learning: true,
		L2miss:   true,
		L3miss:   true,
	}
	/*
		tun := &netlink.Gretun{
			LinkAttrs: netlink.LinkAttrs{
				Name:  tunName,
				Flags: net.FlagUp,
			},
			Local:  net.IP(localIP),
			Remote: net.IP(remoteIP),
		}
	*/
	// add the link
	if err := netlink.LinkAdd(tun); err != nil {
		return nil, err
	}
	tunLink := netlink.Link(tun)
	return &tunLink, nil
}

func getLinkByName(name string) (*netlink.Link, error) {
	itfce, err := netlink.LinkByName(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	return &itfce, nil
}

func deleteItfce(name string) error {
	itfce, err := netlink.LinkByName(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return err
	}
	return netlink.LinkDel(itfce)
}

func validateIfItfceExists(ifName string) bool {
	_, err := netlink.LinkByName(ifName)
	if err != nil {
		// we assume the interface does not exist
		return false
	}
	// ifName/link exists
	return true
}

func validateIfInNSExists(nsPath, ifName string) bool {
	log.Infof("validate itfce %s in container ns %s", ifName, nsPath)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		// container namespace does not exist
		// endpoint/interface is also gone -> return false
		return false
	}
	defer netns.Close()
	if err := netns.Do(func(_ ns.NetNS) error {
		// try to get Link by ifName
		if _, err := netlink.LinkByName(ifName); err != nil {
			// ifName does not exist or lookup failed
			return err
		}
		// ifName/link exists
		return nil
	}); err != nil {
		// we assume the interface does not exist
		return false
	}
	return true
}

func getPeerVethIndexFrimIfInNS(nsPath, ifName string) (int, bool) {
	log.Infof("get peer veth idx from itfce %s in container ns %s", ifName, nsPath)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		// container namespace does not exist
		// endpoint/interface is also gone -> return false
		return 0, false
	}
	defer netns.Close()
	var peerIndex int
	if err := netns.Do(func(_ ns.NetNS) error {
		// try to get Link by ifName
		l, err := netlink.LinkByName(ifName)
		if err != nil {
			// ifName does not exist or lookup failed
			return err
		}
		peerIndex = l.Attrs().ParentIndex
		// ifName/link exists
		return nil
	}); err != nil {
		// we assume the interface does not exist
		return peerIndex, false
	}
	return peerIndex, true
}

func addIfInNS(nsPath, ifName string, veth netlink.Link) error {
	log.Infof("add itfce %s in container ns %s", ifName, nsPath)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		// container namespace does not exist
		return err
	}
	defer netns.Close()
	// move veth endpoint to container namespace
	if err = netlink.LinkSetNsFd(veth, int(netns.Fd())); err != nil {
		return err
	}
	if err := netns.Do(func(_ ns.NetNS) error {
		// change the name to the real interface name
		if err := netlink.LinkSetName(veth, ifName); err != nil {
			return err
		}
		// set the link uo
		if err := netlink.LinkSetUp(veth); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func setIfUp(itfce netlink.Link) error {
	return netlink.LinkSetUp(itfce)
}

func deleteIfInNS(nsPath, ifName string) error {
	log.Infof("delete itfce %s in container: %s", ifName, nsPath)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		// container namespace does not exist
		// dont have to do anything -> return nil
		return nil
	}
	defer netns.Close()
	if err := netns.Do(func(_ ns.NetNS) error {
		itfce, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}
		netlink.LinkDel(itfce)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		log.Infof("delete itfce err: %v", err)
		if strings.Contains(err.Error(), "not found") {
			// if the interface is not found it is already deleted. This can happen in a veth pair.
			// typically when 1 end of the vethpair is deleted the other end also get deleted
			return nil
		}
		return err
	}
	return nil
}

func getPeerIDFromIndex(index int) string {
	//log.Infof("getPeerID: idx: %d", index)
	ll, err := netlink.LinkList()
	if err != nil {
		log.Debugf("cannot list net links err : %s", err.Error())
		return ""
	}

	for _, l := range ll {
		//log.Infof("getRemoteID: link: %s idx %d=%d", l.Attrs().Name, index, l.Attrs().Index)
		if l.Attrs().Index == index {
			if strings.HasPrefix(l.Attrs().Name, vethPrefix) {
				return strings.TrimPrefix(l.Attrs().Name, vethPrefix)
			}
		}
	}
	return ""
}
