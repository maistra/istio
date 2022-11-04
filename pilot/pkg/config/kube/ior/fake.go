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
	"fmt"
	"strings"
	"sync"
	"time"

	v1 "github.com/openshift/api/route/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/controller"
)

// FakeRouter implements routev1.RouteInterface
type FakeRouter struct {
	routes     map[string]*v1.Route
	routesLock sync.Mutex
}

// FakeRouterClient implements routev1.RouteV1Interface
type FakeRouterClient struct {
	routesByNamespace     map[string]routev1.RouteInterface
	routesByNamespaceLock sync.Mutex
}

type fakeKubeClient struct {
	client kube.Client
}

// NewFakeKubeClient creates a new FakeKubeClient
func NewFakeKubeClient(client kube.Client) KubeClient {
	return &fakeKubeClient{client: client}
}

func (c *fakeKubeClient) IsRouteSupported() bool {
	return true
}

func (c *fakeKubeClient) GetActualClient() kube.Client {
	return c.client
}

func (c *fakeKubeClient) GetHandleEventTimeout() time.Duration {
	return time.Millisecond
}

// NewFakeRouterClient creates a new FakeRouterClient
func NewFakeRouterClient() routev1.RouteV1Interface {
	return &FakeRouterClient{
		routesByNamespace: make(map[string]routev1.RouteInterface),
	}
}

// NewFakeRouter creates a new FakeRouter
func NewFakeRouter() routev1.RouteInterface {
	return &FakeRouter{
		routes: make(map[string]*v1.Route),
	}
}

// RESTClient implements routev1.RouteV1Interface
func (rc *FakeRouterClient) RESTClient() rest.Interface {
	panic("not implemented")
}

// Routes implements routev1.RouteV1Interface
func (rc *FakeRouterClient) Routes(namespace string) routev1.RouteInterface {
	rc.routesByNamespaceLock.Lock()
	defer rc.routesByNamespaceLock.Unlock()

	if _, ok := rc.routesByNamespace[namespace]; !ok {
		rc.routesByNamespace[namespace] = NewFakeRouter()
	}

	countCallsIncrement("routes")
	return rc.routesByNamespace[namespace]
}

var generatedHostNumber int

// Create implements routev1.RouteInterface
func (fk *FakeRouter) Create(ctx context.Context, route *v1.Route, opts metav1.CreateOptions) (*v1.Route, error) {
	fk.routesLock.Lock()
	defer fk.routesLock.Unlock()

	if strings.Contains(route.Spec.Host, "/") {
		return nil, fmt.Errorf("invalid hostname")
	}

	if route.Spec.Host == "" {
		generatedHostNumber++
		route.Spec.Host = fmt.Sprintf("generated-host%d.com", generatedHostNumber)
	}

	fk.routes[route.Name] = route

	countCallsIncrement("create")
	return route, nil
}

// Update implements routev1.RouteInterface
func (fk *FakeRouter) Update(ctx context.Context, route *v1.Route, opts metav1.UpdateOptions) (*v1.Route, error) {
	fk.routesLock.Lock()
	defer fk.routesLock.Unlock()

	if _, ok := fk.routes[route.Name]; !ok {
		return nil, fmt.Errorf("exisiting not found")
	}

	if strings.Contains(route.Spec.Host, "/") {
		return nil, fmt.Errorf("invalid hostname")
	}

	if route.Spec.Host == "" {
		generatedHostNumber++
		route.Spec.Host = fmt.Sprintf("generated-host%d.com", generatedHostNumber)
	}

	fk.routes[route.Name] = route

	countCallsIncrement("update")
	return route, nil
}

// UpdateStatus implements routev1.RouteInterface
func (fk *FakeRouter) UpdateStatus(ctx context.Context, route *v1.Route, opts metav1.UpdateOptions) (*v1.Route, error) {
	panic("not implemented")
}

// Delete implements routev1.RouteInterface
func (fk *FakeRouter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	fk.routesLock.Lock()
	defer fk.routesLock.Unlock()

	if _, ok := fk.routes[name]; !ok {
		return fmt.Errorf("route %s not found", name)
	}

	delete(fk.routes, name)

	countCallsIncrement("delete")
	return nil
}

// DeleteCollection implements routev1.RouteInterface
func (fk *FakeRouter) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	panic("not implemented")
}

// Get implements routev1.RouteInterface
func (fk *FakeRouter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.Route, error) {
	panic("not implemented")
}

// List implements routev1.RouteInterface
func (fk *FakeRouter) List(ctx context.Context, opts metav1.ListOptions) (*v1.RouteList, error) {
	fk.routesLock.Lock()
	defer fk.routesLock.Unlock()

	var items []v1.Route
	for _, route := range fk.routes {
		items = append(items, *route)
	}
	result := &v1.RouteList{Items: items}

	countCallsIncrement("list")
	return result, nil
}

// Watch Create implements routev1.RouteInterface
func (fk *FakeRouter) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	panic("not implemented")
}

// Patch implements routev1.RouteInterface
func (fk *FakeRouter) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions,
	subresources ...string,
) (result *v1.Route, err error) {
	panic("not implemented")
}

// fakeMemberRollController implements controller.MemberRollController
type fakeMemberRollController struct {
	listeners  []controller.MemberRollListener
	namespaces []string
	lock       sync.Mutex
}

func newFakeMemberRollController() *fakeMemberRollController {
	return &fakeMemberRollController{}
}

// Register implements controller.MemberRollController
func (fk *fakeMemberRollController) Register(listener controller.MemberRollListener, name string) {
	fk.lock.Lock()
	defer fk.lock.Unlock()

	if listener == nil {
		return
	}

	// ensure that listener has no namespaces until the smmrc initializes it with the actual list of namespaces in the member roll
	listener.SetNamespaces(nil)

	fk.listeners = append(fk.listeners, listener)
}

// Start implements controller.MemberRollController
func (fk *fakeMemberRollController) Start(stopCh <-chan struct{}) {
	panic("not implemented")
}

func (fk *fakeMemberRollController) setNamespaces(namespaces ...string) {
	fk.namespaces = namespaces
	fk.invokeListeners()
}

func (fk *fakeMemberRollController) invokeListeners() {
	fk.lock.Lock()
	defer fk.lock.Unlock()

	for _, l := range fk.listeners {
		l.SetNamespaces(fk.namespaces)
	}
}

var (
	countCalls     = map[string]int{}
	countCallsLock sync.Mutex
)

func countCallsIncrement(k string) {
	countCallsLock.Lock()
	defer countCallsLock.Unlock()
	countCalls[k]++
}
