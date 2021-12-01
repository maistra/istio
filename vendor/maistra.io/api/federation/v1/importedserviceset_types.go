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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ImportedServiceSet is the Schema for configuring imported services. It must be created
// in the same namespace as the control plane. The name of the ImportedServiceSet
// resource must match the name of a ServiceMeshPeer resource defining the remote mesh
// from which the services will be imported. This implies there will be at most one
// ImportedServiceSet resource per peer and control plane.
type ImportedServiceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines rules for matching services to be imported.
	Spec   ImportedServiceSetSpec   `json:"spec,omitempty"`
	Status ImportedServiceSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ImportedServiceSetList contains a list of ImportedService
type ImportedServiceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImportedServiceSet `json:"items"`
}

type ImportedServiceSetSpec struct {
	// DomainSuffix specifies the domain suffix to be applies to imported
	// services.  If no domain suffix is specified, imported services will be
	// named as follows:
	//    <imported-name>.<imported-namespace>.svc.<mesh-name>-imports.local
	// If a domain suffix is specified, imported services will be named as
	// follows:
	//    <imported-name>.<imported-namespace>.<domain-suffix>
	// +optional
	DomainSuffix string `json:"domainSuffix,omitempty"`
	// Locality within which imported services should be associated.
	Locality *ImportedServiceLocality `json:"locality,omitempty"`
	// ImportRules are the rules that determine which services are imported to the
	// mesh.  The list is processed in order and the first spec in the list that
	// applies to a service is the one that will be applied.  This allows more
	// specific selectors to be placed before more general selectors.
	ImportRules []ImportedServiceRule `json:"importRules,omitempty"`
}

type ImportedServiceRule struct {
	// DomainSuffix applies the specified suffix to services imported by this
	// rule.  The behavior is identical to that of ImportedServiceSetSpec.DomainSuffix.
	// +optional
	DomainSuffix string `json:"domainSuffix,omitempty"`
	// ImportAsLocal imports the service as a local service in the mesh.  For
	// example, if an exported service, foo/bar is imported as some-ns/service,
	// the service will be imported as service.some-ns.svc.cluster.local in the
	// some-ns namespace.  If a service of this name already exists in the mesh,
	// the imported service's endpoints will be aggregated with any other
	// workloads associated with the service.  This setting overrides DomainSuffix.
	// +optional
	ImportAsLocal bool `json:"importAsLocal,omitempty"`
	// Type of rule.  Only NameSelector type is supported.
	// +required
	Type ServiceImportExportSelectorType `json:"type"`
	// NameSelector provides a simple name matcher for importing services in
	// the mesh.
	// +optional
	NameSelector *ServiceNameMapping `json:"nameSelector,omitempty"`
}

type ImportedServiceLocality struct {
	// Region within which imported services are located.
	Region string `json:"region,omitempty"`
	// Zone within which imported services are located.  If Zone is specified,
	// Region must also be specified.
	Zone string `json:"zone,omitempty"`
	// Subzone within which imported services are located.  If Subzone is
	// specified, Zone must also be specified.
	Subzone string `json:"subzone,omitempty"`
}

type ImportedServiceSetStatus struct {
	// Imports provides details about the services imported by this mesh.
	// +required
	// +listType=map
	// +listMapKey=exportedName
	// +patchMergeKey=exportedName
	// +patchStrategy=merge,retainKeys
	ImportedServices []PeerServiceMapping `json:"importedServices"`
}
