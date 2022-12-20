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
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "maistra.io/api/federation/v1"

	configmemory "istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	serviceregistrymemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/servicemesh/federation/common"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
	istioenv "istio.io/istio/pkg/test/env"
)

var (
	ignoreChecksum = cmp.FilterPath(func(p cmp.Path) bool { return p.String() == "Checksum" }, cmp.Ignore())
	httpsClient    = http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				//nolint:gosec
				InsecureSkipVerify: true,
			},
		},
		Timeout: time.Second,
	}
)

func TestServiceList(t *testing.T) {
	federation := &v1.ServiceMeshPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1.ServiceMeshPeerSpec{
			Security: v1.ServiceMeshPeerSecurity{
				ClientID: "federation-egress.other-mesh.svc.cluster.local",
			},
		},
	}
	exportAllServices := &v1.ExportedServiceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1.ExportedServiceSetSpec{
			ExportRules: []v1.ExportedServiceRule{
				{
					Type:         v1.NameSelectorType,
					NameSelector: &v1.ServiceNameMapping{},
				},
			},
		},
	}
	testCases := []struct {
		name           string
		remoteName     string
		defaultExports *v1.ExportedServiceSet
		serviceExports *v1.ExportedServiceSet
		services       []*model.Service
		serviceEvents  []struct {
			event model.Event
			svc   *model.Service
		}
		gateways        []model.NetworkGateway
		expectedMessage federationmodel.ServiceListMessage
	}{
		{
			name:            "empty serviceList",
			remoteName:      "test-remote",
			serviceExports:  exportAllServices,
			services:        []*model.Service{},
			expectedMessage: federationmodel.ServiceListMessage{},
		},
		{
			name:       "exported service, no gateway",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
								},
								Alias: &v1.ServiceName{
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
							Hostname:  "service.federation.svc.test-remote-exports.local",
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
			expectedMessage: federationmodel.ServiceListMessage{},
		},
		{
			name:       "exported service + gateway",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
								},
								Alias: &v1.ServiceName{
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
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "service",
							Namespace: "federation",
							Hostname:  "service.federation.svc.test-remote-exports.local",
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
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "productpage",
							Namespace: "bookinfo",
							Hostname:  "productpage.bookinfo.svc.test-remote-exports.local",
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
							Hostname:  "ratings.bookinfo.svc.test-remote-exports.local",
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
			name:       "exportTo - service invisible",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
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
						ExportTo: map[visibility.Instance]bool{
							visibility.None: true,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{},
		},
		{
			name:       "exportTo - service private",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
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
						ExportTo: map[visibility.Instance]bool{
							visibility.Private: true,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{},
		},
		{
			name:       "exportTo - service private to the same namespace",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "istio-system-test",
									Name:      "productpage",
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
						Namespace: "istio-system-test",
						ExportTo: map[visibility.Instance]bool{
							visibility.Private: true,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "productpage",
							Namespace: "istio-system-test",
							Hostname:  "productpage.istio-system-test.svc.test-remote-exports.local",
						},
					},
				},
			},
		},
		{
			name:       "exportTo - foreign namespace",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
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
						ExportTo: map[visibility.Instance]bool{
							visibility.Instance("foreign-namespace"): true,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{},
		},
		{
			name:       "exportTo - same namespace as control plane",
			remoteName: "test-remote",
			serviceExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
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
						ExportTo: map[visibility.Instance]bool{
							visibility.Instance("istio-system-test"): true,
						},
					},
				},
			},
			expectedMessage: federationmodel.ServiceListMessage{
				Services: []*federationmodel.ServiceMessage{
					{
						ServiceKey: federationmodel.ServiceKey{
							Name:      "productpage",
							Namespace: "bookinfo",
							Hostname:  "productpage.bookinfo.svc.test-remote-exports.local",
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serviceDiscovery := serviceregistrymemory.NewServiceDiscovery(tc.services...)
			serviceDiscovery.AddGateways(tc.gateways...)
			env := &model.Environment{
				ServiceDiscovery: serviceDiscovery,
			}
			s := createServer(env)
			stopCh := make(chan struct{})
			go s.Run(stopCh)
			defer close(stopCh)
			s.AddPeer(federation, tc.serviceExports, &common.FakeStatusHandler{})
			for _, e := range tc.serviceEvents {
				s.UpdateService(e.svc, e.svc, e.event)
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

func createServer(env *model.Environment) *Server {
	cert, _ := tls.LoadX509KeyPair(
		filepath.Join(istioenv.IstioSrc, "./tests/testdata/certs/pilot/cert-chain.pem"),
		filepath.Join(istioenv.IstioSrc, "./tests/testdata/certs/pilot/key.pem"))

	s, _ := NewServer(Options{
		BindAddress: "127.0.0.1:0",
		Env:         env,
		Network:     "network1",
		ConfigStore: configmemory.NewController(configmemory.Make(Schemas)),
		//nolint:gosec
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{
				cert,
			},
		},
	})
	return s
}

func getServiceList(t *testing.T, addr, remoteName string) federationmodel.ServiceListMessage {
	resp, err := httpsClient.Get("https://" + addr + "/v1/services/" + remoteName)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
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
	federation := &v1.ServiceMeshPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1.ServiceMeshPeerSpec{
			Security: v1.ServiceMeshPeerSecurity{
				ClientID: "federation-egress.other-mesh.svc.cluster.local",
			},
		},
	}
	exportProductPage := &v1.ExportedServiceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-remote",
			Namespace: "istio-system-test",
		},
		Spec: v1.ExportedServiceSetSpec{
			ExportRules: []v1.ExportedServiceRule{
				{
					Type: v1.NameSelectorType,
					NameSelector: &v1.ServiceNameMapping{
						ServiceName: v1.ServiceName{
							Namespace: "bookinfo",
							Name:      "productpage",
						},
						Alias: &v1.ServiceName{
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
		defaultExports *v1.ExportedServiceSet
		serviceExports *v1.ExportedServiceSet
		updatedExports *v1.ExportedServiceSet
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
							Hostname:  "service.federation.svc.test-remote-exports.local",
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
							Hostname:  "service.federation.svc.test-remote-exports.local",
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
			name:           "no gateways, service exported name changes, filtered service",
			remoteName:     "test-remote",
			serviceExports: exportProductPage,
			updatedExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-remote",
					Namespace: "istio-system-test",
				},
				Spec: v1.ExportedServiceSetSpec{
					ExportRules: []v1.ExportedServiceRule{
						{
							Type: v1.NameSelectorType,
							NameSelector: &v1.ServiceNameMapping{
								ServiceName: v1.ServiceName{
									Namespace: "bookinfo",
									Name:      "productpage",
								},
								Alias: &v1.ServiceName{
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
				{
					Hostname: "ratings.bookinfo.svc.cluster.local",
					Attributes: model.ServiceAttributes{
						Name:      "productpage",
						Namespace: "ratings",
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
							Hostname:  "service.federation.svc.test-remote-exports.local",
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
							Hostname:  "service.cluster.svc.test-remote-exports.local",
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
			expectedWatchEvents: nil,
		},
		{
			name:           "service export removed",
			remoteName:     "test-remote",
			serviceExports: exportProductPage,
			updatedExports: &v1.ExportedServiceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-remote",
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
							Hostname:  "service.federation.svc.test-remote-exports.local",
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
			serviceDiscovery := serviceregistrymemory.NewServiceDiscovery(tc.services...)
			serviceDiscovery.AddGateways(tc.gateways...)
			env := &model.Environment{
				ServiceDiscovery: serviceDiscovery,
			}
			s := createServer(env)
			stopCh := make(chan struct{})
			go s.Run(stopCh)
			defer close(stopCh)
			s.AddPeer(federation, tc.serviceExports, &common.FakeStatusHandler{})
			req, err := http.NewRequest("GET", "https://"+s.Addr()+"/v1/watch/"+tc.remoteName, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := httpsClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
			}
			for _, e := range tc.serviceEvents {
				s.UpdateService(e.svc, e.svc, e.event)
			}
			if tc.updatedExports != nil {
				if err := s.UpdateExportsForMesh(tc.updatedExports); err != nil {
					t.Errorf("failed to update ExportedServiceSet: %s", err)
				}
			}
			for _, e := range tc.gatewayEvents {
				serviceDiscovery.AddGateways(e.newGateways...)
				updatedSvc := &model.Service{
					Attributes: model.ServiceAttributes{
						ClusterExternalAddresses: &model.AddressMap{
							Addresses: map[cluster.ID][]string{
								"network1": {"a"},
							},
						},
					},
				}
				// trigger a gateway resync
				s.UpdateService(updatedSvc, updatedSvc, model.EventUpdate)
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
