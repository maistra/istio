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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	maistraclient "maistra.io/api/client/versioned"
	"maistra.io/api/client/versioned/fake"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/model"
)

var (
	ignoreTimestamps = cmp.FilterPath(func(p cmp.Path) bool {
		switch p.Last().String() {
		case ".LastConnected", ".LastDisconnect", ".LastEvent", ".LastFullSync":
			return true
		}
		return false
	}, cmp.Ignore())
)

func TestStatusManager(t *testing.T) {
	const (
		namespace = "test-namespace"
		name      = "test"
	)
	istiodName := metav1.ObjectMeta{Name: "istiod-test", Namespace: namespace, UID: "12345"}
	meshName := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	testCases := []struct {
		name       string
		mesh       types.NamespacedName
		events     []func(h Handler)
		status     []v1.MeshFederationStatus
		assertions []func(t *testing.T, status *v1.MeshFederationStatus)
	}{
		{
			name: "initial-status",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
		},
		{
			name: "watch-init-connect-error",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.WatchInitiated()
					h.Flush()
				},
				func(h Handler) {
					h.WatchTerminated("503")
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				nil,
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if status.DiscoveryStatus.Inactive[0].Watch.LastConnected.IsZero() {
						t.Errorf("expected LastConnected to be updated")
					}
				},
			},
			status: []v1.MeshFederationStatus{
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Watch: v1.DiscoveryWatchStatus{
										DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
											Connected: false,
										},
									},
								},
							},
						},
					},
				},
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Watch: v1.DiscoveryWatchStatus{
										DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
											Connected:            false,
											LastDisconnectStatus: "503",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "watch-init-connect-success",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.WatchInitiated()
					h.Flush()
				},
				func(h Handler) {
					h.Watching()
				},
				func(h Handler) {
					h.WatchTerminated("200")
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				nil,
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if status.DiscoveryStatus.Active[0].Watch.LastConnected.IsZero() {
						t.Errorf("expected LastConnected to be updated")
					}
				},
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if status.DiscoveryStatus.Inactive[0].Watch.LastDisconnect.IsZero() {
						t.Errorf("expected LastDisconnect to be updated")
					}
				},
			},
			status: []v1.MeshFederationStatus{
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Watch: v1.DiscoveryWatchStatus{
										DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
											Connected: false,
										},
									},
								},
							},
						},
					},
				},
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Active: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Watch: v1.DiscoveryWatchStatus{
										DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
											Connected: true,
										},
									},
								},
							},
						},
					},
				},
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Watch: v1.DiscoveryWatchStatus{
										DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
											Connected:            false,
											LastDisconnectStatus: "200",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "watch-remote",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.RemoteWatchAccepted("10.10.10.10")
				},
				func(h Handler) {
					h.WatchEventSent("10.10.10.10")
				},
				func(h Handler) {
					h.FullSyncSent("10.10.10.10")
				},
				func(h Handler) {
					h.RemoteWatchTerminated("10.10.10.10")
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastConnected.IsZero() {
						t.Errorf("expected LastConnected to be updated")
					}
				},
				nil,
				func(t *testing.T, status *v1.MeshFederationStatus) {
					// full sync causes a push, so we can also verify that an event was seen
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastEvent.IsZero() {
						t.Errorf("expected LastEvent to be updated")
					}
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastFullSync.IsZero() {
						t.Errorf("expected LastFullSync to be updated")
					}
				},
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastDisconnect.IsZero() {
						t.Errorf("expected LastDisconnect to be updated")
					}
				},
			},
			status: []v1.MeshFederationStatus{
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Remotes: []v1.DiscoveryRemoteStatus{
										{
											Source: "10.10.10.10",
											DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
												Connected: true,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Remotes: []v1.DiscoveryRemoteStatus{
										{
											Source: "10.10.10.10",
											DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
												Connected: true,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Remotes: []v1.DiscoveryRemoteStatus{
										{
											Source: "10.10.10.10",
											DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
												Connected: true,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{},
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
								MeshDiscoveryStatus: v1.MeshDiscoveryStatus{
									Remotes: []v1.DiscoveryRemoteStatus{
										{
											Source: "10.10.10.10",
											DiscoveryConnectionStatus: v1.DiscoveryConnectionStatus{
												Connected: false,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "watch-export-added-updated-deleted",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.ExportAdded(
						model.ServiceKey{
							Name:      "real-service",
							Namespace: "real-namespace",
							Hostname:  "real-service.real-namespace.svc.cluster.local",
						},
						"exported-service.exported-namespace.svc.mesh.local")
					h.Flush()
				},
				func(h Handler) {
					h.ExportUpdated(
						model.ServiceKey{
							Name:      "real-service",
							Namespace: "real-namespace",
							Hostname:  "real-service.real-namespace.svc.cluster.local",
						},
						"updated-exported-service.exported-namespace.svc.mesh.local")
					h.Flush()
				},
				func(h Handler) {
					h.ExportRemoved(
						model.ServiceKey{
							Name:      "real-service",
							Namespace: "real-namespace",
							Hostname:  "real-service.real-namespace.svc.cluster.local",
						})
					h.Flush()
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				nil, nil, nil,
			},
			status: []v1.MeshFederationStatus{
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{
						{
							LocalService: v1.ServiceKey{
								Name:      "real-service",
								Namespace: "real-namespace",
								Hostname:  "real-service.real-namespace.svc.cluster.local",
							},
							ExportedName: "exported-service.exported-namespace.svc.mesh.local",
						},
					},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{
						{
							LocalService: v1.ServiceKey{
								Name:      "real-service",
								Namespace: "real-namespace",
								Hostname:  "real-service.real-namespace.svc.cluster.local",
							},
							ExportedName: "updated-exported-service.exported-namespace.svc.mesh.local",
						},
					},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
			},
		},
		{
			name: "watch-export-added-updated-deleted-no-flush",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.ExportAdded(
						model.ServiceKey{
							Name:      "real-service",
							Namespace: "real-namespace",
							Hostname:  "real-service.real-namespace.svc.cluster.local",
						},
						"exported-service.exported-namespace.svc.mesh.local")
				},
				func(h Handler) {
					h.ExportUpdated(
						model.ServiceKey{
							Name:      "real-service",
							Namespace: "real-namespace",
							Hostname:  "real-service.real-namespace.svc.cluster.local",
						},
						"updated-exported-service.exported-namespace.svc.mesh.local")
				},
				func(h Handler) {
					h.ExportRemoved(
						model.ServiceKey{
							Name:      "real-service",
							Namespace: "real-namespace",
							Hostname:  "real-service.real-namespace.svc.cluster.local",
						})
				},
				func(h Handler) {
					h.Flush()
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				nil, nil, nil, nil,
			},
			status: []v1.MeshFederationStatus{
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
			},
		},
		{
			name: "watch-import-added-updated-deleted",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.WatchEventReceived()
					h.ImportAdded(
						model.ServiceKey{
							Name:      "local-service",
							Namespace: "local-namespace",
							Hostname:  "local-service.local-namespace.svc.test-mesh.local",
						},
						"exported-service.exported-namespace.svc.mesh.local")
					h.Flush()
				},
				func(h Handler) {
					h.WatchEventReceived()
					h.ImportUpdated(
						model.ServiceKey{},
						"exported-service.exported-namespace.svc.mesh.local")
					h.Flush()
				},
				func(h Handler) {
					h.WatchEventReceived()
					h.ImportRemoved("exported-service.exported-namespace.svc.mesh.local")
					h.Flush()
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if status.DiscoveryStatus.Inactive[0].Watch.LastEvent.IsZero() {
						t.Errorf("expected LastEvent to be updated")
					}
				},
				nil, nil,
			},
			status: []v1.MeshFederationStatus{
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{
						{
							LocalService: v1.ServiceKey{
								Name:      "local-service",
								Namespace: "local-namespace",
								Hostname:  "local-service.local-namespace.svc.test-mesh.local",
							},
							ExportedName: "exported-service.exported-namespace.svc.mesh.local",
						},
					},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Exports: []v1.MeshServiceMapping{},
					Imports: []v1.MeshServiceMapping{
						{
							LocalService: v1.ServiceKey{},
							ExportedName: "exported-service.exported-namespace.svc.mesh.local",
						},
					},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
			},
		},
		{
			name: "watch-import-added-updated-deleted-no-flush",
			mesh: types.NamespacedName{Namespace: namespace, Name: name},
			events: []func(h Handler){
				func(h Handler) {
					h.WatchEventReceived()
					h.ImportAdded(
						model.ServiceKey{
							Name:      "local-service",
							Namespace: "local-namespace",
							Hostname:  "local-service.local-namespace.svc.test-mesh.local",
						},
						"exported-service.exported-namespace.svc.mesh.local")
				},
				func(h Handler) {
					h.WatchEventReceived()
					h.ImportUpdated(
						model.ServiceKey{},
						"exported-service.exported-namespace.svc.mesh.local")
				},
				func(h Handler) {
					h.WatchEventReceived()
					h.ImportRemoved("exported-service.exported-namespace.svc.mesh.local")
				},
				func(h Handler) {
					h.FullSyncComplete()
				},
			},
			assertions: []func(t *testing.T, status *v1.MeshFederationStatus){
				func(t *testing.T, status *v1.MeshFederationStatus) {
					if !status.DiscoveryStatus.Inactive[0].Watch.LastEvent.IsZero() {
						t.Errorf("did not expect LastEvent to be updated")
					}
					if !status.DiscoveryStatus.Inactive[0].Watch.LastFullSync.IsZero() {
						t.Errorf("did not expect LastFullSync to be updated")
					}
				},
				nil, nil,
				func(t *testing.T, status *v1.MeshFederationStatus) {
					// this should have been updated in one of the previous events
					if status.DiscoveryStatus.Inactive[0].Watch.LastEvent.IsZero() {
						t.Errorf("expected LastEvent to be updated")
					}
					if status.DiscoveryStatus.Inactive[0].Watch.LastFullSync.IsZero() {
						t.Errorf("expected LastFullSync to be updated")
					}
				},
			},
			status: []v1.MeshFederationStatus{
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
				{
					DiscoveryStatus: v1.FederationDiscoveryStatus{
						Inactive: []v1.FederationPodDiscoveryStatus{
							{
								Pod: istiodName.Name,
							},
						},
					},
					Imports: []v1.MeshServiceMapping{},
					Exports: []v1.MeshServiceMapping{},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// verify test is setup correctly
			if len(tc.status) != len(tc.events) || len(tc.assertions) != len(tc.events) {
				t.Fatalf("number of status elements and asserts must equal the number of events")
			}
			kubeClient := kube.NewFakeClient(&corev1.Pod{
				ObjectMeta: istiodName,
			})
			stop := make(chan struct{})
			defer func() { close(stop) }()
			go kubeClient.KubeInformer().Core().V1().Pods().Informer().Run(stop)
			for !kubeClient.KubeInformer().Core().V1().Pods().Informer().HasSynced() {
			}
			cs := fake.NewSimpleClientset()
			rm, err := common.NewResourceManager(common.ControllerOptions{
				KubeClient: kubeClient,
				MaistraCS:  cs,
			}, nil)
			if err != nil {
				t.Fatalf("error creating ResourceManager: %s", err)
			}
			stopChan := make(chan struct{})
			defer close(stopChan)
			go rm.Start(stopChan)
			cs.CoreV1().MeshFederations(namespace).Create(context.TODO(), &v1.MeshFederation{
				ObjectMeta: meshName,
			}, metav1.CreateOptions{})
			leaderStarted := make(chan struct{})
			le := leaderelection.NewLeaderElection(istiodName.Namespace, istiodName.Name, "test", kubeClient)
			manager := NewManager(types.NamespacedName{Name: istiodName.Name, Namespace: istiodName.Namespace}, rm, le)
			le.AddRunFunction(func(stop <-chan struct{}) {
				cache.WaitForCacheSync(stopChan, manager.IsLeader)
				close(leaderStarted)
			})
			go le.Run(stopChan)
			select {
			case <-leaderStarted:
			case <-time.After(30 * time.Second):
				close(stopChan)
				t.Fatalf("timed out waiting for leader election")
			}
			manager.FederationAdded(tc.mesh)
			if err := manager.PushStatus(); err != nil {
				t.Fatalf("error updating initial status: %s", err)
			}
			verifyStatus(t, cs, tc.mesh, &v1.MeshFederationStatus{
				DiscoveryStatus: v1.FederationDiscoveryStatus{
					Inactive: []v1.FederationPodDiscoveryStatus{
						{
							Pod: istiodName.Name,
						},
					},
				},
				Exports: []v1.MeshServiceMapping{},
				Imports: []v1.MeshServiceMapping{},
			}, nil)

			handler := manager.HandlerFor(types.NamespacedName{Namespace: tc.mesh.Namespace, Name: tc.mesh.Name})
			if handler == nil {
				t.Fatalf("nil handler for %s/%s", tc.mesh.Namespace, tc.mesh.Name)
			}
			for index, f := range tc.events {
				f(handler)
				verifyStatus(t, cs, tc.mesh, &tc.status[index], tc.assertions[index])
			}
		})
	}
}

func verifyStatus(t *testing.T, cs maistraclient.Interface, name types.NamespacedName, expected *v1.MeshFederationStatus,
	assert func(*testing.T, *v1.MeshFederationStatus)) {
	t.Helper()

	actual, err := cs.CoreV1().MeshFederations(name.Namespace).Get(context.TODO(), name.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error retrieving FederationStatus %s/%s: %s", name.Namespace, name.Name, err)
		return
	}
	if diff := cmp.Diff(&actual.Status, expected, ignoreTimestamps); diff != "" {
		t.Errorf("comparison failed, -got +want:\n%s", diff)
	}
	if assert != nil {
		assert(t, &actual.Status)
	}
}
