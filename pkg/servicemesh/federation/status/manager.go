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
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"

	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/pkg/log"
)

type Manager interface {
	PeerAdded(mesh types.NamespacedName) Handler
	PeerDeleted(mesh types.NamespacedName)
	HandlerFor(mesh types.NamespacedName) Handler
	IsLeader() bool
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

func NewManager(name types.NamespacedName, rm common.ResourceManager, leaderElection *leaderelection.LeaderElection) Manager {
	manager := &manager{
		rm:             rm,
		logger:         common.Logger.WithLabels("component", "federation-status"),
		name:           name,
		leaderElection: leaderElection,
		handlers:       map[types.NamespacedName]*handler{},
	}
	leaderElection.AddRunFunction(manager.BecomeLeader)
	return manager
}

type manager struct {
	mu     sync.Mutex
	rm     common.ResourceManager
	logger *log.Scope

	name types.NamespacedName

	handlers map[types.NamespacedName]*handler

	leaderElection *leaderelection.LeaderElection
	isLeader       bool
}

var _ Manager = (*manager)(nil)

func (m *manager) BecomeLeader(stop <-chan struct{}) {
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.isLeader = true
	}()
	_ = m.PushStatus()
	<-stop
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isLeader = false
}

func (m *manager) IsLeader() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isLeader
}

func (m *manager) PeerAdded(mesh types.NamespacedName) Handler {
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

func (m *manager) PeerDeleted(mesh types.NamespacedName) {
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if _, exists := m.handlers[mesh]; !exists {
			m.logger.Debugf("deleting unknown handler for mesh %s", mesh)
		}
		delete(m.handlers, mesh)
	}()
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
	handlers := func() []*handler {
		m.mu.Lock()
		defer m.mu.Unlock()
		var handlers []*handler
		for _, handler := range m.handlers {
			handlers = append(handlers, handler)
		}
		return handlers
	}()
	var allErrors []error
	for _, handler := range handlers {
		if err := handler.Flush(); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return errors.NewAggregate(allErrors)
}
