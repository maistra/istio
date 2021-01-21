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
	"net/url"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/google/uuid"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/mec/pkg/model"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	v1alpha1client "istio.io/istio/pkg/servicemesh/client/v1alpha1/clientset/versioned/typed/servicemesh/v1alpha1"
	"istio.io/pkg/log"
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

	client       v1alpha1client.MaistraV1alpha1Interface
	stopChan     <-chan struct{}
	resultChan   chan workerResult
	Queue        chan ExtensionEvent
	enableLogger bool

	mut sync.Mutex
}

type workerResult struct {
	successful bool
	errors     []error
	messages   []string
}

func (r *workerResult) Fail() {
	r.successful = false
}

func (r *workerResult) AddMessage(msg string) {
	r.messages = append(r.messages, msg)
}

func (r *workerResult) AddError(err error) {
	r.errors = append(r.errors, err)
}

func NewWorker(config *rest.Config, pullStrategy model.ImagePullStrategy, baseURL, serveDirectory string) (*Worker, error) {
	client, err := v1alpha1client.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client from config: %v", err)
	}

	return &Worker{
		client:         client,
		Queue:          make(chan ExtensionEvent, 100),
		resultChan:     make(chan workerResult, 100),
		pullStrategy:   pullStrategy,
		baseURL:        baseURL,
		serveDirectory: serveDirectory,
	}, nil
}

func (w *Worker) processEvent(event ExtensionEvent) {
	result := workerResult{
		errors:     []error{},
		messages:   []string{},
		successful: true,
	}
	extension := event.Extension
	result.AddMessage("Processing " + extension.Namespace + "/" + extension.Name)

	if event.Operation == ExtensionEventOperationDelete {
		if len(extension.Status.Deployment.URL) > len(w.baseURL) {
			id := extension.Status.Deployment.URL[len(w.baseURL):]
			filename := path.Join(w.serveDirectory, id)
			os.Remove(filename)
		}
		return
	}
	imageRef := model.StringToImageRef(extension.Spec.Image)

	if imageRef == nil {
		result.AddError(fmt.Errorf("failed to parse spec.image: '%s'", extension.Spec.Image))
		result.Fail()
		w.resultChan <- result
		return
	}

	var img model.Image
	var err error
	if imageRef.SHA256 != "" {
		img, err = w.pullStrategy.GetImage(imageRef)
		if err != nil {
			result.AddError(fmt.Errorf("failed to check whether image is already present: %s", err))
		}
	}
	if img == nil {
		result.AddMessage(fmt.Sprintf("Image %s not present. Pulling", imageRef.String()))
		img, err = w.pullStrategy.PullImage(imageRef)
		if err != nil {
			result.AddError(fmt.Errorf("failed to pull image %s: %v", imageRef.String(), err))
			result.Fail()
			w.resultChan <- result
			return
		}
	}
	var id string
	containerImageChanged := false

	if strings.HasPrefix(extension.Status.Deployment.URL, w.baseURL) {
		url, err := url.Parse(extension.Status.Deployment.URL)
		if err != nil {
			result.AddError(fmt.Errorf("failed to parse status.deployment.url: %s", err))
			result.Fail()
			w.resultChan <- result
		}
		id = path.Base(url.Path)
	}

	if img.SHA256() != extension.Status.Deployment.ContainerSHA256 {
		// if container sha changed, re-generate UUID
		containerImageChanged = true
		if id != "" {
			err := os.Remove(path.Join(w.serveDirectory, id))
			if err != nil {
				result.AddError(fmt.Errorf("failed to delete existing wasm module: %s", err))
			}
		}

		wasmUUID, err := uuid.NewRandom()
		if err != nil {
			result.AddError(fmt.Errorf("failed to generate new UUID: %v", err))
			result.Fail()
			w.resultChan <- result
			return
		}
		id = wasmUUID.String()
	}

	filename := path.Join(w.serveDirectory, id)
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		err = img.CopyWasmModule(filename)
		if err != nil {
			result.AddError(fmt.Errorf("failed to extract wasm module: %v", err))
			result.Fail()
			w.resultChan <- result
			return
		}
	}

	sha, err := generateSHA256(filename)
	if err != nil {
		result.AddError(fmt.Errorf("failed to generate sha256 of wasm module: %v", err))
		result.Fail()
		w.resultChan <- result
		return
	}
	result.AddMessage(fmt.Sprintf("WASM module SHA256 is %s", sha))

	filePath, err := url.Parse(id)
	if err != nil {
		result.AddError(fmt.Errorf("failed to parse new UUID '%s' as URL path: %s", id, err))
		result.Fail()
		w.resultChan <- result

	}
	baseURL, err := url.Parse(w.baseURL)
	if err != nil {
		result.AddError(fmt.Errorf("failed to parse baseURL: %s", err))
		result.Fail()
		w.resultChan <- result
	}

	extension.Status.Deployment.SHA256 = sha
	extension.Status.Deployment.ContainerSHA256 = img.SHA256()
	extension.Status.Deployment.URL = baseURL.ResolveReference(filePath).String()
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
		result.AddMessage("Skipping status update")
		w.resultChan <- result
		return
	}
	extension.Status.ObservedGeneration = extension.Generation
	_, err = w.client.ServiceMeshExtensions(extension.Namespace).UpdateStatus(context.TODO(), extension, v1.UpdateOptions{})
	if err != nil {
		result.AddError(fmt.Errorf("failed to update status of extension: %v", err))
		result.Fail()
	}
	w.resultChan <- result
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
				log.Info("Stopping worker")
				return
			}
		}
	}()
	if w.enableLogger {
		go func() {
			select {
			case result := <-w.resultChan:
				for _, msg := range result.messages {
					log.Info(msg)
				}
				for _, err := range result.errors {
					if !result.successful {
						log.Errorf("%s", err)
					} else {
						log.Warnf("%s", err)
					}
				}
			case <-w.stopChan:
				return
			}
		}()
	}
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
