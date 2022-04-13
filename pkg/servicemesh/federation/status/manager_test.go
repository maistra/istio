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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	maistraclient "maistra.io/api/client/versioned"
	"maistra.io/api/client/versioned/fake"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

var ignoreTimestamps = cmp.FilterPath(func(p cmp.Path) bool {
	switch p.Last().String() {
	case ".LastConnected", ".LastDisconnect", ".LastEvent", ".LastFullSync":
		return true
	}
	return false
}, cmp.Ignore())

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
		name   string
		mesh   types.NamespacedName
		events []func(h Handler)
		status []struct {
			peer    v1.ServiceMeshPeerStatus
			exports v1.ExportedServiceSetStatus
			imports v1.ImportedServiceSetStatus
		}
		assertions []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				nil,
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if status.DiscoveryStatus.Inactive[0].Watch.LastConnected.IsZero() {
						return fmt.Errorf("expected LastConnected to be updated")
					}
					return nil
				},
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				nil,
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if status.DiscoveryStatus.Active[0].Watch.LastConnected.IsZero() {
						return fmt.Errorf("expected LastConnected to be updated")
					}
					return nil
				},
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if status.DiscoveryStatus.Inactive[0].Watch.LastDisconnect.IsZero() {
						return fmt.Errorf("expected LastDisconnect to be updated")
					}
					return nil
				},
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Active: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastConnected.IsZero() {
						return fmt.Errorf("expected LastConnected to be updated")
					}
					return nil
				},
				nil,
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					// full sync causes a push, so we can also verify that an event was seen
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastEvent.IsZero() {
						return fmt.Errorf("expected LastEvent to be updated")
					}
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastFullSync.IsZero() {
						return fmt.Errorf("expected LastFullSync to be updated")
					}
					return nil
				},
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if status.DiscoveryStatus.Inactive[0].Remotes[0].LastDisconnect.IsZero() {
						return fmt.Errorf("expected LastDisconnect to be updated")
					}
					return nil
				},
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
									PeerDiscoveryStatus: v1.PeerDiscoveryStatus{
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				nil, nil, nil,
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
					exports: v1.ExportedServiceSetStatus{
						ExportedServices: []v1.PeerServiceMapping{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
					exports: v1.ExportedServiceSetStatus{
						ExportedServices: []v1.PeerServiceMapping{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				nil, nil, nil, nil,
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if len(status.DiscoveryStatus.Inactive) == 0 ||
						(len(status.DiscoveryStatus.Inactive) > 0 && status.DiscoveryStatus.Inactive[0].Watch.LastEvent.IsZero()) {
						return fmt.Errorf("expected LastEvent to be updated")
					}
					return nil
				},
				nil, nil,
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
					imports: v1.ImportedServiceSetStatus{
						ImportedServices: []v1.PeerServiceMapping{
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
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
					imports: v1.ImportedServiceSetStatus{
						ImportedServices: []v1.PeerServiceMapping{
							{
								LocalService: v1.ServiceKey{},
								ExportedName: "exported-service.exported-namespace.svc.mesh.local",
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
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
			assertions: []func(t *testing.T, status *v1.ServiceMeshPeerStatus) error{
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					if !status.DiscoveryStatus.Inactive[0].Watch.LastEvent.IsZero() {
						return fmt.Errorf("did not expect LastEvent to be updated")
					}
					if !status.DiscoveryStatus.Inactive[0].Watch.LastFullSync.IsZero() {
						return fmt.Errorf("did not expect LastFullSync to be updated")
					}
					return nil
				},
				nil, nil,
				func(t *testing.T, status *v1.ServiceMeshPeerStatus) error {
					// this should have been updated in one of the previous events
					if status.DiscoveryStatus.Inactive[0].Watch.LastEvent.IsZero() {
						return fmt.Errorf("expected LastEvent to be updated")
					}
					if status.DiscoveryStatus.Inactive[0].Watch.LastFullSync.IsZero() {
						return fmt.Errorf("expected LastFullSync to be updated")
					}
					return nil
				},
			},
			status: []struct {
				peer    v1.ServiceMeshPeerStatus
				exports v1.ExportedServiceSetStatus
				imports v1.ImportedServiceSetStatus
			}{
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
				{
					peer: v1.ServiceMeshPeerStatus{
						DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
							Inactive: []v1.PodPeerDiscoveryStatus{
								{
									Pod: istiodName.Name,
								},
							},
						},
					},
				},
			},
		},
	}
	logOpts := log.DefaultOptions()
	logOpts.SetOutputLevel("federation", log.DebugLevel)
	logOpts.JSONEncoding = false
	log.Configure(logOpts)
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
			cs := fake.NewSimpleClientset(
				&v1.ExportedServiceSet{ObjectMeta: metav1.ObjectMeta{Name: tc.mesh.Name, Namespace: tc.mesh.Namespace}},
				&v1.ImportedServiceSet{ObjectMeta: metav1.ObjectMeta{Name: tc.mesh.Name, Namespace: tc.mesh.Namespace}})
			rm, err := common.NewResourceManager(common.ControllerOptions{
				KubeClient:   kubeClient,
				MaistraCS:    cs,
				ResyncPeriod: 1 * time.Millisecond,
			}, nil)
			if err != nil {
				t.Fatalf("error creating ResourceManager: %s", err)
			}
			stopChan := make(chan struct{})
			defer close(stopChan)
			go rm.Start(stopChan)
			cs.FederationV1().ServiceMeshPeers(namespace).Create(context.TODO(), &v1.ServiceMeshPeer{
				ObjectMeta: meshName,
			}, metav1.CreateOptions{})
			leaderStarted := make(chan struct{})
			le := leaderelection.NewLeaderElection(istiodName.Namespace, istiodName.Name, "test", "test", kubeClient)
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
			for !rm.ExportsInformer().Informer().HasSynced() ||
				!rm.ImportsInformer().Informer().HasSynced() ||
				!rm.PeerInformer().Informer().HasSynced() {
			}
			manager.PeerAdded(tc.mesh)
			if err := manager.PushStatus(); err != nil {
				t.Fatalf("error updating initial status: %s", err)
			}
			verifyPeerStatus(t, cs, tc.mesh, &v1.ServiceMeshPeerStatus{
				DiscoveryStatus: v1.ServiceMeshPeerDiscoveryStatus{
					Inactive: []v1.PodPeerDiscoveryStatus{
						{
							Pod: istiodName.Name,
						},
					},
				},
			}, nil)

			handler := manager.HandlerFor(types.NamespacedName{Namespace: tc.mesh.Namespace, Name: tc.mesh.Name})
			if handler == nil {
				t.Fatalf("nil handler for %s/%s", tc.mesh.Namespace, tc.mesh.Name)
			}
			for index, f := range tc.events {
				t.Logf("processing event %d", index)
				f(handler)
				verifyPeerStatus(t, cs, tc.mesh, &tc.status[index].peer, tc.assertions[index])
				verifyExportStatus(t, cs, tc.mesh, &tc.status[index].exports)
				verifyImportStatus(t, cs, tc.mesh, &tc.status[index].imports)
			}
		})
	}
}

func verifyPeerStatus(t *testing.T, cs maistraclient.Interface, name types.NamespacedName, expected *v1.ServiceMeshPeerStatus,
	assert func(*testing.T, *v1.ServiceMeshPeerStatus) error) {
	t.Helper()
	tryMultipleTimes(t, func() error {
		actual, err := cs.FederationV1().ServiceMeshPeers(name.Namespace).Get(context.TODO(), name.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("unexpected error retrieving ServiceMeshPeer %s/%s: %s", name.Namespace, name.Name, err)
		}
		if diff := cmp.Diff(&actual.Status, expected, ignoreTimestamps); diff != "" {
			return fmt.Errorf("comparison failed, -got +want:\n%s", diff)
		}
		if assert != nil {
			return assert(t, &actual.Status)
		}
		return nil
	})
}

func verifyExportStatus(t *testing.T, cs maistraclient.Interface, name types.NamespacedName,
	expected *v1.ExportedServiceSetStatus) {
	t.Helper()
	tryMultipleTimes(t, func() error {
		actual, err := cs.FederationV1().ExportedServiceSets(name.Namespace).Get(context.TODO(), name.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("unexpected error retrieving ExportedServiceSet %s/%s: %s", name.Namespace, name.Name, err)
		}
		if expected.ExportedServices == nil {
			expected.ExportedServices = []v1.PeerServiceMapping{}
		}
		if diff := cmp.Diff(&actual.Status, expected, ignoreTimestamps); diff != "" {
			return fmt.Errorf("comparison failed, -got +want:\n%s", diff)
		}
		return nil
	})
}

func verifyImportStatus(t *testing.T, cs maistraclient.Interface, name types.NamespacedName,
	expected *v1.ImportedServiceSetStatus) {
	t.Helper()
	tryMultipleTimes(t, func() error {
		actual, err := cs.FederationV1().ImportedServiceSets(name.Namespace).Get(context.TODO(), name.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("unexpected error retrieving ImportedServiceSet %s/%s: %s", name.Namespace, name.Name, err)
		}
		if expected.ImportedServices == nil {
			expected.ImportedServices = []v1.PeerServiceMapping{}
		}
		if diff := cmp.Diff(&actual.Status, expected, ignoreTimestamps); diff != "" {
			return fmt.Errorf("comparison failed, -got +want:\n%s", diff)
		}
		return nil
	})
}

func tryMultipleTimes(t *testing.T, fn func() error) {
	t.Helper()
	if err := retry.UntilSuccess(fn, retry.Timeout(10*time.Second), retry.Delay(10*time.Millisecond)); err != nil {
		t.Error(err.Error())
	}
}
