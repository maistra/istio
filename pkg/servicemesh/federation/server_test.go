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
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/protocol"
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
				NetworkGatewayEndpoints: []*ServiceEndpoint{},
				Services:                []*ServiceMessage{},
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
				NetworkGatewayEndpoints: []*ServiceEndpoint{},
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
			resp, err := http.Get("http://" + s.Addr() + "/services/")
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

			ignoreChecksum := cmp.FilterPath(func(p cmp.Path) bool { return p.String() == "Checksum" }, cmp.Ignore())

			if diff := cmp.Diff(serviceList, tc.expectedMessage, ignoreChecksum); diff != "" {
				t.Fatalf("comparison failed, -got +want:\n%s", diff)
			}
		})
	}
}
