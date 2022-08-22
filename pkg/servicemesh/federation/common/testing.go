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

package common

import federationmodel "istio.io/istio/pkg/servicemesh/federation/model"

type FakeStatusHandler struct{}

// Outbound connections
func (m *FakeStatusHandler) WatchInitiated() {
}

func (m *FakeStatusHandler) Watching() {
}

func (m *FakeStatusHandler) WatchEventReceived() {
}

func (m *FakeStatusHandler) FullSyncComplete() {
}

func (m *FakeStatusHandler) WatchTerminated(status string) {
}

// Inbound connections
func (m *FakeStatusHandler) RemoteWatchAccepted(source string) {
}

func (m *FakeStatusHandler) WatchEventSent(source string) {
}

func (m *FakeStatusHandler) FullSyncSent(source string) {
}

func (m *FakeStatusHandler) RemoteWatchTerminated(source string) {
}

// Exports
func (m *FakeStatusHandler) ExportAdded(service federationmodel.ServiceKey, exportedName string) {
}

func (m *FakeStatusHandler) ExportUpdated(service federationmodel.ServiceKey, exportedName string) {
}

func (m *FakeStatusHandler) ExportRemoved(service federationmodel.ServiceKey) {
}

// Imports
func (m *FakeStatusHandler) ImportAdded(service federationmodel.ServiceKey, exportedName string) {
}

func (m *FakeStatusHandler) ImportUpdated(service federationmodel.ServiceKey, exportedName string) {
}

func (m *FakeStatusHandler) ImportRemoved(exportedName string) {
}

// Write status
func (m *FakeStatusHandler) Flush() error {
	return nil
}
