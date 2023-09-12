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

package resolve

type Data struct {
	// Success indicates if the resolution was successfull or not
	Success bool
	// Message indicates why the resolution failed
	Message string
	// Action indicates if the wirer should act wiring this endpoint.
	// For an intercluster wire the local cluster only wires its local endpoint, the other end
	// provides the hostIP information for tunneling but no other information is required
	Action bool
	// PodNodeName defines name of the pod
	PodNodeName string
	// ServiceEndpoint defines  ip address or dns name + port
	ServiceEndpoint string
	// HistIP defines the  ip address of the host
	HostIP string
	// HostNodeName defines the name of the host node
	HostNodeName string
	// ClusterName defines the name of the cluster on which this endpoint resides
	ClusterName string
}
