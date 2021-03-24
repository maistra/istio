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
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	v1 "github.com/openshift/api/route/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"golang.org/x/net/context"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/controller/memberroll"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	maistraPrefix               = "maistra.io/"
	generatedByLabel            = maistraPrefix + "generated-by"
	generatedByValue            = "ior"
	originalHostAnnotation      = maistraPrefix + "original-host"
	gatewayNameLabel            = maistraPrefix + "gateway-name"
	gatewayNamespaceLabel       = maistraPrefix + "gateway-namespace"
	gatewayResourceVersionLabel = maistraPrefix + "gateway-resourceVersion"
)

type syncRoutes struct {
	metadata model.ConfigMeta
	gateway  *networking.Gateway
	routes   []*v1.Route
}

// route manages the integration between Istio Gateways and OpenShift Routes
type route struct {
	pilotNamespace  string
	client          *routev1.RouteV1Client
	kubeClient      kubernetes.Interface
	store           model.ConfigStoreCache
	gatewaysMap     map[string]*syncRoutes
	gatewaysLock    sync.Mutex
	initialSyncRun  bool
	initialSyncLock sync.Mutex
	alive           bool
	stop            <-chan struct{}

	// memberroll functionality
	mrc              memberroll.Controller
	namespaceLock    sync.Mutex
	namespaces       []string
	gotInitialUpdate bool
}

// newRoute returns a new instance of Route object
func newRoute(kubeClient kubernetes.Interface,
	store model.ConfigStoreCache,
	pilotNamespace string,
	mrc memberroll.Controller,
	stop <-chan struct{}) (*route, error) {
	r := &route{}

	err := r.initClient()
	if err != nil {
		return nil, err
	}

	r.kubeClient = kubeClient
	r.pilotNamespace = pilotNamespace
	r.store = store
	r.mrc = mrc
	r.namespaces = []string{pilotNamespace}
	r.stop = stop

	if r.mrc != nil {
		iorLog.Debugf("Registering IOR into SMMR broadcast")
		r.alive = true
		r.mrc.Register(r, "ior")

		go func(stop <-chan struct{}) {
			<-stop
			r.alive = false
			iorLog.Debugf("Unregistering IOR from SMMR broadcast")
		}(stop)
	}

	return r, nil
}

// initialSync runs on initialization only.
//
// It lists all Istio Gateways (source of truth) and OpenShift Routes, compares them and makes the necessary adjustments
// (creation and/or removal of routes) so that gateways and routes be in sync.
func (r *route) initialSync() error {
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
	iorLog.Debugf("initialSync() - Got %d Gateway(s)", len(configs))

	for i, cfg := range configs {
		iorLog.Debugf("initialSync() - Parsing Gateway [%d] %s/%s", i+1, cfg.Namespace, cfg.Name)
		r.addNewSyncRoute(cfg)
	}

	// List the routes and put them into a map. Map key is the route object name
	routes := map[string]v1.Route{}
	for _, ns := range r.namespaces {
		iorLog.Debugf("initialSync() - Listing routes in ns %s", ns)
		routeList, err := r.client.Routes(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", generatedByLabel, generatedByValue),
		})
		if err != nil {
			return fmt.Errorf("could not get list of Routes in namespace %s: %s", ns, err)
		}
		for _, route := range routeList.Items {
			routes[route.Name] = route
		}
	}
	iorLog.Debugf("initialSync() - Got %d route(s) across all %d namespace(s)", len(routes), len(r.namespaces))

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
func (r *route) addNewSyncRoute(cfg model.Config) *syncRoutes {
	gw := cfg.Spec.(*networking.Gateway)
	syncRoute := &syncRoutes{
		metadata: cfg.ConfigMeta,
		gateway:  gw,
	}

	r.gatewaysMap[gatewaysMapKey(cfg.Namespace, cfg.Name)] = syncRoute
	return syncRoute
}

func (r *route) handleAdd(cfg model.Config) error {
	var result *multierror.Error

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	if _, ok := r.gatewaysMap[gatewaysMapKey(cfg.Namespace, cfg.Name)]; ok {
		// This usually occurs in unit tests, when sync is so fast that when the first add events come the sync is already done
		// Just logging a debug message, as this is not really an error
		iorLog.Debugf("gateway %s/%s already exists, not creating route(s) for it", cfg.Namespace, cfg.Name)
		return nil
	}

	syncRoute := r.addNewSyncRoute(cfg)

	for _, server := range syncRoute.gateway.Servers {
		for _, host := range server.Hosts {
			route, err := r.createRoute(cfg.ConfigMeta, syncRoute.gateway, host, server.Tls)
			if err != nil {
				result = multierror.Append(result, err)
			} else {
				syncRoute.routes = append(syncRoute.routes, route)
			}
		}
	}

	return result.ErrorOrNil()
}

func (r *route) handleDel(cfg model.Config) error {
	var result *multierror.Error

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	key := gatewaysMapKey(cfg.Namespace, cfg.Name)
	syncRoute, ok := r.gatewaysMap[key]
	if !ok {
		return fmt.Errorf("could not find an internal reference to gateway %s/%s", cfg.Namespace, cfg.Name)
	}

	iorLog.Debugf("The gateway %s/%s has %d route(s) associated with it. Removing them now.", cfg.Namespace, cfg.Name, len(syncRoute.routes))
	for _, route := range syncRoute.routes {
		result = multierror.Append(result, r.deleteRoute(route))
	}

	delete(r.gatewaysMap, key)

	return result.ErrorOrNil()
}

func (r *route) handleEvent(event model.Event, cfg model.Config) error {
	r.initialSyncLock.Lock()
	initialSyncRun := r.initialSyncRun
	r.initialSyncLock.Unlock()

	// Only handle event updates after initial sync has run
	// This is to prevent having to deal with lots of Add's sent by the store on their first sync
	// We handle this initial state in UpdateNamespaces() below
	if !initialSyncRun {
		iorLog.Debug("Ignoring event because we did not run the initial sync yet")
		return nil
	}

	switch event {
	case model.EventAdd:
		return r.handleAdd(cfg)

	case model.EventUpdate:
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
func (r *route) UpdateNamespaces(namespaces []string) {
	if !r.alive {
		return
	}

	iorLog.Debugf("UpdateNamespaces(%v)", namespaces)
	r.namespaceLock.Lock()
	r.namespaces = namespaces
	r.namespaceLock.Unlock()

	if r.gotInitialUpdate {
		return
	}
	r.gotInitialUpdate = true

	// In the first update we perform an initial sync
	go func() {
		r.initialSyncLock.Lock()
		initialSyncRun := r.initialSyncRun
		r.initialSyncLock.Unlock()

		if !initialSyncRun {
			// But only after gateway store cache is synced
			iorLog.Debug("Waiting for the Gateway store cache to sync before performing our initial sync")
			cache.WaitForNamedCacheSync("Gateways", r.stop, r.store.HasSynced)
			iorLog.Debug("Gateway store cache synced. Performing our initial sync now")

			if err := r.initialSync(); err != nil {
				iorLog.Errora(err)
			}

			r.initialSyncLock.Lock()
			r.initialSyncRun = true
			r.initialSyncLock.Unlock()
		}
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
	err := r.client.Routes(route.Namespace).Delete(context.TODO(), route.ObjectMeta.Name, metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	if err != nil {
		return fmt.Errorf("error deleting route %s/%s: %s", route.ObjectMeta.Namespace, route.ObjectMeta.Name, err)
	}

	iorLog.Infof("Deleted route %s/%s (gateway hostname: %s)", route.ObjectMeta.Namespace, route.ObjectMeta.Name, host)
	return nil
}

func (r *route) createRoute(metadata model.ConfigMeta, gateway *networking.Gateway, originalHost string, tls *networking.ServerTLSSettings) (*v1.Route, error) {
	iorLog.Debugf("Creating route for hostname %s", originalHost)
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

	annotations := map[string]string{
		originalHostAnnotation: originalHost,
	}
	for keyName, keyValue := range metadata.Annotations {
		if !strings.HasPrefix(keyName, "kubectl.kubernetes.io") {
			annotations[keyName] = keyValue
		}
	}

	nr, err := r.client.Routes(serviceNamespace).Create(context.TODO(), &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name: getRouteName(metadata.Namespace, metadata.Name, actualHost),
			Labels: map[string]string{
				generatedByLabel:            generatedByValue,
				gatewayNamespaceLabel:       metadata.Namespace,
				gatewayNameLabel:            metadata.Name,
				gatewayResourceVersionLabel: metadata.ResourceVersion,
			},
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
		return nil, fmt.Errorf("error creating a route for the host %s (gateway: %s/%s): %v", originalHost, metadata.Namespace, metadata.Name, err)
	}

	iorLog.Infof("Created route %s/%s for hostname %s (gateway: %s/%s)",
		nr.ObjectMeta.Namespace, nr.ObjectMeta.Name,
		nr.Spec.Host,
		metadata.Namespace, metadata.Name)

	return nr, nil
}

func (r *route) initClient() error {
	config, err := kube.BuildClientConfig("", "")
	if err != nil {
		return fmt.Errorf("error creating a Kubernetes client: %v", err)
	}

	client, err := routev1.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error creating an OpenShift route client: %v", err)
	}

	r.client = client

	return nil
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
	actualHost := originalHost

	if originalHost == "*" {
		actualHost = ""
		if emitWarning {
			iorLog.Warn("Hostname * is not supported at the moment. Letting OpenShift create it instead.")
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
