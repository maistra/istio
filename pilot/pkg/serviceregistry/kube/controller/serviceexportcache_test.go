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

package controller

import (
	"context"
	"fmt"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/mcs"
	istiotest "istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceExportName      = "test-svc"
	serviceExportNamespace = "test-ns"
	serviceExportPodIP     = "128.0.0.2"
	testCluster            = "test-cluster"
)

var serviceExportNamespacedName = types.NamespacedName{
	Namespace: serviceExportNamespace,
	Name:      serviceExportName,
}

type ClusterLocalMode string

func (m ClusterLocalMode) String() string {
	return string(m)
}

const (
	alwaysClusterLocal ClusterLocalMode = "always cluster local"
	meshWide           ClusterLocalMode = "mesh wide"
)

var ClusterLocalModes = []ClusterLocalMode{alwaysClusterLocal, meshWide}

func TestServiceNotExported(t *testing.T) {
	for _, clusterLocalMode := range ClusterLocalModes {
		t.Run(clusterLocalMode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec := newTestServiceExportCache(t, clusterLocalMode)
			// Check that the endpoint is cluster-local
			ec.checkServiceInstancesOrFail(t, false)
		})
	}
}

func TestServiceExported(t *testing.T) {
	for _, clusterLocalMode := range ClusterLocalModes {
		t.Run(clusterLocalMode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec := newTestServiceExportCache(t, clusterLocalMode)
			// Export the service.
			ec.export(t)

			// Check that the endpoint is mesh-wide
			ec.checkServiceInstancesOrFail(t, true)
		})
	}
}

func TestServiceUnexported(t *testing.T) {
	for _, clusterLocalMode := range ClusterLocalModes {
		t.Run(clusterLocalMode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec := newTestServiceExportCache(t, clusterLocalMode)
			// Export the service and then unexport it immediately.
			ec.export(t)
			ec.unExport(t)

			// Check that the endpoint is cluster-local
			ec.checkServiceInstancesOrFail(t, false)
		})
	}
}

func newServiceExport() *unstructured.Unstructured {
	se := &mcsapi.ServiceExport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: mcs.MCSSchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceExportName,
			Namespace: serviceExportNamespace,
		},
	}
	return toUnstructured(se)
}

func newTestServiceExportCache(t *testing.T, clusterLocalMode ClusterLocalMode) *serviceExportCacheImpl {
	t.Helper()

	istiotest.SetForTest(t, &features.EnableMCSServiceDiscovery, true)
	istiotest.SetForTest(t, &features.EnableMCSClusterLocal, clusterLocalMode == alwaysClusterLocal)

	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID: testCluster,
		CRDs:      []schema.GroupVersionResource{mcs.ServiceExportGVR},
	})

	// Create the test service and endpoints.
	createService(c, serviceExportName, serviceExportNamespace, map[string]string{}, map[string]string{},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	createEndpoints(t, c, serviceExportName, serviceExportNamespace, []string{"tcp-port"}, []string{serviceExportPodIP}, nil, nil)

	ec := c.exports.(*serviceExportCacheImpl)
	// Wait for the resources to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		if svc := ec.GetService(ec.serviceHostname()); svc == nil {
			return false
		}
		inst := ec.getProxyServiceInstances()
		return len(inst) == 1 && inst[0].Service != nil && inst[0].Endpoint != nil
	}, serviceExportTimeout)
	return ec
}

func (ec *serviceExportCacheImpl) serviceHostname() host.Name {
	return kube.ServiceHostname(serviceExportName, serviceExportNamespace, ec.opts.DomainSuffix)
}

func (ec *serviceExportCacheImpl) export(t *testing.T) {
	t.Helper()

	_, err := ec.client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(serviceExportNamespace).Create(context.TODO(),
		newServiceExport(),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the export to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return ec.isExported(serviceExportNamespacedName)
	}, serviceExportTimeout, retry.Message("expected to be exported"))

	// Wait for the XDS event.
	ec.waitForXDS(t, true)
}

func (ec *serviceExportCacheImpl) unExport(t *testing.T) {
	t.Helper()

	_ = ec.client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(serviceExportNamespace).Delete(
		context.TODO(),
		serviceExportName,
		metav1.DeleteOptions{})

	// Wait for the delete to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return !ec.isExported(serviceExportNamespacedName)
	}, serviceExportTimeout)

	// Wait for the XDS event.
	ec.waitForXDS(t, false)
}

func (ec *serviceExportCacheImpl) waitForXDS(t *testing.T, exported bool) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		event := ec.opts.XDSUpdater.(*xdsfake.Updater).WaitOrFail(t, "eds")
		if len(event.Endpoints) != 1 {
			return fmt.Errorf("waitForXDS failed: expected 1 endpoint, found %d", len(event.Endpoints))
		}

		hostName := host.Name(event.ID)
		svc := ec.GetService(hostName)
		if svc == nil {
			return fmt.Errorf("unable to find service for host %s", hostName)
		}
		si := &model.ServiceInstance{
			Service:  svc,
			Endpoint: event.Endpoints[0],
		}
		return ec.checkServiceInstance(exported, si)
	}, serviceExportTimeout)
}

func (ec *serviceExportCacheImpl) getProxyServiceInstances() []*model.ServiceInstance {
	return ec.GetProxyServiceInstances(&model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{serviceExportPodIP},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: serviceExportNamespace,
		Labels: map[string]string{
			"app":                      "prod-app",
			label.SecurityTlsMode.Name: "mutual",
		},
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      ec.Cluster(),
			Labels: map[string]string{
				"app":                      "prod-app",
				label.SecurityTlsMode.Name: "mutual",
			},
		},
	})
}

func (ec *serviceExportCacheImpl) checkServiceInstancesOrFail(t *testing.T, exported bool) {
	t.Helper()
	if err := ec.checkServiceInstances(exported); err != nil {
		t.Fatal(err)
	}
}

func (ec *serviceExportCacheImpl) checkServiceInstances(exported bool) error {
	sis := ec.getProxyServiceInstances()
	if len(sis) != 1 {
		return fmt.Errorf("expected 1 ServiceInstance, found %d", len(sis))
	}
	return ec.checkServiceInstance(exported, sis[0])
}

func (ec *serviceExportCacheImpl) checkServiceInstance(exported bool, si *model.ServiceInstance) error {
	ep := si.Endpoint

	// Should always be discoverable from the same cluster.
	if err := ec.checkDiscoverableFromSameCluster(ep); err != nil {
		return err
	}

	if exported && !features.EnableMCSClusterLocal {
		return ec.checkDiscoverableFromDifferentCluster(ep)
	}

	return ec.checkNotDiscoverableFromDifferentCluster(ep)
}

func (ec *serviceExportCacheImpl) checkDiscoverableFromSameCluster(ep *model.IstioEndpoint) error {
	if !ec.isDiscoverableFromSameCluster(ep) {
		return fmt.Errorf("endpoint was not discoverable from the same cluster")
	}
	return nil
}

func (ec *serviceExportCacheImpl) checkDiscoverableFromDifferentCluster(ep *model.IstioEndpoint) error {
	if !ec.isDiscoverableFromDifferentCluster(ep) {
		return fmt.Errorf("endpoint was not discoverable from a different cluster")
	}
	return nil
}

func (ec *serviceExportCacheImpl) checkNotDiscoverableFromDifferentCluster(ep *model.IstioEndpoint) error {
	if ec.isDiscoverableFromDifferentCluster(ep) {
		return fmt.Errorf("endpoint was discoverable from a different cluster")
	}
	return nil
}

func (ec *serviceExportCacheImpl) isDiscoverableFromSameCluster(ep *model.IstioEndpoint) bool {
	return ep.IsDiscoverableFromProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: ec.Cluster(),
		},
	})
}

func (ec *serviceExportCacheImpl) isDiscoverableFromDifferentCluster(ep *model.IstioEndpoint) bool {
	return ep.IsDiscoverableFromProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: "some-other-cluster",
		},
	})
}
