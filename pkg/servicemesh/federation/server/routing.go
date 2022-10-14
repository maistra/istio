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

package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	rawnetworking "istio.io/api/networking/v1alpha3"
	rawsecurity "istio.io/api/security/v1beta1"
	rawtype "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/ior"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/servicemesh/federation/common"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

func init() {
	schemasBuilder := collection.NewSchemasBuilder()
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Virtualservices)
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Gateways)
	schemasBuilder.MustAdd(collections.IstioSecurityV1Beta1Authorizationpolicies)
	Schemas = schemasBuilder.Build()
}

var (
	Schemas collection.Schemas
	// ensure our config gets ignored if the user wants to change routing for
	// exported services
	armageddonTime = time.Unix(1<<62-1, 0)
)

// hashResourceName applies a sha256 on the host and truncate it to the first n bytes
func hashResourceName(resourceName string, n int) string {
	hash := sha256.Sum256([]byte(resourceName))
	return hex.EncodeToString(hash[:n])
}

func shortenName(name string) string {
	return strings.Replace(name, "-", "", -1)
}

func createResourceName(mesh string, source federationmodel.ServiceKey) string {
	resourceName := fmt.Sprintf("federation-exports-%s-%s-%s", mesh, source.Name, source.Namespace)
	if !labels.IsDNS1123Label(resourceName) {
		resourceName = fmt.Sprintf("federation-exports-%s-%s-%s", shortenName(mesh), shortenName(source.Name), shortenName(source.Namespace))
		if !labels.IsDNS1123Label(resourceName) {
			resourceName = fmt.Sprintf("%s-%s", resourceName[:52], hashResourceName(resourceName, 5))
		}
	}
	return resourceName
}

func (s *meshServer) deleteExportResources(source federationmodel.ServiceKey, target *federationmodel.ServiceMessage) error {
	resourceName := createResourceName(s.mesh.Name, source)
	// Delete() is always successful
	_ = s.configStore.Delete(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), resourceName, s.mesh.Namespace, nil)
	_ = s.configStore.Delete(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(), resourceName, s.mesh.Namespace, nil)
	return s.removeServiceFromAuthorizationPolicy(target)
}

func (s *meshServer) removeServiceFromAuthorizationPolicy(target *federationmodel.ServiceMessage) error {
	// AuthorizationPolicy used to restrict inbound requests to known clients accessing exported services.
	// We use a DENY policy to block any traffic coming in that's not from a known client or destined for an exported service
	name := fmt.Sprintf("federation-exports-%s", s.mesh.Name)
	rawAP := s.configStore.Get(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(), name, s.mesh.Namespace)
	if rawAP == nil {
		// nothing to remove
		s.logger.Warnf("could not find AuthorizationPolicy %s/%s associated with federation export", s.mesh.Namespace, name)
		return nil
	}
	apSpec := rawAP.Spec.(*rawsecurity.AuthorizationPolicy)
	if len(apSpec.Rules) != 1 || len(apSpec.Rules[0].To) != 1 {
		return fmt.Errorf("invalid AuthorizationPolicy for service export")
	}
	for index, host := range apSpec.Rules[0].To[0].Operation.NotHosts {
		if host == target.Hostname {
			apSpec.Rules[0].To[0].Operation.NotHosts = append(apSpec.Rules[0].To[0].Operation.NotHosts[:index],
				apSpec.Rules[0].To[0].Operation.NotHosts[index+1:]...)
			if _, err := s.configStore.Update(*rawAP); err != nil {
				return err
			}
			return nil
		}
	}
	s.logger.Warnf("AuthorizationPolicy %s/%s did not have rule for exported service %s", s.mesh.Namespace, name, target.Hostname)
	return nil
}

func (s *meshServer) createExportResources(source federationmodel.ServiceKey, target *federationmodel.ServiceMessage) error {
	if err := s.createOrUpdateAuthorizationPolicy(target); err != nil {
		return errors.Wrapf(err, "error updating AuthorinzationPolicy resource")
	}
	gateway := s.gatewayForExport(source, target)
	if rawGateway := s.configStore.Get(
		collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), gateway.Name, s.mesh.Namespace); rawGateway == nil {
		if _, err := s.configStore.Create(*gateway); err != nil {
			return errors.Wrapf(err, "error creating Gateway resource")
		}
	} else {
		// overwrite whatever's there
		s.logger.Warnf("Gateway resource %s already exists for exported service (%s => %s).  It will be overwritten.",
			gateway.Name, source.Hostname, target.Hostname)
		if _, err := s.configStore.Update(*gateway); err != nil {
			return errors.Wrapf(err, "error updating Gateway resource")
		}
	}
	vs := s.virtualServiceForExport(source, target)
	if rawVS := s.configStore.Get(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(), vs.Name, s.mesh.Namespace); rawVS == nil {
		if _, err := s.configStore.Create(*vs); err != nil {
			return errors.Wrapf(err, "error creating VirtualService resource")
		}
	} else {
		// overwrite whatever's there
		s.logger.Warnf("VirtualService resource %s already exists for exported service (%s => %s).  It will be overwritten.",
			vs.Name, source.Hostname, target.Hostname)
		if _, err := s.configStore.Update(*vs); err != nil {
			return errors.Wrapf(err, "error updating VirtualService resource")
		}
	}
	return nil
}

func (s *meshServer) createOrUpdateAuthorizationPolicy(target *federationmodel.ServiceMessage) error {
	// AuthorizationPolicy used to restrict inbound requests to known clients accessing exported services.
	// We use a DENY policy to block any traffic coming in that's not from a known client or destined for an exported service
	name := fmt.Sprintf("federation-exports-%s", s.mesh.Name)
	rawAP := s.configStore.Get(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(), name, s.mesh.Namespace)
	if rawAP == nil {
		if s.mesh.Spec.Security.ClientID == "" {
			s.logger.Errorf("no ClientID specified for MeshFederation %s/%s: AuthorizationPolicy for exported services will not be created",
				s.mesh.Namespace, s.mesh.Name)
			return nil
		}
		ap := &config.Config{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
				Name:             name,
				Namespace:        s.mesh.Namespace,
			},
			Spec: &rawsecurity.AuthorizationPolicy{
				Selector: &rawtype.WorkloadSelector{
					MatchLabels: map[string]string{
						"service.istio.io/canonical-name": s.mesh.Spec.Gateways.Ingress.Name,
					},
				},
				Action: rawsecurity.AuthorizationPolicy_DENY,
				Rules: []*rawsecurity.Rule{
					{
						From: []*rawsecurity.Rule_From{
							{
								Source: &rawsecurity.Source{
									NotPrincipals: []string{
										s.mesh.Spec.Security.ClientID,
									},
								},
							},
						},
						To: []*rawsecurity.Rule_To{
							{
								Operation: &rawsecurity.Operation{
									NotHosts: []string{
										target.Hostname,
									},
									Ports: []string{
										strconv.FormatInt(common.DefaultFederationPort, 10),
									},
								},
							},
						},
					},
				},
			},
		}
		if _, err := s.configStore.Create(*ap); err != nil {
			return err
		}
		return nil
	}
	apSpec := rawAP.Spec.(*rawsecurity.AuthorizationPolicy)
	if len(apSpec.Rules) != 1 || len(apSpec.Rules[0].To) != 1 {
		return fmt.Errorf("invalid AuthorizationPolicy for service export")
	}
	for _, host := range apSpec.Rules[0].To[0].Operation.NotHosts {
		if host == target.Hostname {
			// no update required
			return nil
		}
	}
	// add the host to the list of available targets
	apSpec.Rules[0].To[0].Operation.NotHosts = append(apSpec.Rules[0].To[0].Operation.NotHosts, target.Hostname)
	if _, err := s.configStore.Update(*rawAP); err != nil {
		return err
	}
	return nil
}

func (s *meshServer) gatewayForExport(source federationmodel.ServiceKey, target *federationmodel.ServiceMessage) *config.Config {
	resourceName := createResourceName(s.mesh.Name, source)
	mode := rawnetworking.ServerTLSSettings_ISTIO_MUTUAL
	if s.mesh.Spec.Security.AllowDirectInbound {
		// XXX: this will not work, as the exported services will have a different domain suffix
		// for example, svc.mesh2.local as opposed to svc.cluster.local
		mode = rawnetworking.ServerTLSSettings_AUTO_PASSTHROUGH
	}
	gateway := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(),
			Name:             resourceName,
			Namespace:        s.mesh.Namespace,
			Annotations:      map[string]string{ior.ShouldManageRouteAnnotation: "false"},
		},
		Spec: &rawnetworking.Gateway{
			Selector: map[string]string{
				"service.istio.io/canonical-name": s.mesh.Spec.Gateways.Ingress.Name,
			},
			Servers: []*rawnetworking.Server{
				{
					Name: resourceName,
					Hosts: []string{
						target.Hostname,
						fmt.Sprintf("*.%s", target.Hostname),
					},
					Port: &rawnetworking.Port{
						Name:     "tls-federation",
						Number:   uint32(common.DefaultFederationPort),
						Protocol: "TLS",
					},
					Tls: &rawnetworking.ServerTLSSettings{
						Mode: mode,
					},
				},
			},
		},
	}
	return gateway
}

func (s *meshServer) virtualServiceForExport(source federationmodel.ServiceKey, target *federationmodel.ServiceMessage) *config.Config {
	// VirtualService used to route inbound requests to the service.
	name := createResourceName(s.mesh.Name, source)
	ingressGatewayName := fmt.Sprintf("%s/%s", s.mesh.Namespace, name)
	vs := &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:              name,
			Namespace:         s.mesh.Namespace,
			CreationTimestamp: armageddonTime,
		},
		Spec: &rawnetworking.VirtualService{
			Hosts: []string{
				target.Hostname,
			},
			Gateways: []string{
				ingressGatewayName,
			},
			ExportTo: []string{
				".",
			},
			Tcp: []*rawnetworking.TCPRoute{
				{
					Match: []*rawnetworking.L4MatchAttributes{
						{
							Gateways: []string{
								ingressGatewayName,
							},
						},
					},
					Route: []*rawnetworking.RouteDestination{
						{
							Destination: &rawnetworking.Destination{
								Host: source.Hostname,
							},
						},
					},
				},
			},
		},
	}
	return vs
}
