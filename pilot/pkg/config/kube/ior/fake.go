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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/controller"
)

// FakeRouter implements routev1.RouteInterface
type FakeRouter struct {
	namespace      string
	routes         map[string]*v1.Route
	routesLock     sync.Mutex
	callCounts     *map[string]int
	callCountsLock *sync.Mutex
}

type FakeAggregateRouter struct {
	FakeRouter
	routersByNamespace map[string]*FakeRouter
}

// FakeRouterClient implements routev1.RouteV1Interface
type FakeRouterClient struct {
	routersByNamespace     map[string]*FakeRouter
	routersByNamespaceLock sync.Mutex
	callCounts             *map[string]int
	callCountsLock         sync.Mutex
}

type fakeKubeClient struct {
	client kube.Client
}

type FakeRouterInterface interface {
	routev1.RouteInterface
	getRoutes()
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
	cc := make(map[string]int)

	return &FakeRouterClient{
		routersByNamespace: make(map[string]*FakeRouter),
		callCounts:         &cc,
	}
}

// NewFakeRouter creates a new FakeRouter
func NewFakeRouter(namespace string, callCounts *map[string]int, callCountsLock *sync.Mutex) *FakeRouter {
	return &FakeRouter{
		namespace:      namespace,
		routes:         make(map[string]*v1.Route),
		callCounts:     callCounts,
		callCountsLock: callCountsLock,
	}
}

func NewFakeAggregateRouter(routersByNamespace map[string]*FakeRouter, callCounts *map[string]int, callCountsLock *sync.Mutex) *FakeAggregateRouter {
	fk := &FakeAggregateRouter{
		FakeRouter{
			namespace:      "",
			routes:         nil,
			callCounts:     callCounts,
			callCountsLock: callCountsLock,
		},
		routersByNamespace,
	}

	return fk
}

func (fk *FakeRouter) getRoutes() map[string]*v1.Route {
	fk.routesLock.Lock()
	defer fk.routesLock.Unlock()

	res := make(map[string]*v1.Route)

	for n, r := range fk.routes {
		res[fmt.Sprintf("%s/%s", fk.namespace, n)] = r
	}

	return res
}

func (fk *FakeAggregateRouter) getRoutes() map[string]*v1.Route {
	res := make(map[string]*v1.Route)

	for _, r := range fk.routersByNamespace {
		routes := r.getRoutes()

		for k, v := range routes {
			res[k] = v
		}
	}

	return res
}

// RESTClient implements routev1.RouteV1Interface
func (rc *FakeRouterClient) RESTClient() rest.Interface {
	panic("not implemented")
}

// Routes implements routev1.RouteV1Interface
func (rc *FakeRouterClient) Routes(namespace string) routev1.RouteInterface {
	rc.routersByNamespaceLock.Lock()
	defer rc.routersByNamespaceLock.Unlock()

	if namespace == metav1.NamespaceAll {
		return NewFakeAggregateRouter(rc.routersByNamespace, rc.callCounts, &rc.callCountsLock)
	}

	router, ok := rc.routersByNamespace[namespace]

	if !ok {
		router = NewFakeRouter(namespace, rc.callCounts, &rc.callCountsLock)
		rc.routersByNamespace[namespace] = router
	}

	return router
}

func (fk *FakeRouter) GetCallCount(functionName string) int {
	fk.callCountsLock.Lock()
	defer fk.callCountsLock.Unlock()

	cc := *fk.callCounts

	return cc[functionName]
}

func (fk *FakeRouter) incrementCallCount(functionName string) int {
	fk.callCountsLock.Lock()
	cc := *fk.callCounts
	cc[functionName]++
	num := cc[functionName]
	fk.callCountsLock.Unlock()

	return num
}

func (fk *FakeRouter) generateHost() string {
	num := fk.incrementCallCount("generateHost")

	return fmt.Sprintf("generated-host%d.com", num)
}

func (fk *FakeRouter) getRoute(name string) *v1.Route {
	fk.routesLock.Lock()
	defer fk.routesLock.Unlock()

	return fk.routes[name]
}

func (fk *FakeRouter) setRoute(name string, route *v1.Route) error {
	fk.routesLock.Lock()
	if route == nil {
		delete(fk.routes, name)
	} else {
		fk.routes[name] = route
	}
	fk.routesLock.Unlock()

	return nil
}

// Create implements routev1.RouteInterface
func (fk *FakeRouter) Create(ctx context.Context, route *v1.Route, opts metav1.CreateOptions) (*v1.Route, error) {
	if strings.Contains(route.Spec.Host, "/") {
		return nil, fmt.Errorf("invalid hostname")
	}

	if route.Spec.Host == "" {
		route.Spec.Host = fk.generateHost()
	}

	err := fk.setRoute(route.Name, route)

	fk.incrementCallCount("Create")
	return route, err
}

// Update implements routev1.RouteInterface
func (fk *FakeRouter) Update(ctx context.Context, route *v1.Route, opts metav1.UpdateOptions) (*v1.Route, error) {
	curr := fk.getRoute(route.Name)

	if curr == nil {
		return nil, fmt.Errorf("existing route not found")
	}

	if strings.Contains(route.Spec.Host, "/") {
		return nil, fmt.Errorf("invalid hostname")
	}

	if route.Spec.Host == "" {
		route.Spec.Host = fk.generateHost()
	}

	err := fk.setRoute(route.Name, route)

	fk.incrementCallCount("Update")
	return route, err
}

// UpdateStatus implements routev1.RouteInterface
func (fk *FakeRouter) UpdateStatus(ctx context.Context, route *v1.Route, opts metav1.UpdateOptions) (*v1.Route, error) {
	panic("not implemented")
}

// Delete implements routev1.RouteInterface
func (fk *FakeRouter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	route := fk.getRoute(name)
	if route == nil {
		return fmt.Errorf("route %s not found", name)
	}

	fk.setRoute(name, nil)

	fk.incrementCallCount("Delete")
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
	var items []v1.Route
	for _, route := range fk.getRoutes() {
		items = append(items, *route)
	}
	result := &v1.RouteList{Items: items}

	fk.incrementCallCount("List")
	return result, nil
}

func (fk *FakeAggregateRouter) List(ctx context.Context, opts metav1.ListOptions) (*v1.RouteList, error) {
	selector, err := labels.Parse(opts.LabelSelector)
	if err != nil {
		return nil, err
	}

	var items []v1.Route

	for _, route := range fk.getRoutes() {
		var s labels.Set = route.Labels
		if selector.Matches(s) {
			items = append(items, *route)
		}
	}
	result := &v1.RouteList{Items: items}

	fk.incrementCallCount("List")
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
