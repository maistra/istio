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

package federation

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/protocol"
)

var (
	ignoreChecksum = cmp.FilterPath(func(p cmp.Path) bool { return p.String() == "Checksum" }, cmp.Ignore())
)

func TestServiceList(t *testing.T) {
	testCases := []struct {
		name          string
		services      []*model.Service
		serviceEvents []struct {
			event model.Event
			svc   *model.Service
		}
		gateways        []*model.Gateway
		expectedMessage ServiceListMessage
	}{
		{
			name:     "empty serviceList",
			services: []*model.Service{},
			expectedMessage: ServiceListMessage{
				Services: []*ServiceMessage{},
			},
		},
		{
			name: "exported service, no gateway",
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Labels: map[string]string{
							ExportLabel: "service.federation.local",
						},
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
			expectedMessage: ServiceListMessage{
				Services: []*ServiceMessage{
					{
						Name: "service.federation.local",
						ServicePorts: []*ServicePort{
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
			name: "exported service + gateway",
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Labels: map[string]string{
							ExportLabel: "service.federation.local",
						},
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
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			gateways: []*model.Gateway{
				{
					Addr: "127.0.0.1",
					Port: 8080,
				},
			},
			expectedMessage: ServiceListMessage{
				NetworkGatewayEndpoints: []*ServiceEndpoint{
					{
						Port:     8080,
						Hostname: "127.0.0.1",
					},
				},
				Services: []*ServiceMessage{
					{
						Name: "service.federation.local",
						ServicePorts: []*ServicePort{
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
			name: "exported service + gateway, updated",
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Labels: map[string]string{
							ExportLabel: "service.federation.local",
						},
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
					Ports: model.PortList{
						&model.Port{
							Name:     "https",
							Protocol: protocol.HTTPS,
							Port:     443,
						},
					},
				},
			},
			gateways: []*model.Gateway{
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
							Labels: map[string]string{
								ExportLabel: "service2.federation.local",
							},
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
			},
			expectedMessage: ServiceListMessage{
				NetworkGatewayEndpoints: []*ServiceEndpoint{
					{
						Port:     8080,
						Hostname: "127.0.0.1",
					},
				},
				Services: []*ServiceMessage{
					{
						Name: "service2.federation.local",
						ServicePorts: []*ServicePort{
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
			serviceDiscovery := memory.NewServiceDiscovery(tc.services)
			serviceDiscovery.SetGatewaysForNetwork("cluster1", tc.gateways...)
			env := &model.Environment{
				ServiceDiscovery: serviceDiscovery,
			}

			s, _ := NewServer("127.0.0.1:0", env, "cluster1", "cluster1")
			stopCh := make(chan struct{})
			go s.Run(stopCh)
			defer close(stopCh)
			s.resync()
			for _, e := range tc.serviceEvents {
				s.UpdateService(e.svc, e.event)
			}
			serviceList := getServiceList(t, s.Addr())
			tc.expectedMessage.Checksum = serviceList.GenerateChecksum()
			if diff := cmp.Diff(serviceList, tc.expectedMessage); diff != "" {
				t.Fatalf("comparison failed, -got +want:\n%s", diff)
			}
		})
	}
}

func getServiceList(t *testing.T, addr string) ServiceListMessage {
	resp, err := http.Get("http://" + addr + "/services/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	serviceList := ServiceListMessage{}
	err = json.Unmarshal(body, &serviceList)
	if err != nil {
		t.Fatal(err)
	}
	return serviceList
}

func TestWatch(t *testing.T) {
	testCases := []struct {
		name          string
		services      []*model.Service
		serviceEvents []struct {
			event model.Event
			svc   *model.Service
		}
		gateways      []*model.Gateway
		gatewayEvents []struct {
			newGateways []*model.Gateway
		}
		expectedWatchEvents []*WatchEvent
	}{
		{
			name:     "no gateways, service added + removed",
			services: []*model.Service{},
			serviceEvents: []struct {
				event model.Event
				svc   *model.Service
			}{
				{
					event: model.EventAdd,
					svc: &model.Service{
						Hostname: "productpage.bookinfo.svc.cluster.local",
						Attributes: model.ServiceAttributes{
							Labels: map[string]string{
								ExportLabel: "service.federation.local",
							},
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
					},
				},
			},
			expectedWatchEvents: []*WatchEvent{
				{
					Action: ActionAdd,
					Service: &ServiceMessage{
						Name: "service.federation.local",
						ServicePorts: []*ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
				{
					Action: ActionDelete,
					Service: &ServiceMessage{
						Name: "service.federation.local",
						ServicePorts: []*ServicePort{
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
			name: "no gateways, service exported name changes",
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Labels: map[string]string{
							ExportLabel: "service.federation.local",
						},
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
			serviceEvents: []struct {
				event model.Event
				svc   *model.Service
			}{
				{
					event: model.EventUpdate,
					svc: &model.Service{
						Hostname: "productpage.bookinfo.svc.cluster.local",
						Attributes: model.ServiceAttributes{
							Labels: map[string]string{
								ExportLabel: "service.cluster.local",
							},
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
			},
			expectedWatchEvents: []*WatchEvent{
				{
					Action: ActionDelete,
					Service: &ServiceMessage{
						Name: "service.federation.local",
						ServicePorts: []*ServicePort{
							{
								Name:     "https",
								Port:     443,
								Protocol: "HTTPS",
							},
						},
					},
				},
				{
					Action: ActionAdd,
					Service: &ServiceMessage{
						Name: "service.cluster.local",
						ServicePorts: []*ServicePort{
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
			name: "single gateway, public IP changed",
			services: []*model.Service{
				{
					Hostname: "productpage.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Labels: map[string]string{
							ExportLabel: "service.federation.local",
						},
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
			gateways: []*model.Gateway{
				{
					Addr: "127.0.0.1",
					Port: 443,
				},
			},
			gatewayEvents: []struct{ newGateways []*model.Gateway }{
				{
					newGateways: []*model.Gateway{
						{
							Addr: "127.0.0.2",
							Port: 443,
						},
					},
				},
			},
			expectedWatchEvents: []*WatchEvent{
				{
					Action:  ActionUpdate,
					Service: nil,
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serviceDiscovery := memory.NewServiceDiscovery(tc.services)
			serviceDiscovery.SetGatewaysForNetwork("cluster1", tc.gateways...)
			env := &model.Environment{
				ServiceDiscovery: serviceDiscovery,
			}

			s, _ := NewServer("127.0.0.1:0", env, "cluster1", "cluster1")
			stopCh := make(chan struct{})
			go s.Run(stopCh)
			defer close(stopCh)
			s.resync()
			req, err := http.NewRequest("GET", "http://"+s.Addr()+"/watch", nil)
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
			for _, e := range tc.gatewayEvents {
				serviceDiscovery.SetGatewaysForNetwork("cluster1", e.newGateways...)
				// trigger a gateway resync
				s.UpdateService(&model.Service{
					Attributes: model.ServiceAttributes{
						ClusterExternalAddresses: map[string][]string{
							"cluster1": {"a"},
						},
					},
				}, model.EventUpdate)
			}
			svcList := ServiceListMessage{}
			dec := json.NewDecoder(resp.Body)
			for i := 0; i < len(tc.expectedWatchEvents); i++ {
				var e WatchEvent
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
					svcList = getServiceList(t, s.Addr())
				} else if e.Action == ActionAdd {
					svcList.Services = append(svcList.Services, e.Service)
				} else if e.Action == ActionUpdate {
					for i, svc := range svcList.Services {
						if svc.Name == e.Service.Name {
							svcList.Services[i] = e.Service
							break
						}
					}
				} else if e.Action == ActionDelete {
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
