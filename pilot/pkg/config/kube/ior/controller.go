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
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
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
)

// routeController manages the integration between Istio Gateways and OpenShift Routes
type routeController struct {
	store model.ConfigStoreController

	podLister     kclient.Client[*corev1.Pod]
	serviceLister kclient.Client[*corev1.Service]

	routeLister kclient.Client[*v1.Route]
}

// newRouteController returns a new instance of Route object
func newRouteController(kubeClient KubeClient, store model.ConfigStoreController) *routeController {
	for !kubeClient.IsRouteSupported() {
		iorLog.Infof("routes are not supported in this cluster; waiting for Route resource to become available...")
		time.Sleep(10 * time.Second)
	}

	r := &routeController{
		store:         store,
		podLister:     kclient.New[*corev1.Pod](kubeClient.GetActualClient()),
		serviceLister: kclient.New[*corev1.Service](kubeClient.GetActualClient()),
		routeLister:   kclient.New[*v1.Route](kubeClient.GetActualClient()),
	}

	return r
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

func getHost(route v1.Route) string {
	if host := route.ObjectMeta.Annotations[originalHostAnnotation]; host != "" {
		return host
	}
	return route.Spec.Host
}

func (r *routeController) deleteRoute(route *v1.Route) error {
	host := getHost(*route)
	err := r.routeLister.Delete(route.ObjectMeta.Name, route.Namespace)
	if err != nil {
		return errors.Wrapf(err, "error deleting route %s/%s for the host %s",
			route.ObjectMeta.Name,
			route.ObjectMeta.Namespace,
			host)
	}

	iorLog.Infof("route %s/%s deleted for the host %s", route.ObjectMeta.Name, route.ObjectMeta.Namespace, host)
	return nil
}

func filteredRouteAnnotation(routeAnnotation string) bool {
	filteredPrefixes := [...]string{
		ShouldManageRouteAnnotation,
		"kubectl.kubernetes.io",
	}

	for _, prefix := range filteredPrefixes {
		if strings.HasPrefix(routeAnnotation, prefix) {
			return true
		}
	}
	return false
}

func filteredRouteLabel(routeLabel string) bool {
	filteredPrefixes := [...]string{
		maistraPrefix,
		"argocd.argoproj.io/instance",
	}

	for _, prefix := range filteredPrefixes {
		if strings.HasPrefix(routeLabel, prefix) {
			return true
		}
	}
	return false
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

	// Copy annotationMap
	annotationMap := map[string]string{
		originalHostAnnotation: originalHost,
	}
	for keyName, keyValue := range metadata.Annotations {
		if !filteredRouteAnnotation(keyName) {
			annotationMap[keyName] = keyValue
		}
	}

	// Copy labelMap
	labelMap := getDefaultRouteLabelMap(metadata.Name, metadata.Namespace)
	labelMap[gatewayResourceVersionLabel] = metadata.ResourceVersion

	for keyName, keyValue := range metadata.Labels {
		if !filteredRouteLabel(keyName) {
			labelMap[keyName] = keyValue
		}
	}

	return &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        getRouteName(metadata.Namespace, metadata.Name, originalHost),
			Namespace:   serviceNamespace,
			Labels:      labelMap,
			Annotations: annotationMap,
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

func (r *routeController) createRoute(
	metadata config.Meta,
	originalHost string,
	tls *networking.ServerTLSSettings,
	serviceNamespace, serviceName string,
) (*v1.Route, error) {
	iorLog.Debugf("creating route for hostname %s", originalHost)

	nr, err := r.
		routeLister.Create(buildRoute(metadata, originalHost, tls, serviceNamespace, serviceName))
	if err != nil {
		return nil, errors.Wrapf(err, "error creating a route for the host %s from gateway: %s/%s",
			originalHost,
			metadata.Namespace,
			metadata.Name)
	}

	iorLog.Infof("route %s/%s created for hostname %s from gateway %s/%s",
		nr.ObjectMeta.Namespace, nr.ObjectMeta.Name,
		nr.Spec.Host,
		metadata.Namespace, metadata.Name)

	return nr, nil
}

func (r *routeController) updateRoute(
	metadata config.Meta,
	originalHost string,
	tls *networking.ServerTLSSettings,
	serviceNamespace string, serviceName string,
	route *v1.Route,
) (*v1.Route, error) {
	iorLog.Debugf("updating route for hostname %s", originalHost)

	curr := buildRoute(metadata, originalHost, tls, serviceNamespace, serviceName)

	curr.ResourceVersion = route.ResourceVersion

	nr, err := r.
		routeLister.
		Update(curr)
	if err != nil {
		return nil, errors.Wrapf(err, "error updating a route for the host %s from gateway: %s/%s",
			originalHost,
			metadata.Namespace,
			metadata.Name)
	}

	iorLog.Infof("route %s/%s updated for hostname %s from gateway %s/%s",
		nr.ObjectMeta.Namespace, nr.ObjectMeta.Name,
		nr.Spec.Host,
		metadata.Namespace, metadata.Name)

	return nr, nil
}

func (r *routeController) findRoutes(metadata config.Meta) []*v1.Route {
	return r.routeLister.List(
		metav1.NamespaceAll,
		labels.SelectorFromSet(
			getDefaultRouteLabelMap(metadata.Name, metadata.Namespace),
		),
	)
}

// findService tries to find a service that matches with the given gateway selector
// Returns the namespace and service name that is a match, or an error
func (r *routeController) findService(gateway *networking.Gateway) (*types.NamespacedName, error) {
	gwSelector := labels.SelectorFromSet(gateway.Selector)

	// Get the list of pods that match the gateway selector
	pods := r.podLister.List(metav1.NamespaceAll, gwSelector)

	iorLog.Debugf("found %d pod(s) with %s gateway selector", len(pods), gwSelector)

	// Get the list of services in this namespace

	for _, pod := range pods {
		services := r.serviceLister.List(pod.Namespace, labels.Everything())
		iorLog.Debugf("found %d service(s) under %s namespace", len(services), pod.Namespace)
		podLabels := labels.Set(pod.ObjectMeta.Labels)
		// Look for a service whose selector matches the pod labels
		for _, service := range services {
			if len(service.Spec.Selector) == 0 {
				// an empty label selector does not mean that the service should match all pods, but that the
				// service endpoints are managed externally; IOR should therefore ignore this service
				continue
			}
			svcSelector := labels.SelectorFromSet(service.Spec.Selector)

			iorLog.Debugf("matching service selector %s against %s", svcSelector.String(), podLabels)
			if svcSelector.Matches(podLabels) {
				return &types.NamespacedName{Namespace: pod.Namespace, Name: service.Name}, nil
			}

		}
	}

	return nil, fmt.Errorf("could not find a service that matches the gateway selector '%s'", gwSelector.String())
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
		iorLog.Debugf("Hostname contains a namespace part. Ignoring it and considering the %q portion.", originalHost)
	}

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

func (r *routeController) reconcileGateway(config *config.Config, routes []*v1.Route) error {
	gateway, ok := config.Spec.(*networking.Gateway)

	if !ok {
		return fmt.Errorf("could not decode spec as Gateway from %v", config)
	}

	var err error
	var namespacedName *types.NamespacedName

	namespacedName, err = r.findService(gateway)
	if err != nil {
		return errors.Wrapf(err, "gateway %s/%s does not specify a valid service", config.Namespace, config.Name)
	}

	serviceNamespace := namespacedName.Namespace
	serviceName := namespacedName.Name

	routeMap := make(map[string]*v1.Route)

	for _, v := range routes {
		routeMap[v.Name] = v
	}

	var result *multierror.Error

	for _, server := range gateway.Servers {
		for _, host := range server.Hosts {
			var err error

			name := getRouteName(config.Namespace, config.Name, host)

			route, found := routeMap[name]

			if found {
				_, err = r.updateRoute(config.Meta, host, server.Tls, serviceNamespace, serviceName, route)

				// We always want to remove the route to avoid getting deleted.
				delete(routeMap, name)
			} else {
				_, err = r.createRoute(config.Meta, host, server.Tls, serviceNamespace, serviceName)
			}

			if err != nil {
				result = multierror.Append(result, err)
			}
		}
	}

	for k, v := range routeMap {
		iorLog.Debugf("clean up route %s for host %s", k, getHost(*v))
		if err := r.deleteRoute(v); err != nil {
			result = multierror.Append(result, err)
		}
	}

	return result.ErrorOrNil()
}

func (r *routeController) processEvent(old, curr *config.Config, event model.Event) error {
	if iorLog.GetOutputLevel() >= log.DebugLevel {
		debugMessage := fmt.Sprintf("event %v arrived:", event)
		if event == model.EventUpdate {
			debugMessage += fmt.Sprintf("\told object: %v", old)
		}
		debugMessage += fmt.Sprintf("\tnew object: %v", curr)

		iorLog.Debug(debugMessage)
	}

	isManaged, err := isManagedByIOR(*curr)
	if err != nil {
		return err
	}

	if !isManaged {
		iorLog.Debugf("skipped processing routes for gateway %s/%s, as it is annotated by user", curr.Name, curr.Namespace)
		return nil
	}

	config := r.store.Get(collections.Gateway.GroupVersionKind(), curr.Name, curr.Namespace)

	routes := r.findRoutes(curr.Meta)

	if config != nil {
		return r.reconcileGateway(curr, routes)
	}

	var result *multierror.Error

	for _, route := range routes {
		if err := r.deleteRoute(route); err != nil {
			result = multierror.Append(result, err)
		}
	}

	return result.ErrorOrNil()
}

func (r *routeController) Run(stop <-chan struct{}) {
	var aliveLock sync.Mutex
	alive := true

	iorLog.Debugf("Registering IOR into SMMR broadcast")

	go func(stop <-chan struct{}) {
		<-stop
		aliveLock.Lock()
		defer aliveLock.Unlock()
		alive = false
		iorLog.Info("This pod is no longer a leader. IOR stopped responding")
	}(stop)

	iorLog.Debugf("Registering IOR into Gateway broadcast")
	kind := collections.Gateway.GroupVersionKind()
	r.store.RegisterEventHandler(kind, func(old, curr config.Config, evt model.Event) {
		aliveLock.Lock()
		defer aliveLock.Unlock()
		if alive {
			err := r.processEvent(&old, &curr, evt)
			if err != nil {
				iorLog.Errorf("failed to process gateway %s/%s event %s: %s", curr.Name, curr.Namespace, evt.String(), err)
			}
		}
	})
}

func getDefaultRouteLabelMap(name, namespace string) map[string]string {
	return map[string]string{
		generatedByLabel:      generatedByValue,
		gatewayNamespaceLabel: namespace,
		gatewayNameLabel:      name,
	}
}
