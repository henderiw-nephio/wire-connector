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

package wirecontroller

import (
	"fmt"
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/resolve"
	"k8s.io/apimachinery/pkg/types"
)

// resolveEndpoint finds the service endpoint and daemon nodeName based on the network pod (namespace/name)
// - check if the topology matches; for inter-cluster wires resolution can fail since only 1 ep resides in this cluster
// - check if the pod exists in the cache and if it is ready
// -> if ready we get the nodeName the network pod is running on
// - via the nodeName we can find the serviceendpoint in the daemon cache if the daemon is ready
func (r *wc) resolveEndpoint(nsn types.NamespacedName, wirepbEP *wirepb.Endpoint, intercluster bool) *resolve.Data {
	// for localEndpoint we dont need to perform topology lookups
	// find the topology -> provides the clusterName or validates the name exists within the cluster
	t, err := r.topologyCache.Get(types.NamespacedName{Name: nsn.Namespace})
	if err != nil {
		// for intercluster wires we allow the resolution to topology resolution to fail
		// since one ep can reside in the local cluster and the other ep can reside in a remote cluster
		if intercluster {
			if os.Getenv("WIRER_INTERCLUSTER") == "true" {
				return &resolve.Data{Message: fmt.Sprintf("intercluster wires should always resolve a topology: %s", nsn.String())}
			}
			return &resolve.Data{
				Success:         true,
				NoAction:        true,
				// we copy the data from the wireReq since the mgmt cluster resolved them
				PodNodeName:     wirepbEP.HostNodeName,
				ServiceEndpoint: wirepbEP.ServiceEndpoint,
				HostIP:          wirepbEP.HostIP,
				HostNodeName:    wirepbEP.HostNodeName,
				ClusterName:     wirepbEP.ClusterName,
			}
		}
		return &resolve.Data{Message: fmt.Sprintf("topology not found: %s", nsn.String())}
	}
	if !t.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("topology not ready: %s", nsn.String())}
	}
	// the service is only resolved for intercluster wires
	serviceEndpoint := ""
	if intercluster && os.Getenv("WIRER_INTERCLUSTER") == "true" {
		s, err := r.serviceCache.Get(types.NamespacedName{Name: t.ClusterName})
		if err != nil {
			return &resolve.Data{Message: fmt.Sprintf("service not found: %s", nsn.String())}
		}
		if !s.IsReady {
			return &resolve.Data{Message: fmt.Sprintf("service not ready: %s", nsn.String())}
		}
		serviceEndpoint = fmt.Sprintf("%s:%s", s.GRPCAddress, s.GRPCPort)
	}

	pod, err := r.podCache.Get(nsn)
	if err != nil {
		return &resolve.Data{Message: fmt.Sprintf("pod not found: %s", nsn.String())}
	}
	if !pod.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("pod not ready: %s", nsn.String())}
	}
	daemonHostNodeNSN := types.NamespacedName{
		Namespace: t.ClusterName,
		Name:      pod.HostNodeName}
	d, err := r.daemonCache.Get(daemonHostNodeNSN)
	if err != nil {
		return &resolve.Data{Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}
	}
	if !d.IsReady {
		return &resolve.Data{Message: fmt.Sprintf("wireDaemon not found: %s", daemonHostNodeNSN.String())}
	}
	if d.GRPCAddress == "" || d.GRPCPort == "" {
		return &resolve.Data{Message: fmt.Sprintf("wireDaemon no grpc address/port: %s", daemonHostNodeNSN.String())}
	}

	if intercluster && os.Getenv("WIRER_INTERCLUSTER") == "true" {
		return &resolve.Data{
			Success:         true,
			NoAction:        false,
			PodNodeName:     pod.HostNodeName,
			ServiceEndpoint: serviceEndpoint,
			HostIP:          d.HostIP,
			HostNodeName:    pod.HostNodeName,
			ClusterName:     t.ClusterName,
		}
	}
	return &resolve.Data{
		Success:         true,
		NoAction:        false,
		PodNodeName:     pod.HostNodeName,
		ServiceEndpoint: fmt.Sprintf("%s:%s", d.GRPCAddress, d.GRPCPort),
		HostIP:          d.HostIP,
		HostNodeName:    pod.HostNodeName,
		ClusterName:     t.ClusterName,
	}
}
