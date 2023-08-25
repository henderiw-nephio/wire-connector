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

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Config struct {
	//PodCache wire.Cache[pod.Pod]
	XDP xdp.XDP
	CRI cri.CRI
}

func New(cfg *Config) wire.DaemonWirer {
	l := ctrl.Log.WithName("wiredaemon")
	return &daemon{
		//wireCache: wire.NewCache[daemonwire.Wire](),
		cri: cfg.CRI,
		xdp: cfg.XDP,
		l:   l,
	}
}

type daemon struct {
	//wireCache wire.Cache[daemonwire.Wire]
	xdp xdp.XDP
	cri cri.CRI
	//logger
	l logr.Logger
}

// TODO optimize this by using a cache to avoid querying all the time
func (r *daemon) getContainerNsPath(ctx context.Context, nodeNSN types.NamespacedName) (string, error) {
	containers, err := r.cri.ListContainers(ctx, nil)
	if err != nil {
		r.l.Error(err, "cannot get containers from cri")
		return "", err
	}

	for _, c := range containers {
		containerName := ""
		if c.GetMetadata() != nil {
			containerName = c.GetMetadata().GetName()
		}
		info, err := r.cri.GetContainerInfo(ctx, c.GetId())
		if err != nil {
			r.l.Error(err, "cannot get container info", "name", containerName, "id", c.GetId())
			continue
		}
		r.l.Info("container", "name", containerName, "name", fmt.Sprintf("%s=%s", nodeNSN.Name, info.PodName), "namespace", fmt.Sprintf("%s=%s", nodeNSN.Namespace, info.Namespace))
		if info.PodName == nodeNSN.Name && info.Namespace == nodeNSN.Namespace {
			return info.NsPath, nil
		}
	}
	return "", fmt.Errorf("not found")
}
