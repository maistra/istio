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

package federation

import (
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	rawnetworking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/servicemesh/federation/common"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

// ensure our config gets ignored if the user wants to change routing for
// exported services
var (
	armageddonTime     = time.Unix(1<<62-1, 0)
	prefixResourceName = "fed-imp"
)

func createResourceName(mesh cluster.ID, source federationmodel.ServiceKey) string {
	return common.FormatResourceName(prefixResourceName, mesh.String(), source.Namespace, source.Name)
}

func (c *Controller) deleteRoutingResources(remote federationmodel.ServiceKey) error {
	resourceName := createResourceName(c.clusterID, remote)
	// Delete() is always successful
	_ = c.configStore.Delete(gvk.Gateway, resourceName, c.namespace, nil)
	return c.configStore.Delete(gvk.VirtualService, resourceName, c.namespace, nil)
}

func (c *Controller) createRoutingResources(remote, local federationmodel.ServiceKey) error {
	resourceName := createResourceName(c.clusterID, remote)
	gateway := c.gatewayForImport(remote, local)
	if rawGateway := c.configStore.Get(gvk.Gateway, gateway.Name, c.namespace); rawGateway == nil {
		if gateway != nil {
			if _, err := c.configStore.Create(*gateway); err != nil {
				return errors.Wrapf(err, "error creating Gateway resource")
			}
		}
	} else if gateway == nil {
		_ = c.configStore.Delete(gvk.Gateway, resourceName, c.namespace, nil)
	} else {
		existingGateway := rawGateway.Spec.(*rawnetworking.Gateway)
		if len(existingGateway.Servers) > 0 && len(existingGateway.Servers[0].Hosts) > 0 && existingGateway.Servers[0].Hosts[0] != local.Hostname {
			// overwrite whatever's there
			c.logger.Warnf("Gateway resource %s already exists for exported service (%s => %s).  It will be overwritten.",
				gateway.Name, remote.Hostname, local.Hostname)
			if _, err := c.configStore.Update(*gateway); err != nil {
				return errors.Wrapf(err, "error updating Gateway resource")
			}
		}
	}
	vs := c.virtualServiceForImport(remote, local)
	if rawVS := c.configStore.Get(gvk.VirtualService, vs.Name, c.namespace); rawVS == nil {
		if _, err := c.configStore.Create(*vs); err != nil {
			return errors.Wrapf(err, "error creating VirtualService resource")
		}
	} else {
		existingVS := rawVS.Spec.(*rawnetworking.VirtualService)
		if (len(existingVS.Hosts) > 0 && !strings.HasSuffix(existingVS.Hosts[0], "/"+local.Hostname)) ||
			(len(existingVS.ExportTo) > 0 && existingVS.ExportTo[0] != vs.Spec.(*rawnetworking.VirtualService).ExportTo[0]) {
			// overwrite whatever's there
			c.logger.Warnf("VirtualService resource %s already exists for exported service (%s => %s).  It will be overwritten.",
				vs.Name, remote.Hostname, local.Hostname)
			if _, err := c.configStore.Update(*vs); err != nil {
				return errors.Wrapf(err, "error updating VirtualService resource")
			}
		}
	}
	return nil
}

func (c *Controller) gatewayForImport(remote, local federationmodel.ServiceKey) *config.Config {
	resourceName := createResourceName(c.clusterID, remote)
	mode := rawnetworking.ServerTLSSettings_ISTIO_MUTUAL
	if c.useDirectCalls {
		// no gateway when directly accessing services
		return nil
	}
	gateway := &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Gateway,
			Name:             resourceName,
			Namespace:        c.namespace,
		},
		Spec: &rawnetworking.Gateway{
			Selector: map[string]string{
				"service.istio.io/canonical-name": c.egressName,
			},
			Servers: []*rawnetworking.Server{
				{
					Name: resourceName,
					Hosts: []string{
						local.Hostname,
						"*." + local.Hostname,
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

func (c *Controller) virtualServiceForImport(remote, local federationmodel.ServiceKey) *config.Config {
	// VirtualService used to route inbound requests to the service.
	name := createResourceName(c.clusterID, remote)
	egressGatewayName := fmt.Sprintf("%s/%s", c.namespace, name)
	exportTo := "."
	creationTime := time.Now()
	if c.useDirectCalls {
		exportTo = "*"
		creationTime = armageddonTime
	}
	vs := &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  gvk.VirtualService,
			Name:              name,
			Namespace:         c.namespace,
			CreationTimestamp: creationTime,
		},
		Spec: &rawnetworking.VirtualService{
			Hosts: []string{
				local.Hostname,
				"*." + local.Hostname,
			},
			Gateways: []string{
				egressGatewayName,
			},
			ExportTo: []string{
				exportTo,
			},
			Tcp: []*rawnetworking.TCPRoute{
				{
					Match: []*rawnetworking.L4MatchAttributes{
						{
							Gateways: []string{
								egressGatewayName,
							},
						},
					},
					Route: []*rawnetworking.RouteDestination{
						{
							Destination: &rawnetworking.Destination{
								Host: remote.Hostname,
							},
						},
					},
				},
			},
		},
	}
	return vs
}
