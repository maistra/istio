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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "github.com/openshift/api/route/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/controller"
)

const (
	maistraPrefix               = "maistra.io/"
	generatedByLabel            = maistraPrefix + "generated-by"
	generatedByValue            = "ior"
	originalHostAnnotation      = maistraPrefix + "original-host"
	gatewayNameLabel            = maistraPrefix + "gateway-name"
	gatewayNamespaceLabel       = maistraPrefix + "gateway-namespace"
	gatewayResourceVersionLabel = maistraPrefix + "gateway-resourceVersion"
	ShouldManageRouteAnnotation = maistraPrefix + "manageRoute"

	eventDuplicatedMessage = "event UPDATE arrived but resourceVersions are the same - ignoring"
)

type syncRoutes struct {
	metadata config.Meta
	gateway  *networking.Gateway
	routes   []*v1.Route
}

// route manages the integration between Istio Gateways and OpenShift Routes
type route struct {
	pilotNamespace     string
	routerClient       routev1.RouteV1Interface
	kubeClient         kubernetes.Interface
	store              model.ConfigStoreController
	gatewaysMap        map[string]*syncRoutes
	gatewaysLock       sync.Mutex
	initialSyncRun     chan struct{}
	alive              bool
	stop               <-chan struct{}
	handleEventTimeout time.Duration
	errorChannel       chan error

	// memberroll functionality
	mrc              controller.MemberRollController
	namespaceLock    sync.Mutex
	namespaces       []string
	gotInitialUpdate bool
}

// NewRouterClient returns an OpenShift client for Routers
func NewRouterClient() (routev1.RouteV1Interface, error) {
	config, err := kube.BuildClientConfig("", "")
	if err != nil {
		return nil, err
	}

	client, err := routev1.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// newRoute returns a new instance of Route object
func newRoute(
	kubeClient KubeClient,
	routerClient routev1.RouteV1Interface,
	store model.ConfigStoreController,
	pilotNamespace string,
	mrc controller.MemberRollController,
	stop <-chan struct{},
	errorChannel chan error,
) (*route, error) {
	if !kubeClient.IsRouteSupported() {
		return nil, fmt.Errorf("routes are not supported in this cluster")
	}

	r := &route{}

	r.kubeClient = kubeClient.GetActualClient().Kube()
	r.routerClient = routerClient
	r.pilotNamespace = pilotNamespace
	r.store = store
	r.mrc = mrc
	r.namespaces = []string{pilotNamespace}
	r.stop = stop
	r.initialSyncRun = make(chan struct{})
	r.handleEventTimeout = kubeClient.GetHandleEventTimeout()
	r.errorChannel = errorChannel

	if r.mrc != nil {
		IORLog.Debugf("Registering IOR into SMMR broadcast")
		r.alive = true
		r.mrc.Register(r, "ior")

		go func(stop <-chan struct{}) {
			<-stop
			r.alive = false
			IORLog.Debugf("Unregistering IOR from SMMR broadcast")
		}(stop)
	}

	return r, nil
}

// initialSync runs on initialization only.
//
// It lists all Istio Gateways (source of truth) and OpenShift Routes, compares them and makes the necessary adjustments
// (creation and/or removal of routes) so that gateways and routes be in sync.
func (r *route) initialSync(initialNamespaces []string) error {
	var result *multierror.Error
	r.gatewaysMap = make(map[string]*syncRoutes)

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	// List the gateways and put them into the gatewaysMap
	// The store must be synced otherwise we might get an empty list
	// We enforce this before calling this function in UpdateNamespaces()
	configs, err := r.store.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), model.NamespaceAll)
	if err != nil {
		return fmt.Errorf("could not get list of Gateways: %s", err)
	}
	IORLog.Debugf("initialSync() - Got %d Gateway(s)", len(configs))

	for i, cfg := range configs {
		if err := r.ensureNamespaceExists(cfg); err != nil {
			result = multierror.Append(result, err)
			continue
		}
		manageRoute, err := isManagedByIOR(cfg)
		if err != nil {
			result = multierror.Append(result, err)
			continue
		}
		if !manageRoute {
			IORLog.Debugf("initialSync() - Ignoring Gateway %s/%s as it is not managed by Istiod", cfg.Namespace, cfg.Name)
			continue
		}

		IORLog.Debugf("initialSync() - Parsing Gateway [%d] %s/%s", i+1, cfg.Namespace, cfg.Name)
		r.addNewSyncRoute(cfg)
	}

	// List the routes and put them into a map. Map key is the route object name
	routes := map[string]v1.Route{}
	for _, ns := range initialNamespaces {
		IORLog.Debugf("initialSync() - Listing routes in ns %s", ns)
		routeList, err := r.routerClient.Routes(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", generatedByLabel, generatedByValue),
		})
		if err != nil {
			return fmt.Errorf("could not get list of Routes in namespace %s: %s", ns, err)
		}
		for _, route := range routeList.Items {
			routes[route.Name] = route
		}
	}
	IORLog.Debugf("initialSync() - Got %d route(s) across all %d namespace(s)", len(routes), len(initialNamespaces))

	// Now that we have maps and routes mapped we can compare them (Gateways are the source of truth)
	for _, syncRoute := range r.gatewaysMap {
		for _, server := range syncRoute.gateway.Servers {
			for _, host := range server.Hosts {
				actualHost, _ := getActualHost(host, false)
				routeName := getRouteName(syncRoute.metadata.Namespace, syncRoute.metadata.Name, actualHost)
				route, ok := routes[routeName]
				if ok {
					// A route for this host was found, remove its entry in this map so that in the end only orphan routes are left
					delete(routes, routeName)

					// Route matches, no need to create one. Put it in the gatewaysMap and move to the next one
					if syncRoute.metadata.ResourceVersion == route.Labels[gatewayResourceVersionLabel] {
						syncRoute.routes = append(syncRoute.routes, &route)
						continue
					}

					// Route does not match, remove it.
					result = multierror.Append(result, r.deleteRoute(&route))
				}

				// Route is not found or was removed above because it didn't match. We need to create one now.
				route2, err := r.createRoute(syncRoute.metadata, syncRoute.gateway, host, server.Tls)
				if err != nil {
					result = multierror.Append(result, err)
				} else {
					// Put it in the gatewaysMap and move to the next one
					syncRoute.routes = append(syncRoute.routes, route2)
				}
			}
		}
	}

	// At this point there are routes for every hostname in every Gateway.
	// The `routes` map should only contain "orphan" routes, i.e., routes that do not belong to any Gateway
	//
	for _, route := range routes {
		result = multierror.Append(result, r.deleteRoute(&route))
	}

	return result.ErrorOrNil()
}

func gatewaysMapKey(namespace, name string) string {
	return namespace + "/" + name
}

// addNewSyncRoute creates a new syncRoutes and adds it to the gatewaysMap
// Must be called with gatewaysLock locked
func (r *route) addNewSyncRoute(cfg config.Config) *syncRoutes {
	gw := cfg.Spec.(*networking.Gateway)
	syncRoute := &syncRoutes{
		metadata: cfg.Meta,
		gateway:  gw,
	}

	r.gatewaysMap[gatewaysMapKey(cfg.Namespace, cfg.Name)] = syncRoute
	return syncRoute
}

// ensureNamespaceExists makes sure the gateway namespace is present in r.namespaces
// r.namespaces is updated by the SMMR controller, in SetNamespaces()
// This handles the case where an ADD event comes before SetNamespaces() is called and
// the unlikely case an ADD event arrives for a gateway whose namespace does not belong to the SMMR at all
func (r *route) ensureNamespaceExists(cfg config.Config) error {
	timeout := time.After(r.handleEventTimeout) // production default is 10s, but test default is only 1ms

	for {
		r.namespaceLock.Lock()
		namespaces := r.namespaces
		r.namespaceLock.Unlock()

		for _, ns := range namespaces {
			if ns == cfg.Namespace {
				IORLog.Debugf("Namespace %s found in SMMR", cfg.Namespace)
				return nil
			}
		}

		select {
		case <-timeout:
			IORLog.Debugf("Namespace %s not found in SMMR. Aborting.", cfg.Namespace)
			return fmt.Errorf("could not handle the ADD event for %s/%s: SMMR does not recognize this namespace", cfg.Namespace, cfg.Name)
		default:
			IORLog.Debugf("Namespace %s not found in SMMR, trying again", cfg.Namespace)
		}
		time.Sleep(r.handleEventTimeout / 100)
	}
}

func (r *route) handleAdd(cfg config.Config) error {
	var result *multierror.Error

	if err := r.ensureNamespaceExists(cfg); err != nil {
		return err
	}

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	if _, ok := r.gatewaysMap[gatewaysMapKey(cfg.Namespace, cfg.Name)]; ok {
		IORLog.Infof("gateway %s/%s already exists, not creating route(s) for it", cfg.Namespace, cfg.Name)
		return nil
	}

	syncRoute := r.addNewSyncRoute(cfg)

	for _, server := range syncRoute.gateway.Servers {
		for _, host := range server.Hosts {
			route, err := r.createRoute(cfg.Meta, syncRoute.gateway, host, server.Tls)
			if err != nil {
				result = multierror.Append(result, err)
			} else {
				syncRoute.routes = append(syncRoute.routes, route)
			}
		}
	}

	return result.ErrorOrNil()
}

func isManagedByIOR(cfg config.Config) (bool, error) {
	// We don't manage egress gateways, but we can only look for the default label here.
	// Users can still use generic labels (e.g. "app: my-ingressgateway" as in the istio docs) to refer to the gateway pod
	gw := cfg.Spec.(*networking.Gateway)
	if istioLabel, ok := gw.Selector["istio"]; ok && istioLabel == "egressgateway" {
		return false, nil
	}

	manageRouteValue, ok := cfg.Annotations[ShouldManageRouteAnnotation]
	if !ok {
		// Manage routes by default, when annotation is not found.
		return true, nil
	}

	manageRoute, err := strconv.ParseBool(manageRouteValue)
	if err != nil {
		return false, fmt.Errorf("could not parse annotation %q: %s", ShouldManageRouteAnnotation, err)
	}

	return manageRoute, nil
}

func (r *route) handleDel(cfg config.Config) error {
	var result *multierror.Error

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	key := gatewaysMapKey(cfg.Namespace, cfg.Name)
	syncRoute, ok := r.gatewaysMap[key]
	if !ok {
		return fmt.Errorf("could not find an internal reference to gateway %s/%s", cfg.Namespace, cfg.Name)
	}

	IORLog.Debugf("The gateway %s/%s has %d route(s) associated with it. Removing them now.", cfg.Namespace, cfg.Name, len(syncRoute.routes))
	for _, route := range syncRoute.routes {
		result = multierror.Append(result, r.deleteRoute(route))
	}

	delete(r.gatewaysMap, key)

	return result.ErrorOrNil()
}

func (r *route) verifyResourceVersions(cfg config.Config) error {
	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	key := gatewaysMapKey(cfg.Namespace, cfg.Name)
	syncRoute, ok := r.gatewaysMap[key]
	if !ok {
		return fmt.Errorf("could not find an internal reference to gateway %s/%s", cfg.Namespace, cfg.Name)
	}

	if syncRoute.metadata.ResourceVersion != cfg.ResourceVersion {
		return nil
	}

	return fmt.Errorf(eventDuplicatedMessage)
}

func (r *route) handleEvent(event model.Event, cfg config.Config) error {
	// Block until initial sync has finished
	<-r.initialSyncRun

	manageRoute, err := isManagedByIOR(cfg)
	if err != nil {
		return err
	}
	if !manageRoute {
		IORLog.Infof("Ignoring Gateway %s/%s as it is not managed by Istiod", cfg.Namespace, cfg.Name)
		return nil
	}

	switch event {
	case model.EventAdd:
		return r.handleAdd(cfg)

	case model.EventUpdate:
		if err = r.verifyResourceVersions(cfg); err != nil {
			return err
		}

		var result *multierror.Error
		result = multierror.Append(result, r.handleDel(cfg))
		result = multierror.Append(result, r.handleAdd(cfg))
		return result.ErrorOrNil()

	case model.EventDelete:
		return r.handleDel(cfg)
	}

	return fmt.Errorf("unknown event type %s", event)
}

// Trigerred by SMMR controller when SMMR changes
func (r *route) SetNamespaces(namespaces []string) {
	if !r.alive {
		return
	}

	if namespaces == nil {
		return
	}

	IORLog.Debugf("UpdateNamespaces(%v)", namespaces)
	r.namespaceLock.Lock()
	r.namespaces = namespaces
	r.namespaceLock.Unlock()

	if r.gotInitialUpdate {
		return
	}
	r.gotInitialUpdate = true

	// In the first update we perform an initial sync
	go func() {
		// But only after gateway store cache is synced
		IORLog.Debug("Waiting for the Gateway store cache to sync before performing our initial sync")
		if !cache.WaitForNamedCacheSync("Gateways", r.stop, r.store.HasSynced) {
			IORLog.Infof("Failed to sync Gateway store cache. Not performing initial sync.")
			return
		}
		IORLog.Debug("Gateway store cache synced. Performing our initial sync now")

		if err := r.initialSync(namespaces); err != nil {
			IORLog.Error(err)
			if r.errorChannel != nil {
				r.errorChannel <- err
			}
		}
		IORLog.Debug("Initial sync finished")
		close(r.initialSyncRun)
	}()
}

func getHost(route v1.Route) string {
	if host := route.ObjectMeta.Annotations[originalHostAnnotation]; host != "" {
		return host
	}
	return route.Spec.Host
}

func (r *route) deleteRoute(route *v1.Route) error {
	var immediate int64
	host := getHost(*route)
	err := r.routerClient.Routes(route.Namespace).Delete(context.TODO(), route.ObjectMeta.Name, metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	if err != nil {
		return fmt.Errorf("error deleting route %s/%s: %s", route.ObjectMeta.Namespace, route.ObjectMeta.Name, err)
	}

	IORLog.Infof("Deleted route %s/%s (gateway hostname: %s)", route.ObjectMeta.Namespace, route.ObjectMeta.Name, host)
	return nil
}

func (r *route) createRoute(metadata config.Meta, gateway *networking.Gateway, originalHost string, tls *networking.ServerTLSSettings) (*v1.Route, error) {
	IORLog.Debugf("Creating route for hostname %s", originalHost)
	actualHost, wildcard := getActualHost(originalHost, true)

	var tlsConfig *v1.TLSConfig
	targetPort := "http2"
	if tls != nil {
		tlsConfig = &v1.TLSConfig{Termination: v1.TLSTerminationPassthrough}
		targetPort = "https"
		if tls.HttpsRedirect {
			tlsConfig.InsecureEdgeTerminationPolicy = v1.InsecureEdgeTerminationPolicyRedirect
		}
	}

	serviceNamespace, serviceName, err := r.findService(gateway)
	if err != nil {
		return nil, err
	}

	// Copy annotations
	annotations := map[string]string{
		originalHostAnnotation: originalHost,
	}
	for keyName, keyValue := range metadata.Annotations {
		if !strings.HasPrefix(keyName, "kubectl.kubernetes.io") && keyName != ShouldManageRouteAnnotation {
			annotations[keyName] = keyValue
		}
	}

	// Copy labels
	labels := map[string]string{
		generatedByLabel:            generatedByValue,
		gatewayNamespaceLabel:       metadata.Namespace,
		gatewayNameLabel:            metadata.Name,
		gatewayResourceVersionLabel: metadata.ResourceVersion,
	}
	for keyName, keyValue := range metadata.Labels {
		if !strings.HasPrefix(keyName, maistraPrefix) {
			labels[keyName] = keyValue
		}
	}

	nr, err := r.routerClient.Routes(serviceNamespace).Create(context.TODO(), &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        getRouteName(metadata.Namespace, metadata.Name, actualHost),
			Namespace:   serviceNamespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: v1.RouteSpec{
			Host: actualHost,
			Port: &v1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: targetPort,
				},
			},
			To: v1.RouteTargetReference{
				Name: serviceName,
			},
			TLS:            tlsConfig,
			WildcardPolicy: wildcard,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error creating a route for the host %s (gateway: %s/%s): %s", originalHost, metadata.Namespace, metadata.Name, err)
	}

	IORLog.Infof("Created route %s/%s for hostname %s (gateway: %s/%s)",
		nr.ObjectMeta.Namespace, nr.ObjectMeta.Name,
		nr.Spec.Host,
		metadata.Namespace, metadata.Name)

	return nr, nil
}

// findService tries to find a service that matches with the given gateway selector
// Returns the namespace and service name that is a match, or an error
func (r *route) findService(gateway *networking.Gateway) (string, string, error) {
	r.namespaceLock.Lock()
	namespaces := r.namespaces
	r.namespaceLock.Unlock()

	gwSelector := labels.SelectorFromSet(gateway.Selector)

	for _, ns := range namespaces {
		// Get the list of pods that match the gateway selector
		podList, err := r.kubeClient.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{LabelSelector: gwSelector.String()})
		if err != nil {
			return "", "", fmt.Errorf("could not get the list of pods in namespace %s: %v", ns, err)
		}

		// Get the list of services in this namespace
		svcList, err := r.kubeClient.CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return "", "", fmt.Errorf("could not get the list of services in namespace %s: %v", ns, err)
		}

		// Look for a service whose selector matches the pod labels
		for _, pod := range podList.Items {
			podLabels := labels.Set(pod.ObjectMeta.Labels)

			for _, svc := range svcList.Items {
				svcSelector := labels.SelectorFromSet(svc.Spec.Selector)
				if svcSelector.Matches(podLabels) {
					return ns, svc.Name, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("could not find a service that matches the gateway selector `%s'. Namespaces where we looked at: %v",
		gwSelector.String(), namespaces)
}

func getRouteName(namespace, name, actualHost string) string {
	return fmt.Sprintf("%s-%s-%s", namespace, name, hostHash(actualHost))
}

// getActualHost returns the actual hostname to be used in the route
// `emitWarning` should be false when this function is used internally, without user interaction
// It also returns the route's WildcardPolicy based on the hostname
func getActualHost(originalHost string, emitWarning bool) (string, v1.WildcardPolicyType) {
	wildcard := v1.WildcardPolicyNone

	if strings.Contains(originalHost, "/") {
		originalHost = strings.SplitN(originalHost, "/", 2)[1]
		IORLog.Debugf("Hostname contains a namespace part. Ignoring it and considering the %q portion.", originalHost)
	}

	actualHost := originalHost

	if originalHost == "*" {
		actualHost = ""
		if emitWarning {
			IORLog.Warn("Hostname * is not supported at the moment. Letting OpenShift create it instead.")
		}
	} else if strings.HasPrefix(originalHost, "*.") {
		// FIXME: Update link below to version 4.5 when it's out
		// Wildcards are not enabled by default in OCP 3.x.
		// See https://docs.openshift.com/container-platform/3.11/install_config/router/default_haproxy_router.html#using-wildcard-routes
		// FIXME(2): Is there a way to check if OCP supports wildcard and print out a warning if not?
		wildcard = v1.WildcardPolicySubdomain
		actualHost = "wildcard." + strings.TrimPrefix(originalHost, "*.")
	}

	return actualHost, wildcard
}

// hostHash applies a sha256 on the host and truncate it to the first 8 bytes
// This gives enough uniqueness for a given hostname
func hostHash(name string) string {
	if name == "" {
		name = "star"
	}

	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:8])
}

func (r *route) processEvent(old, curr config.Config, event model.Event) {
	_, ok := curr.Spec.(*networking.Gateway)

	if !ok {
		IORLog.Errorf("could not decode object as Gateway. Object = %v", curr)
		return
	}

	debugMessage := fmt.Sprintf("Event %v arrived:", event)
	if event == model.EventUpdate {
		debugMessage += fmt.Sprintf("\tOld object: %v", old)
	}
	debugMessage += fmt.Sprintf("\tNew object: %v", curr)
	IORLog.Debug(debugMessage)

	if err := r.handleEvent(event, curr); err != nil {
		IORLog.Errora(err)
		if r.errorChannel != nil {
			r.errorChannel <- err
		}
	}
}

func (r *route) Run(stop <-chan struct{}) {
	alive := true
	go func(s <-chan struct{}) {
		// Stop responding to events when we are no longer a leader.
		// The worker may be in the middle of handling an event. It will finish what it is doing then stop.
		<-s
		IORLog.Info("This pod is no longer a leader. IOR stopped responding")
		alive = false
	}(stop)

	kind := collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind()

	r.store.RegisterEventHandler(kind, func(old, curr config.Config, evt model.Event) {
		if alive {
			r.processEvent(old, curr, evt)
		}
	})
}
