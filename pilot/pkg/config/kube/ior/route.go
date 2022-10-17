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
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

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
	routes   map[string]*v1.Route
}

// route manages the integration between Istio Gateways and OpenShift Routes
type route struct {
	pilotNamespace     string
	routerClient       routev1.RouteV1Interface
	kubeClient         kubernetes.Interface
	store              model.ConfigStoreController
	gatewaysMap        map[string]*syncRoutes
	gatewaysLock       sync.Mutex
	stop               <-chan struct{}
	handleEventTimeout time.Duration
	errorChannel       chan error

	// memberroll functionality
	mrc           controller.MemberRollController
	namespaces    []string
	namespaceLock sync.Mutex
}

// newRouterClient returns an OpenShift client for Routers
func newRouterClient() (routev1.RouteV1Interface, error) {
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
	r.handleEventTimeout = kubeClient.GetHandleEventTimeout()
	r.errorChannel = errorChannel

	r.gatewayMap = make(map[string]*syncRoutes)

	return r, nil
}

func gatewayMapKey(namespace, name string) string {
	return namespace + "/" + name
}

// addNewSyncRoute creates a new syncRoutes and adds it to the gatewaysMap
// Must be called with gatewaysLock locked
func (r *route) addNewSyncRoute(cfg config.Config) *syncRoutes {
	gw := cfg.Spec.(*networking.Gateway)
	syncRoute := &syncRoutes{
		metadata: cfg.Meta,
		gateway:  gw,
		routes:   make(map[string]*v1.Route),
	}

	r.gatewayMap[gatewayMapKey(cfg.Namespace, cfg.Name)] = syncRoute
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

func (r *route) onGatewayAdded(cfg config.Config) error {
	var result *multierror.Error

	if err := r.ensureNamespaceExists(cfg); err != nil {
		return err
	}

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	if _, ok := r.gatewayMap[gatewayMapKey(cfg.Namespace, cfg.Name)]; ok {
		IORLog.Infof("gateway %s/%s already exists, not creating route(s) for it", cfg.Namespace, cfg.Name)
		return nil
	}

	syncRoute := r.addNewSyncRoute(cfg)

	serviceNamespace, serviceName, err := r.findService(syncRoute.gateway)
	if err != nil {
		return err
	}

	for _, server := range syncRoute.gateway.Servers {
		for _, host := range server.Hosts {
			route, err := r.createRoute(cfg.Meta, host, server.Tls, serviceNamespace, serviceName)
			if err != nil {
				result = multierror.Append(result, err)
			} else {
				syncRoute.routes[getRouteName(cfg.Meta.Namespace, cfg.Meta.Name, host)] = route
			}
		}
	}

	return result.ErrorOrNil()
}

func (r *route) onGatewayUpdated(cfg config.Config) error {
	var result *multierror.Error

	if err := r.ensureNamespaceExists(cfg); err != nil {
		return err
	}

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	curr, gotGateway := r.gatewayMap[gatewayMapKey(cfg.Namespace, cfg.Name)]

	if !gotGateway {
		return fmt.Errorf("gateway %s/%s does not exists, IOR failed to update", cfg.Namespace, cfg.Name)
	}

	new := r.addNewSyncRoute(cfg)

	serviceNamespace, serviceName, err := r.findService(new.gateway)
	if err != nil {
		return errors.Wrapf(err, "gateway %s/%s does not specify a valid service target.", cfg.Namespace, cfg.Name)
	}

	cr := curr.routes

	for _, server := range new.gateway.Servers {
		for _, host := range server.Hosts {
			var route *v1.Route
			var gotRoute bool
			var err error

			name := getRouteName(cfg.Meta.Namespace, cfg.Meta.Name, host)

			_, gotRoute = cr[name]

			if gotRoute {
				route, err = r.updateRoute(cfg.Meta, host, server.Tls, serviceNamespace, serviceName)
			} else {
				route, err = r.createRoute(cfg.Meta, host, server.Tls, serviceNamespace, serviceName)
			}

			if err != nil {
				result = multierror.Append(result, err)
			} else {
				new.routes[name] = route
				delete(cr, name)
			}
		}
	}

	for _, route := range cr {
		r.deleteRoute(route)
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

func (r *route) onGatewayRemoved(cfg config.Config) error {
	var result *multierror.Error

	r.gatewaysLock.Lock()
	defer r.gatewaysLock.Unlock()

	key := gatewayMapKey(cfg.Namespace, cfg.Name)
	syncRoute, ok := r.gatewayMap[key]
	if !ok {
		return fmt.Errorf("could not find an internal reference to gateway %s/%s", cfg.Namespace, cfg.Name)
	}

	IORLog.Debugf("The gateway %s/%s has %d route(s) associated with it. Removing them now.", cfg.Namespace, cfg.Name, len(syncRoute.routes))
	for _, route := range syncRoute.routes {
		result = multierror.Append(result, r.deleteRoute(route))
	}

	delete(r.gatewayMap, key)

	return result.ErrorOrNil()
}

func (r *route) handleEvent(event model.Event, cfg config.Config) error {
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
		return r.onGatewayAdded(cfg)

	case model.EventUpdate:
		return r.onGatewayUpdated(cfg)

	case model.EventDelete:
		return r.onGatewayRemoved(cfg)
	}

	return fmt.Errorf("unknown event type %s", event)
}

// Trigerred by SMMR controller when SMMR changes
func (r *route) SetNamespaces(namespaces []string) {
	IORLog.Debugf("UpdateNamespaces(%v)", namespaces)
	r.namespaceLock.Lock()
	r.namespaces = namespaces
	r.namespaceLock.Unlock()
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

func buildRoute(metadata config.Meta, originalHost string, tls *networking.ServerTLSSettings, serviceNamespace string, serviceName string) *v1.Route {
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

	return &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        getRouteName(metadata.Namespace, metadata.Name, originalHost),
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
	}
}

func (r *route) createRoute(metadata config.Meta, originalHost string, tls *networking.ServerTLSSettings, serviceNamespace string, serviceName string) (*v1.Route, error) {
	IORLog.Debugf("Creating route for hostname %s", originalHost)

	nr, err := r.routerClient.Routes(serviceNamespace).Create(context.TODO(), buildRoute(metadata, originalHost, tls, serviceNamespace, serviceName), metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error creating a route for the host %s (gateway: %s/%s): %s", originalHost, metadata.Namespace, metadata.Name, err)
	}

	IORLog.Infof("Created route %s/%s for hostname %s (gateway: %s/%s)",
		nr.ObjectMeta.Namespace, nr.ObjectMeta.Name,
		nr.Spec.Host,
		metadata.Namespace, metadata.Name)

	return nr, nil
}

func (r *route) updateRoute(metadata config.Meta, originalHost string, tls *networking.ServerTLSSettings, serviceNamespace string, serviceName string) (*v1.Route, error) {
	IORLog.Debugf("Updating route for hostname %s", originalHost)

	nr, err := r.routerClient.Routes(serviceNamespace).Update(context.TODO(), buildRoute(metadata, originalHost, tls, serviceNamespace, serviceName), metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error updating a route for the host %s (gateway: %s/%s): %s", originalHost, metadata.Namespace, metadata.Name, err)
	}

	IORLog.Infof("Updated route %s/%s for hostname %s (gateway: %s/%s)",
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

func getRouteName(namespace, name, host string) string {
	return fmt.Sprintf("%s-%s-%s", namespace, name, hostHash(host))
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
	var aliveLock sync.Mutex
	alive := true

	go func(stop <-chan struct{}) {
		<-stop
		aliveLock.Lock()
		defer aliveLock.Unlock()
		alive = false
		IORLog.Info("This pod is no longer a leader. IOR stopped responding")
	}(stop)

	IORLog.Debugf("Registering IOR into SMMR broadcast")
	r.mrc.Register(r, "ior")

	IORLog.Debugf("Registering IOR into Gateway broadcast")
	kind := collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind()
	r.store.RegisterEventHandler(kind, func(old, curr config.Config, evt model.Event) {
		aliveLock.Lock()
		defer aliveLock.Unlock()
		if alive {
			r.processEvent(old, curr, evt)
		}
	})
}
