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

package fake

import (
	"io/ioutil"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/mec/pkg/model"
)

const (
	FakeContainerSHA256  = "sha256:997890bc85c5796408ceb20b0ca75dabe6fe868136e926d24ad0f36aa424f99d"
	FakeContainer2SHA256 = "sha256:04a68a66858919123dafb8b8f8d7b7c80f7f14f129bfd12930339c879acfce2a"
	FakeModule           = "test"
	FakeModuleSHA256     = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	FakeModule2          = "another"
	FakeModule2SHA256    = "ae448ac86c4e8e4dec645729708ef41873ae79c6dff84eff73360989487f08e5"
)

var (
	FakeManifestYAML = `
schemaVersion: 1

name: testExtension
description: bogus
phase: PostAuthN
priority: 12
`

	FakeManifest = model.Manifest{
		SchemaVersion: model.ManifestSchemaVersion1,
		Name:          "testExtension",
		Description:   "bogus",
		Phase:         v1.FilterPhasePostAuthN,
		Priority:      12,
	}
	FakeManifest2 = model.Manifest{
		SchemaVersion: model.ManifestSchemaVersion1,
		Name:          "another extension",
		Description:   "description",
		Phase:         v1.FilterPhasePreAuthZ,
		Priority:      8,
	}
)

type image struct {
	manifest     *model.Manifest
	fileContent  []byte
	containerSHA string
}

func (i *image) CopyWasmModule(outputFile string) error {
	return ioutil.WriteFile(outputFile, i.fileContent, os.ModePerm)
}
func (i *image) GetManifest() *model.Manifest {
	return i.manifest
}
func (i *image) SHA256() string {
	return i.containerSHA
}

type PullStrategy struct {
	pulledImages map[string]model.Image
}

func (p *PullStrategy) PullImage(imageRef *model.ImageRef,
	namespace string,
	pullPolicy corev1.PullPolicy,
	pullSecrets []corev1.LocalObjectReference,
	smeName string,
	smeUID types.UID) (model.Image, error) {

	if p.pulledImages == nil {
		p.pulledImages = make(map[string]model.Image)
	}
	if strings.Contains(imageRef.String(), "other") {
		p.pulledImages[imageRef.String()] = &image{
			manifest:     &FakeManifest2,
			fileContent:  []byte(FakeModule2),
			containerSHA: FakeContainer2SHA256,
		}
	} else {
		p.pulledImages[imageRef.String()] = &image{
			manifest:     &FakeManifest,
			fileContent:  []byte(FakeModule),
			containerSHA: FakeContainerSHA256,
		}
	}
	return p.pulledImages[imageRef.String()], nil
}

func (p *PullStrategy) GetImage(imageRef *model.ImageRef) (model.Image, error) {
	if img, ok := p.pulledImages[imageRef.String()]; ok {
		return img, nil
	}
	return nil, nil
}

func (p *PullStrategy) Login(registryURL, token string) (string, error) {
	return "", nil
}
