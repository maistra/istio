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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=maistra-io

// MeshFederation is the Schema for joining two meshes together.  The resource
// name will be used to identify the 'cluster' to which imported services
// belong.
type MeshFederation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshFederationSpec   `json:"spec,omitempty"`
	Status MeshFederationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MeshFederationList contains a list of MeshFederation
type MeshFederationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MeshFederation `json:"items"`
}

// MeshFederationGateway defines resourcing associated with a gateway used to
// federate service traffic between meshes.
type MeshFederationGateway struct {
	// ServiceAccountName is the name of the ServiceAccount to use to run this pod.
	// Defaults to federation-<ingress|egress>-<remote-mesh-name>
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// Compute Resources required by this container.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// If specified, the pod's scheduling constraints
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// If specified, the pod's tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

type MeshFederationSecurity struct {
	// ClientID of the remote mesh.  This is used to authenticate incoming
	// requrests from the remote mesh's discovery client.
	// +required
	ClientID string `json:"clientID,omitempty"`

	// TrustDomain of remote mesh.
	// +required
	TrustDomain string `json:"trustDomain,omitempty"`

	// Name of ConfigMap containing certificate chain to be used to
	// validate the remote.  This is also used to validate certificates used by
	// the remote services (both client and server certificates).  The name of
	// the entry should be root-cert.pem.  If unspecified, it will look for a
	// ConfigMap named <meshfederation-name>-ca-root-cert, e.g. if this resource is
	// named mesh1, it will look for a ConfigMap named mesh1-ca-root-cert.
	// +optional
	CertificateChain corev1.TypedLocalObjectReference `json:"certificateChain,omitempty"`

	// AllowDirectInbound determines whether or not external service
	// invocations will be terminated at the ingress gateway.
	// XXX: should this also be configurable per exported service?
	// +optional
	AllowDirectInbound bool `json:"-"`

	// AllowDirectOutbound determines whether or not external service
	// invocations will be proxied through and terminated at the egress gateway
	// XXX: should this also be configurable per imported service?
	// +optional
	AllowDirectOutbound bool `json:"-"`
}

type MeshFederationGateways struct {
	// Gateway through which inbound federated service traffic will travel.
	// +optional
	Ingress corev1.LocalObjectReference `json:"ingress,omitempty"`

	// Gateway through which outbound federated service traffic will travel.
	// This is not required if AllowDirectOutbound is set to true.
	Egress corev1.LocalObjectReference `json:"egress,omitempty"`
}

// TODO
type MeshFederationSpec struct {
	// Remote configures details related to the remote mesh with which this mesh
	// is federating.
	// +required
	Remote MeshFederationRemote `json:"remote,omitempty"`

	Gateways MeshFederationGateways `json:"gateways,omitempty"`

	Security MeshFederationSecurity `json:"security,omitempty"`
}

type MeshFederationRemote struct {
	// Addresses are the addresses to which discovery and service requests
	// should be sent (i.e. the addresses of ingress gateways on the remote
	// mesh).  These may be specified as resolveable DNS names or IP addresses.
	Addresses []string `json:"addresses,omitempty"`
	// DiscoveryPort is the port on which the addresses are handling discovery
	// requests.  Defaults to 8188, if unspecified.
	DiscoveryPort int32 `json:"discoveryPort,omitempty"`
	// ServicePort is the port on which the addresses are handling service
	// requests.  Defaults to 15443, if unspecified.
	ServicePort int32 `json:"servicePort,omitempty"`
}

// TODO
type MeshFederationStatus struct {
}
