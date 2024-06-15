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
	k8sioapicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	networking "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/retry"
)

const prefixedLabel = maistraPrefix + "fake"

func newClients(
	t *testing.T,
	k8sClient kube.Client,
) (
	*crdclient.Client,
	KubeClient,
	kclient.Client[*routeapiv1.Route],
	*routeController,
) {
	t.Helper()

	if k8sClient == nil {
		newCrdWatcher := kube.NewCrdWatcher
		kube.NewCrdWatcher = kube.NewFastCrdWatcher
		k8sClient = kube.NewFakeClient()

		kube.NewCrdWatcher = newCrdWatcher
	}

	iorKubeClient := NewFakeKubeClient(k8sClient)
	store := crdclient.New(k8sClient, crdclient.Option{})

	r := newRouteController(iorKubeClient, store)

	return store, iorKubeClient, r.routeLister, r
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
	kclient.Client[*routeapiv1.Route],
) {
	store, iorKubeClient, routeLister, r := newClients(t, nil)

	r.Run(stop)
	runClients(store, iorKubeClient, stop)

	return store, iorKubeClient, routeLister
}

func TestCreate(t *testing.T) {
	cases := []struct {
		testName           string
		ns                 string
		hosts              []string
		gwSelector         map[string]string
		expectedRoutes     int
		expectedError      string
		tls                bool
		gatewayAnnotations map[string]string
		routeAnnotations   map[string]string
		gatewayLabels      map[string]string
		routeLabels        map[string]string
	}{
		{
			testName:       "One host",
			ns:             "istio-system",
			hosts:          []string{"one.org"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
		{
			testName:       "Two hosts",
			ns:             "istio-system",
			hosts:          []string{"two.org", "three.com"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 2,
		},
		{
			testName:       "Wildcard 1",
			ns:             "istio-system",
			hosts:          []string{"*"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
		{
			testName:       "Wildcard 2",
			ns:             "istio-system",
			hosts:          []string{"*.a.com"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
		{
			testName:      "Invalid gateway",
			ns:            "istio-system",
			hosts:         []string{"fail.com"},
			gwSelector:    map[string]string{"istio": "nonexistent"},
			expectedError: "could not find a service that matches the gateway selector `istio=nonexistent'",
		},
		{
			testName:       "TLS 1",
			ns:             "istio-system",
			hosts:          []string{"one.org"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
			tls:            true,
		},
		{
			testName:           "Gateway not managed",
			ns:                 "istio-system",
			hosts:              []string{"notmanaged.org"},
			gwSelector:         map[string]string{"istio": "ingressgateway"},
			gatewayAnnotations: map[string]string{ShouldManageRouteAnnotation: "false", "foo": "bar"},
			routeAnnotations:   map[string]string{"foo": "bar"},
		},
		{
			testName:           "Gateway explicitly managed",
			ns:                 "istio-system",
			hosts:              []string{"explicitlymanaged.org"},
			gwSelector:         map[string]string{"istio": "ingressgateway"},
			expectedRoutes:     1,
			gatewayAnnotations: map[string]string{ShouldManageRouteAnnotation: "TRUE", "foo": "bar"},
			routeAnnotations:   map[string]string{originalHostAnnotation: "explicitlymanaged.org", "foo": "bar"},
		},
		{
			testName:           "Gateway explicitly managed with an invalid value",
			ns:                 "istio-system",
			hosts:              []string{"explicitlymanaged.org"},
			gwSelector:         map[string]string{"istio": "ingressgateway"},
			expectedError:      fmt.Sprintf("could not parse annotation %q:", ShouldManageRouteAnnotation),
			gatewayAnnotations: map[string]string{ShouldManageRouteAnnotation: "ABC", "foo": "bar"},
			routeAnnotations:   map[string]string{"foo": "bar"},
		},
		{
			testName:       "all annotations except kubectl.kubernetes.io should be copied from Gateway to Route",
			ns:             "istio-system",
			hosts:          []string{"istio.io"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
			gatewayAnnotations: map[string]string{
				"foo": "bar", "argocd.argoproj.io/sync-options": "Prune=false",
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
			routeAnnotations: map[string]string{
				"foo": "bar", "argocd.argoproj.io/sync-options": "Prune=false",
				originalHostAnnotation: "istio.io",
			},
		},
		{
			testName:       "all labels except maistra.io and argocd.argoproj.io/instance should be copied from Gateway to Route",
			ns:             "istio-system",
			hosts:          []string{"istio.io"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
			gatewayLabels: map[string]string{
				"argocd.argoproj.io/instance":    "app",
				"argocd.argoproj.io/secret-type": "cluster",
				"foo":                            "bar",
				"maistra.io/manageRoute":         "false",
				"maistra.io/gateway-name":        "random",
			},
			routeLabels: map[string]string{
				"argocd.argoproj.io/secret-type":     "cluster",
				"foo":                                "bar",
				"maistra.io/gateway-name":            "gw10",
				"maistra.io/gateway-namespace":       "istio-system",
				"maistra.io/gateway-resourceVersion": "1",
				"maistra.io/generated-by":            "ior",
			},
		},
		{
			testName:   "Egress gateway must be ignored",
			ns:         "istio-system",
			hosts:      []string{"egress.org"},
			gwSelector: map[string]string{"istio": "egressgateway"},
		},
		{
			testName:       "Host with all namespaces",
			ns:             "istio-system",
			hosts:          []string{"*/one.org"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
		{
			testName:       "Host with current namespace",
			ns:             "istio-system",
			hosts:          []string{"./one.org"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
		{
			testName:       "Host with a specific namespace",
			ns:             "istio-system",
			hosts:          []string{"ns1/one.org"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
		{
			testName:       "Host with a namespace and wildcard",
			ns:             "istio-system",
			hosts:          []string{"*/*.one.org"},
			gwSelector:     map[string]string{"istio": "ingressgateway"},
			expectedRoutes: 1,
		},
	}

	iorLog.SetOutputLevel(log.DebugLevel)

	controlPlaneNs := "istio-system"
	stop := make(chan struct{})
	defer func() { close(stop) }()
	store, k8sClient, routerClient := initClients(t, stop)

	// create unrelated service with an empty selector to confirm that IOR is not picking it up
	createService(t, k8sClient.GetActualClient(), controlPlaneNs, "service-with-empty-selector", nil)

	createIngressGateway(t, k8sClient.GetActualClient(), controlPlaneNs, map[string]string{"istio": "ingressgateway"})

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			gatewayName := fmt.Sprintf("gw%d", i)
			createGateway(t, store, c.ns, gatewayName, c.hosts, c.gwSelector, c.tls, c.gatewayAnnotations, c.gatewayLabels)

			list := getRoutes(t, routerClient, controlPlaneNs, c.expectedRoutes, time.Second)

			// Only continue the validation if any route is expected to be created
			if c.expectedRoutes > 0 {
				validateRoutes(t, c.hosts, list, gatewayName, c.tls, c.routeAnnotations, c.routeLabels)

				// Remove the gateway and expect all routes get removed
				deleteGateway(t, k8sClient.GetActualClient().Istio(), c.ns, gatewayName)
				_ = getRoutes(t, routerClient, c.ns, 0, time.Second)
			}
		})
	}
}

func validateRoutes(t *testing.T, hosts []string, list *[]*routeapiv1.Route, gatewayName string, tls bool,
	expectedAnnotations map[string]string, expectedLabels map[string]string,
) {
	for _, host := range hosts {
		route := findRouteByHost(list, host)
		if route == nil {
			t.Fatalf("could not find a route with hostname %s", host)
		}

		// Check metadata
		if expectedAnnotations != nil {
			if len(route.Annotations) != len(expectedAnnotations) {
				t.Fatalf("route was created with unexpected annotations: %s", route.Annotations)
			}
			for key, val := range expectedAnnotations {
				if route.Annotations[key] != val {
					t.Fatalf("annotation '%s' was not copied to route: expected '%s', got '%s'", key, val, route.Annotations[key])
				}
			}
		}
		if _, found := route.Annotations[ShouldManageRouteAnnotation]; found {
			t.Fatalf("annotation %q should not be copied to the route", ShouldManageRouteAnnotation)
		}
		if expectedLabels != nil {
			if len(route.Labels) != len(expectedLabels) {
				t.Fatalf("route was created with unexpected labels: %s", route.Labels)
			}
			for key, val := range expectedLabels {
				if route.Labels[key] != val {
					t.Fatalf("label '%s' was not copied to route: expected '%s', got '%s'", key, val, route.Labels[key])
				}
			}
		}
		if route.Labels[gatewayNameLabel] != gatewayName {
			t.Fatalf("wrong label, expecting %s, got %s", gatewayName, route.Annotations[gatewayNameLabel])
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

		// check service
		if route.Spec.To.Name != "gw-service" {
			t.Fatalf("Route points to wrong service: %s", route.Spec.To.Name)
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

	iorLog.SetOutputLevel(log.DebugLevel)

	stop := make(chan struct{})
	defer func() { close(stop) }()
	store, k8sClient, routerClient := initClients(t, stop)

	controlPlane := "istio-system"
	createIngressGateway(t, k8sClient.GetActualClient(), controlPlane, map[string]string{"istio": "ingressgateway"})
	createGateway(t, store, controlPlane, "gw", []string{"abc.com"}, map[string]string{"istio": "ingressgateway"}, false, nil, nil)

	list := getRoutes(t, routerClient, controlPlane, 1, time.Second)

	for i, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			editGateway(t, store, c.ns, "gw", c.hosts, c.gwSelector, c.tls, fmt.Sprintf("%d", i+2))
			list = getRoutes(t, routerClient, controlPlane, c.expectedRoutes, time.Second)

			validateRoutes(t, c.hosts, list, "gw", c.tls, nil, nil)
		})
	}
}

// TestConcurrency makes sure IOR can respond to events even when doing its initial sync
func TestConcurrency(t *testing.T) {
	iorLog.SetOutputLevel(log.DebugLevel)
	stop := make(chan struct{})
	defer func() { close(stop) }()
	store, k8sClient, routerClient := initClients(t, stop)

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

	// And expect all `qty * 2` gateways to be created
	_ = getRoutes(t, routerClient, "istio-system", (qty * runs), time.Minute)
}

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

	iorLog.SetOutputLevel(log.DebugLevel)

	stop := make(chan struct{})
	defer func() { close(stop) }()
	iorStop := make(chan struct{})
	store, kubeClient, routerClient, r := newClients(t, nil)
	r.Run(iorStop)
	runClients(store, kubeClient, stop)

	createIngressGateway(t, kubeClient.GetActualClient(), watchedNamespace, map[string]string{"istio": "ingressgateway"})
	createGateway(t, store, initialState.ns, initialState.name, initialState.hosts, map[string]string{"istio": "ingressgateway"}, initialState.tls, nil, nil)

	list := getRoutes(t, routerClient, watchedNamespace, 2, time.Second)
	validateRoutes(t, initialState.hosts, list, initialState.name, initialState.tls, nil, nil)

	close(iorStop)

	backupIOR := newRouteController(kubeClient, store)
	backupIOR.Run(stop)

	store.HasSynced()
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
			nil,
			nil)
	}
}

// getRoutes is a helper function that keeps trying getting a list of routes until it gets `size` items.
// It returns the list of routes itself and the number of retries it run
func getRoutes(t *testing.T, routerClient kclient.Client[*routeapiv1.Route], ns string, size int, timeout time.Duration) *[]*routeapiv1.Route {
	var list []*routeapiv1.Route

	t.Helper()
	count := 0

	retry.UntilSuccessOrFail(t, func() error {
		time.Sleep(time.Second)
		list = routerClient.List(ns, labels.Everything())
		count++

		if len(list) != size {
			return fmt.Errorf("expected %d route(s), got %d", size, len(list))
		}
		return nil
	}, retry.Timeout(timeout))

	return &list
}

func findRouteByHost(list *[]*routeapiv1.Route, host string) *routeapiv1.Route {
	for _, route := range *list {
		if route.Annotations[originalHostAnnotation] == host {
			return route
		}
	}
	return nil
}

func createIngressGateway(t *testing.T, client kube.Client, ns string, labels map[string]string) {
	t.Helper()
	createPod(t, client, ns, "gw-pod", labels)
	createService(t, client, ns, "gw-service", labels)
}

func createPod(t *testing.T, client kube.Client, ns, name string, labels map[string]string) {
	t.Helper()

	_, err := client.Kube().CoreV1().Pods(ns).Create(context.TODO(), &k8sioapicorev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func createService(t *testing.T, client kube.Client, ns, name string, selector map[string]string) {
	t.Helper()

	_, err := client.Kube().CoreV1().Services(ns).Create(context.TODO(), &k8sioapicorev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: k8sioapicorev1.ServiceSpec{
			Selector: selector,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func createGateway(t *testing.T, store model.ConfigStoreController, ns string, name string, hosts []string, gwSelector map[string]string,
	tls bool, annotations map[string]string, labels map[string]string,
) {
	t.Helper()

	var tlsConfig *networking.ServerTLSSettings
	if tls {
		tlsConfig = &networking.ServerTLSSettings{HttpsRedirect: true}
	}

	_, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.Gateway.GroupVersionKind(),
			Namespace:        ns,
			Name:             name,
			Annotations:      annotations,
			Labels:           labels,
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
			GroupVersionKind: collections.Gateway.GroupVersionKind(),
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

func deleteGateway(t *testing.T, istioClient istioclient.Interface, ns string, name string) {
	t.Helper()

	var immediate int64
	err := istioClient.NetworkingV1alpha3().Gateways(ns).Delete(context.TODO(), name, metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	if err != nil {
		t.Fatal(err)
	}
}
