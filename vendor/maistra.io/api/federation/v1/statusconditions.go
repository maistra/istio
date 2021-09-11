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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StatusConditions struct {
	// Represents the latest available observations of a federation's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

type ConditionType string

const (
	// Connected indicates that one or more instances of istiod are currently
	// connected to the remote mesh.
	ConnectedServiceMeshPeerCondition ConditionType = "Connected"
	// Degraded indicates that one or more instances of istiod are currently
	// not connected to the remote mesh.
	DegradedServiceMeshPeerCondition ConditionType = "Degraded"
	// Serving indicates that one or more instances of istiod is currently
	// serving discovery information to a remote mesh.
	ServingServiceMeshPeerCondition ConditionType = "Serving"
	// Ready indicates that all instances of istiod are connected to the remote
	// mesh.
	ReadyServiceMeshPeerCondition ConditionType = "Ready"
	// Exporting indicates that the mesh is exporting services to the remote
	// mesh.
	ExportingExportedServiceSetCondition ConditionType = "Exporting"
	// Importing indicates that the mesh is importing services from the remote
	// mesh.
	ImportingImportedServiceSetCondition ConditionType = "Importing"
)

const (
	ConnectedConditionReason          = "Connected"
	NotConnectedConditionReason       = "NotConnected"
	DegradedConditionReason           = "Degraded"
	NotDegradedConditionReason        = "NotDegraded"
	ServingConditionReason            = "Serving"
	NotServingConditionReason         = "NotServing"
	ReadyConditionReason              = "Ready"
	NotReadyConditionReason           = "NotReady"
	ExportingConditionReason          = "Exporting"
	NoRulesMatchedConditionReason     = "NoRulesMatched"
	NoRulesDefinedConditionReason     = "NoRulesDefined"
	ImportingConditionReason          = "Importing"
	NoExportedServicesConditionReason = "NoExportedServices"
)

// Condition describes the state of a federation at a certain point.
type Condition struct {
	// Type of federation condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// GetCondition removes a condition for the list of conditions
func (s *StatusConditions) GetCondition(conditionType ConditionType) Condition {
	if s == nil {
		return Condition{Type: conditionType, Status: corev1.ConditionUnknown}
	}
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			return s.Conditions[i]
		}
	}
	return Condition{Type: conditionType, Status: corev1.ConditionUnknown}
}

// SetCondition sets a specific condition in the list of conditions
func (s *StatusConditions) SetCondition(condition Condition) *StatusConditions {
	if s == nil {
		return nil
	}
	// These only get serialized out to the second.  This can break update
	// skipping, as the time in the resource returned from the client may not
	// match the time in our cached status during a reconcile.  We truncate here
	// to save any problems down the line.
	now := metav1.NewTime(time.Now().Truncate(time.Second))
	for i, prevCondition := range s.Conditions {
		if prevCondition.Type == condition.Type {
			if prevCondition.Status != condition.Status {
				condition.LastTransitionTime = now
			} else {
				condition.LastTransitionTime = prevCondition.LastTransitionTime
			}
			s.Conditions[i] = condition
			return s
		}
	}

	// If the condition does not exist,
	// initialize the lastTransitionTime
	condition.LastTransitionTime = now
	s.Conditions = append(s.Conditions, condition)
	return s
}

// RemoveCondition removes a condition for the list of conditions
func (s *StatusConditions) RemoveCondition(conditionType ConditionType) *StatusConditions {
	if s == nil {
		return nil
	}
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			s.Conditions = append(s.Conditions[:i], s.Conditions[i+1:]...)
			return s
		}
	}
	return s
}
