package main

import (
	"flag"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func main() {
	flag.Parse()
	log.SetLevel(log.InfoLevel)

	for {
		nll, err := netlink.LinkList()
		if err != nil {
			log.Error(err)
		}
		log.Infof("netlink(s): %d", len(nll))
		for _, l := range nll {
			log.Infof("netlink: %v", l)
		}

		time.Sleep(5 * time.Second)
	}
}
