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
	Success         bool   // inidctaes if the resolution was successfull or not
	Message         string // indicates why the resolution failed
	PodNodeName     string // name of the pod
	ServiceEndpoint string // ip address or dns name + port
	HostIP          string // ip address
	HostNodeName    string // name of the host node
}
