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
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "maistra.io/api/core/v1"

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
		manager:   manager,
		mesh:      mesh,
		logger:    manager.logger.WithLabels("mesh", mesh.String()),
		discovery: map[string]*v1.DiscoveryRemoteStatus{},
		exports:   map[v1.ServiceKey]v1.MeshServiceMapping{},
		imports:   map[string]v1.MeshServiceMapping{},
		status: v1.FederationStatusDetails{
			Mesh:    mesh.String(),
			Exports: []v1.MeshServiceMapping{},
			Imports: []v1.MeshServiceMapping{},
		},
	}
}

type handler struct {
	mu      sync.Mutex
	manager *manager
	mesh    types.NamespacedName
	logger  *log.Scope

	discovery map[string]*v1.DiscoveryRemoteStatus
	exports   map[v1.ServiceKey]v1.MeshServiceMapping
	imports   map[string]v1.MeshServiceMapping

	discoveryDirty bool
	exportsDirty   bool
	importsDirty   bool
	dirty          bool

	status v1.FederationStatusDetails
}

var _ Handler = (*handler)(nil)

// Outbound connections
func (h *handler) WatchInitiated() {
	h.logger.Debugf("%s.WatchInitiated()", h.mesh)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.status.Discovery.Watch.Connected = false
	h.status.Discovery.Watch.LastConnected = metav1.Now()

	h.dirty = true

	// we don't flush on initiation, as we expect either a Watching() or
	// WatchTerminated() immediately following this
}

func (h *handler) Watching() {
	h.logger.Debugf("%s.Watching()", h.mesh)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.status.Discovery.Watch.Connected = true

		h.dirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for MeshFederation %s: %s", h.mesh, err)
	}
}

func (h *handler) WatchEventReceived() {
	h.logger.Debugf("%s.WatchEventReceived()", h.mesh)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.status.Discovery.Watch.LastEvent = metav1.Now()

	h.dirty = true
}

func (h *handler) FullSyncComplete() {
	h.logger.Debugf("%s.FullSyncComplete()", h.mesh)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.status.Discovery.Watch.LastFullSync = metav1.Now()

		h.dirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for MeshFederation %s: %s", h.mesh, err)
	}
}

func (h *handler) WatchTerminated(status string) {
	h.logger.Debugf("%s.WatchTerminated(%s)", h.mesh, status)

	func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		if h.status.Discovery.Watch.Connected {
			// only update the disconnect time if we successfully connected
			h.status.Discovery.Watch.LastDisconnect = metav1.Now()
		}
		h.status.Discovery.Watch.Connected = false
		h.status.Discovery.Watch.LastDisconnectStatus = status

		h.dirty = true
	}()

	if err := h.Flush(); err != nil {
		h.logger.Errorf("error updating status for MeshFederation %s: %s", h.mesh, err)
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
		h.logger.Errorf("error updating status for MeshFederation %s: %s", h.mesh, err)
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
		h.logger.Errorf("error updating status for MeshFederation %s: %s", h.mesh, err)
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
		h.logger.Errorf("error updating status for MeshFederation %s: %s", h.mesh, err)
	}
}

func statusServiceKeyFor(service model.ServiceKey) v1.ServiceKey {
	return v1.ServiceKey{
		Name:      service.Name,
		Namespace: service.Namespace,
		Hostname:  service.Hostname,
	}
}

func statusMappingFor(service model.ServiceKey, exportedName string) v1.MeshServiceMapping {
	return v1.MeshServiceMapping{
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

	key := statusServiceKeyFor(service)
	if _, ok := h.exports[key]; !ok {
		h.logger.Debugf("ExportRemoved called when export mapping does not exist: %+v", key)
		return
	}
	delete(h.exports, key)
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

	if _, ok := h.imports[exportedName]; !ok {
		h.logger.Debugf("ImportRemoved called when import mapping does not exist: %s", exportedName)
		return
	}
	delete(h.imports, exportedName)
	h.importsDirty = true
}

func (h *handler) isDirty() bool {
	return h.dirty || h.exportsDirty || h.importsDirty || h.discoveryDirty
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
	push := func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.pruneOldRemotes()
		return h.isDirty()
	}()

	if push {
		return h.manager.PushStatus()
	}
	return nil
}

func (h *handler) currentStatus() *v1.FederationStatusDetails {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.pruneOldRemotes()
	if !h.isDirty() {
		// nothing to do
		return h.status.DeepCopy()
	}

	// see if we need to update the export status
	if h.exportsDirty {
		exports := []v1.MeshServiceMapping{}
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
		h.status.Exports = exports
		h.exportsDirty = false
	}

	// see if we need to update the import status
	if h.importsDirty {
		imports := []v1.MeshServiceMapping{}
		for _, mapping := range h.imports {
			imports = append(imports, mapping)
		}
		sort.Slice(imports, func(i, j int) bool { return strings.Compare(imports[i].ExportedName, imports[j].ExportedName) < 0 })
		h.status.Imports = imports
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
		h.status.Discovery.Remotes = remoteStatuses
		h.discoveryDirty = false
	}

	h.dirty = false

	return h.status.DeepCopy()
}
