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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const prefixedLabel = maistraPrefix + "fake"

func newClients(
	t *testing.T,
	k8sClient kube.Client,
) (
	*crdclient.Client,
	KubeClient,
	routev1.RouteV1Interface,
	*fakeMemberRollController,
	*routeController,
) {
	t.Helper()

	if k8sClient == nil {
		k8sClient = kube.NewFakeClient()
	}

	iorKubeClient := NewFakeKubeClient(k8sClient)
	routerClient := NewFakeRouterClient()
	store, err := crdclient.New(k8sClient, crdclient.Option{})
	if err != nil {
		t.Fatal(err)
	}

	r := newRoute(iorKubeClient, routerClient, store)

	return store.(*crdclient.Client), iorKubeClient, routerClient, newFakeMemberRollController(), r
}

func runClients(
	store model.ConfigStoreController,
	kubeClient KubeClient,
	stop <-chan struct{},
) {
	go store.Run(stop)
	kubeClient.GetActualClient().RunAndWait(stop)
}

func initClients(
	t *testing.T,
	stop <-chan struct{},
) (
	model.ConfigStoreController,
	KubeClient,
	routev1.RouteV1Interface,
	*fakeMemberRollController,
	*routeController,
) {
	store, iorKubeClient, routerClient, mrc, r := newClients(t, nil)

	runClients(store, iorKubeClient, stop)

	return store, iorKubeClient, routerClient, mrc, r
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
		annotations    map[string]string
	}{
		{
			"One host",
			"istio-system",
			[]string{"one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
		{
			"Two hosts",
			"istio-system",
			[]string{"two.org", "three.com"},
			map[string]string{"istio": "ingressgateway"},
			2,
			"",
			false,
			nil,
		},
		{
			"Wildcard 1",
			"istio-system",
			[]string{"*"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
		{
			"Wildcard 2",
			"istio-system",
			[]string{"*.a.com"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
		{
			"Invalid gateway",
			"istio-system",
			[]string{"fail.com"},
			map[string]string{"istio": "nonexistent"},
			0,
			"could not find a service that matches the gateway selector `istio=nonexistent'",
			false,
			nil,
		},
		{
			"TLS 1",
			"istio-system",
			[]string{"one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			true,
			nil,
		},
		{
			"Gateway not managed",
			"istio-system",
			[]string{"notmanaged.org"},
			map[string]string{"istio": "ingressgateway"},
			0,
			"",
			false,
			map[string]string{ShouldManageRouteAnnotation: "false", "foo": "bar"},
		},
		{
			"Gateway explicitly managed",
			"istio-system",
			[]string{"explicitlymanaged.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			map[string]string{ShouldManageRouteAnnotation: "TRUE", "foo": "bar"},
		},
		{
			"Gateway explicitly managed with an invalid value",
			"istio-system",
			[]string{"explicitlymanaged.org"},
			map[string]string{"istio": "ingressgateway"},
			0,
			fmt.Sprintf("could not parse annotation %q:", ShouldManageRouteAnnotation),
			false,
			map[string]string{ShouldManageRouteAnnotation: "ABC", "foo": "bar"},
		},
		{
			"Egress gateway must be ignored",
			"istio-system",
			[]string{"egress.org"},
			map[string]string{"istio": "egressgateway"},
			0,
			"",
			false,
			nil,
		},
		{
			"Host with all namespaces",
			"istio-system",
			[]string{"*/one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
		{
			"Host with current namespace",
			"istio-system",
			[]string{"./one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
		{
			"Host with a specific namespace",
			"istio-system",
			[]string{"ns1/one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
		{
			"Host with a namespace and wildcard",
			"istio-system",
			[]string{"*/*.one.org"},
			map[string]string{"istio": "ingressgateway"},
			1,
			"",
			false,
			nil,
		},
	}

	IORLog.SetOutputLevel(log.DebugLevel)

	controlPlaneNs := "istio-system"
	stop := make(chan struct{})
	defer func() { close(stop) }()
	store, k8sClient, routerClient, mrc, r := initClients(t, stop)
	r.Run(stop)
	mrc.setNamespaces(controlPlaneNs)

	createIngressGateway(t, k8sClient.GetActualClient(), controlPlaneNs, map[string]string{"istio": "ingressgateway"})

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			gatewayName := fmt.Sprintf("gw%d", i)
			createGateway(t, store, c.ns, gatewayName, c.hosts, c.gwSelector, c.tls, c.annotations)

			list := getRoutes(t, routerClient, controlPlaneNs, c.expectedRoutes, time.Second)

			// Only continue the validation if any route is expected to be created
			if c.expectedRoutes > 0 {
				validateRoutes(t, c.hosts, list, gatewayName, c.tls)

				// Remove the gateway and expect all routes get removed
				deleteGateway(t, store, c.ns, gatewayName)
				_ = getRoutes(t, routerClient, c.ns, 0, time.Second)
			}
		})
	}
}

func validateRoutes(t *testing.T, hosts []string, list *routeapiv1.RouteList, gatewayName string, tls bool) {
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
		if _, found := route.Annotations[ShouldManageRouteAnnotation]; found {
			t.Fatalf("annotation %q should not be copied to the route", ShouldManageRouteAnnotation)
		}
		if route.Labels["foo"] != "bar" {
			t.Fatal("gateway labels were not copied to route")
		}
		if _, found := route.Labels[prefixedLabel]; found {
			t.Fatalf("label %q should not be copied to the route", prefixedLabel)
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

	IORLog.SetOutputLevel(log.DebugLevel)

	stop := make(chan struct{})
	defer func() { close(stop) }()
	store, k8sClient, routerClient, mrc, r := initClients(t, stop)
	r.Run(stop)
	mrc.setNamespaces("istio-system")

	controlPlane := "istio-system"
	createIngressGateway(t, k8sClient.GetActualClient(), controlPlane, map[string]string{"istio": "ingressgateway"})
	createGateway(t, store, controlPlane, "gw", []string{"abc.com"}, map[string]string{"istio": "ingressgateway"}, false, nil)

	list := getRoutes(t, routerClient, controlPlane, 1, time.Second)

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			editGateway(t, store, c.ns, "gw", c.hosts, c.gwSelector, c.tls, fmt.Sprintf("%d", i+2))
			list = getRoutes(t, routerClient, controlPlane, c.expectedRoutes, time.Second)

			validateRoutes(t, c.hosts, list, "gw", c.tls)
		})
	}
}

// TestConcurrency makes sure IOR can respond to events even when doing its initial sync
func TestConcurrency(t *testing.T) {
	IORLog.SetOutputLevel(log.DebugLevel)
	stop := make(chan struct{})
	defer func() { close(stop) }()
	store, k8sClient, routerClient, mrc, r := initClients(t, stop)
	r.Run(stop)

	qty := 10
	runs := 10

	// Create a bunch of namespaces and gateways
	createIngressGateway(t, k8sClient.GetActualClient(), "istio-system", map[string]string{"istio": "ingressgateway"})

	// At the same time, while IOR is processing those initial `qty` gateways, create `qty` more
	for i := 0; i < runs; i++ {
		go func(j int) {
			createGateways(t, store, (qty*j)+1, (qty*j)+qty)
		}(i)
	}

	mrc.setNamespaces(generateNamespaces(qty * runs)...)

	// And expect all `qty * 2` gateways to be created
	_ = getRoutes(t, routerClient, "istio-system", (qty * runs), time.Minute)
}

const (
	createFuncName = "Create"
	deleteFuncName = "Delete"
	listFuncName   = "List"
)

func TestStatelessness(t *testing.T) {
	type state struct {
		name           string
		ns             string
		hosts          []string
		gwSelector     map[string]string
		expectedRoutes int
		tls            bool
	}

	watchedNamespace := "istio-system"

	initialState := state{
		"gw",
		watchedNamespace,
		[]string{"ghi.org", "jkl.com"},
		map[string]string{"istio": "ingressgateway"},
		2,
		false,
	}

	IORLog.SetOutputLevel(log.DebugLevel)

	stop := make(chan struct{})
	defer func() { close(stop) }()
	iorStop := make(chan struct{})
	store, kubeClient, routerClient, mrc, r := newClients(t, nil)
	runClients(store, kubeClient, stop)
	r.Run(iorStop)

	mrc.setNamespaces(watchedNamespace)

	createIngressGateway(t, kubeClient.GetActualClient(), watchedNamespace, map[string]string{"istio": "ingressgateway"})
	createGateway(t, store, initialState.ns, initialState.name, initialState.hosts, map[string]string{"istio": "ingressgateway"}, initialState.tls, nil)

	list := getRoutes(t, routerClient, watchedNamespace, 2, time.Second)
	validateRoutes(t, initialState.hosts, list, initialState.name, initialState.tls)

	fr, ok := routerClient.Routes(initialState.name).(*FakeRouter)

	if !ok {
		t.Fatal(fmt.Errorf("failed to convert to FakeRouter"))
	}

	listCallCount := fr.GetCallCount(listFuncName)
	createCallCount := fr.GetCallCount(createFuncName)
	deleteCallCount := fr.GetCallCount(deleteFuncName)

	close(iorStop)

	backupIOR := newRoute(kubeClient, routerClient, store)
	backupIOR.Run(stop)

	store.SyncAll()

	if fr.GetCallCount(listFuncName) == listCallCount {
		t.Fatal(fmt.Errorf("expect to call List, but got the same %d", listCallCount))
	}

	if fr.GetCallCount(createFuncName) > createCallCount {
		t.Fatal(
			fmt.Errorf(
				"expect not to call Create, initially %d and got %d after",
				createCallCount,
				fr.GetCallCount(createFuncName),
			),
		)
	}

	if fr.GetCallCount(deleteFuncName) > deleteCallCount {
		t.Fatal(
			fmt.Errorf(
				"expect not to call Delete, initially %d and got %d after",
				deleteCallCount,
				fr.GetCallCount(deleteFuncName),
			),
		)
	}
}

func generateNamespaces(qty int) []string {
	var result []string

	for i := 1; i <= qty; i++ {
		result = append(result, fmt.Sprintf("ns%d", i))
	}

	return append(result, "istio-system")
}

func createGateways(t *testing.T, store model.ConfigStoreController, begin, end int) {
	for i := begin; i <= end; i++ {
		createGateway(t,
			store,
			fmt.Sprintf("ns%d", i),
			fmt.Sprintf("gw-ns%d", i),
			[]string{fmt.Sprintf("d%d.com", i)},
			map[string]string{"istio": "ingressgateway"},
			false,
			nil)
	}
}

// getRoutes is a helper function that keeps trying getting a list of routes until it gets `size` items.
// It returns the list of routes itself and the number of retries it run
func getRoutes(t *testing.T, routerClient routev1.RouteV1Interface, ns string, size int, timeout time.Duration) *routeapiv1.RouteList {
	var list *routeapiv1.RouteList

	t.Helper()
	count := 0

	retry.UntilSuccessOrFail(t, func() error {
		var err error

		time.Sleep(time.Millisecond * 100)
		list, err = routerClient.Routes(ns).List(context.TODO(), v1.ListOptions{})
		count++
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

	_, err := client.Kube().CoreV1().Pods(ns).Create(context.TODO(), &k8sioapicorev1.Pod{
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

	_, err := client.Kube().CoreV1().Services(ns).Create(context.TODO(), &k8sioapicorev1.Service{
		Spec: k8sioapicorev1.ServiceSpec{
			Selector: labels,
		},
	}, v1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func createGateway(t *testing.T, store model.ConfigStoreController, ns string, name string, hosts []string, gwSelector map[string]string,
	tls bool, annotations map[string]string,
) {
	t.Helper()

	var tlsConfig *networking.ServerTLSSettings
	if tls {
		tlsConfig = &networking.ServerTLSSettings{HttpsRedirect: true}
	}

	if annotations == nil {
		annotations = map[string]string{"foo": "bar"}
	}

	_, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Namespace:        ns,
			Name:             name,
			Annotations:      annotations,
			Labels:           map[string]string{"foo": "bar", prefixedLabel: "present"},
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

func editGateway(t *testing.T, store model.ConfigStoreController, ns string, name string, hosts []string,
	gwSelector map[string]string, tls bool, resource string,
) {
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
			Labels:           map[string]string{"foo": "bar", prefixedLabel: "present"},
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

func deleteGateway(t *testing.T, store model.ConfigStoreController, ns string, name string) {
	t.Helper()

	err := store.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), name, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
}
