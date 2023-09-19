// Copyright Istio Authors
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

package xds_test

import (
	"fmt"
	"reflect"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func TestDeltaAds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.ClusterType)
	ads.RequestResponseAck(nil)
}

func TestDeltaAdsClusterUpdate(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.EndpointType)
	nonce := ""
	sendEDSReqAndVerify := func(add, remove, expect []string) {
		t.Helper()
		res := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
			ResponseNonce:            nonce,
			ResourceNamesSubscribe:   add,
			ResourceNamesUnsubscribe: remove,
		})
		nonce = res.Nonce
		got := xdstest.MapKeys(xdstest.ExtractLoadAssignments(xdstest.UnmarshalClusterLoadAssignment(t, model.ResourcesToAny(res.Resources))))
		if !reflect.DeepEqual(expect, got) {
			t.Fatalf("expected clusters %v got %v", expect, got)
		}
	}

	sendEDSReqAndVerify([]string{"outbound|80||local.default.svc.cluster.local"}, nil, []string{"outbound|80||local.default.svc.cluster.local"})
	// Only send the one that is requested
	sendEDSReqAndVerify([]string{"outbound|81||local.default.svc.cluster.local"}, nil, []string{"outbound|81||local.default.svc.cluster.local"})
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResponseNonce:            nonce,
		ResourceNamesUnsubscribe: []string{"outbound|81||local.default.svc.cluster.local"},
	})
	ads.ExpectNoResponse()
}

func TestDeltaEDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-locality.yaml"),
	})
	addTestClientEndpoints(s.MemRegistry)
	s.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))

	// Wait until the above debounce, to ensure we can precisely check XDS responses without spurious pushes
	s.EnsureSynced(t)

	ads := s.ConnectDeltaADS().WithType(v3.EndpointType)
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"outbound|80||test-1.default"},
	})
	resp := ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|80||test-1.default" {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"outbound|8080||" + edsIncSvc},
	})
	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// update endpoint
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	t.Logf("update svc")
	// update svc, only send the eds for this service
	s.MemRegistry.AddHTTPService(edsIncSvc, "10.10.1.3", 8080)

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// delete svc, only send eds for this service
	s.MemRegistry.RemoveService(edsIncSvc)

	resp = ads.ExpectResponse()
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}
	if len(resp.Resources) != 0 {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
}

func TestDeltaReconnectRequests(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		Services: []*model.Service{
			{
				Hostname:       "adsupdate.example.com",
				DefaultAddress: "10.11.0.1",
				Ports: []*model.Port{
					{
						Name:     "http-main",
						Port:     2080,
						Protocol: protocol.HTTP,
					},
				},
				Attributes: model.ServiceAttributes{
					Name:      "adsupdate",
					Namespace: "default",
				},
			},
			{
				Hostname:       "adsstatic.example.com",
				DefaultAddress: "10.11.0.2",
				Ports: []*model.Port{
					{
						Name:     "http-main",
						Port:     2080,
						Protocol: protocol.HTTP,
					},
				},
				Attributes: model.ServiceAttributes{
					Name:      "adsstatic",
					Namespace: "default",
				},
			},
		},
	})

	const updateCluster = "outbound|2080||adsupdate.example.com"
	const staticCluster = "outbound|2080||adsstatic.example.com"
	ads := s.ConnectDeltaADS()
	// Send initial request
	res := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{TypeUrl: v3.ClusterType})
	// we must get the cluster back
	if resn := xdstest.ExtractResource(res.Resources); !resn.Contains(updateCluster) || !resn.Contains(staticCluster) {
		t.Fatalf("unexpected resources: %v", resn)
	}

	// A push should get a response
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true})
	ads.ExpectResponse()

	// Close the connection
	ads.Cleanup()

	// Service is removed while connection is closed
	s.MemRegistry.RemoveService("adsupdate.example.com")
	s.Discovery.ConfigUpdate(&model.PushRequest{
		Full:           true,
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "adsupdate.example.com", Namespace: "default"}),
	})
	s.EnsureSynced(t)

	ads = s.ConnectDeltaADS()
	// Send initial request
	res = ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		TypeUrl: v3.ClusterType,
		InitialResourceVersions: map[string]string{
			// This time we include the version map, since it is a reconnect
			staticCluster: "",
			updateCluster: "",
		},
	})
	// we must NOT get the cluster back
	if resn := xdstest.ExtractResource(res.Resources); resn.Contains(updateCluster) || !resn.Contains(staticCluster) {
		t.Fatalf("unexpected resources: %v", resn)
	}
	// It should be removed
	if resn := sets.New(res.RemovedResources...); !resn.Contains(updateCluster) {
		t.Fatalf("unexpected remove resources: %v", resn)
	}
}

func TestDeltaWDS(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	wlA := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:       fmt.Sprintf("Kubernetes//Pod/%s/%s", "test", "a"),
			Namespace: "test",
			Name:      "a",
		},
	}
	wlB := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:       fmt.Sprintf("Kubernetes//Pod/%s/%s", "test", "b"),
			Namespace: "test",
			Name:      "n",
		},
	}
	wlC := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:       fmt.Sprintf("Kubernetes//Pod/%s/%s", "test", "c"),
			Namespace: "test",
			Name:      "c",
		},
	}
	svcA := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "a",
			Namespace: "default",
			Hostname:  "a.default.svc.cluster.local",
		},
	}
	svcB := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "b",
			Namespace: "default",
			Hostname:  "b.default.svc.cluster.local",
		},
	}
	svcC := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "c",
			Namespace: "default",
			Hostname:  "c.default.svc.cluster.local",
		},
	}
	s.MemRegistry.AddWorkloadInfo(wlA, wlB, wlC)
	s.MemRegistry.AddServiceInfo(svcA, svcB, svcC)

	// Wait until the above debounce, to ensure we can precisely check XDS responses without spurious pushes
	s.EnsureSynced(t)

	ads := s.ConnectDeltaADS().WithType(v3.AddressType).WithID("ztunnel~1.1.1.1~test.default~default.svc.cluster.local")
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"*"},
	})
	resp := ads.ExpectResponse()
	if len(resp.Resources) != 6 {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// simulate a svc update
	s.XdsUpdater.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{
			Kind: kind.Address, Name: svcA.ResourceName(), Namespace: svcA.Namespace,
		}),
	})

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != svcA.ResourceName() {
		t.Fatalf("received unexpected address resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// simulate a svc delete
	s.MemRegistry.RemoveServiceInfo(svcA)
	s.XdsUpdater.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{
			Kind: kind.Address, Name: svcA.ResourceName(), Namespace: svcA.Namespace,
		}),
	})

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 0 {
		t.Fatalf("received unexpected address resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != svcA.ResourceName() {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// delete workload
	s.MemRegistry.RemoveWorkloadInfo(wlA)
	// a full push and a pod delete event
	// This is a merged push request
	s.XdsUpdater.ConfigUpdate(&model.PushRequest{
		Full: true,
	})

	resp = ads.ExpectResponse()
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != wlA.ResourceName() {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}
	if len(resp.Resources) != 4 {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
}
