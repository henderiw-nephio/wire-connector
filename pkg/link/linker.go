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
	"strings"

	//"github.com/containernetworking/plugins/pkg/ns"
	"github.com/google/uuid"
	"github.com/henderiw-nephio/wire-connector/pkg/ns"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func createLinkInContainer(nsPath, ifNameA, ifNameB string) error {
	log.Infof("create link in container %s ifNameA: %s, ifNameB: %s", nsPath, ifNameA, ifNameB)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		log.Infof("add itfce; container ns %s does not exist, err: %v", nsPath, err)
		return err
	}
	defer netns.Close()
	return addLinkInContainer(netns, ifNameA, ifNameB)
}

func addLinkInContainer(netns ns.NetNS, ifNameA, ifNameB string) error {
	if err := netns.Do(func(_ ns.NetNS) error {
		linkA := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  ifNameA,
				Flags: net.FlagUp,
			},
			PeerName: ifNameB,
		}
		if err := netlink.LinkAdd(linkA); err != nil {
			log.Infof("addLinkInContainer failed, err: %v", err)
			return err
		}

		linkB, err := netlink.LinkByName(ifNameB)
		if err != nil {
			log.Infof("addLinkInContainer get failed, err: %v", err)
			return err
		}

		if err := netlink.LinkSetUp(linkA); err != nil {
			log.Infof("addLinkInContainer failed to set container itfce %q up, err: %v", ifNameA, err)
			return err
		}
		if err := netlink.LinkSetUp(linkB); err != nil {
			log.Infof("addLinkInContainer failed to set container itfce %q up, err: %v", ifNameB, err)
			return err
		}
		return nil

	}); err != nil {
		log.Infof("addLinkInContainer ns do failed err: %v", err)
		return err
	}
	return nil
}

func linkToNS(link netlink.Link, ifName string, nsPath string) error {
	log.Infof("add itfce %s to container ns %s", ifName, nsPath)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		log.Infof("add itfce; container ns %s does not exist, err: %v", nsPath, err)
		return err
	}
	//defer netns.Close()
	// move veth endpoint to namespace
	if err = netlink.LinkSetNsFd(link, int(netns.Fd())); err != nil {
		log.Infof("linkToNS move ep to namespace failed, err: %s", err)
		return err
	}

	if err := setupContainerVeth(netns, link, ifName, 1500); err != nil {
		return err
	}
	return nil
}

func setupContainerVeth(netns ns.NetNS, link netlink.Link, ifName string, mtu int) error {
	if err := netns.Do(func(_ ns.NetNS) error {
		if err := netlink.LinkSetName(link, ifName); err != nil {
			log.Infof("setupContainerVeth failed to rename container itfce, err: %v", err)
			return err
		}

		if err := netlink.LinkSetUp(link); err != nil {
			log.Infof("setupContainerVeth failed to set container itfce %q up, err: %v", ifName, err)
			return err
		}
		return nil
	}); err != nil {
		log.Infof("setupContainerVeth ns do failed err: %v", err)
		return err
	}
	return nil
}

// toNS puts a veth endpoint to a given netns and renames its random name to a desired name.
/*
func linkToNS(link netlink.Link, linkName string, netNsPath string) error {
	log.Info("linkToNS", "linkName", linkName, "netNsPath", netNsPath)
	vethNS, err := ns.GetNS(netNsPath)
	if err != nil {
		log.Info("linkToNS get ns", "netNsPath", netNsPath, "err", err)
		return err
	}
	// move veth endpoint to namespace
	if err = netlink.LinkSetNsFd(link, int(vethNS.Fd())); err != nil {
		log.Info("linkToNS move ep to namespace failed", "err", err)
		return err
	}
	err = vethNS.Do(func(_ ns.NetNS) error {
		if err = netlink.LinkSetName(link, linkName); err != nil {
			log.Info("linkToNS failed to rename link", "err", err)
			return fmt.Errorf("failed to rename link: %v", err)
		}

		if err = netlink.LinkSetUp(link); err != nil {
			log.Info("linkToNS failed to set up", "linkName", linkName, "err", err)
			return fmt.Errorf("failed to set %q up: %v", linkName, err)
		}
		return nil
	})
	log.Info("linkToNS get ns", "netNsPath", netNsPath, "err", err)
	return err
}
*/
/*
func deleteFromNS(linkName, netNsPath string) error {
	vethNS, err := ns.GetNS(netNsPath)
	if err != nil {
		return err
	}
	err = vethNS.Do(func(_ ns.NetNS) error {

		interf, err := netlink.LinkByName(linkName)
		if err != nil {
			err = fmt.Errorf("failed to lookup %q: %v", linkName, err)
			return err
		}

		netlink.LinkDel(interf)
		if err != nil {
			err = fmt.Errorf("failed to delete %q: %v", linkName, err)
			return err
		}
		return nil
	})
	return err
}
*/

func deleteFromNS(ifName, nsPath string) error {
	log.Infof("delete itfce %s from container: %s", ifName, nsPath)
	netns, err := ns.GetNS(nsPath)
	if err != nil {
		log.Infof("delete itfce; container ns %s does not exist, err: %v", nsPath, err)
		return err
	}
	//defer netns.Close()
	return deleteContainerItfce(netns, ifName)

}

func deleteContainerItfce(netns ns.NetNS, ifName string) error {
	if err := netns.Do(func(_ ns.NetNS) error {
		itfce, err := netlink.LinkByName(ifName)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				// if the link is not found it is already deleted. This can happen in a veth pair.
				// typically when 1 end of the vethpair is deleted the other end also get deleted
				return nil
			}
			log.Infof("deleteContainerItfce: container itfce %s lookup failed err: %v", ifName, err)
			return err
		}

		netlink.LinkDel(itfce)
		if err != nil {
			log.Infof("deleteContainerItfce: container itfce %s delete failed err: %v", ifName, err)
			return err
		}
		return nil
	}); err != nil {
		log.Infof("deleteContainerItfce ns do failed err: %v", err)
		return err
	}
	return nil
}

func genIfName() string {
	s, _ := uuid.New().MarshalText() // .MarshalText() always return a nil error
	return string(s[:8])
}
