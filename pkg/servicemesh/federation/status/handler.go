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
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/pkg/log"
)

const (
	// used to prune old remote connection statuses from the status, e.g. if a
	// pod was deleted, etc.
	staleRemoteStatusDuration = 5 * time.Minute
)

func newHandler(manager *manager, mesh types.NamespacedName) *handler {
	return &handler{
		manager:        manager,
		mesh:           mesh,
		logger:         manager.logger.WithLabels("peer", mesh.String()),
		discovery:      map[string]*v1.DiscoveryRemoteStatus{},
		exports:        map[v1.ServiceKey]v1.PeerServiceMapping{},
		exportsStatus:  []v1.PeerServiceMapping{},
		imports:        map[string]v1.PeerServiceMapping{},
		importsStatus:  []v1.PeerServiceMapping{},
		discoveryDirty: true,
		watchDirty:     true,
		exportsDirty:   true,
		importsDirty:   true,
	}
}

type handler struct {
	mu      sync.Mutex
	manager *manager
	mesh    types.NamespacedName
	logger  *log.Scope

	discovery map[string]*v1.DiscoveryRemoteStatus
	exports   map[v1.ServiceKey]v1.PeerServiceMapping
	imports   map[string]v1.PeerServiceMapping

	discoveryDirty bool
	exportsDirty   bool
	importsDirty   bool
	watchDirty     bool

	discoveryStatus v1.PeerDiscoveryStatus
	exportsStatus   []v1.PeerServiceMapping
	importsStatus   []v1.PeerServiceMapping
}

var _ Handler = (*handler)(nil)

// Outbound connections
func (h *handler) WatchInitiated() {
	h.logger.Debugf("%s.WatchInitiated()", h.mesh)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.discoveryStatus.Watch.Connected = false
	h.discoveryStatus.Watch.LastConnected = metav1.Now()

	h.watchDirty = true

	// we don't flush on initiation, as we expect either a Watching() or
	// WatchTerminated() immediately following this
}

func (h *handler) Watching() {
	h.logger.Debugf("%s.Watching()", h.mesh)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.discoveryStatus.Watch.Connected = true

		h.watchDirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for ServiceMeshPeer %s: %s", h.mesh, err)
	}
}

func (h *handler) WatchEventReceived() {
	h.logger.Debugf("%s.WatchEventReceived()", h.mesh)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.discoveryStatus.Watch.LastEvent = metav1.Now()

	h.watchDirty = true
}

func (h *handler) FullSyncComplete() {
	h.logger.Debugf("%s.FullSyncComplete()", h.mesh)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.discoveryStatus.Watch.LastFullSync = metav1.Now()

		h.watchDirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for ServiceMeshPeer %s: %s", h.mesh, err)
	}
}

func (h *handler) WatchTerminated(status string) {
	h.logger.Debugf("%s.WatchTerminated(%s)", h.mesh, status)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		if h.discoveryStatus.Watch.Connected {
			// only update the disconnect time if we successfully connected
			h.discoveryStatus.Watch.LastDisconnect = metav1.Now()
		}
		h.discoveryStatus.Watch.Connected = false
		h.discoveryStatus.Watch.LastDisconnectStatus = status

		h.watchDirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for ServiceMeshPeer %s: %s", h.mesh, err)
	}
}

// Inbound connections
func (h *handler) RemoteWatchAccepted(source string) {
	h.logger.Debugf("%s.RemoteWatchAccepted(%s)", h.mesh, source)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		remoteStatus, ok := h.discovery[source]
		if ok {
			h.logger.Debugf("RemoteWatchAccepted called when watch status already exists: %s", source)
		} else {
			remoteStatus = &v1.DiscoveryRemoteStatus{
				Source: source,
			}
			h.discovery[source] = remoteStatus
		}
		remoteStatus.Connected = true
		remoteStatus.LastConnected = metav1.Now()

		h.discoveryDirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for ServiceMeshPeer %s: %s", h.mesh, err)
	}
}

func (h *handler) WatchEventSent(source string) {
	h.logger.Debugf("%s.WatchEventSent(%s)", h.mesh, source)

	h.mu.Lock()
	defer h.mu.Unlock()

	if remoteStatus, ok := h.discovery[source]; !ok {
		h.logger.Debugf("WatchEventSent called when no status exists: %s", source)
	} else {
		remoteStatus.LastEvent = metav1.Now()
		h.discoveryDirty = true
	}
}

func (h *handler) FullSyncSent(source string) {
	h.logger.Debugf("%s.FullSyncSent(%s)", h.mesh, source)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		if remoteStatus, ok := h.discovery[source]; !ok {
			h.logger.Debugf("skipping FullSyncSent event: no status for source %s", source)
		} else {
			remoteStatus.LastFullSync = metav1.Now()
			h.discoveryDirty = true
		}
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for ServiceMeshPeer %s: %s", h.mesh, err)
	}
}

func (h *handler) RemoteWatchTerminated(source string) {
	h.logger.Debugf("%s.RemoteWatchTerminated(%s)", h.mesh, source)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		remoteStatus, ok := h.discovery[source]
		if !ok {
			h.logger.Debugf("RemoteWatchTerminated called when no status exists: %s", source)
			return
		}

		remoteStatus.Connected = false
		remoteStatus.LastDisconnect = metav1.Now()

		h.discoveryDirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for ServiceMeshPeer %s: %s", h.mesh, err)
	}
}

func statusServiceKeyFor(service model.ServiceKey) v1.ServiceKey {
	return v1.ServiceKey{
		Name:      service.Name,
		Namespace: service.Namespace,
		Hostname:  service.Hostname,
	}
}

func statusMappingFor(service model.ServiceKey, exportedName string) v1.PeerServiceMapping {
	return v1.PeerServiceMapping{
		LocalService: statusServiceKeyFor(service),
		ExportedName: exportedName,
	}
}

// Exports
func (h *handler) ExportAdded(service model.ServiceKey, exportedName string) {
	h.logger.Debugf("%s.ExportAdded(%+v, %s)", h.mesh, service, exportedName)

	h.mu.Lock()
	defer h.mu.Unlock()

	mapping := statusMappingFor(service, exportedName)
	if existing, ok := h.exports[mapping.LocalService]; ok {
		h.logger.Debugf("ExportAdded called when export mapping already exists: existing=%+v, new=%+v", existing, mapping)
		if reflect.DeepEqual(existing, mapping) {
			return
		}
	}
	h.exports[mapping.LocalService] = mapping
	h.exportsDirty = true
}

func (h *handler) ExportUpdated(service model.ServiceKey, exportedName string) {
	h.logger.Debugf("%s.ExportUpdated(%+v, %s)", h.mesh, service, exportedName)

	h.mu.Lock()
	defer h.mu.Unlock()

	mapping := statusMappingFor(service, exportedName)
	if existing, ok := h.exports[mapping.LocalService]; !ok {
		h.logger.Debugf("ExportUpdated called when export mapping does not exist: %+v", mapping.LocalService)
	} else if reflect.DeepEqual(existing, mapping) {
		return
	}
	h.exports[mapping.LocalService] = mapping
	h.exportsDirty = true
}

func (h *handler) ExportRemoved(service model.ServiceKey) {
	h.logger.Debugf("%s.ExportRemoved(%+v)", h.mesh, service)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Debugf("h.exports: %+v", h.exports)
	key := statusServiceKeyFor(service)
	if _, ok := h.exports[key]; !ok {
		h.logger.Debugf("ExportRemoved called when export mapping does not exist: %+v", key)
		return
	}
	delete(h.exports, key)
	h.logger.Debugf("h.exports: %+v", h.exports)
	h.exportsDirty = true
}

// Imports
func (h *handler) ImportAdded(service model.ServiceKey, exportedName string) {
	h.logger.Debugf("%s.ImportAdded(%+v, %s)", h.mesh, service, exportedName)

	h.mu.Lock()
	defer h.mu.Unlock()

	mapping := statusMappingFor(service, exportedName)
	if existing, ok := h.imports[mapping.ExportedName]; ok {
		h.logger.Debugf("ImportAdded called when import mapping already exists: existing=%+v, new=%+v", existing, mapping)
		if reflect.DeepEqual(existing, mapping) {
			return
		}
	}
	h.imports[mapping.ExportedName] = mapping
	h.importsDirty = true
}

func (h *handler) ImportUpdated(service model.ServiceKey, exportedName string) {
	h.logger.Debugf("%s.ImportUpdated(%+v, %s)", h.mesh, service, exportedName)

	h.mu.Lock()
	defer h.mu.Unlock()

	mapping := statusMappingFor(service, exportedName)
	if existing, ok := h.imports[mapping.ExportedName]; !ok {
		h.logger.Debugf("ImportUpdated called when import mapping does not exist: %s", mapping.ExportedName)
	} else if reflect.DeepEqual(existing, mapping) {
		return
	}
	h.imports[mapping.ExportedName] = mapping
	h.importsDirty = true
}

func (h *handler) ImportRemoved(exportedName string) {
	h.logger.Debugf("%s.ImportRemoved(%s)", h.mesh, exportedName)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Debugf("h.imports: %v", h.imports)
	if _, ok := h.imports[exportedName]; !ok {
		h.logger.Debugf("ImportRemoved called when import mapping does not exist: %s", exportedName)
		return
	}
	delete(h.imports, exportedName)
	h.logger.Debugf("h.imports: %v", h.imports)
	h.importsDirty = true
}

func (h *handler) shouldPush() (bool, bool) {
	// only push exports/imports if we're the leader
	isLeader := h.manager.IsLeader()
	return h.watchDirty || h.discoveryDirty || (isLeader && (h.exportsDirty || h.importsDirty)), isLeader
}

func (h *handler) pruneOldRemotes() {
	var connections []string
	for connection, status := range h.discovery {
		if status.Connected || metav1.Now().Sub(status.LastDisconnect.Time) < staleRemoteStatusDuration {
			continue
		}
		h.logger.Debugf("removing stale remote watch status for connection from %s", connection)
		connections = append(connections, connection)
	}
	h.discoveryDirty = h.discoveryDirty || len(connections) > 0
	for _, connection := range connections {
		delete(h.discovery, connection)
	}
}

// Write status
func (h *handler) Flush() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneOldRemotes()
	push, isLeader := h.shouldPush()

	if !push {
		h.logger.Debugf("no status changes to push")
		return nil
	}

	// see if we need to update the export status
	if h.exportsDirty {
		var exports []v1.PeerServiceMapping
		for _, mapping := range h.exports {
			exports = append(exports, mapping)
		}
		sort.Slice(exports, func(i, j int) bool {
			diff := strings.Compare(exports[i].LocalService.Namespace, exports[j].LocalService.Namespace)
			if diff == 0 {
				diff = strings.Compare(exports[i].LocalService.Name, exports[j].LocalService.Name)
				if diff == 0 {
					// we really shouldn't ever get here, as there should never be an overlap of namespace/name
					diff = strings.Compare(exports[i].ExportedName, exports[j].ExportedName)
				}
			}
			return diff < 0
		})
		h.exportsStatus = exports
		h.exportsDirty = false
	}

	// see if we need to update the import status
	if h.importsDirty {
		var imports []v1.PeerServiceMapping
		for _, mapping := range h.imports {
			imports = append(imports, mapping)
		}
		sort.Slice(imports, func(i, j int) bool { return strings.Compare(imports[i].ExportedName, imports[j].ExportedName) < 0 })
		h.importsStatus = imports
		h.importsDirty = false
	}

	// see if we need to update the discovery status
	if h.discoveryDirty {
		var remoteStatuses []v1.DiscoveryRemoteStatus
		for _, status := range h.discovery {
			remoteStatuses = append(remoteStatuses, *status)
		}
		sort.Slice(remoteStatuses,
			func(i, j int) bool {
				return strings.Compare(remoteStatuses[i].Source, remoteStatuses[j].Source) < 0
			})
		h.discoveryStatus.Remotes = remoteStatuses
		h.discoveryDirty = false
	}

	peer, err := h.manager.rm.PeerInformer().Lister().ServiceMeshPeers(h.mesh.Namespace).Get(h.mesh.Name)
	if err != nil {
		if apierrors.IsGone(err) || apierrors.IsNotFound(err) {
			h.logger.Debugf("could not locate ServiceMeshPeer %s for status update", h.mesh)
			return nil
		}
		return err
	}

	newStatus := v1.ServiceMeshPeerStatus{}
	newStatus.DiscoveryStatus = peer.Status.DeepCopy().DiscoveryStatus
	if h.discoveryStatus.Watch.Connected {
		newStatus.DiscoveryStatus.Active = h.putDiscoveryStatus(newStatus.DiscoveryStatus.Active, h.discoveryStatus)
		newStatus.DiscoveryStatus.Inactive = h.removeDiscoveryStatus(newStatus.DiscoveryStatus.Inactive)
	} else {
		newStatus.DiscoveryStatus.Inactive = h.putDiscoveryStatus(newStatus.DiscoveryStatus.Inactive, h.discoveryStatus)
		newStatus.DiscoveryStatus.Active = h.removeDiscoveryStatus(newStatus.DiscoveryStatus.Active)
	}

	var allErrors []error
	if isLeader {
		// clean up deleted pods
		newStatus.DiscoveryStatus.Inactive = h.removeDeadPods(newStatus.DiscoveryStatus.Inactive)
		newStatus.DiscoveryStatus.Active = h.removeDeadPods(newStatus.DiscoveryStatus.Active)
		if err := h.patchExports(); err != nil && !(apierrors.IsGone(err) || apierrors.IsNotFound(err)) {
			allErrors = append(allErrors, err)
		}
		if err := h.patchImports(); err != nil && !(apierrors.IsGone(err) || apierrors.IsNotFound(err)) {
			allErrors = append(allErrors, err)
		}
	}

	if reflect.DeepEqual(peer.Status, newStatus) { // TODO: peer.Status may be stale, causing us to skip the update when we shouldn't
		h.logger.Debugf("no status updates for ServiceMeshPeer %s", h.mesh)
		return nil
	}

	updatedPeer := peer.DeepCopy()
	updatedPeer.Status = newStatus

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err = h.manager.rm.MaistraClientSet().FederationV1().ServiceMeshPeers(h.mesh.Namespace).UpdateStatus(context.TODO(), updatedPeer, metav1.UpdateOptions{})
		return err
	}); err != nil {
		if err != nil && !(apierrors.IsGone(err) || apierrors.IsNotFound(err)) {
			return utilerrors.NewAggregate(append(allErrors, err))
		}
	}
	h.watchDirty = false
	return utilerrors.NewAggregate(allErrors)
}

func (h *handler) patchExports() error {
	exportSet, err := h.manager.rm.ExportsInformer().Lister().ExportedServiceSets(h.mesh.Namespace).Get(h.mesh.Name)
	if err != nil {
		if (apierrors.IsGone(err) || apierrors.IsNotFound(err)) && len(h.exports) == 0 {
			h.logger.Debugf("could not locate ExportedServiceSet %s for status update", h.mesh)
			return nil
		}
		return err
	}

	exportedServices := h.exportsStatus
	if exportedServices == nil {
		exportedServices = []v1.PeerServiceMapping{}
	}
	newStatus := v1.ExportedServiceSetStatus{ExportedServices: exportedServices}

	h.logger.Debugf("exportSet.Status=%+v", exportSet.Status)
	h.logger.Debugf("newStatus=%+v", newStatus)
	if reflect.DeepEqual(exportSet.Status, newStatus) { // TODO: exportSet.Status may be stale, causing us to skip the update when we shouldn't
		h.logger.Debugf("no status updates for ExportedServiceSet %s", h.mesh)
		return nil
	}

	updatedExportSet := exportSet.DeepCopy()
	updatedExportSet.Status = newStatus

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err = h.manager.rm.MaistraClientSet().FederationV1().ExportedServiceSets(h.mesh.Namespace).
			UpdateStatus(context.TODO(), updatedExportSet, metav1.UpdateOptions{})
		return err
	}); err != nil {
		if !(apierrors.IsGone(err) || apierrors.IsNotFound(err)) {
			return err
		}
	}
	return nil
}

func (h *handler) patchImports() error {
	importSet, err := h.manager.rm.ImportsInformer().Lister().ImportedServiceSets(h.mesh.Namespace).Get(h.mesh.Name)
	if err != nil {
		if (apierrors.IsGone(err) || apierrors.IsNotFound(err)) && len(h.exports) == 0 {
			h.logger.Debugf("could not locate ImportedServiceSet %s for status update", h.mesh)
			return nil
		}
		return err
	}

	importedServices := h.importsStatus
	if importedServices == nil {
		importedServices = []v1.PeerServiceMapping{}
	}
	newStatus := v1.ImportedServiceSetStatus{ImportedServices: importedServices}

	h.logger.Debugf("importSet.Status=%+v", importSet.Status)
	h.logger.Debugf("newStatus=%+v", newStatus)
	if reflect.DeepEqual(importSet.Status, newStatus) { // TODO: importSet.Status may be stale, causing us to skip the update when we shouldn't
		h.logger.Debugf("no status updates for ImportedServiceSet %s", h.mesh)
		return nil
	}

	updatedImportSet := importSet.DeepCopy()
	updatedImportSet.Status = newStatus

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err = h.manager.rm.MaistraClientSet().FederationV1().ImportedServiceSets(h.mesh.Namespace).
			UpdateStatus(context.TODO(), updatedImportSet, metav1.UpdateOptions{})
		return err
	}); err != nil {
		if !(apierrors.IsGone(err) || apierrors.IsNotFound(err)) {
			return err
		}
	}
	return nil
}

func (h *handler) putDiscoveryStatus(statuses []v1.PodPeerDiscoveryStatus, newStatus v1.PeerDiscoveryStatus) []v1.PodPeerDiscoveryStatus {
	count := len(statuses)
	index := sort.Search(count, func(i int) bool { return statuses[i].Pod == h.manager.name.Name })
	if index < count {
		status := statuses[index]
		status.PeerDiscoveryStatus = newStatus
		statuses[index] = status
		return statuses
	}
	statuses = append(statuses, v1.PodPeerDiscoveryStatus{Pod: h.manager.name.Name, PeerDiscoveryStatus: newStatus})
	sort.Slice(statuses, func(i, j int) bool { return strings.Compare(statuses[i].Pod, statuses[j].Pod) < 0 })
	return statuses
}

func (h *handler) removeDiscoveryStatus(statuses []v1.PodPeerDiscoveryStatus) []v1.PodPeerDiscoveryStatus {
	count := len(statuses)
	index := sort.Search(count, func(i int) bool { return statuses[i].Pod == h.manager.name.Name })
	if index < count {
		return append(statuses[:index], statuses[index+1:]...)
	}
	return statuses
}

func (h *handler) removeDeadPods(statuses []v1.PodPeerDiscoveryStatus) []v1.PodPeerDiscoveryStatus {
	var filteredStatuses []v1.PodPeerDiscoveryStatus
	for index, status := range statuses {
		// XXX: this shouldn't be necessary, but patching isn't working correctly
		if index == 0 || statuses[index].Pod != statuses[index-1].Pod {
			if _, err := h.manager.rm.KubeClient().KubeInformer().Core().V1().Pods().Lister().Pods(h.manager.name.Namespace).Get(status.Pod); err == nil {
				filteredStatuses = append(filteredStatuses, status)
			}
		}
	}
	return filteredStatuses
}
