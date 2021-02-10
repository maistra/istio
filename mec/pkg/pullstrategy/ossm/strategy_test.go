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
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/client-go/image/clientset/versioned/fake"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/mec/pkg/model"
	"istio.io/istio/mec/pkg/podman"
	fakestrategy "istio.io/istio/mec/pkg/pullstrategy/fake"
)

type fakePodman struct {
	pulledImages []string
	shaMap       map[string]string
}

func (p *fakePodman) assignSHA256(image, sha string) {
	if p.shaMap == nil {
		p.shaMap = map[string]string{}
	}
	p.shaMap[image] = sha
}

func (p *fakePodman) Login(token, registry string) (string, error) {
	return "", nil
}

func (p *fakePodman) Pull(image string) (string, error) {
	imageRef := model.StringToImageRef(image)
	if imageRef.SHA256 != "" {
		p.pulledImages = append(p.pulledImages, imageRef.SHA256)
	}

	return imageRef.SHA256, nil
}

func (p *fakePodman) Create(image string) (string, error) {
	return "c" + image, nil
}

func (p *fakePodman) Copy(from, to string) (string, error) {
	if strings.Contains(from, "manifest.yaml") {
		err := ioutil.WriteFile(to, []byte(fakestrategy.FakeManifestYAML), os.ModePerm)
		if err != nil {
			return "", err
		}
	}
	return "", nil
}

func (p *fakePodman) RemoveContainer(containerID string) error {
	return nil
}

func (p *fakePodman) GetImageID(image string) (string, error) {
	if strings.Contains(image, "broken") {
		return "", fmt.Errorf("returning random error")
	}
	for _, pulledImage := range p.pulledImages {
		if pulledImage == image {
			return pulledImage, nil
		}
	}
	return "", nil
}

func TestGetImage(t *testing.T) {
	testCases := []struct {
		name          string
		imageRef      *model.ImageRef
		imageSHA256   string
		pullImage     bool
		expectedError bool
		expectedImage model.Image
	}{
		{
			name:          "fail_noSHA256",
			imageRef:      model.StringToImageRef("docker.io/test/test:latest"),
			pullImage:     true,
			expectedError: true,
		},
		{
			name: "fail_cannotPull",
			imageRef: &model.ImageRef{
				SHA256: "broken6dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
			},
			pullImage:     false,
			expectedError: true,
		},
		{
			name:          "pass_imageNotPresent",
			imageRef:      model.StringToImageRef("docker.io/test/repo@sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3"),
			pullImage:     false,
			expectedError: false,
			expectedImage: nil,
		},
		{
			name:      "pass_imagePresent",
			imageRef:  model.StringToImageRef("docker.io/test/test@sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3"),
			pullImage: true,
			expectedImage: &ossmImage{
				imageID:  "41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
				sha256:   "sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
				manifest: &fakestrategy.FakeManifest,
			},
		},
	}
	fakePodman := &fakePodman{}
	namespace := v1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "test",
	}}
	clientSet := fake.NewSimpleClientset(&namespace)
	strategy := &ossmPullStrategy{
		client:    clientSet.ImageV1(),
		namespace: namespace.Name,
		podman:    fakePodman,
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.imageSHA256 != "" {
				fakePodman.assignSHA256(tc.imageRef.String(), tc.imageSHA256)
			}
			if tc.pullImage {
				strategy.podman.Pull(tc.imageRef.String())
			}
			image, err := strategy.GetImage(tc.imageRef)
			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got %s", err)
				}
				if !cmp.Equal(image, tc.expectedImage, cmp.AllowUnexported(ossmImage{}), cmpopts.IgnoreInterfaces(struct{ podman.Podman }{})) {
					t.Errorf(
						"Image comparison failed: +got -want\n%v",
						cmp.Diff(tc.expectedImage, image, cmp.AllowUnexported(ossmImage{}), cmpopts.IgnoreInterfaces(struct{ podman.Podman }{})),
					)
				}
			}
		})
	}
}

func TestPullImage(t *testing.T) {
	testCases := []struct {
		name          string
		imageStream   *imagev1.ImageStream
		imageRef      *model.ImageRef
		expectedError bool
		expectedImage model.Image
	}{
		{
			name: "pass_imageStreamPresent",
			imageStream: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							From: &v1.ObjectReference{
								Name: "docker.io/test/test:latest",
							},
						},
					},
				},
				Status: imagev1.ImageStreamStatus{
					DockerImageRepository: "docker.io/test/test",
					Tags: []imagev1.NamedTagEventList{
						{
							Conditions: []imagev1.TagEventCondition{
								{
									Status: v1.ConditionTrue,
								},
							},
							Items: []imagev1.TagEvent{
								{
									Image: "sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
								},
							},
						},
					},
				},
			},
			imageRef:      model.StringToImageRef("docker.io/test/test:latest"),
			expectedError: false,
			expectedImage: &ossmImage{
				imageID:  "41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
				sha256:   "sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
				manifest: &fakestrategy.FakeManifest,
			},
		},
		{
			name: "fail_statusConditionFalse",
			imageStream: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							From: &v1.ObjectReference{
								Name: "docker.io/test/test:latest",
							},
						},
					},
				},
				Status: imagev1.ImageStreamStatus{
					DockerImageRepository: "docker.io/test/test",
					Tags: []imagev1.NamedTagEventList{
						{
							Conditions: []imagev1.TagEventCondition{
								{
									Status: v1.ConditionFalse,
								},
							},
							Items: []imagev1.TagEvent{
								{
									Image: "sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
								},
							},
						},
					},
				},
			},
			imageRef:      model.StringToImageRef("docker.io/test/test:latest"),
			expectedError: true,
		},
		{
			name: "fail_noStatus",
			imageStream: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							From: &v1.ObjectReference{
								Name: "docker.io/test/test:latest",
							},
						},
					},
				},
			},
			imageRef:      model.StringToImageRef("docker.io/test/test:latest"),
			expectedError: true,
		},
		{
			name: "fail_noDockerImageRepository",
			imageStream: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							From: &v1.ObjectReference{
								Name: "docker.io/test/test:latest",
							},
						},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Conditions: []imagev1.TagEventCondition{
								{
									Status: v1.ConditionTrue,
								},
							},
							Items: []imagev1.TagEvent{
								{
									Image: "sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
								},
							},
						},
					},
				},
			},
			imageRef:      model.StringToImageRef("docker.io/test/test:latest"),
			expectedError: true,
		},
	}
	fakePodman := &fakePodman{}
	namespace := v1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "test",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clientSet *fake.Clientset
			if tc.imageStream != nil {
				tc.imageStream.Name = getImageStreamName(tc.imageRef)
				tc.imageStream.Namespace = namespace.Name
				clientSet = fake.NewSimpleClientset(&namespace, tc.imageStream)
			} else {
				clientSet = fake.NewSimpleClientset(&namespace)
			}

			strategy := &ossmPullStrategy{
				client:    clientSet.ImageV1(),
				namespace: namespace.Name,
				podman:    fakePodman,
			}
			image, err := strategy.PullImage(tc.imageRef)
			if tc.expectedError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got %s", err)
				}
				if !cmp.Equal(image, tc.expectedImage, cmp.AllowUnexported(ossmImage{}), cmpopts.IgnoreInterfaces(struct{ podman.Podman }{})) {
					t.Errorf(
						"Image comparison failed: +got -want\n%v",
						cmp.Diff(tc.expectedImage, image, cmp.AllowUnexported(ossmImage{}), cmpopts.IgnoreInterfaces(struct{ podman.Podman }{})),
					)
				}
			}
		})
	}
}
