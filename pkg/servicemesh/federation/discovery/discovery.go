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
	v1 "maistra.io/api/federation/v1"

	meshv1alpha1 "istio.io/api/mesh/v1alpha1"
	rawnetworking "istio.io/api/networking/v1alpha3"
	rawsecurity "istio.io/api/security/v1beta1"
	rawtype "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/ior"
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

const generationAnnotation = "reconciledGeneration"

var Schemas collection.Schemas

// only to facilitate processing errors from the store
type notFoundErrorChecker struct{}

func (e *notFoundErrorChecker) Error() string {
	return "item not found"
}

func (e *notFoundErrorChecker) Is(other error) bool {
	return other.Error() == "item not found" || strings.HasSuffix(other.Error(), "does not exist")
}

var memoryStoreErrNotFound = &notFoundErrorChecker{}

func (c *Controller) deleteDiscoveryResources(
	_ context.Context, instance *v1.ServiceMeshPeer,
) error {
	c.Logger.Infof("deleting discovery resources for Federation cluster %s", instance.Name)
	var allErrors []error
	rootName := discoveryResourceName(instance)
	egressName := discoveryEgressResourceName(instance)
	ingressName := discoveryIngressResourceName(instance)
	// XXX: the only errors possible on delete are "does not exist" and "unknown
	// type", so maybe we should just skip error checking?
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
		rootName, instance.Namespace, nil); err != nil && !errors.Is(memoryStoreErrNotFound, err) {
		c.Logger.Errorf("error deleting discovery Service %s for Federation cluster %s: %s",
			rootName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		ingressName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery ingress VirtualService %s for Federation cluster %s: %s",
			ingressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		egressName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery egress VirtualService %s for Federation cluster %s: %s",
			egressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		ingressName, instance.Namespace, nil); err != nil && !errors.Is(memoryStoreErrNotFound, err) {
		c.Logger.Errorf("error deleting discovery ingress Gateway %s for Federation cluster %s: %s",
			ingressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
		egressName, instance.Namespace, nil); err != nil && !errors.Is(memoryStoreErrNotFound, err) {
		c.Logger.Errorf("error deleting discovery egress Gateway %s for Federation cluster %s: %s",
			egressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
		egressName, instance.Namespace, nil); err != nil && !errors.Is(err, memoryStoreErrNotFound) {
		c.Logger.Errorf("error deleting discovery DestinationRule %s for Federation cluster %s: %s",
			egressName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	if err := c.Delete(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
		rootName, instance.Namespace, nil); err != nil && !errors.Is(memoryStoreErrNotFound, err) {
		c.Logger.Errorf("error deleting discovery AuthorizationPolicy %s for Federation cluster %s: %s",
			rootName, instance.Name, err)
		allErrors = append(allErrors, err)
	}
	return utilerrors.NewAggregate(allErrors)
}

func (c *Controller) createDiscoveryResources(
	_ context.Context, instance *v1.ServiceMeshPeer, meshConfig *meshv1alpha1.MeshConfig,
) (err error) {
	var s, ap, dr, ig, eg, evs, ivs *config.Config

	defer func() {
		if err != nil {
			c.Logger.Errorf("error creating discovery configuration for Federation cluster %s: %s", instance.Name, err)
			c.Logger.Infof("rolling back discovery Service for %s", instance.Name)
			if s != nil {
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
					s.Name, s.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery Service %s: %s", s.Name, newErr)
				}
			}
			c.Logger.Infof("rolling back discovery AuthorizationPolicy for Federation cluster %s", instance.Name)
			if ap != nil {
				if newErr := c.Delete(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
					ap.Name, ap.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery AuthorizationPolicy %s: %s", ap.Name, newErr)
				}
			}
			if dr != nil {
				c.Logger.Infof("rolling back discovery DestinationRule for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					dr.Name, dr.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery DestinationRule %s: %s", dr.Name, newErr)
				}
			}
			if ig != nil {
				c.Logger.Infof("rolling back discovery ingress Gateway for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
					ig.Name, ig.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery ingress Gateway %s: %s", ig.Name, newErr)
				}
			}
			if eg != nil {
				c.Logger.Infof("rolling back discovery egress Gateway for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
					eg.Name, eg.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery egress Gateway %s: %s", eg.Name, newErr)
				}
			}
			if evs != nil {
				c.Logger.Infof("rolling back discovery egress VirtualService for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
					evs.Name, evs.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery egress VirtualService %s: %s", evs.Name, newErr)
				}
			}
			if ivs != nil {
				c.Logger.Infof("rolling back discovery ingress VirtualService for Federation cluster %s", instance.Name)
				if newErr := c.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
					ivs.Name, ivs.Namespace, nil); newErr != nil && !errors.Is(newErr, memoryStoreErrNotFound) {
					c.Logger.Errorf("error deleting discovery ingress VirtualService %s: %s", ivs.Name, newErr)
				}
			}
		}
	}()

	s = c.discoveryService(instance)
	err = c.upsertConfig(*s)
	if err != nil {
		s = nil
	}

	ap = c.discoveryAuthorizationPolicy(instance)
	err = c.upsertConfig(*ap)
	if err != nil {
		ap = nil
	}

	dr = c.discoveryDestinationRule(instance)
	err = c.upsertConfig(*dr)
	if err != nil {
		dr = nil
	}

	ig = c.discoveryIngressGateway(instance)
	err = c.upsertConfig(*ig)
	if err != nil {
		ig = nil
	}

	eg = c.discoveryEgressGateway(instance)
	err = c.upsertConfig(*eg)
	if err != nil {
		eg = nil
	}

	evs = c.discoveryVirtualService(instance)
	err = c.upsertConfig(*evs)
	if err != nil {
		evs = nil
	}

	ivs = c.discoveryIngressVirtualService(instance, meshConfig)
	err = c.upsertConfig(*ivs)
	if err != nil {
		ivs = nil
	}
	return
}

func (c *Controller) upsertConfig(cfg config.Config) (err error) {
	existing := c.Get(cfg.GroupVersionKind, cfg.Name, cfg.Namespace)
	if existing == nil {
		_, err = c.Create(cfg)
		if err != nil {
			return err
		}
	} else if existing.Annotations[generationAnnotation] != cfg.Annotations[generationAnnotation] {
		_, err = c.Update(cfg)
		if err != nil {
			// don't trigger roll back
			c.Logger.Errorf("error updating resource %s: %s", cfg.Name, err)
			err = nil
		}
	}
	return err
}

func discoveryResourceName(instance *v1.ServiceMeshPeer) string {
	return fmt.Sprintf("federation-discovery-%s", instance.Name)
}

func discoveryEgressResourceName(instance *v1.ServiceMeshPeer) string {
	return fmt.Sprintf("%s-egress", discoveryResourceName(instance))
}

func discoveryIngressResourceName(instance *v1.ServiceMeshPeer) string {
	return fmt.Sprintf("%s-ingress", discoveryResourceName(instance))
}

func federationIngressLabels(instance *v1.ServiceMeshPeer) map[string]string {
	return map[string]string{
		"service.istio.io/canonical-name": instance.Spec.Gateways.Ingress.Name,
	}
}

func federationEgressLabels(instance *v1.ServiceMeshPeer) map[string]string {
	return map[string]string{
		"service.istio.io/canonical-name": instance.Spec.Gateways.Egress.Name,
	}
}

func istiodServiceAddress(addr string) string {
	portIndex := strings.Index(addr, ":")
	if portIndex >= 0 {
		return addr[:portIndex]
	}
	return addr
}

func (c *Controller) discoveryService(instance *v1.ServiceMeshPeer) *config.Config {
	// This is used for routing out of the egress gateway, primarily to configure mtls for discovery and
	// to give the gateway an endpoint to route to (i.e. it creates a cluster with an endpoint in the gateway).
	// This should turn into a service entry for the other mesh's network.
	name := discoveryResourceName(instance)
	discoveryPort := instance.Spec.Remote.DiscoveryPort
	if discoveryPort == 0 {
		discoveryPort = common.DefaultDiscoveryPort
	}
	serviceSpec := &rawnetworking.ServiceEntry{
		Hosts: []string{
			common.DiscoveryServiceHostname(instance),
		},
		Location: rawnetworking.ServiceEntry_MESH_EXTERNAL,
		Ports: []*rawnetworking.Port{
			{
				Name:       "https-discovery",
				Number:     uint32(discoveryPort),
				Protocol:   "HTTPS",
				TargetPort: uint32(discoveryPort),
			},
		},
		Resolution: rawnetworking.ServiceEntry_DNS,
		ExportTo: []string{
			".",
		},
	}
	for _, address := range instance.Spec.Remote.Addresses {
		serviceSpec.Endpoints = append(serviceSpec.Endpoints, &rawnetworking.WorkloadEntry{
			Address: address,
			Ports: map[string]uint32{
				"https-discovery": uint32(discoveryPort),
			},
		})
	}
	service := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Labels: map[string]string{
				"topology.istio.io/network": fmt.Sprintf("network-%s", instance.Name),
			},
			Annotations: map[string]string{
				generationAnnotation: fmt.Sprintf("%d", instance.Generation),
			},
		},
		Spec: serviceSpec,
	}
	return service
}

func (c *Controller) discoveryIngressGateway(instance *v1.ServiceMeshPeer) *config.Config {
	// Gateway definition for handling inbound discovery requests
	name := discoveryIngressResourceName(instance)
	discoveryPort := common.DefaultDiscoveryPort
	gateway := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Annotations: map[string]string{
				generationAnnotation:            fmt.Sprintf("%d", instance.Generation),
				ior.ShouldManageRouteAnnotation: "false",
			},
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

func (c *Controller) discoveryEgressGateway(instance *v1.ServiceMeshPeer) *config.Config {
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
			Annotations: map[string]string{
				generationAnnotation:            fmt.Sprintf("%d", instance.Generation),
				ior.ShouldManageRouteAnnotation: "false",
			},
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

func (c *Controller) discoveryAuthorizationPolicy(instance *v1.ServiceMeshPeer) *config.Config {
	// AuthorizationPolicy used to restrict inbound discovery requests to known clients.
	name := discoveryResourceName(instance)
	discoveryPort := common.DefaultDiscoveryPort
	ap := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Annotations: map[string]string{
				generationAnnotation: fmt.Sprintf("%d", instance.Generation),
			},
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

func (c *Controller) discoveryVirtualService(instance *v1.ServiceMeshPeer) *config.Config {
	// VirtualService used to route inbound and outbound discovery requests.
	name := discoveryEgressResourceName(instance)
	egressGatewayName := fmt.Sprintf("%s/%s", instance.Namespace, name)
	egressGatewayService := fmt.Sprintf("%s.%s.svc.%s", instance.Spec.Gateways.Egress.Name, instance.Namespace, c.env.DomainSuffix)
	discoveryService := common.DiscoveryServiceHostname(instance)
	discoveryPort := common.DefaultDiscoveryPort
	remoteDiscoveryPort := discoveryPort
	if instance.Spec.Remote.DiscoveryPort > 0 {
		remoteDiscoveryPort = int(instance.Spec.Remote.DiscoveryPort)
	}
	vs := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Annotations: map[string]string{
				generationAnnotation: fmt.Sprintf("%d", instance.Generation),
			},
		},
		Spec: &rawnetworking.VirtualService{
			Hosts: []string{
				egressGatewayService,
			},
			Gateways: []string{
				egressGatewayName,
			},
			ExportTo: []string{
				".",
			},
			Http: []*rawnetworking.HTTPRoute{
				{
					// Outbound discovery requests
					Name: name,
					Match: []*rawnetworking.HTTPMatchRequest{
						{
							Port: uint32(discoveryPort),
							Headers: map[string]*rawnetworking.StringMatch{
								"discovery-service": {
									MatchType: &rawnetworking.StringMatch_Exact{
										Exact: discoveryService,
									},
								},
								"remote": {
									MatchType: &rawnetworking.StringMatch_Exact{
										Exact: fmt.Sprint(common.RemoteChecksum(instance.Spec.Remote)),
									},
								},
							},
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
									Number: uint32(remoteDiscoveryPort),
								},
								// to configure mtls appropriately
								Subset: name,
							},
						},
					},
				},
			},
		},
	}
	return vs
}

func (c *Controller) discoveryIngressVirtualService(
	instance *v1.ServiceMeshPeer,
	meshConfig *meshv1alpha1.MeshConfig,
) *config.Config {
	// VirtualService used to route inbound and outbound discovery requests.
	name := discoveryIngressResourceName(instance)
	istiodService := istiodServiceAddress(meshConfig.DefaultConfig.DiscoveryAddress)
	if svcIndex := strings.LastIndex(istiodService, ".svc"); svcIndex >= 0 {
		istiodService = istiodService[:svcIndex] + ".svc." + c.env.DomainSuffix
	}
	ingressGatewayName := fmt.Sprintf("%s/%s", instance.Namespace, name)
	discoveryPort := common.DefaultDiscoveryPort
	vs := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Annotations: map[string]string{
				generationAnnotation: fmt.Sprintf("%d", instance.Generation),
			},
		},
		Spec: &rawnetworking.VirtualService{
			Hosts: []string{
				"*",
			},
			Gateways: []string{
				ingressGatewayName,
			},
			ExportTo: []string{
				".",
			},
			Http: []*rawnetworking.HTTPRoute{
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
									Exact: "/v1/services/",
								},
							},
						},
					},
					Rewrite: &rawnetworking.HTTPRewrite{
						Authority: istiodService,
						Uri:       "/v1/services/" + instance.Name,
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
									Exact: "/v1/watch",
								},
							},
						},
					},
					Rewrite: &rawnetworking.HTTPRewrite{
						Authority: istiodService,
						Uri:       "/v1/watch/" + instance.Name,
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

func (c *Controller) discoveryDestinationRule(instance *v1.ServiceMeshPeer) *config.Config {
	// DestinationRule to configure mTLS for outbound discovery requests
	name := discoveryEgressResourceName(instance)
	discoveryHost := common.DiscoveryServiceHostname(instance)
	discoveryPort := instance.Spec.Remote.DiscoveryPort
	if discoveryPort == 0 {
		discoveryPort = common.DefaultDiscoveryPort
	}
	dr := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        instance.Namespace,
			Annotations: map[string]string{
				generationAnnotation: fmt.Sprintf("%d", instance.Generation),
			},
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
