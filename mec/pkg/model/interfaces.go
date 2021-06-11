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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type ImagePullStrategy interface {
	// PullImage will go and Pull and image from a remote registry
	PullImage(
		image *ImageRef,
		namespace string,
		pullPolicy corev1.PullPolicy,
		pullSecrets []corev1.LocalObjectReference,
		smeName string,
		smeUID types.UID) (Image, error)
	// GetImage returns an image that has been pulled previously
	GetImage(image *ImageRef) (Image, error)
	// Login is used to provide credentials to the ImagePullStrategy
	Login(registryURL, token string) (string, error)
}

type Image interface {
	CopyWasmModule(outputFile string) error
	GetManifest() *Manifest
	SHA256() string
}
