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
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/cluster"

	configmemory "istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	serviceregistrymemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

var ignoreChecksum = cmp.FilterPath(func(p cmp.Path) bool { return p.String() == "Checksum" }, cmp.Ignore())

func TestServiceList(t *testing.T) {
	federation := &v1alpha1.MeshFederation{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1alpha1.MeshFederationSpec{
			Security: &v1alpha1.MeshFederationSecurity{
				ClientID: "federation-egress.other-mesh.svc.cluster.local",
			},
		},
	}
	exportAllServices := &v1alpha1.ServiceExports{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1alpha1.ServiceExportsSpec{
			Exports: []v1alpha1.ServiceExportRule{
				{
					Type:         v1alpha1.NameSelectorType,
					NameSelector: &v1alpha1.ServiceNameMapping{},
				},
			},
		},
	}
	testCases := []struct {
		name           string
		remoteName     string
		defaultExports *v1alpha1.ServiceExports
		serviceExports *v1alpha1.ServiceExports
		services       []*model.Service
		serviceEvents  []struct {
			event model.Event
			svc   *model.Service
		}
		gateways        []model.NetworkGateway
		expectedMessage federationmodel.ServiceListMessage
	}{
		{
			name:           "empty serviceList",
			remoteName:     "test-remote",
			serviceExports: exportAllServices,
			services:       []*model.Service{},
			expectedMessage: federationmodel.ServiceListMessage{
				Services: []*federationmodel.ServiceMessage{},
			},
		},
		{
			name:       "exported service, no gateway",
			remoteName: "test-remote",
			serviceExports: &v1alpha1.ServiceExports{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1alpha1.ServiceExportsSpec{
					Exports: []v1alpha1.ServiceExportRule{
						{
							Type: v1alpha1.NameSelectorType,
							NameSelector: &v1alpha1.ServiceNameMapping{
								Name: v1alpha1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
								},
								Alias: &v1alpha1.ServiceName{
									Namespace: "federation",
									Name:      "service",
								},
							},
						},
					},
				},
			},
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "federation",
							Hostname:  "service.federation.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
			},
		},
		{
			name:           "service, no exports, no gateway",
			remoteName:     "test-remote",
			serviceExports: nil,
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{
				Services: []*federationmodel.ServiceMessage{},
			},
		},
		{
			name:       "exported service + gateway",
			remoteName: "test-remote",
			serviceExports: &v1alpha1.ServiceExports{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1alpha1.ServiceExportsSpec{
					Exports: []v1alpha1.ServiceExportRule{
						{
							Type: v1alpha1.NameSelectorType,
							NameSelector: &v1alpha1.ServiceNameMapping{
								Name: v1alpha1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
								},
								Alias: &v1alpha1.ServiceName{
									Namespace: "federation",
									Name:      "service",
								},
							},
						},
					},
				},
			},
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
				{
					Hostname: "ratings.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "ratings",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			gateways: []model.NetworkGateway{
				{
					Addr: "127.0.0.1",
					Port: 8080,
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{
				NetworkGatewayEndpoints: []*federationmodel.ServiceEndpoint{
					{
						Port:     8080,
						Hostname: "127.0.0.1",
					},
				},
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "federation",
							Hostname:  "service.federation.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
			},
		},
		{
			name:           "exported service + gateway, updated",
			remoteName:     "test-remote",
			serviceExports: exportAllServices,
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
				{
					Hostname: "ratings.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "ratings",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			gateways: []model.NetworkGateway{
				{
					Addr: "127.0.0.1",
					Port: 8080,
				},
			},
			serviceEvents: []struct {
				event model.Event
				svc   *model.Service
			}{
				{
					event: model.EventUpdate,
					svc: &model.Service{
						Hostname: "productpage.bookinfo.svc.cluster.local",
						Attributes: model.ServiceAttributes{
							Name:      "productpage",
							Namespace: "bookinfo",
						},
						Ports: model.PortList{
							&model.Port{
								Name:     "https",
								Protocol: protocol.HTTPS,
								Port:     8443,
							},
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{
				NetworkGatewayEndpoints: []*federationmodel.ServiceEndpoint{
					{
						Port:     8080,
						Hostname: "127.0.0.1",
					},
				},
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "productpage",
							Namespace: "bookinfo",
							Hostname:  "productpage.bookinfo.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     8443,
								Protocol: "HTTPS",
							},
						},
					},
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "ratings",
							Namespace: "bookinfo",
							Hostname:  "ratings.bookinfo.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serviceDiscovery := serviceregistrymemory.NewServiceDiscovery(tc.services)
			serviceDiscovery.AddGateways(tc.gateways...)
			env := &model.Environment{
				ServiceDiscovery: serviceDiscovery,
			}

			s, _ := NewServer(Options{
				BindAddress: "127.0.0.1:0",
				Env:         env,
				Network:     "network1",
				ConfigStore: configmemory.NewController(configmemory.Make(Schemas)),
			})
			stopCh := make(chan struct{})
			go s.Run(stopCh)
			defer close(stopCh)
			s.resyncNetworkGateways()
			s.AddMeshFederation(federation, tc.serviceExports)
			for _, e := range tc.serviceEvents {
				s.UpdateService(e.svc, e.event)
			}
			serviceList := getServiceList(t, s.Addr(), tc.remoteName)
			tc.expectedMessage.Checksum = tc.expectedMessage.GenerateChecksum()
			if tc.expectedMessage.Checksum != serviceList.GenerateChecksum() {
				t.Errorf("checksums don't match")
			}
			if diff := cmp.Diff(serviceList, tc.expectedMessage); diff != "" {
				t.Fatalf("comparison failed, -got +want:\n%s", diff)
			}
		})
	}
}

func getServiceList(t *testing.T, addr, remoteName string) federationmodel.ServiceListMessage {
	resp, err := http.Get("http://" + addr + "/services/" + remoteName)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	serviceList := federationmodel.ServiceListMessage{}
	err = json.Unmarshal(body, &serviceList)
	if err != nil {
		t.Fatal(err)
	}
	return serviceList
}

func TestWatch(t *testing.T) {
	federation := &v1alpha1.MeshFederation{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1alpha1.MeshFederationSpec{
			Security: &v1alpha1.MeshFederationSecurity{
				ClientID: "federation-egress.other-mesh.svc.cluster.local",
			},
		},
	}
	exportProductPage := &v1alpha1.ServiceExports{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1alpha1.ServiceExportsSpec{
			Exports: []v1alpha1.ServiceExportRule{
				{
					Type: v1alpha1.NameSelectorType,
					NameSelector: &v1alpha1.ServiceNameMapping{
						Name: v1alpha1.ServiceName{
							Namespace: "bookinfo",
							Name:      "productpage",
						},
						Alias: &v1alpha1.ServiceName{
							Namespace: "federation",
							Name:      "service",
						},
					},
				},
			},
		},
	}
	testCases := []struct {
		name           string
		remoteName     string
		defaultExports *v1alpha1.ServiceExports
		serviceExports *v1alpha1.ServiceExports
		updatedExports *v1alpha1.ServiceExports
		services       []*model.Service
		serviceEvents  []struct {
			event model.Event
			svc   *model.Service
		}
		gateways      []model.NetworkGateway
		gatewayEvents []struct {
			newGateways []model.NetworkGateway
		}
		expectedWatchEvents []*federationmodel.WatchEvent
	}{
		{
			name:           "no gateways, service added + removed",
			remoteName:     "test-remote",
			serviceExports: exportProductPage,
			services:       []*model.Service{},
			serviceEvents: []struct {
				event model.Event
				svc   *model.Service
			}{
				{
					event: model.EventAdd,
					svc: &model.Service{
						Hostname: "productpage.bookinfo.svc.cluster.local",
						Attributes: model.ServiceAttributes{
							Name:      "productpage",
							Namespace: "bookinfo",
						},
						Ports: model.PortList{
							&model.Port{
								Name:     "https",
								Protocol: protocol.HTTPS,
								Port:     443,
							},
						},
					},
				},
				{
					event: model.EventDelete,
					svc: &model.Service{
						Hostname: "productpage.bookinfo.svc.cluster.local",
						Attributes: model.ServiceAttributes{
							Name:      "productpage",
							Namespace: "bookinfo",
						},
					},
				},
			},
			expectedWatchEvents: []*federationmodel.WatchEvent{
				{
					Action: federationmodel.ActionAdd,
					Service: &federationmodel.ServiceMessage{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "federation",
							Hostname:  "service.federation.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
				{
					Action: federationmodel.ActionDelete,
					Service: &federationmodel.ServiceMessage{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "federation",
							Hostname:  "service.federation.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
			},
		},
		{
			name:           "no gateways, service exported name changes",
			remoteName:     "test-remote",
			serviceExports: exportProductPage,
			updatedExports: &v1alpha1.ServiceExports{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1alpha1.ServiceExportsSpec{
					Exports: []v1alpha1.ServiceExportRule{
						{
							Type: v1alpha1.NameSelectorType,
							NameSelector: &v1alpha1.ServiceNameMapping{
								Name: v1alpha1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
								},
								Alias: &v1alpha1.ServiceName{
									Namespace: "cluster",
									Name:      "service",
								},
							},
						},
					},
				},
			},
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			serviceEvents: nil,
			expectedWatchEvents: []*federationmodel.WatchEvent{
				{
					Action: federationmodel.ActionDelete,
					Service: &federationmodel.ServiceMessage{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "federation",
							Hostname:  "service.federation.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
				{
					Action: federationmodel.ActionAdd,
					Service: &federationmodel.ServiceMessage{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "cluster",
							Hostname:  "service.cluster.svc.test-remote.local",
						},
						ServicePorts: []*federationmodel.ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
			},
		},
		{
			name:           "single gateway, public IP changed",
			remoteName:     "test-remote",
			serviceExports: exportProductPage,
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "bookinfo",
					},
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			gateways: []model.NetworkGateway{
				{
					Addr: "127.0.0.1",
					Port: 443,
				},
			},
			gatewayEvents: []struct{ newGateways []model.NetworkGateway }{
				{
					newGateways: []model.NetworkGateway{
						{
							Addr: "127.0.0.2",
							Port: 443,
						},
					},
				},
			},
			expectedWatchEvents: []*federationmodel.WatchEvent{
				{
					Action:  federationmodel.ActionUpdate,
					Service: nil,
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serviceDiscovery := serviceregistrymemory.NewServiceDiscovery(tc.services)
			serviceDiscovery.AddGateways(tc.gateways...)
			env := &model.Environment{
				ServiceDiscovery: serviceDiscovery,
			}

			s, _ := NewServer(Options{
				BindAddress: "127.0.0.1:0",
				Env:         env,
				Network:     "network1",
				ConfigStore: configmemory.NewController(configmemory.Make(Schemas)),
			})
			stopCh := make(chan struct{})
			go s.Run(stopCh)
			defer close(stopCh)
			s.resyncNetworkGateways()
			s.AddMeshFederation(federation, tc.serviceExports)
			req, err := http.NewRequest("GET", "http://"+s.Addr()+"/watch/"+tc.remoteName, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
			}
			for _, e := range tc.serviceEvents {
				s.UpdateService(e.svc, e.event)
			}
			if tc.updatedExports != nil {
				s.UpdateExportsForMesh(tc.updatedExports)
			}
			for _, e := range tc.gatewayEvents {
				serviceDiscovery.AddGateways(e.newGateways...)
				// trigger a gateway resync
				s.UpdateService(&model.Service{
					Attributes: model.ServiceAttributes{
						ClusterExternalAddresses: model.AddressMap{
							Addresses: map[cluster.ID][]string{
								cluster.ID("network1"): {"a"},
							},
						},
					},
				}, model.EventUpdate)
			}
			svcList := federationmodel.ServiceListMessage{}
			dec := json.NewDecoder(resp.Body)
			for i := 0; i < len(tc.expectedWatchEvents); i++ {
				var e federationmodel.WatchEvent
				err := dec.Decode(&e)
				if err != nil {
					if err == io.EOF {
						break
					}
					t.Fatal(err)
				}
				if diff := cmp.Diff(&e, tc.expectedWatchEvents[i], ignoreChecksum); diff != "" {
					t.Fatalf("comparison failed, -got +want:\n%s", diff)
				}

				if e.Service == nil {
					svcList = getServiceList(t, s.Addr(), tc.remoteName)
				} else if e.Action == federationmodel.ActionAdd {
					svcList.Services = append(svcList.Services, e.Service)
				} else if e.Action == federationmodel.ActionUpdate {
					for i, svc := range svcList.Services {
						if svc.Name == e.Service.Name {
							svcList.Services[i] = e.Service
							break
						}
					}
				} else if e.Action == federationmodel.ActionDelete {
					for i, svc := range svcList.Services {
						if svc.Name == e.Service.Name {
							svcList.Services = append(svcList.Services[:i], svcList.Services[i+1:]...)
							break
						}
					}
				}
				if e.Checksum != svcList.GenerateChecksum() {
					t.Fatalf("checksum mismatch, expected %d but got %d", svcList.GenerateChecksum(), e.Checksum)
				}
			}
		})
	}
}
