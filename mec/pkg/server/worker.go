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

var workerlog = log.RegisterScope("worker", "Worker function", 0)

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

func (op ExtensionEventOperation) String() string {
	switch op {
	case ExtensionEventOperationAdd:
		return "ADD"
	case ExtensionEventOperationDelete:
		return "DEL"
	case ExtensionEventOperationUpdate:
		return "UPD"
	default:
		return "UNKNOWN"
	}
}

type Worker struct {
	baseURL        string
	serveDirectory string

	pullStrategy model.ImagePullStrategy

	client   v1alpha1client.ServicemeshV1alpha1Interface
	stopChan <-chan struct{}
	Queue    chan ExtensionEvent

	mut sync.Mutex
}

func NewWorker(config *rest.Config, pullStrategy model.ImagePullStrategy, baseURL, serveDirectory string) (*Worker, error) {
	client, err := v1alpha1client.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client from config: %v", err)
	}

	return &Worker{
		client:         client,
		Queue:          make(chan ExtensionEvent, 100),
		pullStrategy:   pullStrategy,
		baseURL:        baseURL,
		serveDirectory: serveDirectory,
	}, nil
}

func (w *Worker) processEvent(event ExtensionEvent) {
	extension := event.Extension
	workerlog.Debugf("Event %s arrived for %s/%s", event.Operation, extension.Namespace, extension.Name)
	workerlog.Debugf("Extension object: %+v", extension)

	if event.Operation == ExtensionEventOperationDelete {
		if len(extension.Status.Deployment.URL) > len(w.baseURL) {
			id := extension.Status.Deployment.URL[len(w.baseURL):]
			filename := path.Join(w.serveDirectory, id)
			os.Remove(filename)
		}
		return
	}

	// MAISTRA-2252
	if event.Operation == ExtensionEventOperationUpdate &&
		extension.Status.Deployment.Ready &&
		extension.Status.ObservedGeneration == extension.Generation {
		workerlog.Debug("Skipping update, current extension is up to date.")
		return
	}

	imageRef := model.StringToImageRef(extension.Spec.Image)
	if imageRef == nil {
		workerlog.Errorf("failed to parse spec.image: '%s'", extension.Spec.Image)
		return
	}
	workerlog.Debugf("Image Ref: %+v", imageRef)

	var img model.Image
	var err error
	// Only happens if image is in the format: quay.io/repo/image@sha256:...
	if imageRef.SHA256 != "" {
		workerlog.Debug("Trying to get an already pulled image")
		// FIXME: GetImage() is broken and always returns an error
		// MAISTRA-2249
		img, err = w.pullStrategy.GetImage(imageRef)
		if err != nil {
			workerlog.Errorf("failed to check whether image is already present: %v", err)
		}
	}
	if img == nil {
		workerlog.Debugf("Image %s not present. Pulling", imageRef.String())
		img, err = w.pullStrategy.PullImage(imageRef, extension.Spec.ImagePullPolicy, extension.Spec.ImagePullSecrets)
		if err != nil {
			workerlog.Errorf("failed to pull image %s: %v", imageRef.String(), err)
			return
		}
	}
	var id string
	containerImageChanged := false

	if strings.HasPrefix(extension.Status.Deployment.URL, w.baseURL) {
		workerlog.Debug("Image is already in the http server, retrieving its ID")
		url, err := url.Parse(extension.Status.Deployment.URL)
		if err != nil {
			workerlog.Errorf("failed to parse status.deployment.url: %s", err)
			return
		}
		id = path.Base(url.Path)
		workerlog.Debugf("Got ID = %s", id)
	}

	workerlog.Debugf("Checking if SHA's match: %s - %s", img.SHA256(), extension.Status.Deployment.ContainerSHA256)
	if img.SHA256() != extension.Status.Deployment.ContainerSHA256 {
		workerlog.Debug("They differ, setting containerImageChanged = true")
		// if container sha changed, re-generate UUID
		containerImageChanged = true
		if id != "" {
			err := os.Remove(path.Join(w.serveDirectory, id))
			if err != nil {
				workerlog.Errorf("failed to delete existing wasm module: %s", err)
			}
		}

		wasmUUID, err := uuid.NewRandom()
		if err != nil {
			workerlog.Errorf("failed to generate new UUID: %v", err)
			return
		}
		id = wasmUUID.String()
		workerlog.Debugf("Created a new id for this image: %s", id)
	}

	filename := path.Join(w.serveDirectory, id)
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		workerlog.Debugf("Copying the extension to the web server location: %s", filename)
		err = img.CopyWasmModule(filename)
		if err != nil {
			workerlog.Errorf("failed to extract wasm module: %v", err)
			return
		}
	}

	sha, err := generateSHA256(filename)
	if err != nil {
		workerlog.Errorf("failed to generate sha256 of wasm module: %v", err)
		return
	}
	workerlog.Debugf("WASM module SHA256 is %s", sha)

	filePath, err := url.Parse(id)
	if err != nil {
		workerlog.Errorf("failed to parse new UUID '%s' as URL path: %s", id, err)
	}
	baseURL, err := url.Parse(w.baseURL)
	if err != nil {
		workerlog.Errorf("failed to parse baseURL: %s", err)
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
		workerlog.Debug("Skipping status update")
		return
	}

	extension.Status.ObservedGeneration = extension.Generation
	workerlog.Debugf("Updating extension status with: %v", extension.Status)
	_, err = w.client.ServiceMeshExtensions(extension.Namespace).UpdateStatus(context.TODO(), extension, v1.UpdateOptions{})
	if err != nil {
		workerlog.Errorf("failed to update status of extension: %v", err)
	}
}

func (w *Worker) Start(stopChan <-chan struct{}) {
	w.mut.Lock()
	defer w.mut.Unlock()

	if w.stopChan != nil {
		return
	}
	w.stopChan = stopChan
	workerlog.Info("Starting worker")
	go func() {
		for {
			select {
			case event := <-w.Queue:
				w.processEvent(event)
			case <-w.stopChan:
				workerlog.Info("Stopping worker")
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
