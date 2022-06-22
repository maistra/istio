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

package aggregate

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/federation"
	"istio.io/istio/pilot/pkg/serviceregistry/mock"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

type mockMeshConfigHolder struct {
	trustDomainAliases []string
}

func (mh mockMeshConfigHolder) Mesh() *meshconfig.MeshConfig {
	return &meshconfig.MeshConfig{
		TrustDomainAliases: mh.trustDomainAliases,
	}
}

var (
	meshHolder mockMeshConfigHolder
	discovery1 *mock.ServiceDiscovery
	discovery2 *mock.ServiceDiscovery
)

func buildMockController() *Controller {
	discovery1 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.ReplicatedFooServiceName: mock.ReplicatedFooServiceV1.DeepCopy(),
			mock.HelloService.Hostname:    mock.HelloService.DeepCopy(),
			mock.ExtHTTPService.Hostname:  mock.ExtHTTPService.DeepCopy(),
		}, 2)

	discovery2 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.ReplicatedFooServiceName: mock.ReplicatedFooServiceV2.DeepCopy(),
			mock.WorldService.Hostname:    mock.WorldService.DeepCopy(),
			mock.ExtHTTPSService.Hostname: mock.ExtHTTPSService.DeepCopy(),
		}, 2)

	registry1 := serviceregistry.Simple{
		ProviderID:       serviceregistry.ProviderID("mockAdapter1"),
		ServiceDiscovery: discovery1,
		Controller:       &mock.Controller{},
	}

	registry2 := serviceregistry.Simple{
		ProviderID:       serviceregistry.ProviderID("mockAdapter2"),
		ServiceDiscovery: discovery2,
		Controller:       &mock.Controller{},
	}

	ctls := NewController(Options{&meshHolder})
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	return ctls
}

func buildMockControllerForMultiCluster() *Controller {
	discovery1 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname: mock.MakeService("hello.default.svc.cluster.local", "10.1.1.0", []string{}),
		}, 2)

	discovery2 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname: mock.MakeService("hello.default.svc.cluster.local", "10.1.2.0", []string{}),
			mock.WorldService.Hostname: mock.WorldService.DeepCopy(),
		}, 2)

	registry1 := serviceregistry.Simple{
		ProviderID:       serviceregistry.Kubernetes,
		ClusterID:        "cluster-1",
		ServiceDiscovery: discovery1,
		Controller:       &mock.Controller{},
	}

	registry2 := serviceregistry.Simple{
		ProviderID:       serviceregistry.Kubernetes,
		ClusterID:        "cluster-2",
		ServiceDiscovery: discovery2,
		Controller:       &mock.Controller{},
	}

	ctls := NewController(Options{})
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	return ctls
}

func buildMockControllerForFederation() *Controller {
	discovery1 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname: mock.MakeService("hello.default.svc.cluster.local", "10.1.1.0", []string{}),
		}, 2)

	discovery2 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname: mock.MakeService("hello.default.svc.cluster.local", "10.1.2.0", []string{}),
			mock.WorldService.Hostname: mock.WorldService.DeepCopy(),
		}, 2)

	registry1 := serviceregistry.Simple{
		ProviderID:       serviceregistry.Kubernetes,
		ClusterID:        "cluster-1",
		ServiceDiscovery: discovery1,
		Controller:       &mock.Controller{},
	}

	registry2 := serviceregistry.Simple{
		ProviderID:       serviceregistry.Federation,
		ClusterID:        "cluster-2",
		ServiceDiscovery: discovery2,
		Controller:       &federation.Controller{},
	}

	ctls := NewController(Options{})
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	return ctls
}

func TestServicesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.ServicesError = errors.New("mock Services() error")

	// List Services from aggregate controller
	_, err := aggregateCtl.Services()
	if err == nil {
		t.Fatal("Aggregate controller should return error if one discovery client experience error")
	}
}
func TestServicesForMultiCluster(t *testing.T) {
	aggregateCtl := buildMockControllerForMultiCluster()
	// List Services from aggregate controller
	services, err := aggregateCtl.Services()
	if err != nil {
		t.Fatalf("Services() encountered unexpected error: %v", err)
	}

	// Set up ground truth hostname values
	serviceMap := map[host.Name]bool{
		mock.HelloService.Hostname: false,
		mock.WorldService.Hostname: false,
	}

	svcCount := 0
	// Compare return value to ground truth
	for _, svc := range services {
		if counted, existed := serviceMap[svc.Hostname]; existed && !counted {
			svcCount++
			serviceMap[svc.Hostname] = true
		}
	}

	if svcCount != len(serviceMap) {
		t.Fatalf("Service map expected size %d, actual %v", svcCount, serviceMap)
	}

	//Now verify ClusterVIPs for each service
	ClusterVIPs := map[host.Name]map[string]string{
		mock.HelloService.Hostname: {
			"cluster-1": "10.1.1.0",
			"cluster-2": "10.1.2.0",
		},
		mock.WorldService.Hostname: {
			"cluster-2": "10.2.0.0",
		},
	}
	for _, svc := range services {
		if !reflect.DeepEqual(svc.ClusterVIPs, ClusterVIPs[svc.Hostname]) {
			t.Fatalf("Service %s ClusterVIPs actual %v, expected %v", svc.Hostname, svc.ClusterVIPs, ClusterVIPs[svc.Hostname])
		}
	}
	t.Logf("Return service ClusterVIPs match ground truth")
}

func TestServices(t *testing.T) {
	aggregateCtl := buildMockController()
	// List Services from aggregate controller
	services, err := aggregateCtl.Services()

	// Set up ground truth hostname values
	serviceMap := map[host.Name]bool{
		mock.HelloService.Hostname:    false,
		mock.ExtHTTPService.Hostname:  false,
		mock.WorldService.Hostname:    false,
		mock.ExtHTTPSService.Hostname: false,
	}

	if err != nil {
		t.Fatalf("Services() encountered unexpected error: %v", err)
	}

	svcCount := 0
	// Compare return value to ground truth
	for _, svc := range services {
		if counted, existed := serviceMap[svc.Hostname]; existed && !counted {
			svcCount++
			serviceMap[svc.Hostname] = true
		}
	}

	if svcCount != len(serviceMap) {
		t.Fatal("Return services does not match ground truth")
	}
}

func TestGetService(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get service from mockAdapter1
	svc, err := aggregateCtl.GetService(mock.HelloService.Hostname)
	if err != nil {
		t.Fatalf("GetService() encountered unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("Fail to get service")
	}
	if svc.Hostname != mock.HelloService.Hostname {
		t.Fatal("Returned service is incorrect")
	}

	// Get service from mockAdapter2
	svc, err = aggregateCtl.GetService(mock.WorldService.Hostname)
	if err != nil {
		t.Fatalf("GetService() encountered unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("Fail to get service")
	}
	if svc.Hostname != mock.WorldService.Hostname {
		t.Fatal("Returned service is incorrect")
	}
}

func TestGetServiceError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.GetServiceError = errors.New("mock GetService() error")

	// Get service from client with error
	svc, err := aggregateCtl.GetService(mock.HelloService.Hostname)
	if err == nil {
		fmt.Println(svc)
		t.Fatal("Aggregate controller should return error if one discovery client experiences " +
			"error and no service is found")
	}
	if svc != nil {
		t.Fatal("GetService() should return nil if no service found")
	}

	// Get service from client without error
	svc, err = aggregateCtl.GetService(mock.WorldService.Hostname)
	if err != nil {
		t.Fatal("Aggregate controller should not return error if service is found")
	}
	if svc.Hostname != mock.WorldService.Hostname {
		t.Fatal("Returned service is incorrect")
	}
}

func TestGetProxyServiceInstances(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get Instances from mockAdapter1
	instances := aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.HelloInstanceV0}})
	if len(instances) != 6 {
		t.Fatalf("Returned GetProxyServiceInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.HelloService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}

	// Get Instances from mockAdapter2
	instances = aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.MakeIP(mock.WorldService, 1)}})
	if len(instances) != 6 {
		t.Fatalf("Returned GetProxyServiceInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}
}

func TestGetProxyWorkloadLabels(t *testing.T) {
	// If no registries return workload labels, we must return nil, rather than an empty list.
	// This ensures callers can distinguish between no labels, and labels not found.
	aggregateCtl := buildMockController()

	instances := aggregateCtl.GetProxyWorkloadLabels(&model.Proxy{IPAddresses: []string{mock.HelloInstanceV0}})
	if instances != nil {
		t.Fatalf("expected nil workload labels, got: %v", instances)
	}
}

func TestGetProxyServiceInstancesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.GetProxyServiceInstancesError = errors.New("mock GetProxyServiceInstances() error")

	// Get Instances from client with error
	instances := aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.HelloInstanceV0}})
	if len(instances) != 0 {
		t.Fatal("GetProxyServiceInstances() should return no instances is client experiences error")
	}

	// Get Instances from client without error
	instances = aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.MakeIP(mock.WorldService, 1)}})
	if len(instances) != 6 {
		t.Fatalf("Returned GetProxyServiceInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}
}

func TestInstances(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get Instances from mockAdapter1
	instances := aggregateCtl.InstancesByPort(mock.HelloService,
		80,
		labels.Collection{})
	if len(instances) != 2 {
		t.Fatal("Returned wrong number of instances from controller")
	}
	for _, instance := range instances {
		if instance.Service.Hostname != mock.HelloService.Hostname {
			t.Fatal("Returned instance's hostname does not match desired value")
		}
		if _, ok := instance.Service.Ports.Get(mock.PortHTTPName); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}

	// Get Instances from mockAdapter2
	instances = aggregateCtl.InstancesByPort(mock.WorldService,
		80,
		labels.Collection{})
	if len(instances) != 2 {
		t.Fatal("Returned wrong number of instances from controller")
	}
	for _, instance := range instances {
		if instance.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned instance's hostname does not match desired value")
		}
		if _, ok := instance.Service.Ports.Get(mock.PortHTTPName); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}
}

func TestGetIstioServiceAccounts(t *testing.T) {
	aggregateCtl := buildMockController()
	testCases := []struct {
		name               string
		svc                *model.Service
		trustDomainAliases []string
		want               []string
	}{
		{
			name: "HelloEmpty",
			svc:  mock.HelloService,
			want: []string{},
		},
		{
			name: "World",
			svc:  mock.WorldService,
			want: []string{
				"spiffe://cluster.local/ns/default/sa/world1",
				"spiffe://cluster.local/ns/default/sa/world2",
			},
		},
		{
			name: "ReplicatedFoo",
			svc:  mock.ReplicatedFooServiceV1,
			want: []string{
				"spiffe://cluster.local/ns/default/sa/foo-share",
				"spiffe://cluster.local/ns/default/sa/foo1",
				"spiffe://cluster.local/ns/default/sa/foo2",
			},
		},
		{
			name:               "ExpansionByTrustDomainAliases",
			trustDomainAliases: []string{"cluster.local", "example.com"},
			svc:                mock.WorldService,
			want: []string{
				"spiffe://cluster.local/ns/default/sa/world1",
				"spiffe://cluster.local/ns/default/sa/world2",
				"spiffe://example.com/ns/default/sa/world1",
				"spiffe://example.com/ns/default/sa/world2",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			meshHolder.trustDomainAliases = tc.trustDomainAliases
			accounts := aggregateCtl.GetIstioServiceAccounts(tc.svc, []int{})
			if diff := cmp.Diff(accounts, tc.want); diff != "" {
				t.Errorf("unexpected service account, diff %v", diff)
			}
		})
	}
}

// Test the aggregate controller with federation providers
func TestFederationGetIstioServiceAccounts(t *testing.T) {
	federationCtl := buildMockControllerForFederation()
	testCases := []struct {
		name               string
		svc                *model.Service
		trustDomainAliases []string
		want               []string
	}{
		{
			name: "HelloEmpty",
			svc:  mock.HelloService,
			want: []string{},
		},
		{
			name: "World",
			svc:  mock.WorldService,
			want: []string{
				"spiffe://cluster.local/ns/default/sa/world1",
				"spiffe://cluster.local/ns/default/sa/world2",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			meshHolder.trustDomainAliases = tc.trustDomainAliases
			accounts := federationCtl.GetIstioServiceAccounts(tc.svc, []int{})
			if diff := cmp.Diff(accounts, tc.want); diff != "" {
				t.Errorf("unexpected service account, diff %v", diff)
			}
		})
	}
}

func TestAddRegistry(t *testing.T) {

	registries := []serviceregistry.Simple{
		{
			ProviderID: "registry1",
			ClusterID:  "cluster1",
		},
		{
			ProviderID: "registry2",
			ClusterID:  "cluster2",
		},
	}
	ctrl := NewController(Options{})
	for _, r := range registries {
		ctrl.AddRegistry(r)
	}
	if l := len(ctrl.registries); l != 2 {
		t.Fatalf("Expected length of the registries slice should be 2, got %d", l)
	}
}

func TestGetDeleteRegistry(t *testing.T) {
	registries := []serviceregistry.Simple{
		{
			ProviderID: "registry1",
			ClusterID:  "cluster1",
		},
		{
			ProviderID: "registry2",
			ClusterID:  "cluster2",
		},
		{
			ProviderID: "registry3",
			ClusterID:  "cluster3",
		},
	}
	ctrl := NewController(Options{})
	for _, r := range registries {
		ctrl.AddRegistry(r)
	}

	// Test Get
	result := ctrl.GetRegistries()
	if l := len(result); l != 3 {
		t.Fatalf("Expected length of the registries slice should be 3, got %d", l)
	}

	// Test Delete cluster2
	ctrl.DeleteRegistry(registries[1].ClusterID, registries[1].ProviderID)
	result = ctrl.GetRegistries()
	if l := len(result); l != 2 {
		t.Fatalf("Expected length of the registries slice should be 2, got %d", l)
	}
	// check left registries are orders as before
	if !reflect.DeepEqual(result[0], registries[0]) || !reflect.DeepEqual(result[1], registries[2]) {
		t.Fatalf("Expected registries order has been changed")
	}
}

func TestSkipSearchingRegistryForProxy(t *testing.T) {
	cluster1 := serviceregistry.Simple{ClusterID: "cluster-1", ProviderID: serviceregistry.Kubernetes}
	cluster2 := serviceregistry.Simple{ClusterID: "cluster-2", ProviderID: serviceregistry.Kubernetes}
	// external registries may eventually be associated with a cluster
	external := serviceregistry.Simple{ClusterID: "cluster-1", ProviderID: serviceregistry.External}

	cases := []struct {
		nodeClusterID string
		registry      serviceregistry.Instance
		want          bool
	}{
		// matching kube registry
		{"cluster-1", cluster1, false},
		// unmatching kube registry
		{"cluster-1", cluster2, true},
		// always search external
		{"cluster-1", external, false},
		{"cluster-2", external, false},
		{"", external, false},
		// always search for empty node cluster id
		{"", cluster1, false},
		{"", cluster2, false},
		{"", external, false},
	}

	for i, c := range cases {
		got := skipSearchingRegistryForProxy(c.nodeClusterID, c.registry)
		if got != c.want {
			t.Errorf("%s: got %v want %v",
				fmt.Sprintf("[%v] registry=%v node=%v", i, c.registry, c.nodeClusterID),
				got, c.want)
		}
	}
}
