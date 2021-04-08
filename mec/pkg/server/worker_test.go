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

package server

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fakestrategy "istio.io/istio/mec/pkg/pullstrategy/fake"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	"istio.io/istio/pkg/servicemesh/client/v1alpha1/clientset/versioned/fake"
	"istio.io/pkg/log"
)

const (
	baseURL = "http://localhost:8080"
)

var (
	oneHundred = 100
	twoHundred = 200
)

func TestWorker(t *testing.T) {
	testCases := []struct {
		name           string
		events         []ExtensionEvent
		extension      v1alpha1.ServiceMeshExtension
		expectedStatus v1alpha1.ServiceMeshExtensionStatus
		expectedError  bool
	}{
		{
			name: "invalid_resource",
			extension: v1alpha1.ServiceMeshExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "test",
					Generation: 1,
				},
			},
			expectedStatus: v1alpha1.ServiceMeshExtensionStatus{},
			expectedError:  true,
		},
		{
			name: "valid_resource",
			extension: v1alpha1.ServiceMeshExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "test",
					Generation: 1,
				},
				Spec: v1alpha1.ServiceMeshExtensionSpec{
					Image: "docker.io/test/test:latest",
				},
			},
			expectedStatus: v1alpha1.ServiceMeshExtensionStatus{
				Phase:    fakestrategy.FakeManifest.Phase,
				Priority: fakestrategy.FakeManifest.Priority,
				Deployment: v1alpha1.DeploymentStatus{
					Ready:           true,
					ContainerSHA256: fakestrategy.FakeContainerSHA256,
					SHA256:          fakestrategy.FakeModuleSHA256,
				},
				ObservedGeneration: 1,
			},
		},
		{
			name: "valid_resource_update_module",
			extension: v1alpha1.ServiceMeshExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "test",
					Generation: 1,
				},
				Spec: v1alpha1.ServiceMeshExtensionSpec{
					Image: "docker.io/test/test:latest",
				},
			},
			events: []ExtensionEvent{
				{
					Extension: &v1alpha1.ServiceMeshExtension{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "test",
							Namespace:  "test",
							Generation: 2,
						},
						Spec: v1alpha1.ServiceMeshExtensionSpec{
							Image: "docker.io/other/test:latest",
						},
					},
					Operation: ExtensionEventOperationUpdate,
				},
			},
			expectedStatus: v1alpha1.ServiceMeshExtensionStatus{
				Phase:    fakestrategy.FakeManifest2.Phase,
				Priority: fakestrategy.FakeManifest2.Priority,
				Deployment: v1alpha1.DeploymentStatus{
					Ready:           true,
					ContainerSHA256: fakestrategy.FakeContainer2SHA256,
					SHA256:          fakestrategy.FakeModule2SHA256,
				},
				ObservedGeneration: 2,
			},
		},
		{
			name: "valid_resource_update_priority",
			extension: v1alpha1.ServiceMeshExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "test",
					Generation: 1,
				},
				Spec: v1alpha1.ServiceMeshExtensionSpec{
					Image: "docker.io/test/test:latest",
				},
			},
			events: []ExtensionEvent{
				{
					Extension: &v1alpha1.ServiceMeshExtension{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "test",
							Namespace:  "test",
							Generation: 3,
						},
						Spec: v1alpha1.ServiceMeshExtensionSpec{
							Image:    "docker.io/test/test:latest",
							Priority: &oneHundred,
						},
					},
					Operation: ExtensionEventOperationUpdate,
				},
				{
					Extension: &v1alpha1.ServiceMeshExtension{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "test",
							Namespace:  "test",
							Generation: 4,
						},
						Spec: v1alpha1.ServiceMeshExtensionSpec{
							Image:    "docker.io/test/test:latest",
							Priority: &twoHundred,
						},
					},
					Operation: ExtensionEventOperationUpdate,
				},
			},
			expectedStatus: v1alpha1.ServiceMeshExtensionStatus{
				Phase:    fakestrategy.FakeManifest.Phase,
				Priority: 200,
				Deployment: v1alpha1.DeploymentStatus{
					Ready:           true,
					ContainerSHA256: fakestrategy.FakeContainerSHA256,
					SHA256:          fakestrategy.FakeModuleSHA256,
				},
				ObservedGeneration: 4,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()
			tmpDir, err := ioutil.TempDir("", "workertest")
			if err != nil {
				t.Fatalf("failed to create temp dir: %s", err)
			}
			defer func() {
				err = os.RemoveAll(tmpDir)
				if err != nil {
					t.Fatalf("Failed to remove temp directory %s", tmpDir)
				}
			}()
			w := createWorker(tmpDir, clientset)
			stopChan := make(chan struct{})
			w.Start(stopChan)
			w.client.ServiceMeshExtensions(tc.extension.Namespace).Create(context.TODO(), &tc.extension, metav1.CreateOptions{})
			w.Queue <- ExtensionEvent{
				Extension: &tc.extension,
				Operation: ExtensionEventOperationAdd,
			}
			res := <-w.resultChan
			for _, msg := range res.messages {
				log.Info(msg)
			}
			for _, err := range res.errors {
				if !res.successful && !tc.expectedError {
					log.Fatalf("expected no error but got: %s", err)
				} else {
					log.Infof("%s", err)
				}
			}

			for _, event := range tc.events {
				ext := event.Extension.DeepCopy()
				updatedExtension, err := w.client.ServiceMeshExtensions(tc.extension.Namespace).Get(context.TODO(), tc.extension.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("failed to Get() extension: %s", err)
				}
				ext.Status = *updatedExtension.Status.DeepCopy()
				switch event.Operation {
				case ExtensionEventOperationAdd:
					w.client.ServiceMeshExtensions(event.Extension.Namespace).Create(context.TODO(), ext, metav1.CreateOptions{})
				case ExtensionEventOperationUpdate:
					w.client.ServiceMeshExtensions(event.Extension.Namespace).Update(context.TODO(), ext, metav1.UpdateOptions{})
				case ExtensionEventOperationDelete:
					w.client.ServiceMeshExtensions(event.Extension.Namespace).Delete(context.TODO(), ext.Name, metav1.DeleteOptions{})
				}
				w.Queue <- ExtensionEvent{
					Extension: ext,
					Operation: event.Operation,
				}
				res := <-w.resultChan
				for _, msg := range res.messages {
					log.Info(msg)
				}
				for _, err := range res.errors {
					if !res.successful && !tc.expectedError {
						log.Fatalf("expected no error but got: %s", err)
					} else {
						log.Infof("%s", err)
					}
				}
			}
			stopChan <- struct{}{}
			updatedExtension, err := w.client.ServiceMeshExtensions(tc.extension.Namespace).Get(context.TODO(), tc.extension.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to Get() extension: %s", err)
			}
			// ignore Deployment.URL because it contains a random UUID
			if !cmp.Equal(tc.expectedStatus, updatedExtension.Status, cmpopts.IgnoreFields(v1alpha1.DeploymentStatus{}, "URL")) {
				t.Fatalf("comparison failed -got +want: %s", cmp.Diff(tc.expectedStatus, updatedExtension.Status, cmpopts.IgnoreFields(v1alpha1.DeploymentStatus{}, "URL")))
			}
			if !cmp.Equal(tc.expectedStatus, v1alpha1.ServiceMeshExtensionStatus{}) {
				// validate URL
				url, err := url.Parse(updatedExtension.Status.Deployment.URL)
				if err != nil {
					t.Fatalf("failed to parse baseURL: %s", err)
				}
				if fmt.Sprintf("%s://%s", url.Scheme, url.Host) != baseURL {
					t.Fatalf("generated base URL path is invalid: %s", updatedExtension.Status.Deployment.URL)
				}
				if _, err := uuid.Parse(strings.TrimLeft(url.Path, "/")); err != nil {
					t.Fatalf("generated URL path is invalid: %s", updatedExtension.Status.Deployment.URL)
				}
			}
		})
	}
}

func createWorker(tmpDir string, clientset *fake.Clientset) *Worker {
	return &Worker{
		baseURL:        baseURL,
		client:         clientset.MaistraV1alpha1(),
		mut:            sync.Mutex{},
		pullStrategy:   &fakestrategy.PullStrategy{},
		serveDirectory: tmpDir,
		Queue:          make(chan ExtensionEvent),
		resultChan:     make(chan workerResult, 100),
		enableLogger:   false,
	}
}
