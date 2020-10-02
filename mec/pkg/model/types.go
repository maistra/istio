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
	"fmt"
	"strings"

	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
)

type ImageRef struct {
	Hub        string
	Repository string
	Tag        string
	SHA256     string
}

type Manifest struct {
	SchemaVersion ManifestSchemaVersion `json:"schemaVersion"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Version       string                `json:"version"`
	Phase         v1alpha1.FilterPhase  `json:"phase"`
	Priority      int                   `json:"priority"`
	Module        string                `json:"module"`
}

type ManifestSchemaVersion string

const (
	ManifestSchemaVersion1 = "1"
)

func (i *ImageRef) String() string {
	if i.SHA256 != "" {
		return fmt.Sprintf("%s/%s@sha256:%s", i.Hub, i.Repository, i.SHA256)
	}
	return fmt.Sprintf("%s/%s:%s", i.Hub, i.Repository, i.Tag)
}

func StringToImageRef(ref string) *ImageRef {
	colonSplit := strings.SplitN(ref, ":", 2)
	tag := colonSplit[1]
	slashSplit := strings.Split(colonSplit[0], "/")
	repo := slashSplit[len(slashSplit)-1]
	hub := strings.Join(slashSplit[:len(slashSplit)-1], "/")

	shaIndex := strings.Index(tag, "sha256:")
	if shaIndex == -1 {
		return &ImageRef{
			Hub:        hub,
			Tag:        tag,
			Repository: repo,
		}
	}

	return &ImageRef{
		Hub:        hub,
		SHA256:     tag[shaIndex+7:],
		Repository: repo,
	}
}
