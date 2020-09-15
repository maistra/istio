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
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"sync"

	"github.com/google/uuid"

	"istio.io/istio/mec/pkg/model"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	v1alpha1client "istio.io/istio/pkg/servicemesh/client/v1alpha1/clientset/versioned/typed/servicemesh/v1alpha1"
	"istio.io/pkg/log"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const (
	ExtensionEventOperationAdd    = 0
	ExtensionEventOperationDelete = 1
	ExtensionEventOperationUpdate = 2
)

type ExtensionEvent struct {
	Extension *v1alpha1.ServiceMeshExtension
	Operation ExtensionEventOperation
}

type ExtensionEventOperation int

type Worker struct {
	baseURL        string
	serveDirectory string

	pullStrategy model.ImagePullStrategy

	config   *rest.Config
	stopChan <-chan struct{}
	Queue    chan ExtensionEvent

	mut sync.Mutex
}

func NewWorker(config *rest.Config, pullStrategy model.ImagePullStrategy, baseURL, serveDirectory string) *Worker {
	return &Worker{
		config:         config,
		Queue:          make(chan ExtensionEvent),
		pullStrategy:   pullStrategy,
		baseURL:        baseURL,
		serveDirectory: serveDirectory,
	}
}

func (w *Worker) processEvent(event ExtensionEvent) {
	extension := event.Extension

	if event.Operation == ExtensionEventOperationDelete {
		if len(extension.Status.Deployment.URL) > len(w.baseURL) {
			id := extension.Status.Deployment.URL[len(w.baseURL):]
			filename := path.Join(w.serveDirectory, id)
			os.Remove(filename)
		}
		return
	}
	imageRef := model.StringToImageRef(extension.Spec.Image)

	// don't overwrite a user-provided sha
	if imageRef.SHA256 == "" {
		imageRef.SHA256 = extension.Status.Deployment.ContainerSHA256
	}

	img, err := w.pullStrategy.GetImage(imageRef)
	if err != nil {
		log.Infof("Image %s not present. Pulling", imageRef.String())
		img, err = w.pullStrategy.PullImage(imageRef)
		if err != nil {
			log.Errorf("Error pulling image %s: %v", imageRef.String(), err)
			return
		}
	}
	var id string
	containerImageChanged := false
	if img.SHA256() != extension.Status.Deployment.ContainerSHA256 {
		// if container sha changed, re-generate UUID
		containerImageChanged = true
		// delete old file
		if len(extension.Status.Deployment.URL) > len(w.baseURL) {
			id := extension.Status.Deployment.URL[len(w.baseURL):]
			filename := path.Join(w.serveDirectory, id)
			os.Remove(filename)
		}

		wasmUUID, err := uuid.NewRandom()
		if err != nil {
			log.Errorf("Error: %v", err)
		}
		id = wasmUUID.String()
	} else {
		id = extension.Status.Deployment.URL[len(w.baseURL):]
	}
	filename := path.Join(w.serveDirectory, id)

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		err = img.CopyWasmModule(filename)
		if err != nil {
			log.Errorf("Error: %v", err)
		}
	}

	sha, err := generateSHA256(filename)
	if err != nil {
		log.Errorf("Error: %v", err)
	}
	log.Infof("SHA256 is %s", sha)
	extension.Status.Deployment.SHA256 = sha
	extension.Status.Deployment.ContainerSHA256 = img.SHA256()
	extension.Status.Deployment.URL = w.baseURL + "/" + id
	extension.Status.Deployment.Ready = true

	manifest := img.GetManifest()

	// apply defaults from manifest for phase and priority
	if extension.Spec.Phase == nil {
		extension.Status.Phase = manifest.Phase
	} else {
		extension.Status.Phase = *extension.Spec.Phase
	}
	if extension.Spec.Priority == nil {
		extension.Status.Priority = manifest.Priority
	} else {
		extension.Status.Priority = *extension.Spec.Priority
	}

	if !containerImageChanged && extension.Generation > 0 && extension.Status.ObservedGeneration == extension.Generation {
		log.Info("Skipping status update")
		return
	}
	extension.Status.ObservedGeneration = extension.Generation

	client, err := v1alpha1client.NewForConfig(w.config)
	if err != nil {
		log.Errorf("Error: %v", err)
	}
	_, err = client.ServiceMeshExtensions(extension.Namespace).UpdateStatus(context.TODO(), extension, v1.UpdateOptions{})
	if err != nil {
		log.Errorf("Error: %v", err)
	}
}

func (w *Worker) Start(stopChan <-chan struct{}) {
	w.mut.Lock()
	defer w.mut.Unlock()

	if w.stopChan != nil {
		return
	}
	w.stopChan = stopChan
	log.Info("Starting worker")
	go func() {
		for {
			select {
			case event := <-w.Queue:
				w.processEvent(event)
			case <-w.stopChan:
				log.Info("Interrupt!")
				return
			}
		}
	}()
}

func generateSHA256(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
