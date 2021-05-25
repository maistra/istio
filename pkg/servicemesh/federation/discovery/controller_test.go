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
package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"maistra.io/api/client/versioned/fake"
	maistrav1alpha1 "maistra.io/api/core/v1alpha1"

	"istio.io/api/mesh/v1alpha1"
	configmemory "istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/federation/common"
)

type fakeManager struct{}

func (m *fakeManager) AddMeshFederation(mesh *maistrav1alpha1.MeshFederation, exports *maistrav1alpha1.ServiceExports) error {
	return nil
}
func (m *fakeManager) DeleteMeshFederation(name string) {}
func (m *fakeManager) UpdateExportsForMesh(exports *maistrav1alpha1.ServiceExports) error {
	return nil
}
func (m *fakeManager) DeleteExportsForMesh(name string) {}

func TestValidOptions(t *testing.T) {
	opt := Options{
		ControllerOptions: common.ControllerOptions{
			KubeClient: kube.NewFakeClient(),
		},
		ServiceController: &aggregate.Controller{},
		XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
		Env:               &model.Environment{},
		FederationManager: &fakeManager{},
	}
	if err := opt.validate(); err != nil {
		t.Errorf("unexpected error")
	}
}

func TestInvalidOptions(t *testing.T) {
	testCases := []struct {
		name string
		opt  Options
	}{
		{
			name: "client",
			opt: Options{
				ControllerOptions: common.ControllerOptions{
					KubeClient: nil,
				},
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
				Env:               &model.Environment{},
			},
		},
		{
			name: "service-controller",
			opt: Options{
				ControllerOptions: common.ControllerOptions{
					KubeClient: kube.NewFakeClient(),
				},
				ServiceController: nil,
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
				Env:               &model.Environment{},
			},
		},
		{
			name: "xds-updater",
			opt: Options{
				ControllerOptions: common.ControllerOptions{
					KubeClient: kube.NewFakeClient(),
				},
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        nil,
				Env:               &model.Environment{},
			},
		},
		{
			name: "env",
			opt: Options{
				ControllerOptions: common.ControllerOptions{
					KubeClient: kube.NewFakeClient(),
				},
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
				Env:               nil,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewController(tc.opt); err == nil {
				t.Errorf("expected error")
			}
		})
	}
}

type options struct {
	client            kube.Client
	serviceController *aggregate.Controller
	xdsUpdater        *v1alpha3.FakeXdsUpdater
	env               *model.Environment
}

func newTestOptions(discoveryAddress string) options {
	client := kube.NewFakeClient()
	meshConfig := &v1alpha1.MeshConfig{
		DefaultConfig: &v1alpha1.ProxyConfig{
			DiscoveryAddress: discoveryAddress,
		},
	}
	meshWatcher := mesh.NewFixedWatcher(meshConfig)
	serviceController := aggregate.NewController(aggregate.Options{
		MeshHolder: meshWatcher,
	})
	xdsUpdater := &v1alpha3.FakeXdsUpdater{}
	env := &model.Environment{
		ServiceDiscovery: serviceController,
		Watcher:          meshWatcher,
	}
	return options{
		client:            client,
		serviceController: serviceController,
		xdsUpdater:        xdsUpdater,
		env:               env,
	}
}

func TestReconcile(t *testing.T) {
	resyncPeriod := 30 * time.Second
	options := newTestOptions("test.address")
	controller := internalNewController(fake.NewSimpleClientset(), nil, Options{
		ControllerOptions: common.ControllerOptions{
			KubeClient:   kube.NewFakeClient(),
			ResyncPeriod: resyncPeriod,
			Namespace:    "",
		},
		ServiceController: options.serviceController,
		XDSUpdater:        options.xdsUpdater,
		Env:               options.env,
		FederationManager: &fakeManager{},
		ConfigStore:       configmemory.NewController(configmemory.Make(Schemas)),
	})

	name := "test"
	namespace := "test"
	federation := &maistrav1alpha1.MeshFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: maistrav1alpha1.MeshFederationSpec{
			NetworkAddress: "test.mesh",
			Gateways: maistrav1alpha1.MeshFederationGateways{
				Ingress: corev1.LocalObjectReference{
					Name: "test-ingress",
				},
				Egress: corev1.LocalObjectReference{
					Name: "test-egress",
				},
			},
			Security: &maistrav1alpha1.MeshFederationSecurity{
				ClientID:            "cluster.local/ns/test-mesh/sa/test-egress-service-account",
				TrustDomain:         "test.local",
				CertificateChain:    "dummy",
				AllowDirectInbound:  false,
				AllowDirectOutbound: false,
			},
		},
	}
	cs := controller.cs
	fedwatch, err := cs.CoreV1alpha1().MeshFederations(namespace).Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("failed to watch for MeshFederation")
		return
	}
	newFederation, err := cs.CoreV1alpha1().MeshFederations(namespace).Create(context.TODO(), federation, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("failed to create MeshFederation")
		fedwatch.Stop()
		return
	}
	// wait for object to show up
	func() {
		defer fedwatch.Stop()
		select {
		case event := <-fedwatch.ResultChan():
			if event.Type == watch.Added {
				metaObj, _ := meta.Accessor(event.Object)
				if metaObj != nil && metaObj.GetName() == name && metaObj.GetNamespace() == namespace {
					break
				}
			}
			t.Errorf("unexpected watch event: %#v", event)
		case <-time.After(5 * time.Second):
			t.Errorf("timed out waiting for watch event")
		}
	}()
	if err := controller.reconcile(fmt.Sprintf("%s/%s", namespace, name)); err != nil {
		t.Errorf("unexpected error reconciling new MeshFederation: %#v", err)
	}
	// verify registry has been created
	if controller.getRegistry(cluster.ID(newFederation.Name)) == nil {
		t.Errorf("failed to create service registry for federation")
	}
	// verify resources have been created
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource == nil {
		t.Errorf("resource doesn't exist")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource == nil {
		t.Errorf("resource doesn't exist")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		discoveryIngressResourceName(federation), namespace); resource == nil {
		t.Errorf("resource doesn't exist")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		discoveryEgressResourceName(federation), namespace); resource == nil {
		t.Errorf("resource doesn't exist")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource == nil {
		t.Errorf("resource doesn't exist")
	}
	if resource := controller.Get(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource == nil {
		t.Errorf("resource doesn't exist")
	}

	// now delete
	fedwatch, err = cs.CoreV1alpha1().MeshFederations(namespace).Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("failed to watch for MeshFederation")
		return
	}
	if err = cs.CoreV1alpha1().MeshFederations(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		t.Errorf("error deleting MeshFederation")
		fedwatch.Stop()
		return
	}

	// wait for deletion to show up
	func() {
		defer fedwatch.Stop()
		select {
		case event := <-fedwatch.ResultChan():
			if event.Type == watch.Deleted {
				metaObj, _ := meta.Accessor(event.Object)
				if metaObj != nil && metaObj.GetName() == name && metaObj.GetNamespace() == namespace {
					break
				}
			}
			t.Errorf("unexpected watch event: %#v", event)
		case <-time.After(5 * time.Second):
			t.Errorf("timed out waiting for watch event")
		}
	}()

	if err := controller.reconcile(fmt.Sprintf("%s/%s", namespace, name)); err != nil {
		t.Errorf("unexpected error reconciling new MeshFederation: %#v", err)
	}
	// verify registry has been deleted
	if controller.getRegistry(cluster.ID(newFederation.Name)) != nil {
		t.Errorf("failed to delete service registry for federation")
	}
	// verify resources have been deleted
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource != nil {
		t.Errorf("resource not deleted")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource != nil {
		t.Errorf("resource not deleted")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		discoveryIngressResourceName(federation), namespace); resource != nil {
		t.Errorf("resource not deleted")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		discoveryEgressResourceName(federation), namespace); resource != nil {
		t.Errorf("resource not deleted")
	}
	if resource := controller.Get(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource != nil {
		t.Errorf("resource not deleted")
	}
	if resource := controller.Get(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
		discoveryResourceName(federation), namespace); resource != nil {
		t.Errorf("resource not deleted")
	}
}
