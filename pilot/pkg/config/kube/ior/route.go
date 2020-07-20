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

	"github.com/hashicorp/go-multierror"
	v1 "github.com/openshift/api/route/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
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

// route manages the integration between Istio Gateways and OpenShift Routes
type route struct {
	pilotNamespace string
	client         *routev1.RouteV1Client
	kubeClient     kubernetes.Interface
	store          model.ConfigStoreCache
}

// newRoute returns a new instance of Route object
func newRoute(kubeClient kubernetes.Interface, store model.ConfigStoreCache, pilotNamespace string) (*route, error) {
	r := &route{}

	err := r.initClient()
	if err != nil {
		return nil, err
	}

	r.kubeClient = kubeClient
	r.pilotNamespace = pilotNamespace
	r.store = store

	return r, nil
}

func (r *route) syncGatewaysAndRoutes() error {
	configs, err := r.store.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), model.NamespaceAll)
	if err != nil {
		return fmt.Errorf("could not get list of Gateways: %s", err)
	}

	routes, err := r.client.Routes(r.pilotNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", generatedByLabel, generatedByValue),
	})
	if err != nil {
		return fmt.Errorf("could not get list of Routes: %s", err)
	}

	var result *multierror.Error
	routesMap := make(map[string]*v1.Route, len(routes.Items))
	for _, route := range routes.Items {
		_, err := findConfig(configs, route.Labels[gatewayNameLabel], route.Labels[gatewayNamespaceLabel], route.Labels[gatewayResourceVersionLabel])
		if err != nil {
			result = multierror.Append(r.deleteRoute(&route))
		} else {
			routesMap[getHost(route)] = &route
		}
	}

	for _, cfg := range configs {
		gateway := cfg.Spec.(*networking.Gateway)
		iorLog.Debugf("Found Gateway: %s/%s", cfg.Namespace, cfg.Name)

		for _, server := range gateway.Servers {
			for _, host := range server.Hosts {
				_, ok := routesMap[host]
				if !ok {
					result = multierror.Append(r.createRoute(cfg.Meta, gateway, host, server.Tls != nil))
				}

			}
		}
	}

	return result.ErrorOrNil()
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
	err := r.client.Routes(r.pilotNamespace).Delete(context.TODO(), route.ObjectMeta.Name, metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	if err != nil {
		return fmt.Errorf("error deleting route %s/%s: %s", route.ObjectMeta.Namespace, route.ObjectMeta.Name, err)
	}

	iorLog.Infof("Deleted route %s/%s (gateway hostname: %s)", route.ObjectMeta.Namespace, route.ObjectMeta.Name, host)
	return nil
}

func (r *route) createRoute(metadata config.Meta, gateway *networking.Gateway, originalHost string, tls bool) error {
	var wildcard = v1.WildcardPolicyNone
	actualHost := originalHost

	iorLog.Debugf("Creating route for hostname %s", originalHost)

	if originalHost == "*" {
		iorLog.Warnf("Gateway %s/%s: Hostname * is not supported at the moment. Letting OpenShift create it instead.", metadata.Namespace, metadata.Name)
		actualHost = ""
	} else if strings.HasPrefix(originalHost, "*.") {
		// FIXME: Update link below to version 4.5 when it's out
		// Wildcards are not enabled by default in OCP 3.x.
		// See https://docs.openshift.com/container-platform/3.11/install_config/router/default_haproxy_router.html#using-wildcard-routes
		// FIXME(2): Is there a way to check if OCP supports wildcard and print out a warning if not?
		wildcard = v1.WildcardPolicySubdomain
		actualHost = "wildcard." + strings.TrimPrefix(originalHost, "*.")
	}

	var tlsConfig *v1.TLSConfig
	targetPort := "http2"
	if tls {
		tlsConfig = &v1.TLSConfig{Termination: v1.TLSTerminationPassthrough}
		targetPort = "https"
	}

	serviceNamespace, serviceName, err := r.findService(gateway)
	if err != nil {
		return err
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
			GenerateName: fmt.Sprintf("%s-%s-", metadata.Namespace, metadata.Name),
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
		return fmt.Errorf("error creating a route for the host %s (gateway: %s/%s): %s", originalHost, metadata.Namespace, metadata.Name, err)
	}

	iorLog.Infof("Created route %s/%s for hostname %s (gateway: %s/%s)",
		nr.ObjectMeta.Namespace, nr.ObjectMeta.Name,
		nr.Spec.Host,
		metadata.Namespace, metadata.Name)

	return nil
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

// findService tries to find a service that matches with the given gateway selector, in the given namespaces
// Returns the namespace and service name that is a match, or an error
func (r *route) findService(gateway *networking.Gateway) (string, string, error) {
	gwSelector := labels.SelectorFromSet(gateway.Selector)

	// FIXME: Should we look for ingress gateway pod/service in all mesh members instead of just in the control plane namespace?
	namespaces := []string{r.pilotNamespace}

	for _, ns := range namespaces {
		// Get the list of pods that match the gateway selector
		podList, err := r.kubeClient.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{LabelSelector: gwSelector.String()})
		if err != nil { // FIXME: check for NotFound
			return "", "", fmt.Errorf("could not get the list of pods: %v", err)
		}

		// Get the list of services in this namespace
		svcList, err := r.kubeClient.CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil { // FIXME: check for NotFound
			return "", "", fmt.Errorf("could not get the list of services: %v", err)
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

func findConfig(list []config.Config, name, namespace, resourceVersion string) (config.Config, error) {
	for _, item := range list {
		if item.Name == name && item.Namespace == namespace && item.ResourceVersion == resourceVersion {
			return item, nil
		}
	}
	return config.Config{}, fmt.Errorf("config not found")
}
