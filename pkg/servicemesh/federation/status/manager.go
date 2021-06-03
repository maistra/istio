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

package status

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	maistraclient "maistra.io/api/client/versioned"
	"maistra.io/api/core/v1alpha1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/pkg/log"
)

var federationStatusPatchMetadata strategicpatch.LookupPatchMeta

func init() {
	var err error
	federationStatusPatchMetadata, err = strategicpatch.NewPatchMetaFromStruct(&v1alpha1.FederationStatus{})
	if err != nil {
		panic("error creating strategicpatch.LookupPatchMeta for use with FederationStatus resources")
	}
}

type Manager interface {
	FederationAdded(mesh types.NamespacedName) Handler
	FederationDeleted(mesh types.NamespacedName)
	HandlerFor(mesh types.NamespacedName) Handler
	PushStatus() error
}

type Handler interface {
	// Outbound connections
	WatchInitiated()
	Watching()
	WatchEventReceived()
	FullSyncComplete()
	WatchTerminated(status string)

	// Inbound connections
	RemoteWatchAccepted(source string)
	WatchEventSent(source string)
	FullSyncSent(source string)
	RemoteWatchTerminated(source string)

	// Exports
	ExportAdded(service model.ServiceKey, exportedName string)
	ExportUpdated(service model.ServiceKey, exportedName string)
	ExportRemoved(service model.ServiceKey)

	// Imports
	ImportAdded(service model.ServiceKey, exportedName string)
	ImportUpdated(service model.ServiceKey, exportedName string)
	ImportRemoved(exportedName string)

	// Write status
	Flush() error
}

func NewManager(name types.NamespacedName, kubeClient kube.Client) (Manager, error) {
	cs, err := maistraclient.NewForConfig(kubeClient.RESTConfig())
	if err != nil {
		return nil, fmt.Errorf("error creating ClientSet for ServiceMesh: %v", err)
	}

	return newManager(name, kubeClient, cs), nil
}

func newManager(name types.NamespacedName, kubeClient kube.Client, cs maistraclient.Interface) *manager {
	return &manager{
		client:   kubeClient,
		cs:       cs,
		logger:   common.Logger.WithLabels("component", "federation-status"),
		name:     name,
		handlers: map[types.NamespacedName]*handler{},
		status:   v1alpha1.FederationStatusStatus{Meshes: []v1alpha1.FederationStatusDetails{}},
	}
}

type manager struct {
	mu     sync.Mutex
	client kube.Client
	cs     maistraclient.Interface
	logger *log.Scope

	name types.NamespacedName

	handlers map[types.NamespacedName]*handler

	status                v1alpha1.FederationStatusStatus
	missingCRDErrorLogged bool
}

var _ Manager = (*manager)(nil)

func (m *manager) FederationAdded(mesh types.NamespacedName) Handler {
	m.mu.Lock()
	defer m.mu.Unlock()

	if handler, exists := m.handlers[mesh]; exists {
		m.logger.Debugf("already have a handler for mesh %s", mesh)
		return handler
	}
	handler := newHandler(m, mesh)
	m.handlers[mesh] = handler
	return handler
}

func (m *manager) FederationDeleted(mesh types.NamespacedName) {
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if _, exists := m.handlers[mesh]; !exists {
			m.logger.Debugf("deleting unknown handler for mesh %s", mesh)
		}
		delete(m.handlers, mesh)
	}()

	if err := m.PushStatus(); err != nil {
		m.logger.Errorf("unexpected error updating FederationStatus for %s", m.name.String())
	}
}

func (m *manager) HandlerFor(mesh types.NamespacedName) Handler {
	handler, exists := m.handlers[mesh]
	if !exists {
		m.logger.Debugf("handler for mesh %s does not exist", mesh.String())
		return nil
	}
	return handler
}

func (m *manager) PushStatus() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newStatus := &v1alpha1.FederationStatus{
		Status: v1alpha1.FederationStatusStatus{Meshes: []v1alpha1.FederationStatusDetails{}},
	}
	for _, handler := range m.handlers {
		newStatus.Status.Meshes = append(newStatus.Status.Meshes, *handler.currentStatus())
	}
	sort.Slice(newStatus.Status.Meshes,
		func(i, j int) bool {
			return strings.Compare(newStatus.Status.Meshes[i].Mesh, newStatus.Status.Meshes[j].Mesh) < 0
		})

	newBytes, err := json.Marshal(newStatus)
	if err != nil {
		return err
	}
	oldBytes, err := json.Marshal(&v1alpha1.FederationStatus{
		Status: m.status,
	})
	if err != nil {
		return err
	}

	m.logger.Debugf("old bytes:\n%s\n", string(oldBytes))
	m.logger.Debugf("new bytes:\n%s\n", string(newBytes))

	patch, err := strategicpatch.CreateTwoWayMergePatchUsingLookupPatchMeta(oldBytes, newBytes, federationStatusPatchMetadata)
	if err != nil {
		return err
	}

	m.logger.Debugf("status patch:\n%s\n", string(patch))

	if len(patch) == 0 || string(patch) == "{}" {
		// nothing to patch
		return nil
	}

	fedStatus, err := m.cs.CoreV1alpha1().FederationStatuses(m.name.Namespace).
		Patch(context.TODO(), m.name.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err == nil {
		m.missingCRDErrorLogged = false
		m.status = *fedStatus.Status.DeepCopy()
		return nil
	} else if meta.IsNoMatchError(err) {
		// The FederationStatus resource is not installed.  We won't error in this case.
		if !m.missingCRDErrorLogged {
			m.logger.Errorf("status will not be updated: FederationStatus CRD is not installed")
			// prevent spamming the logs
			m.missingCRDErrorLogged = true
		}
		return nil
	} else if !(errors.IsGone(err) || errors.IsNotFound(err)) {
		return err
	}

	// if we get here, it means the resource doesn't exist
	pilotPod, err := m.client.KubeInformer().Core().V1().Pods().Lister().Pods(m.name.Namespace).Get(m.name.Name)
	if err != nil {
		// this should really _never_ happen unless the POD_* env is incorrect.  we're running in the pod!
		return err
	}
	controller := true
	newFedStatus := &v1alpha1.FederationStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.name.Name,
			Namespace: m.name.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       m.name.Name,
					UID:        pilotPod.GetUID(),
					Controller: &controller,
				},
			},
		},
		Status: *m.status.DeepCopy(),
	}
	_, err = m.cs.CoreV1alpha1().FederationStatuses(m.name.Namespace).
		Create(context.TODO(), newFedStatus, metav1.CreateOptions{})
	if err == nil {
		// we can't create status, so we need to apply it separately
		fedStatus, err = m.cs.CoreV1alpha1().FederationStatuses(m.name.Namespace).
			Patch(context.TODO(), m.name.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
		if err == nil {
			m.status = *fedStatus.Status.DeepCopy()
		}
	}
	return err
}
