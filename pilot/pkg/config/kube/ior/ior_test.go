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
	"github.com/stretchr/testify/assert"
	k8sioapicorev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const prefixedLabel = maistraPrefix + "fake"

func initClients(
	t *testing.T,
	stop <-chan struct{},
	errorChannel chan error,
	mrc memberroll.MemberRollController,
) (model.ConfigStoreCache, KubeClient, routev1.RouteV1Interface, *route) {
	t.Helper()

	k8sClient := kube.NewFakeClient()
	iorKubeClient := NewFakeKubeClient(k8sClient)
	routerClient := NewFakeRouterClient()
	store, err := crdclient.New(k8sClient, "", "", false)
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

	r, err := newRoute(iorKubeClient, routerClient, store, "istio-system", mrc, stop, errorChannel)
	if err != nil {
		t.Fatal(err)
	}

	return store, iorKubeClient, routerClient, r
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
			"Non-existing namespace",
			"non-existing",
			[]string{"fail.com"},
			map[string]string{"istio": "ingressgateway"},
			0,
			"could not handle the ADD event for non-existing",
			false,
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

	var stop chan struct{}
	var errorChannel chan error
	var store model.ConfigStoreCache
	var k8sClient KubeClient
	var routerClient routev1.RouteV1Interface
	var mrc *fakeMemberRollController
	var r *route
	controlPlaneNs := "istio-system"

	for _, testType := range []string{"initialSync", "events"} {
		if testType == "events" {
			stop = make(chan struct{})
			defer func() { close(stop) }()
			errorChannel = make(chan error)
			mrc = newFakeMemberRollController()
			store, k8sClient, routerClient, r = initClients(t, stop, errorChannel, mrc)
			r.Run(stop)
			mrc.setNamespaces(controlPlaneNs)

			createIngressGateway(t, k8sClient.GetActualClient(), controlPlaneNs, map[string]string{"istio": "ingressgateway"})
		}

		for i, c := range cases {
			t.Run(testType+"-"+c.testName, func(t *testing.T) {
				if testType == "initialSync" {
					stop = make(chan struct{})
					defer func() { close(stop) }()
					errorChannel = make(chan error)
					mrc = newFakeMemberRollController()
					store, k8sClient, routerClient, r = initClients(t, stop, errorChannel, mrc)
					createIngressGateway(t, k8sClient.GetActualClient(), controlPlaneNs, map[string]string{"istio": "ingressgateway"})
					r.Run(stop)
				}
				gatewayName := fmt.Sprintf("gw%d", i)
				createGateway(t, store, c.ns, gatewayName, c.hosts, c.gwSelector, c.tls, c.annotations)
				if testType == "initialSync" {
					mrc.setNamespaces(controlPlaneNs)
				}
				list, _ := getRoutes(t, routerClient, controlPlaneNs, c.expectedRoutes, time.Second)
				if err := getError(errorChannel); err != nil {
					if c.expectedError == "" {
						t.Fatal(err)
					}

					if !strings.Contains(err.Error(), c.expectedError) {
						t.Fatalf("expected error message containing `%s', got: %s", c.expectedError, err.Error())
					}

					// Error is expected and matches the golden string, nothing to do
				} else {
					if c.expectedError != "" {
						t.Fatalf("expected error message containing `%s', got success", c.expectedError)
					}

					// Only continue the validation if any route is expected to be created
					if c.expectedRoutes > 0 {
						validateRoutes(t, c.hosts, list, gatewayName, c.tls)

						// Remove the gateway and expect all routes get removed
						deleteGateway(t, store, c.ns, gatewayName)
						_, _ = getRoutes(t, routerClient, c.ns, 0, time.Second)
						if err := getError(errorChannel); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
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

	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient, r := initClients(t, stop, errorChannel, mrc)
	r.Run(stop)

	controlPlane := "istio-system"
	createIngressGateway(t, k8sClient.GetActualClient(), controlPlane, map[string]string{"istio": "ingressgateway"})
	createGateway(t, store, controlPlane, "gw", []string{"abc.com"}, map[string]string{"istio": "ingressgateway"}, false, nil)
	mrc.setNamespaces("istio-system")

	list, _ := getRoutes(t, routerClient, controlPlane, 1, time.Second)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			editGateway(t, store, c.ns, "gw", c.hosts, c.gwSelector, c.tls, fmt.Sprintf("%d", i+2))
			list, _ = getRoutes(t, routerClient, controlPlane, c.expectedRoutes, time.Second)
			if err := getError(errorChannel); err != nil {
				t.Fatal(err)
			}

			validateRoutes(t, c.hosts, list, "gw", c.tls)
		})
	}
}

// TestPerf makes sure we are not doing more API calls than necessary
func TestPerf(t *testing.T) {
	IORLog.SetOutputLevel(log.DebugLevel)
	countCallsReset()

	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient, r := initClients(t, stop, errorChannel, mrc)
	r.Run(stop)

	// Create a bunch of namespaces and gateways, and make sure they don't take too long to be created
	createIngressGateway(t, k8sClient.GetActualClient(), "istio-system", map[string]string{"istio": "ingressgateway"})
	qty := 100
	qtyNamespaces := qty + 1
	createGateways(t, store, 1, qty)
	mrc.setNamespaces(generateNamespaces(qty)...)

	// It takes ~ 2s on my laptop, it's slower on prow
	_, ignore := getRoutes(t, routerClient, "istio-system", qty, time.Minute)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, qty, countCallsGet("create"), "wrong number of calls to client.Routes().Create()")
	assert.Equal(t, 0, countCallsGet("delete"), "wrong number of calls to client.Routes().Delete()")
	assert.Equal(t, qtyNamespaces, countCallsGet("list")-ignore, "wrong number of calls to client.Routes().List()")
	// qty=number of Create() calls; qtyNamespaces=number of List() calls
	assert.Equal(t, qty+qtyNamespaces, countCallsGet("routes")-ignore, "wrong number of calls to client.Routes()")

	// Now we have a lot of routes created, let's create one more gateway. We don't expect a lot of new API calls
	countCallsReset()
	createGateway(t, store, "ns1", "gw-ns1-1", []string{"instant.com"}, map[string]string{"istio": "ingressgateway"}, false, nil)
	_, ignore = getRoutes(t, routerClient, "istio-system", qty+1, time.Second)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, countCallsGet("create"), "wrong number of calls to client.Routes().Create()")
	assert.Equal(t, 0, countCallsGet("delete"), "wrong number of calls to client.Routes().Delete()")
	assert.Equal(t, 0, countCallsGet("list")-ignore, "wrong number of calls to client.Routes().List()")
	assert.Equal(t, 1, countCallsGet("routes")-ignore, "wrong number of calls to client.Routes()")

	// Editing. We don't expect a lot of new API calls
	countCallsReset()
	editGateway(t, store, "ns1", "gw-ns1-1", []string{"edited.com", "edited-other.com"}, map[string]string{"istio": "ingressgateway"}, false, "2")
	_, ignore = getRoutes(t, routerClient, "istio-system", qty+2, time.Second)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, countCallsGet("create"), "wrong number of calls to client.Routes().Create()")
	assert.Equal(t, 1, countCallsGet("delete"), "wrong number of calls to client.Routes().Delete()")
	assert.Equal(t, 0, countCallsGet("list")-ignore, "wrong number of calls to client.Routes().List()")
	assert.Equal(t, 3, countCallsGet("routes")-ignore, "wrong number of calls to client.Routes()")

	// Same for deletion. We don't expect a lot of new API calls
	countCallsReset()
	deleteGateway(t, store, "ns1", "gw-ns1-1")
	_, ignore = getRoutes(t, routerClient, "istio-system", qty, time.Second)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 0, countCallsGet("create"), "wrong number of calls to client.Routes().Create()")
	assert.Equal(t, 2, countCallsGet("delete"), "wrong number of calls to client.Routes().Delete()")
	assert.Equal(t, 0, countCallsGet("list")-ignore, "wrong number of calls to client.Routes().List()")
	assert.Equal(t, 2, countCallsGet("routes")-ignore, "wrong number of calls to client.Routes()")
}

// TestConcurrency makes sure IOR can respond to events even when doing its initial sync
func TestConcurrency(t *testing.T) {
	IORLog.SetOutputLevel(log.DebugLevel)
	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient, r := initClients(t, stop, errorChannel, mrc)
	r.Run(stop)

	// Create a bunch of namespaces and gateways
	createIngressGateway(t, k8sClient.GetActualClient(), "istio-system", map[string]string{"istio": "ingressgateway"})
	qty := 50
	createGateways(t, store, 1, qty)
	mrc.setNamespaces(generateNamespaces(qty)...)

	// At the same time, while IOR is processing those initial `qty` gateways, create `qty` more
	go func() {
		mrc.setNamespaces(generateNamespaces(qty * 2)...)
		createGateways(t, store, qty+1, qty*2)
	}()

	// And expect all `qty * 2` gateways to be created
	_, _ = getRoutes(t, routerClient, "istio-system", (qty * 2), time.Minute)
	if err := getError(errorChannel); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateUpdateEvents(t *testing.T) {
	IORLog.SetOutputLevel(log.DebugLevel)
	stop := make(chan struct{})
	defer func() { close(stop) }()
	errorChannel := make(chan error)
	mrc := newFakeMemberRollController()
	store, k8sClient, routerClient, route := initClients(t, stop, errorChannel, mrc)
	route.Run(stop)

	r, err := newRoute(k8sClient, routerClient, store, "istio-system", mrc, stop, errorChannel)
	if err != nil {
		t.Fatal(err)
	}

	mrc.setNamespaces("istio-system")
	createIngressGateway(t, k8sClient.GetActualClient(), "istio-system", map[string]string{"istio": "ingressgateway"})

	cfg := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Namespace:        "istio-system",
			Name:             "a",
			ResourceVersion:  "1",
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Hosts: []string{"a.com"},
				},
			},
		},
	}

	// Create the first router, should work just fine
	err = r.handleEvent(model.EventAdd, cfg)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		r.gatewaysLock.Lock()
		defer r.gatewaysLock.Unlock()
		if len(r.gatewaysMap) != 1 {
			t.Fatal("error creating the first route")
		}
	}()

	// Simulate an UPDATE event with the same data, should be ignored
	err = r.handleEvent(model.EventUpdate, cfg)
	if err == nil {
		t.Fatalf("expecting the error: %q, but got nothing", eventDuplicatedMessage)
	}
	if msg := err.Error(); msg != eventDuplicatedMessage {
		t.Fatalf("expecting the error: %q, but got %q", eventDuplicatedMessage, msg)
	}
}

func generateNamespaces(qty int) []string {
	var result []string

	for i := 1; i <= qty; i++ {
		result = append(result, fmt.Sprintf("ns%d", i))
	}

	return append(result, "istio-system")
}

func createGateways(t *testing.T, store model.ConfigStoreCache, begin, end int) {
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

// getError tries to read an error from the error channel.
// It tries 3 times beforing returning nil, in case of there's no error in the channel,
// this is to give some time to async functions to run and fill the channel properly
func getError(errorChannel chan error) error {
	for i := 1; i < 3; i++ {
		select {
		case err := <-errorChannel:
			return err
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// getRoutes is a helper function that keeps trying getting a list of routes until it gets `size` items.
// It returns the list of routes itself and the number of retries it run
func getRoutes(t *testing.T, routerClient routev1.RouteV1Interface, ns string, size int, timeout time.Duration) (*routeapiv1.RouteList, int) {
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

	return list, count
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

func createGateway(t *testing.T, store model.ConfigStoreCache, ns string, name string, hosts []string, gwSelector map[string]string,
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

func deleteGateway(t *testing.T, store model.ConfigStoreCache, ns string, name string) {
	t.Helper()

	err := store.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), name, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
}
