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
// +groupName=maistra.io

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

// MeshFederationSpec configures details required to support federation with
// another service mesh.
type MeshFederationSpec struct {
	// Remote configures details related to the remote mesh with which this mesh
	// is federating.
	// +required
	Remote MeshFederationRemote `json:"remote,omitempty"`

	// Gateways configures the gateways used to facilitate ingress and egress
	// with the other mesh.
	Gateways MeshFederationGateways `json:"gateways,omitempty"`

	// Security configures details for securing communication with the other
	// mesh.
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

// MeshFederationStatus provides information related to the other mesh.
type MeshFederationStatus struct {
	// DiscoveryStatus represents the discovery status of each pilot/istiod pod
	// in the mesh.
	// +optional
	DiscoveryStatus FederationDiscoveryStatus `json:"discoveryStatus,omitempty"`
	// Exports provides details about the services exported by this mesh.
	// +required
	// +listType=map
	// +listMapKey=exportedName
	// +patchMergeKey=exportedName
	// +patchStrategy=merge,retainKeys
	Exports []MeshServiceMapping `json:"exports"`
	// Imports provides details about the services imported by this mesh.
	// +required
	// +listType=map
	// +listMapKey=exportedName
	// +patchMergeKey=exportedName
	// +patchStrategy=merge,retainKeys
	Imports []MeshServiceMapping `json:"imports"`
}

// FederationDiscoveryStatus provides details about the discovery status of each
// pilot/istiod instance in the mesh.  This is separated into lists of active
// and inactive pods.  Active pods will all have their watch.connected value set
// to true.
type FederationDiscoveryStatus struct {
	// Active represents the pilot/istiod pods actively watching the other mesh
	// for discovery.
	// +optional
	// +nullable
	// +listType=map
	// +listMapKey=pod
	// +patchMergeKey=pod
	// +patchStrategy=merge,retainKeys
	Active []FederationPodDiscoveryStatus `json:"active,omitempty"`
	// Inactive represents the pilot/istiod pods not actively watching the other
	// mesh for discovery.
	// +optional
	// +nullable
	// +listType=map
	// +listMapKey=pod
	// +patchMergeKey=pod
	// +patchStrategy=merge,retainKeys
	Inactive []FederationPodDiscoveryStatus `json:"inactive,omitempty"`
}

// FederationPodDiscoveryStatus provides discovery details related to a specific
// pilot/istiod pod.
type FederationPodDiscoveryStatus struct {
	// Discovery provides details about the connection to the remote mesh.
	// +required
	MeshDiscoveryStatus `json:",inline"`
	// Pod is the pod name to which these details apply.  This maps to a
	// a pilot/istiod pod.
	// +required
	Pod string `json:"pod"`
}

// ServiceKey provides all the details about a Service
type ServiceKey struct {
	// Name represents the simple name of the service, e.g. the metadata.name
	// field of a kubernetes Service.
	// +required
	Name string `json:"name"`
	// Namespace represents the namespace within which the service resides.
	// +required
	Namespace string `json:"namespace"`
	// Hostname represents fully qualified domain name (FQDN) used to access
	// the service.
	// +required
	Hostname string `json:"hostname"`
}

// MeshServiceMapping represents the name mapping between an exported service
// and its local counterpart.
type MeshServiceMapping struct {
	// LocalService represents the service in the local (i.e. this) mesh. For an
	// exporting mesh, this would be the service being exported. For an
	// importing mesh, this would be the imported service.
	// +required
	LocalService ServiceKey `json:"localService"`
	// ExportedName represents the fully qualified domain name (FQDN) of an
	// exported service.  For an exporting mesh, this is the name that is
	// exported to the remote mesh. For an importing mesh, this would be the
	// name of the service exported by the remote mesh.
	// +required
	ExportedName string `json:"exportedName"`
}

// MeshDiscoveryStatus represents the status of the discovery connection between
// meshes.
type MeshDiscoveryStatus struct {
	// Remotes represents details related to the inbound connections from remote
	// meshes.
	// +optional
	// +listType=map
	// +listMapKey=source
	// +patchMergeKey=source
	// +patchStrategy=merge,retainKeys
	Remotes []DiscoveryRemoteStatus `json:"remotes,omitempty"`
	// Watch represents details related to the outbound connection to the
	// remote mesh.
	// +required
	Watch DiscoveryWatchStatus `json:"watch,omitempty"`
}

// DiscoveryRemoteStatus represents details related to an inbound connection
// from a remote mesh.
type DiscoveryRemoteStatus struct {
	DiscoveryConnectionStatus `json:",inline"`
	// Source represents the source of the remote watch.
	// +required
	Source string `json:"source"`
}

// DiscoveryWatchStatus represents details related to the outbound connection
// to the remote mesh.
type DiscoveryWatchStatus struct {
	DiscoveryConnectionStatus `json:",inline"`
}

// DiscoveryConnectionStatus represents details related to connections with
// remote meshes.
type DiscoveryConnectionStatus struct {
	// Connected identfies an active connection with the remote mesh.
	// +required
	Connected bool `json:"connected"`
	// LastConnected represents the last time a connection with the remote mesh
	// was successful.
	// +optional
	LastConnected metav1.Time `json:"lastConnected,omitempty"`
	// LastEvent represents the last time an event was received from the remote
	// mesh.
	// +optional
	LastEvent metav1.Time `json:"lastEvent,omitempty"`
	// LastFullSync represents the last time a full sync was performed with the
	// remote mesh.
	// +optional
	LastFullSync metav1.Time `json:"lastFullSync,omitempty"`
	// LastDisconnect represents the last time the connection with the remote
	// mesh was disconnected.
	// +optional
	LastDisconnect metav1.Time `json:"lastDisconnect,omitempty"`
	// LastDisconnectStatus is the status returned the last time the connection
	// with the remote mesh was terminated.
	// +optional
	LastDisconnectStatus string `json:"lastDisconnectStatus,omitempty"`
}
