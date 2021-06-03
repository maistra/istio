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
package discovery

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"maistra.io/api/core/v1alpha1"

	meshv1alpha1 "istio.io/api/mesh/v1alpha1"
	rawnetworking "istio.io/api/networking/v1alpha3"
	rawsecurity "istio.io/api/security/v1beta1"
	rawtype "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/servicemesh/federation/common"
)

func init() {
	schemasBuilder := collection.NewSchemasBuilder()
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Destinationrules)
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Virtualservices)
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Gateways)
	schemasBuilder.MustAdd(collections.IstioSecurityV1Beta1Authorizationpolicies)
	// XXX: we should consider adding this directly to the service registry
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Serviceentries)
	Schemas = schemasBuilder.Build()
}

var Schemas collection.Schemas

// only to facilitate processing errors from the store
type storeErrorChecker struct {
	s string
}

func (e *storeErrorChecker) Error() string {
	return e.s
}

func (e *storeErrorChecker) Is(other error) bool {
	return other.Error() == e.s
}

var (
	memoryStoreErrNotFound      = &storeErrorChecker{"item not found"}
	memoryStoreErrAlreadyExists = &storeErrorChecker{"item already exists"}
)

func (c *Controller) deleteDiscoveryResources(
	_ context.Context, instance *v1alpha1.MeshFederation) error {
	c.Logger.Infof("deleting discovery resources for Federation cluster %s", instance.Name)
	var allErrors []error
	rootName := discoveryResourceName(instance)
	egressName := discoveryEgressResourceName(instance)
	ingressName := discoveryIngressResourceName(instance)
	// XXX: the only errors possible on delete are "does not exist" and "unknown
	// type", so maybe we should just skip error checking?
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
		rootName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery Service %s for Federation cluster %s: %v",
			rootName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		rootName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery VirtualService %s for Federation cluster %s: %v",
			rootName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		ingressName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery ingress Gateway %s for Federation cluster %s: %v",
			ingressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		egressName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery egress Gateway %s for Federation cluster %s: %v",
			egressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
		rootName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery DestinationRule %s for Federation cluster %s: %v",
			rootName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
		rootName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery AuthorizationPolicy %s for Federation cluster %s: %v",
			rootName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	return utilerrors.NewAggregate(allErrors)
}

func (c *Controller) createDiscoveryResources(
	_ context.Context, instance *v1alpha1.MeshFederation, meshConfig *meshv1alpha1.MeshConfig) (err error) {
	var s, ap, dr, ig, eg, vs *config.Config

	defer func() {
		if err != nil {
			c.Logger.Errorf("error creating discovery configuration for Federation cluster %s: %v", instance.Name, err)
			c.Logger.Infof("rolling back discovery Service for %s", instance.Name)
			if s != nil {
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
					s.Name, s.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery Service %s: %v", s.Name, newErr)
				}
			}
			c.Logger.Infof("rolling back discovery AuthorizationPolicy for Federation cluster %s", instance.Name)
			if ap != nil {
				if newErr := c.Delete(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
					ap.Name, ap.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery AuthorizationPolicy %s: %v", ap.Name, newErr)
				}
			}
			if dr != nil {
				c.Logger.Infof("rolling back discovery DestinationRule for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					dr.Name, dr.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery DestinationRule %s: %v", dr.Name, newErr)
				}
			}
			if ig != nil {
				c.Logger.Infof("rolling back discovery ingress Gateway for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
					ig.Name, ig.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery ingress Gateway %s: %v", ig.Name, newErr)
				}
			}
			if eg != nil {
				c.Logger.Infof("rolling back discovery egress Gateway for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
					eg.Name, eg.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery ingress Gateway %s: %v", eg.Name, newErr)
				}
			}
			if vs != nil {
				c.Logger.Infof("rolling back discovery VirtualService for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
					vs.Name, vs.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery VirtualService %s: %v", vs.Name, newErr)
				}
			}
		}
	}()

	s = c.discoveryService(instance)
	_, err = c.Create(*s)
	if err != nil {
		if errors.Is(err, memoryStoreErrAlreadyExists) {
			// XXX: We don't support upgrade, so ignore this for now
			err = nil
		} else {
			s = nil
			return
		}
	}

	ap = c.discoveryAuthorizationPolicy(instance)
	_, err = c.Create(*ap)
	if err != nil {
		if errors.Is(err, memoryStoreErrAlreadyExists) {
			// XXX: We don't support upgrade, so ignore this for now
			err = nil
		} else {
			ap = nil
			return
		}
	}

	dr = c.discoveryDestinationRule(instance)
	_, err = c.Create(*dr)
	if err != nil {
		if errors.Is(err, memoryStoreErrAlreadyExists) {
			// XXX: We don't support upgrade, so ignore this for now
			err = nil
		} else {
			dr = nil
			return
		}
	}

	ig = c.discoveryIngressGateway(instance)
	_, err = c.Create(*ig)
	if err != nil {
		if errors.Is(err, memoryStoreErrAlreadyExists) {
			// XXX: We don't support upgrade, so ignore this for now
			err = nil
		} else {
			ig = nil
			return
		}
	}

	eg = c.discoveryEgressGateway(instance)
	_, err = c.Create(*eg)
	if err != nil {
		if errors.Is(err, memoryStoreErrAlreadyExists) {
			// XXX: We don't support upgrade, so ignore this for now
			err = nil
		} else {
			return
		}
	}

	vs = c.discoveryVirtualService(instance, meshConfig)
	_, err = c.Create(*vs)
	if err != nil {
		if errors.Is(err, memoryStoreErrAlreadyExists) {
			// XXX: We don't support upgrade, so ignore this for now
			err = nil
		} else {
			vs = nil
			return
		}
	}

	return
}

func discoveryResourceName(instance *v1alpha1.MeshFederation) string {
	return fmt.Sprintf("federation-discovery-%s", instance.Name)
}

func discoveryEgressResourceName(instance *v1alpha1.MeshFederation) string {
	return fmt.Sprintf("%s-egress", discoveryResourceName(instance))
}

func discoveryIngressResourceName(instance *v1alpha1.MeshFederation) string {
	return fmt.Sprintf("%s-ingress", discoveryResourceName(instance))
}

func federationIngressLabels(instance *v1alpha1.MeshFederation) map[string]string {
	return map[string]string{
		"service.istio.io/canonical-name": instance.Spec.Gateways.Ingress.Name,
	}
}

func federationEgressLabels(instance *v1alpha1.MeshFederation) map[string]string {
	return map[string]string{
		"service.istio.io/canonical-name": instance.Spec.Gateways.Egress.Name,
	}
}

func serviceAddressPort(addr string) (string, string) {
	portIndex := strings.Index(addr, ":")
	if portIndex >= 0 {
		return addr[:portIndex], addr[portIndex:]
	}
	return addr, "80"
}

func (c *Controller) discoveryHostname(instance *v1alpha1.MeshFederation) string {
	return fmt.Sprintf("discovery.%s.svc.%s.local", instance.Namespace, instance.Name)
}

func (c *Controller) discoveryService(instance *v1alpha1.MeshFederation) *config.Config {
	// This is used for routing out of the egress gateway, primarily to configure mtls for discovery and
	// to give the gateway an endpoint to route to (i.e. it creates a cluster with an endpoint in the gateway).
	// This should turn into a service entry for the other mesh's network.
	name := discoveryResourceName(instance)
	discoveryHost := instance.Spec.NetworkAddress
	discoveryPort := common.DefaultDiscoveryPort
	service := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Labels: map[string]string{
				"topology.istio.io/network": fmt.Sprintf("network-%s", instance.Name),
			},
		},
		Spec: &rawnetworking.ServiceEntry{
			Hosts: []string{
				c.discoveryHostname(instance),
			},
			Location: rawnetworking.ServiceEntry_MESH_EXTERNAL,
			Ports: []*rawnetworking.Port{
				{
					Name:       "https-discovery",
					Number:     uint32(8188),
					Protocol:   "HTTPS",
					TargetPort: uint32(8188),
				},
			},
			Resolution: rawnetworking.ServiceEntry_DNS,
			Endpoints: []*rawnetworking.WorkloadEntry{
				{
					Address: discoveryHost,
					Ports: map[string]uint32{
						"https-discovery": uint32(discoveryPort),
					},
				},
			},
			ExportTo: []string{
				".",
			},
		},
	}
	return service
}

func (c *Controller) discoveryIngressGateway(instance *v1alpha1.MeshFederation) *config.Config {
	// Gateway definition for handling inbound discovery requests
	name := discoveryIngressResourceName(instance)
	discoveryPort := common.DefaultDiscoveryPort
	gateway := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
		},
		Spec: &rawnetworking.Gateway{
			Selector: federationIngressLabels(instance),
			Servers: []*rawnetworking.Server{
				{
					Name: name,
					Hosts: []string{
						"*",
					},
					Port: &rawnetworking.Port{
						Name:     "https-discovery",
						Number:   uint32(discoveryPort),
						Protocol: "HTTPS",
					},
					Tls: &rawnetworking.ServerTLSSettings{
						Mode: rawnetworking.ServerTLSSettings_ISTIO_MUTUAL,
					},
				},
			},
		},
	}
	return gateway
}

func (c *Controller) discoveryEgressGateway(instance *v1alpha1.MeshFederation) *config.Config {
	// Gateway definition for routing outbound discovery.  This is used to terminate source mtls for discovery.
	name := discoveryEgressResourceName(instance)
	egressGatewayServiceName := fmt.Sprintf("%s.%s.svc.%s",
		instance.Spec.Gateways.Egress.Name, instance.Namespace, c.env.DomainSuffix)
	discoveryPort := common.DefaultDiscoveryPort
	gateway := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
		},
		Spec: &rawnetworking.Gateway{
			Selector: federationEgressLabels(instance),
			Servers: []*rawnetworking.Server{
				{
					Name: name,
					Hosts: []string{
						egressGatewayServiceName,
					},
					Port: &rawnetworking.Port{
						// XXX: this will eventually be encrypted
						Name:     "http-discovery",
						Number:   uint32(discoveryPort),
						Protocol: "HTTP",
					},
				},
			},
		},
	}
	return gateway
}

func (c *Controller) discoveryAuthorizationPolicy(instance *v1alpha1.MeshFederation) *config.Config {
	// AuthorizationPolicy used to restrict inbound discovery requests to known clients.
	name := discoveryResourceName(instance)
	discoveryPort := common.DefaultDiscoveryPort
	ap := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
		},
		Spec: &rawsecurity.AuthorizationPolicy{
			Selector: &rawtype.WorkloadSelector{
				MatchLabels: federationIngressLabels(instance),
			},
			Action: rawsecurity.AuthorizationPolicy_DENY,
			Rules: []*rawsecurity.Rule{
				{
					From: []*rawsecurity.Rule_From{
						{
							Source: &rawsecurity.Source{
								NotPrincipals: []string{
									instance.Spec.Security.ClientID,
								},
							},
						},
					},
					To: []*rawsecurity.Rule_To{
						{
							Operation: &rawsecurity.Operation{
								Ports: []string{
									strconv.FormatInt(int64(discoveryPort), 10),
								},
							},
						},
					},
				},
			},
		},
	}
	return ap
}

func (c *Controller) discoveryVirtualService(
	instance *v1alpha1.MeshFederation, meshConfig *meshv1alpha1.MeshConfig) *config.Config {
	// VirtualService used to route inbound and outbound discovery requests.
	name := discoveryResourceName(instance)
	istiodService, _ := serviceAddressPort(meshConfig.DefaultConfig.DiscoveryAddress)
	if svcIndex := strings.LastIndex(istiodService, ".svc"); svcIndex >= 0 {
		istiodService = istiodService[:svcIndex] + ".svc." + c.env.DomainSuffix
	}
	ingressGatewayName := fmt.Sprintf("%s/%s-ingress", instance.Namespace, name)
	egressGatewayName := fmt.Sprintf("%s/%s-egress", instance.Namespace, name)
	discoveryService := c.discoveryHostname(instance)
	discoveryHost := instance.Spec.NetworkAddress
	discoveryPort := common.DefaultDiscoveryPort
	vs := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
		},
		Spec: &rawnetworking.VirtualService{
			Hosts: []string{
				"*",
			},
			Gateways: []string{
				ingressGatewayName,
				egressGatewayName,
			},
			ExportTo: []string{
				".",
			},
			Http: []*rawnetworking.HTTPRoute{
				{
					// Outbound discovery requests
					Name: fmt.Sprintf("%s-egress", name),
					Match: []*rawnetworking.HTTPMatchRequest{
						{
							Gateways: []string{
								egressGatewayName,
							},
							Headers: map[string]*rawnetworking.StringMatch{
								"discovery-address": {
									MatchType: &rawnetworking.StringMatch_Exact{
										Exact: discoveryHost,
									},
								},
							},
							Port: uint32(discoveryPort),
						},
					},
					Rewrite: &rawnetworking.HTTPRewrite{
						// Allows us to get the correct endpoint for outbound
						Authority: discoveryService,
					},
					Route: []*rawnetworking.HTTPRouteDestination{
						{
							Destination: &rawnetworking.Destination{
								Host: discoveryService,
								Port: &rawnetworking.PortSelector{
									Number: uint32(discoveryPort),
								},
								// to configure mtls appropriately
								Subset: name,
							},
						},
					},
				},
				{
					// inbound descovery /services/ requests
					Name: fmt.Sprintf("%s-ingress-services", name),
					Match: []*rawnetworking.HTTPMatchRequest{
						{
							Gateways: []string{
								ingressGatewayName,
							},
							Port: uint32(discoveryPort),
							Uri: &rawnetworking.StringMatch{
								MatchType: &rawnetworking.StringMatch_Exact{
									Exact: "/services/",
								},
							},
						},
					},
					Rewrite: &rawnetworking.HTTPRewrite{
						Authority: istiodService,
						Uri:       "/services/" + instance.Name,
					},
					Route: []*rawnetworking.HTTPRouteDestination{
						{
							Destination: &rawnetworking.Destination{
								Host: istiodService,
								Port: &rawnetworking.PortSelector{
									Number: uint32(discoveryPort),
								},
							},
						},
					},
				},
				{
					// inbound discovery /watch requests
					Name: fmt.Sprintf("%s-ingress-watch", name),
					Match: []*rawnetworking.HTTPMatchRequest{
						{
							Gateways: []string{
								ingressGatewayName,
							},
							Port: uint32(discoveryPort),
							Uri: &rawnetworking.StringMatch{
								MatchType: &rawnetworking.StringMatch_Exact{
									Exact: "/watch",
								},
							},
						},
					},
					Rewrite: &rawnetworking.HTTPRewrite{
						Authority: istiodService,
						Uri:       "/watch/" + instance.Name,
					},
					Route: []*rawnetworking.HTTPRouteDestination{
						{
							Destination: &rawnetworking.Destination{
								Host: istiodService,
								Port: &rawnetworking.PortSelector{
									Number: uint32(discoveryPort),
								},
							},
						},
					},
				},
			},
		},
	}
	return vs
}

func (c *Controller) discoveryDestinationRule(instance *v1alpha1.MeshFederation) *config.Config {
	// DestinationRule to configure mTLS for outbound discovery requests
	name := discoveryResourceName(instance)
	discoveryHost := c.discoveryHostname(instance)
	discoveryPort := common.DefaultDiscoveryPort
	dr := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
		},
		Spec: &rawnetworking.DestinationRule{
			// the "fake" discovery service
			Host: discoveryHost,
			ExportTo: []string{
				".",
			},
			Subsets: []*rawnetworking.Subset{
				{
					Name: name,
					TrafficPolicy: &rawnetworking.TrafficPolicy{
						PortLevelSettings: []*rawnetworking.TrafficPolicy_PortTrafficPolicy{
							{
								Port: &rawnetworking.PortSelector{
									Number: uint32(discoveryPort),
								},
								Tls: &rawnetworking.ClientTLSSettings{
									Mode: rawnetworking.ClientTLSSettings_ISTIO_MUTUAL,
									Sni:  discoveryHost,
								},
							},
						},
					},
				},
			},
		},
	}
	return dr
}
