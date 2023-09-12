/*
Copyright 2022 Nokia.

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
	"context"
	"fmt"
	"log/slog"

	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/types"

)

type Config struct {
	//PodCache wire.Cache[pod.Pod]
	XDP xdp.XDP
	CRI cri.CRI
}

func New(ctx context.Context, cfg *Config) wirer.DaemonWirer {
	return &daemon{
		//wireCache: wire.NewCache[daemonwire.Wire](),
		cri: cfg.CRI,
		xdp: cfg.XDP,
		l:   log.FromContext(ctx).WithGroup("wirer-daemon"),
	}
}

type daemon struct {
	//wireCache wire.Cache[daemonwire.Wire]
	xdp xdp.XDP
	cri cri.CRI
	//logger
	l *slog.Logger
}

// TODO optimize this by using a cache to avoid querying all the time
func (r *daemon) getContainerNsPath(ctx context.Context, nsn types.NamespacedName) (string, error) {
	log  := r.l.With("nsn", nsn)
	containers, err := r.cri.ListContainers(ctx, nil)
	if err != nil {
		log.Error("cannot get containers from cri", "err", err)
		return "", err
	}

	for _, c := range containers {
		containerName := ""
		if c.GetMetadata() != nil {
			containerName = c.GetMetadata().GetName()
		}
		info, err := r.cri.GetContainerInfo(ctx, c.GetId())
		if err != nil {
			log.Error("cannot get container info", "err", err, "name", containerName, "id", c.GetId())
			return "", err
		}
		log.Info("container", "name", containerName, "name", fmt.Sprintf("%s=%s", nsn.Name, info.PodName), "namespace", fmt.Sprintf("%s=%s", nsn.Namespace, info.Namespace))
		if info.PodName == nsn.Name && info.Namespace == nsn.Namespace {
			return info.NsPath, nil
		}
	}
	return "", fmt.Errorf("not found")
}
