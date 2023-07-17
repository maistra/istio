// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configdump

type ZtunnelWorkload struct {
	WorkloadIP        string    `json:"workloadIp"`
	Waypoint          *Waypoint `json:"waypoint"`
	GatewayIP         []byte    `json:"gatewayIp"`
	Protocol          string    `json:"protocol"`
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	ServiceAccount    string    `json:"serviceAccount"`
	WorkloadName      string    `json:"workloadName"`
	WorkloadType      string    `json:"workloadType"`
	CanonicalName     string    `json:"canonicalName"`
	CanonicalRevision string    `json:"canonicalRevision"`
	Node              string    `json:"node"`
	NativeHbone       bool      `json:"nativeHbone"`
}

type Waypoint struct {
	Destination struct {
		Address string `json:"address"`
		Content string `json:"content"`
	} `json:"destination"`
}

type ZtunnelService struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Hostname  string         `json:"hostname"`
	Addresses []string       `json:"addresses"`
	Ports     map[string]int `json:"ports"`
}

type ZtunnelDump struct {
	Workloads    map[string]*ZtunnelWorkload `json:"workloads"`
	Services     map[string]*ZtunnelService  `json:"services_by_ip"`
	Certificates []*CertsDump                `json:"certificates"`
}

type CertsDump struct {
	Identity  string  `json:"identity"`
	State     string  `json:"state"`
	CaCert    []*Cert `json:"ca_cert"`
	CertChain []*Cert `json:"cert_chain"`
}

type Cert struct {
	Pem            string `json:"pem"`
	SerialNumber   string `json:"serial_number"`
	ValidFrom      string `json:"valid_from"`
	ExpirationTime string `json:"expiration_time"`
}
