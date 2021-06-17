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

package ossm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"strings"

	imagev1 "github.com/openshift/api/image/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	maistrav1 "maistra.io/api/core/v1"

	"istio.io/istio/mec/pkg/model"
	"istio.io/istio/mec/pkg/podman"
	"istio.io/pkg/log"
)

var strategylog = log.RegisterScope("strategy", "Strategy", 0)

const (
	imageStreamPrefix = "ossm-extension-"
)

type ossmPullStrategy struct {
	client imagev1client.ImageV1Interface
	podman podman.Podman
}

func NewOSSMPullStrategy(config *rest.Config) (model.ImagePullStrategy, error) {
	cl, err := imagev1client.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &ossmPullStrategy{
		client: cl,
		podman: podman.NewPodman(),
	}, nil
}

func (p *ossmPullStrategy) createImageStreamImport(imageStreamName string, namespace string, image *model.ImageRef) error {
	isi := &imagev1.ImageStreamImport{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageStreamName,
		},
		Spec: imagev1.ImageStreamImportSpec{
			Import: true,
			Images: []imagev1.ImageImportSpec{
				{
					From: corev1.ObjectReference{
						Kind: "DockerImage",
						Name: image.String(),
					},
					ReferencePolicy: imagev1.TagReferencePolicy{
						Type: imagev1.SourceTagReferencePolicy,
					},
				},
			},
		},
	}

	createdIsi, err := p.client.ImageStreamImports(namespace).Create(context.TODO(), isi, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	strategylog.Infof("Created ImageStreamImport %s", createdIsi.Name)
	return nil
}

type ossmImage struct {
	imageID  string
	sha256   string
	manifest *model.Manifest
	podman   podman.Podman
}

func (p *ossmPullStrategy) GetImage(image *model.ImageRef) (model.Image, error) {
	// only works with imageRefs that come with a SHA256 value
	if image.SHA256 == "" {
		return nil, fmt.Errorf("getImage() only works for pinned images")
	}
	imageID, err := p.podman.GetImageID(image.SHA256)
	if err != nil {
		return nil, err
	} else if imageID == "" {
		return nil, nil
	}
	manifest, err := p.extractManifest(imageID)
	if err != nil {
		strategylog.Errorf("failed to extract manifest from container image: %s", err)
		return nil, err
	}
	return &ossmImage{
		manifest: manifest,
		imageID:  imageID,
		sha256:   "sha256:" + image.SHA256,
		podman:   p.podman,
	}, nil
}

// Pull retrieves an image from a remote registry
func (p *ossmPullStrategy) PullImage(image *model.ImageRef,
	namespace string,
	pullPolicy corev1.PullPolicy,
	pullSecrets []corev1.LocalObjectReference,
	smeName string,
	smeUID types.UID) (model.Image, error) {
	var imageStream *imagev1.ImageStream
	var err error
	imageStreamName := getImageStreamName(image)

	if (pullPolicy == corev1.PullAlways) || (pullPolicy == "" && image.Tag == "latest") {
		err := p.createImageStreamImport(imageStreamName, namespace, image)
		if err != nil {
			return nil, fmt.Errorf("failed to create ImageStreamImport: %s", err)
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		imageStream, err = p.client.ImageStreams(namespace).Get(context.TODO(), imageStreamName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			err := p.createImageStreamImport(imageStreamName, namespace, image)
			if err != nil {
				return nil, fmt.Errorf("failed to create ImageStreamImport: %s", err)
			}
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to get ImageStream %s: %s", imageStreamName, err)
		}

		if imageStream != nil {
			tagFound := false
			for _, tag := range imageStream.Spec.Tags {
				if tag.From.Name == image.String() {
					tagFound = true
					break
				}
			}
			if !tagFound {
				err := p.createImageStreamImport(imageStreamName, namespace, image)
				if err != nil {
					return nil, fmt.Errorf("failed to create ImageStreamImport: %s", err)
				}
			}
		}
	}

	if err != nil {
		return nil, err
	}

	// Set the ownerReferences of the ImageStream to be the Extension so that it gets garbage collected
	// This cannot be done at ImageStreamImport creation above because it doesn't get copied into the ImageStream it creates
	patchBytes, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"ownerReferences": []map[string]interface{}{
				{
					"apiVersion":         maistrav1.SchemeGroupVersion.String(),
					"kind":               "ServiceMeshExtension",
					"name":               smeName,
					"uid":                smeUID,
					"blockOwnerDeletion": true,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Using StrategicMergePatchType because having more than one ownerReference is ok, since more than one SME can refer to the same docker image,
	// thus we want to append an item to the ownerReferences array, not replace it
	_, err = p.client.ImageStreams(namespace).Patch(context.TODO(), imageStreamName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set OwnerReferences to ImageStream %s: %v", imageStreamName, err)
	}

	if len(imageStream.Status.Tags) == 0 || len(imageStream.Status.Tags[0].Items) == 0 || imageStream.Status.DockerImageRepository == "" {
		return nil, fmt.Errorf("failed to pull Image: ImageStream has not processed image yet")
	}

	for _, condition := range imageStream.Status.Tags[0].Conditions {
		if condition.Status == corev1.ConditionFalse {
			return nil, fmt.Errorf("failed to pull image: %s", condition.Message)
		}
	}

	repo := imageStream.Status.DockerImageRepository
	// The first item always points to the latest image. This is handy for the PullAlways case.
	sha := imageStream.Status.Tags[0].Items[0].Image
	strategylog.Infof("Pulling container image %s", repo+"@"+sha)
	imageID, err := p.podman.Pull(repo + "@" + sha)
	if err != nil {
		strategylog.Errorf("failed to pull image: %s", err)
		return nil, err
	}
	strategylog.Infof("Pulled container image with ID %s", imageID)
	manifest, err := p.extractManifest(imageID)
	if err != nil {
		strategylog.Errorf("failed to extract manifest from container image: %s", err)
		return nil, err
	}

	return &ossmImage{
		manifest: manifest,
		imageID:  imageID,
		sha256:   sha,
		podman:   p.podman,
	}, nil
}

func (p *ossmPullStrategy) Login(registryURL, token string) (output string, err error) {
	output, err = p.podman.Login(registryURL, token)
	return output, err
}

func getImageStreamName(image *model.ImageRef) string {
	reponame := image.Repository
	if len(reponame) > 8 {
		reponame = reponame[:8]
	}
	postfix := fmt.Sprintf("-%x", sha256.Sum256([]byte(image.String())))[:9]
	return imageStreamPrefix + reponame + postfix
}

func (p *ossmPullStrategy) extractManifest(imageID string) (*model.Manifest, error) {
	containerID, err := p.podman.Create(imageID)
	if err != nil {
		strategylog.Errorf("failed to create an image: %s", err)
		return nil, err
	}
	strategylog.Infof("Created container with ID %s", containerID)
	strategylog.Infof("Extracting manifest from container with ID %s", containerID)

	tmpDir, err := ioutil.TempDir("", containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %s", err)
	}
	manifestFile := path.Join(tmpDir, "manifest.yaml")
	_, err = p.podman.Copy(containerID+":/manifest.yaml", manifestFile)
	if err != nil {
		strategylog.Errorf("failed to copy an image: %s", err)
		return nil, err
	}
	manifestBytes, err := ioutil.ReadFile(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest.yaml: %s", err)
	}
	manifest := &model.Manifest{}
	err = yaml.Unmarshal(manifestBytes, manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest.yaml: %s", err)
	}
	err = p.podman.RemoveContainer(containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove container: %s", err)
	}
	strategylog.Infof("Deleted container with ID %s", containerID)
	return manifest, nil
}

func (ref *ossmImage) CopyWasmModule(outputFile string) error {
	containerID, err := ref.podman.Create(ref.imageID)
	if err != nil {
		return err
	}
	strategylog.Infof("Created container with ID %s", containerID)
	strategylog.Infof("Extracting WASM module from container with ID %s", containerID)
	_, err = ref.podman.Copy(containerID+":/"+ref.manifest.Module, outputFile)
	if err != nil {
		return err
	}
	err = ref.podman.RemoveContainer(containerID)
	if err != nil {
		return err
	}
	strategylog.Infof("Deleted container with ID %s", containerID)
	return nil
}

func (ref *ossmImage) GetManifest() *model.Manifest {
	return ref.manifest
}

func (ref *ossmImage) SHA256() string {
	return strings.Split(ref.sha256, "sha256:")[1]
}
