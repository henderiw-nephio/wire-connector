package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func main() {
	flag.Parse()
	log.SetLevel(log.InfoLevel)

	c, err := cri.New()
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	for {
		nll, err := netlink.LinkList()
		if err != nil {
			log.Error(err)
		}
		log.Infof("netlink(s): %d", len(nll))
		for _, l := range nll {
			log.Infof("netlink type: %v", l.Type())
			log.Infof("netlink attr: %#v", l.Attrs())
		}

		containers, err := c.ListContainers(context.TODO(), nil)
		if err != nil {
			log.Error(err)
		}
		log.Infof("containers: %d", len(containers))
		for _, container := range containers {
			containerName := ""
			if container.GetMetadata() != nil {
				containerName = container.GetMetadata().GetName()
			}

			log.Infof("container name %s, id: %s, state: %s", containerName, container.GetId(), container.GetState())
		}
		time.Sleep(5 * time.Second)
	}
}
