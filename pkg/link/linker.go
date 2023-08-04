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

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

// toNS puts a veth endpoint to a given netns and renames its random name to a desired name.
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

func genIfName() string {
	s, _ := uuid.New().MarshalText() // .MarshalText() always return a nil error
	return string(s[:8])
}
