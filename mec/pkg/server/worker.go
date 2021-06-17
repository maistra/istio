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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	v1client "maistra.io/api/client/versioned/typed/core/v1"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/mec/pkg/model"
	"istio.io/pkg/log"
)

var workerlog = log.RegisterScope("worker", "Worker function", 0)

const (
	ExtensionEventOperationAdd    = 0
	ExtensionEventOperationDelete = 1
	ExtensionEventOperationUpdate = 2
)

type ExtensionEvent struct {
	Extension *v1.ServiceMeshExtension
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

	client       v1client.CoreV1Interface
	errorChannel chan error
	stopChan     <-chan struct{}
	Queue        chan ExtensionEvent

	mut sync.Mutex
}

func NewWorker(config *rest.Config, pullStrategy model.ImagePullStrategy, baseURL, serveDirectory string, errorChannel chan error) (*Worker, error) {
	client, err := v1client.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client from config: %v", err)
	}

	return &Worker{
		client:         client,
		Queue:          make(chan ExtensionEvent, 100),
		pullStrategy:   pullStrategy,
		baseURL:        baseURL,
		serveDirectory: serveDirectory,
		errorChannel:   errorChannel,
	}, nil
}

func (w *Worker) processEvent(event ExtensionEvent) error {
	extension := event.Extension
	workerlog.Debugf("Event %s arrived for %s/%s", event.Operation, extension.Namespace, extension.Name)

	if event.Operation == ExtensionEventOperationDelete {
		if len(extension.Status.Deployment.URL) > len(w.baseURL) {
			id := extension.Status.Deployment.URL[len(w.baseURL):]
			filename := path.Join(w.serveDirectory, id)
			os.Remove(filename)
		}
		return nil
	}

	if event.Operation == ExtensionEventOperationUpdate &&
		extension.Status.Deployment.Ready &&
		extension.Status.ObservedGeneration == extension.Generation {
		workerlog.Debug("Skipping update, current extension is up to date.")
		return nil
	}

	imageRef := model.StringToImageRef(extension.Spec.Image)
	if imageRef == nil {
		message := fmt.Sprintf("failed to parse spec.image: %q", extension.Spec.Image)
		if err := w.updateStatusNotReady(extension, message); err != nil {
			workerlog.Error(err)
		}
		return fmt.Errorf(message)
	}
	workerlog.Debugf("Image Ref: %+v", imageRef)

	var img model.Image
	var err error
	// Only happens if image is in the format: quay.io/repo/image@sha256:...
	if imageRef.SHA256 != "" {
		workerlog.Debug("Trying to get an already pulled image")
		img, err = w.pullStrategy.GetImage(imageRef)
		if err != nil {
			workerlog.Errorf("failed to check whether image is already present: %v", err)
		}
	}
	if img == nil {
		workerlog.Debugf("Image %s not present. Pulling", imageRef.String())

		pullPolicy := extension.Spec.ImagePullPolicy
		if !extension.Status.Deployment.Ready {
			// Ready is false, force pull the image again, it might be the image stream is stuck
			pullPolicy = corev1.PullAlways
		}

		img, err = w.pullStrategy.PullImage(
			imageRef,
			extension.Namespace,
			pullPolicy,
			extension.Spec.ImagePullSecrets,
			extension.Name,
			extension.UID)
		if err != nil {
			message := fmt.Sprintf("failed to pull image %q: %v", imageRef.String(), err)
			if err := w.updateStatusNotReady(extension, message); err != nil {
				workerlog.Error(err)
			}
			return fmt.Errorf(message)
		}
	}
	var id string
	containerImageChanged := false

	if strings.HasPrefix(extension.Status.Deployment.URL, w.baseURL) {
		workerlog.Debug("Image is already in the http server, retrieving its ID")
		url, err := url.Parse(extension.Status.Deployment.URL)
		if err != nil {
			message := fmt.Sprintf("failed to parse status.deployment.url: %v", err)
			if err := w.updateStatusNotReady(extension, message); err != nil {
				workerlog.Error(err)
			}
			return fmt.Errorf(message)
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
			message := fmt.Sprintf("failed to generate new UUID: %v", err)
			if err := w.updateStatusNotReady(extension, message); err != nil {
				workerlog.Error(err)
			}
			return fmt.Errorf(message)
		}
		id = wasmUUID.String()
		workerlog.Debugf("Created a new id for this image: %s", id)
	}

	filename := path.Join(w.serveDirectory, id)
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		workerlog.Debugf("Copying the extension to the web server location: %s", filename)
		err = img.CopyWasmModule(filename)
		if err != nil {
			message := fmt.Sprintf("failed to extract wasm module: %v", err)
			if err := w.updateStatusNotReady(extension, message); err != nil {
				workerlog.Error(err)
			}
			return fmt.Errorf(message)
		}
	}

	sha, err := generateSHA256(filename)
	if err != nil {
		message := fmt.Sprintf("failed to generate sha256 of wasm module: %v", err)
		if err := w.updateStatusNotReady(extension, message); err != nil {
			workerlog.Error(err)
		}
		return fmt.Errorf(message)
	}
	workerlog.Debugf("WASM module SHA256 is %s", sha)

	filePath, err := url.Parse(id)
	if err != nil {
		message := fmt.Sprintf("failed to parse new UUID %q as URL path: %v", id, err)
		if err := w.updateStatusNotReady(extension, message); err != nil {
			workerlog.Error(err)
		}
		return fmt.Errorf(message)
	}
	baseURL, err := url.Parse(w.baseURL)
	if err != nil {
		message := fmt.Sprintf("failed to parse baseURL: %v", err)
		if err := w.updateStatusNotReady(extension, message); err != nil {
			workerlog.Error(err)
		}
		return fmt.Errorf(message)
	}

	extension.Status.Deployment.SHA256 = sha
	extension.Status.Deployment.ContainerSHA256 = img.SHA256()
	extension.Status.Deployment.URL = baseURL.ResolveReference(filePath).String()
	extension.Status.Deployment.Ready = true
	extension.Status.Deployment.Message = ""

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

	// TODO(jwendell): Is this necessary?
	if !containerImageChanged && extension.Generation > 0 && extension.Status.ObservedGeneration == extension.Generation {
		workerlog.Debug("Skipping status update")
		return nil
	}

	extension.Status.ObservedGeneration = extension.Generation
	return w.updateStatus(extension)
}

func (w *Worker) updateStatus(extension *v1.ServiceMeshExtension) error {
	workerlog.Debugf("Updating extension status with: %+v", extension.Status)
	if _, err := w.client.ServiceMeshExtensions(extension.Namespace).UpdateStatus(context.TODO(), extension, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update status of extension: %v", err)
	}

	return nil
}

func (w *Worker) updateStatusNotReady(extension *v1.ServiceMeshExtension, message string) error {
	// Reset all fields, including ready=false
	extension.Status.Deployment = v1.DeploymentStatus{}
	extension.Status.Deployment.Message = message

	return w.updateStatus(extension)
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
				if err := w.processEvent(event); err != nil {
					workerlog.Error(err)
					if w.errorChannel != nil {
						go func() { w.errorChannel <- err }()
					}
				}
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
