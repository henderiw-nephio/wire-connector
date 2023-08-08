package xdp

import (
	"context"
	"log"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)


// $BPF_CLANG and $BPF_CFLAGS are set by the Makefile.
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc $BPF_CLANG -cflags $BPF_CFLAGS bpf ../../ebpf/xconnect/xconnect.c -- -I../headers

type XDP interface {
	Init(ctx context.Context) error
	UpsertXConnextBPFMap(from *netlink.Link, to *netlink.Link) error
	DeleteXConnectBPFMap(from *netlink.Link) error
}

type xdpApp struct {
	objs bpfObjects
}

func NewXdpApp() (XDP, error) {
	if err := IncreaseResourceLimits(); err != nil {
		return nil, err
	}
	return &xdpApp{}, nil
}

func (r *xdpApp) Init(ctx context.Context) error {
	r.objs = bpfObjects{}
	if err := loadBpfObjects(&r.objs, nil); err != nil {
		return err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Printf("ctx.Done")
				r.objs.Close()
				return
			}
		}
	}()
	return nil
}

// forcing xdpgeneric for veth because https://www.netdevconf.org/0x13/session.html?talk-veth-xdp
// tuntap also requires this probably for the same reasons
func xdpFlags(linkType string) int {
	if linkType == "veth" || linkType == "tuntap" {
		return 2
	}
	return 0 // native xdp (xdpdrv) by default
}

func (r *xdpApp) UpsertXConnextBPFMap(from *netlink.Link, to *netlink.Link) error {
	if err := r.objs.bpfMaps.XconnectMap.Put(uint32((*from).Attrs().Index), uint32((*to).Attrs().Index)); err != nil {
		return err
	}
	if err := netlink.LinkSetXdpFdWithFlags(*from, r.objs.bpfPrograms.XdpXconnect.FD(), xdpFlags((*from).Type())); err != nil {
		return err
	}
	return nil
}

func (r *xdpApp) DeleteXConnectBPFMap(from *netlink.Link) error {
	if err := netlink.LinkSetXdpFdWithFlags(*from, -1, xdpFlags((*from).Type())); err != nil {
		return err
	}
	if err := r.objs.bpfMaps.XconnectMap.Delete(uint32((*from).Attrs().Index)); err != nil {
		return err
	}
	return nil
}

// increaseResourceLimits https://prototype-kernel.readthedocs.io/en/latest/bpf/troubleshooting.html#memory-ulimits
func IncreaseResourceLimits() error {
	return unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{
		Cur: unix.RLIM_INFINITY,
		Max: unix.RLIM_INFINITY,
	})
}
