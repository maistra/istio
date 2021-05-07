// Copyright Red Hat, Inc.
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

package model

import hashstructure "github.com/mitchellh/hashstructure/v2"

type ServiceKey struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Hostname  string `json:"hostname"`
}

type ServiceListMessage struct {
	Checksum                uint64             `json:"checksum" hash:"ignore"`
	NetworkGatewayEndpoints []*ServiceEndpoint `json:"networkGatewayEndpoints" hash:"set"`
	Services                []*ServiceMessage  `json:"services" hash:"set"`
}

type ServiceMessage struct {
	ServiceKey   `json:"inline"`
	ServicePorts []*ServicePort `json:"servicePorts"`
}

type ServicePort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type ServiceEndpoint struct {
	Port     int    `json:"port"`
	Hostname string `json:"hostname"`
}

type WatchEvent struct {
	Action   string          `json:"action"`
	Service  *ServiceMessage `json:"service"`
	Checksum uint64          `json:"checksum"`
}

var (
	ActionAdd    = "add"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

func (s *ServiceListMessage) GenerateChecksum() uint64 {
	checksum, err := hashstructure.Hash(s, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return 0
	}
	return checksum
}

func (s *ServiceMessage) GenerateChecksum() uint64 {
	checksum, err := hashstructure.Hash(s, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return 0
	}
	return checksum
}
