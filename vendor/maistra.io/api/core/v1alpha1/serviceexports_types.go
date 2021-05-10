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

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceExport is the Schema for configuring exported services.  The name of
// the ServiceExports resource must match the name of a MeshFederation resource
// defining the remote mesh to which the services will be exported.
type ServiceExports struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines rules for matching services to be exported.
	Spec   ServiceExportsSpec  `json:"spec,omitempty"`
	Status ServiceExportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceExportList contains a list of ServiceExport
type ServiceExportsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceMeshExtension `json:"items"`
}

type ServiceExportSelectorType string

const (
	LabelSelectorType ServiceExportSelectorType = "Label"
	NameSelectorType  ServiceExportSelectorType = "Name"
)

type ServiceName struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"names,omitempty"`
}

func (s ServiceName) String() string {
	return fmt.Sprintf("%s/%s", s.Namespace, s.Name)
}

// XXX: this messes up crd generation
// func (s ServiceName) NamespacedName() types.NamespacedName {
// 	return types.NamespacedName{Namespace: s.Namespace, Name: s.Name}
// }

const MatchAny = "*"

type ServiceNameMapping struct {
	Name  ServiceName  `json:"name,omitempty"`
	Alias *ServiceName `json:"alias,omitempty"`
}

type ServiceExportLabelSelector struct {
	// Namespace specifies to which namespace the selector applies.  An empty
	// value applies to all namespaces in the mesh.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Selector used to select Service resources in the namespace/mesh.  An
	// empty selector selects all services.
	// +required
	Selector metav1.LabelSelector `json:"selector,omitempty"`

	// Aliases is a map of aliases to apply to exported services.  If a name is
	// not found in the map, the original service name is exported.  A '*' will
	// match any name. The Aliases list will be processed in order, with the
	// first match found being applied to the exported service.
	// Examples:
	// */foo->*/bar will match foo service in any namesapce, exporting it as bar from its original namespace.
	// */foo->bar/bar will match foo service in any namespace, exporting it as bar/bar.
	// foo/*->bar/* will match any service in foo namespace, exporting it from the bar namespace with its original name
	// */*->bar/* will match any service and export it from the bar namespace with its original name.
	// */*->*/* is the same as not specifying anything
	// +optional
	Aliases []ServiceNameMapping `json:"aliases,omitempty"`
}

type ServiceExportsSpec struct {
	// Exports are the rules that determine which services are exported from the
	// mesh.  The list is processed in order and the first spec in the list that
	// applies to a service is the one that will be applied.  This allows more
	// specific selectors to be placed before more general selectors.
	Exports []ServiceExportRule `json:"exports,omitempty"`
}

type ServiceExportRule struct {
	// +required
	Type ServiceExportSelectorType `json:"type"`
	// LabelSelector provides a mechanism for selecting services to export by
	// using a label selector to match Service resources for export.
	// +optional
	LabelSelector *ServiceExportLabelSelector `json:"labelSelector,omitempty"`
	// NameSelector provides a simple name matcher for exporting services in
	// the mesh.
	// +optional
	NameSelector *ServiceNameMapping `json:"nameSelector,omitempty"`
}

type ServiceExportStatus struct {
}
