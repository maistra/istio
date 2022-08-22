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

import (
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/pkg/config/labels"
)

// ExtensionWrapper is a wrapper around extensions
type ExtensionWrapper struct {
	Name             string
	WorkloadSelector labels.Instance
	Config           *v1.ServiceMeshExtensionConfig
	Image            string
	FilterURL        string
	SHA256           string
	Phase            v1.FilterPhase
	Priority         int
}

func ToWrapper(extension *v1.ServiceMeshExtension) *ExtensionWrapper {
	return &ExtensionWrapper{
		Name:             extension.Name,
		WorkloadSelector: extension.Spec.WorkloadSelector.Labels,
		Config:           extension.Spec.Config.DeepCopy(),
		Image:            extension.Spec.Image,
		FilterURL:        extension.Status.Deployment.URL,
		SHA256:           extension.Status.Deployment.SHA256,
		Phase:            extension.Status.Phase,
		Priority:         extension.Status.Priority,
	}
}
