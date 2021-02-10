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
	SchemaVersion ManifestSchemaVersion `yaml:"schemaVersion"`
	Name          string                `yaml:"name"`
	Description   string                `yaml:"description"`
	Version       string                `yaml:"version"`
	Phase         v1alpha1.FilterPhase  `yaml:"phase"`
	Priority      int                   `yaml:"priority"`
	Module        string                `yaml:"module"`
}

type ManifestSchemaVersion string

const (
	ManifestSchemaVersion1 = "1"

	legalSHA256Chars = "0123456789abcdef"
)

func (i *ImageRef) String() string {
	if i.SHA256 != "" {
		return fmt.Sprintf("%s/%s@sha256:%s", i.Hub, i.Repository, i.SHA256)
	}
	return fmt.Sprintf("%s/%s:%s", i.Hub, i.Repository, i.Tag)
}

func StringToImageRef(ref string) (result *ImageRef) {
	var tag, sha string
	var uri string
	if i := strings.Index(ref, "@"); i > -1 {
		uri = ref[:i]
		sha = ref[i+8:] // len("@sha256:") == 8
		if len(sha) != 64 {
			return nil
		}
		for i := 0; i < len(sha); i++ {
			if !strings.Contains(legalSHA256Chars, sha[i:i+1]) { //nolint
				return nil
			}
		}
	} else {
		refSplit := strings.Split(ref, ":")
		if len(refSplit) < 2 || len(refSplit) > 3 {
			return nil
		} else if len(refSplit) == 3 {
			// hostname can come with port
			refSplit = []string{strings.Join(refSplit[:2], ":"), refSplit[2]}
		}
		uri = refSplit[0]
		tag = refSplit[1]
	}

	uriSplit := strings.Split(uri, "/")
	if len(uriSplit) < 2 {
		return nil
	}
	hub := strings.Join(uriSplit[:len(uriSplit)-1], "/")
	repo := uriSplit[len(uriSplit)-1]

	if sha != "" {
		result = &ImageRef{
			Hub:        hub,
			Repository: repo,
			SHA256:     sha,
		}
	} else {
		result = &ImageRef{
			Hub:        hub,
			Repository: repo,
			Tag:        tag,
		}
	}

	return result
}
