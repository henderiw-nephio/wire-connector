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

package pod

import (
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	corev1 "k8s.io/api/core/v1"
)

type Pod struct {
	wire.Object
	HostNodeName string // pointer to the lease name, which is the hosts nodeName
	HostIP       string // host ip address
}

func IsPodReady(p *corev1.Pod) bool {
	if p.Spec.NodeName == "" {
		return false
	}
	if len(p.Status.ContainerStatuses) == 0 {
		return false
	}
	if !p.Status.ContainerStatuses[0].Ready {
		return false
	}
	if len(p.Status.PodIPs) == 0 {
		return false
	}
	if p.Status.HostIP == "" {
		return false
	}
	return true
}