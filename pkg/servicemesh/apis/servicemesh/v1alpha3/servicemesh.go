package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceMeshMemberRoll is the Schema for the servicemeshmemberrolls API
// +k8s:openapi-gen=true
type ServiceMeshMemberRoll struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ServiceMeshMemberRollSpec `json:"spec,omitempty"`
	// +optional
	Status ServiceMeshMemberRollStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceMeshMemberRollList contains a list of ServiceMeshMemberRoll
type ServiceMeshMemberRollList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceMeshMemberRoll `json:"items"`
}

// ServiceMeshMemberRollSpec defines the members of the mesh
type ServiceMeshMemberRollSpec struct {
	Members []string `json:"members,omitempty"`
}

// ServiceMeshMemberRollStatus contains the state last used to reconcile the list
type ServiceMeshMemberRollStatus struct {
	ObservedGeneration    int64 `json:"observedGeneration,omitempty"`
	ServiceMeshGeneration int64 `json:"meshGeneration,omitempty"`
}
