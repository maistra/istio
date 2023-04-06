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

import "github.com/mitchellh/hashstructure/v2"

type ServiceKey struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
}

type ServiceListMessage struct {
	Checksum uint64            `json:"checksum" hash:"ignore"`
	Services []*ServiceMessage `json:"services,omitempty" hash:"set"`
}

type ServiceMessage struct {
	// `json:",inline"` is not an official json struct-tag. see https://github.com/mgechev/revive/issues/520
	// other files with the same struct-tag are skipped, because they have "generated" header
	//nolint:revive
	ServiceKey      `json:",inline"`
	ServicePorts    []*ServicePort `json:"servicePorts,omitempty"`
	ServiceAccounts []string       `json:"serviceAccounts,omitempty"`
}

type ServicePort struct {
	Name     string `json:"name,omitempty"`
	Port     int    `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

type ServiceEndpoint struct {
	Port     int    `json:"port,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

type WatchEvent struct {
	Action   string          `json:"action,omitempty"`
	Service  *ServiceMessage `json:"service,omitempty"`
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

type TrustBundleProvider interface {
	GetTrustBundles() map[string]string
}
