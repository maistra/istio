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

package ior

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	routeapiv1 "github.com/openshift/api/route/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	k8sioapicorev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/istio/pkg/test/util/retry"
)

func initClients(t *testing.T, stop <-chan struct{}, errorChannel chan error, mrc memberroll.MemberRollController) (model.ConfigStoreCache, kube.Client, routev1.RouteV1Interface) {
	t.Helper()

	k8sClient := kube.NewFakeClient()
	iorKubeClient := NewFakeKubeClient(k8sClient)
	routerClient := NewFakeRouterClient()
	store, err := crdclient.New(k8sClient, "", controller.Options{EnableCRDScan: false})
	if err != nil {
		t.Fatal(err)
	}

	go store.Run(stop)
	k8sClient.RunAndWait(stop)
	cache.WaitForCacheSync(stop, store.HasSynced)
	retry.UntilSuccessOrFail(t, func() error {
		if !store.HasSynced() {
			return fmt.Errorf("store has not synced yet")
		}
		return nil
	}, retry.Timeout(time.Second))

	if err := Register(iorKubeClient, routerClient, store, "istio-system", mrc, stop, errorChannel); err != nil {
		t.Fatal(err)
	}

	return store, k8sClient, routerClient
}

func generateNamespaces(qty int) []string {
	var result []string

	for i := 1; i <= qty; i++ {
		result = append(result, fmt.Sprintf("ns%d", i))
	}

	return append(result, "istio-system")
}

func createGateways(t *testing.T, store model.ConfigStoreCache, qty int) {
	for i := 1; i <= qty; i++ {
		createGateway(t, store, fmt.Sprintf("ns%d", i), fmt.Sprintf("gw-ns%d", i), []string{fmt.Sprintf("d%d.com", i)}, map[string]string{"istio": "ingressgateway"}, false)
	}
}

func TestCreate(t *testing.T) {
	cases := []struct {
		testName       string
		ns             string
		hosts          []string
		gwSelector     map[string]string
		expectedRoutes int
		expectedError  string
		tls            bool
	}{
		{
			"One host",
			"istio-system",
			[]string{"one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
		},
		{
			"Two hosts",
			"istio-system",
			[]string{"two.org", "three.com"},
			map[string]string{"istio": "ingressgateway"},
			2,
			"",
			false,
		},
		{
			"Wildcard 1",
			"istio-system",
			[]string{"*"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
		},
		{
			"Wildcard 2",
			"istio-system",
			[]string{"*.a.com"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
		},
		{
			"Invalid gateway",
			"non-existent",
			[]string{"fail.com"},
			map[string]string{"istio": "nonexistent"},
			0,
			"could not find a service that matches the gateway selector `istio=nonexistent'",
			false,
		},
		{
			"TLS 1",
			"istio-system",
			[]string{"one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			true,
		},
	}

	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient := initClients(t, stop, errorChannel, mrc)
	mrc.setNamespaces("istio-system")

	controlPlane := "istio-system"
	createIngressGateway(t, k8sClient, controlPlane, map[string]string{"istio": "ingressgateway"})

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			gatewayName := fmt.Sprintf("gw%d", i)
			createGateway(t, store, c.ns, gatewayName, c.hosts, c.gwSelector, c.tls)

			list := getRoutes(t, routerClient, controlPlane, c.expectedRoutes, time.Second)
			if err := getError(errorChannel); err != nil {
				if c.expectedError == "" {
					t.Fatal(err)
				}

				if !strings.Contains(err.Error(), c.expectedError) {
					t.Fatalf("expected error message containing `%s', got: %s", c.expectedError, err.Error())
				}

				// Error is expected and matches the golden string, nothing to do
			} else {
				validateRoutes(t, c.hosts, list, gatewayName, c.tls)
			}

			// Remove the gateway and expect all routes get removed
			deleteGateway(t, store, c.ns, gatewayName)
			_ = getRoutes(t, routerClient, c.ns, 0, time.Second)
		})
	}
}

func validateRoutes(t *testing.T, hosts []string, list *routeapiv1.RouteList, gatewayName string, tls bool) {
	t.Helper()
	for _, host := range hosts {
		route := findRouteByHost(list, host)
		if route == nil {
			t.Fatalf("could not find a route with hostname %s", host)
		}

		// Check metadata
		if route.Labels[gatewayNameLabel] != gatewayName {
			t.Fatalf("wrong label, expecting %s, got %s", gatewayName, route.Annotations[gatewayNameLabel])
		}
		if route.Annotations["foo"] != "bar" {
			t.Fatal("gateway annotations were not copied to route")
		}

		// Check hostname
		if host == "*" && route.Spec.Host == "*" {
			t.Fatal("Route's host wrongly set to *")
		}
		if strings.Contains(host, "*.") && !strings.Contains(route.Spec.Host, "wildcard.") {
			t.Fatal("Route's host wrongly not set to wildcard.")
		}

		// TLS
		if tls {
			if route.Spec.TLS.InsecureEdgeTerminationPolicy != routeapiv1.InsecureEdgeTerminationPolicyRedirect {
				t.Fatalf("wrong InsecureEdgeTerminationPolicy: %v", route.Spec.TLS.InsecureEdgeTerminationPolicy)
			}
			if route.Spec.TLS.Termination != routeapiv1.TLSTerminationPassthrough {
				t.Fatalf("wrong Termination: %v", route.Spec.TLS.Termination)
			}
		}
	}

}

func TestEdit(t *testing.T) {
	cases := []struct {
		testName       string
		ns             string
		hosts          []string
		gwSelector     map[string]string
		expectedRoutes int
		expectedError  string
		tls            bool
	}{
		{
			"One host",
			"istio-system",
			[]string{"def.com"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
		},
		{
			"Two hosts",
			"istio-system",
			[]string{"ghi.org", "jkl.com"},
			map[string]string{"istio": "ingressgateway"},
			2,
			"",
			false,
		},
		{
			"Wildcard 1",
			"istio-system",
			[]string{"*"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
		},
		{
			"Wildcard 2",
			"istio-system",
			[]string{"*.a.com"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
		},
		{
			"TLS 1",
			"istio-system",
			[]string{"one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			true,
		},
	}

	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient := initClients(t, stop, errorChannel, mrc)

	controlPlane := "istio-system"
	createIngressGateway(t, k8sClient, controlPlane, map[string]string{"istio": "ingressgateway"})
	createGateway(t, store, controlPlane, "gw", []string{"abc.com"}, map[string]string{"istio": "ingressgateway"}, false)
	mrc.setNamespaces("istio-system")

	list := getRoutes(t, routerClient, controlPlane, 1, time.Second)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			editGateway(t, store, c.ns, "gw", c.hosts, c.gwSelector, c.tls, fmt.Sprintf("%d", i+2))
			list = getRoutes(t, routerClient, controlPlane, c.expectedRoutes, time.Second)
			if err := getError(errorChannel); err != nil {
				t.Fatal(err)
			}

			validateRoutes(t, c.hosts, list, "gw", c.tls)
		})
	}
}

func TestPerf(t *testing.T) {
	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient := initClients(t, stop, errorChannel, mrc)

	// Create a bunch of namespaces and gateways, and make sure they don't take too long to be created
	createIngressGateway(t, k8sClient, "istio-system", map[string]string{"istio": "ingressgateway"})
	qty := 200
	createGateways(t, store, qty)
	mrc.setNamespaces(generateNamespaces(qty)...)

	// It takes ~ 6s on my laptop
	_ = getRoutes(t, routerClient, "istio-system", qty, time.Second*10)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}

	// Now we have a lot of routes created, let's create one more gateway. The route creation should be fast and not linear.
	start := time.Now()
	createGateway(t, store, "ns1", "gw-ns1-1", []string{"instant.com"}, map[string]string{"istio": "ingressgateway"}, false)
	_ = getRoutes(t, routerClient, "istio-system", qty+1, time.Second)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}

	// It takes ~ 100ms on my laptop
	limit := time.Millisecond * 500
	if duration := time.Since(start); duration > limit {
		t.Fatalf("Time to add the a single router (%v) exceeded %v", duration, limit)
	}

}

func getError(errorChannel chan error) error {
	select {
	case err := <-errorChannel:
		return err
	default:
		return nil
	}
}

func getRoutes(t *testing.T, routerClient routev1.RouteV1Interface, ns string, size int, timeout time.Duration) *routeapiv1.RouteList {
	var list *routeapiv1.RouteList

	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		var err error

		time.Sleep(time.Millisecond * 100)
		list, err = routerClient.Routes(ns).List(context.TODO(), v1.ListOptions{})
		if err != nil {
			return err
		}
		if len(list.Items) != size {
			return fmt.Errorf("expected %d route(s), got %d", size, len(list.Items))
		}
		return nil
	}, retry.Timeout(timeout))

	return list
}

func findRouteByHost(list *routeapiv1.RouteList, host string) *routeapiv1.Route {
	for _, route := range list.Items {
		if route.Annotations[originalHostAnnotation] == host {
			return &route
		}
	}
	return nil
}

func createIngressGateway(t *testing.T, client kube.Client, ns string, labels map[string]string) {
	t.Helper()
	createPod(t, client, ns, labels)
	createService(t, client, ns, labels)
}

func createPod(t *testing.T, client kube.Client, ns string, labels map[string]string) {
	t.Helper()

	_, err := client.CoreV1().Pods(ns).Create(context.TODO(), &k8sioapicorev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Labels: labels,
		},
	}, v1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func createService(t *testing.T, client kube.Client, ns string, labels map[string]string) {
	t.Helper()

	_, err := client.CoreV1().Services(ns).Create(context.TODO(), &k8sioapicorev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Labels: labels,
		},
	}, v1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func createGateway(t *testing.T, store model.ConfigStoreCache, ns string, name string, hosts []string, gwSelector map[string]string, tls bool) {
	t.Helper()

	var tlsConfig *networking.ServerTLSSettings
	if tls {
		tlsConfig = &networking.ServerTLSSettings{HttpsRedirect: true}
	}
	_, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Namespace:        ns,
			Name:             name,
			Annotations:      map[string]string{"foo": "bar"},
			ResourceVersion:  "1",
		},
		Spec: &networking.Gateway{
			Selector: gwSelector,
			Servers: []*networking.Server{
				{
					Hosts: hosts,
					Tls:   tlsConfig,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func editGateway(t *testing.T, store model.ConfigStoreCache, ns string, name string, hosts []string, gwSelector map[string]string, tls bool, resource string) {
	t.Helper()

	var tlsConfig *networking.ServerTLSSettings
	if tls {
		tlsConfig = &networking.ServerTLSSettings{HttpsRedirect: true}
	}
	_, err := store.Update(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Namespace:        ns,
			Name:             name,
			Annotations:      map[string]string{"foo": "bar"},
			ResourceVersion:  resource,
		},
		Spec: &networking.Gateway{
			Selector: gwSelector,
			Servers: []*networking.Server{
				{
					Hosts: hosts,
					Tls:   tlsConfig,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func deleteGateway(t *testing.T, store model.ConfigStoreCache, ns string, name string) {
	t.Helper()

	err := store.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), name, ns)
	if err != nil {
		t.Fatal(err)
	}
}
