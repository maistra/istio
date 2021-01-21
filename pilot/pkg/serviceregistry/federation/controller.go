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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/servicemesh/federation"
	"istio.io/pkg/log"
)

var _ model.ServiceDiscovery = &Controller{}
var _ model.Controller = &Controller{}
var _ serviceregistry.Instance = &Controller{}

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	meshHolder        mesh.Holder
	discoveryEndpoint string
	networkName       string
	clusterID         string
	resyncPeriod      time.Duration

	xdsUpdater model.XDSUpdater

	storeLock     sync.RWMutex
	serviceStore  []*model.Service
	instanceStore map[host.Name][]*model.ServiceInstance
	gatewayStore  []*model.Gateway
}

type Options struct {
	MeshHolder        mesh.Holder
	DiscoveryEndpoint string
	NetworkName       string
	ClusterID         string
	XDSUpdater        model.XDSUpdater
	ResyncPeriod      time.Duration
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	return &Controller{
		meshHolder:        opt.MeshHolder,
		discoveryEndpoint: opt.DiscoveryEndpoint,
		networkName:       opt.NetworkName,
		clusterID:         opt.ClusterID,
		xdsUpdater:        opt.XDSUpdater,
		resyncPeriod:      opt.ResyncPeriod,
	}
}

func (c *Controller) Cluster() string {
	return c.clusterID
}

func (c *Controller) Provider() serviceregistry.ProviderID {
	return serviceregistry.Federation
}

func (c *Controller) pollServices() *federation.ServiceListMessage {
	url := c.discoveryEndpoint + "/services/"
	resp, err := http.Get(url)
	if err != nil {
		log.Errorf("Failed to GET URL: '%s': %s", url, err)
		return nil
	}

	respBytes := []byte{}
	_, err = resp.Body.Read(respBytes)
	if err != nil {
		log.Errorf("Failed to read response body from URL '%s': %s", url, err)
		return nil
	}

	var serviceList federation.ServiceListMessage
	err = json.NewDecoder(resp.Body).Decode(&serviceList)
	if err != nil {
		log.Errorf("Failed to unmarshal response bytes: %s", err)
		return nil
	}
	return &serviceList
}

func getNameAndNamespaceFromHostname(hostname string) (string, string) {
	fragments := strings.Split(hostname, ".")
	if len(fragments) != 5 {
		log.Warnf("Can't parse hostname: %s", hostname)
		return hostname, ""
	}
	return fragments[0], fragments[1]
}

func (c *Controller) convertServices(serviceList *federation.ServiceListMessage) {
	services := []*model.Service{}
	instances := make(map[host.Name][]*model.ServiceInstance)
	for _, s := range serviceList.Services {
		name, namespace := getNameAndNamespaceFromHostname(s.Name)
		svc := &model.Service{
			Attributes: model.ServiceAttributes{
				Name:      name,
				Namespace: namespace,
			},
			Resolution:      model.ClientSideLB,
			Address:         constants.UnspecifiedIP,
			Hostname:        host.Name(s.Name),
			Ports:           model.PortList{},
			ServiceAccounts: []string{},
		}
		for _, port := range s.ServicePorts {
			svc.Ports = append(svc.Ports, &model.Port{
				Name:     port.Name,
				Port:     port.Port,
				Protocol: protocol.Instance(port.Protocol),
			})
		}

		services = append(services, svc)
		for _, port := range svc.Ports {
			for _, networkGateway := range serviceList.NetworkGatewayEndpoints {
				if list, ok := instances[svc.Hostname]; !ok || list == nil {
					instances[svc.Hostname] = []*model.ServiceInstance{}
				}
				instances[svc.Hostname] = append(instances[svc.Hostname], &model.ServiceInstance{
					Service:     svc,
					ServicePort: port,
					Endpoint: &model.IstioEndpoint{
						Address:      networkGateway.Hostname,
						EndpointPort: uint32(port.Port),
						Network:      c.networkName,
						Locality: model.Locality{
							ClusterID: c.clusterID,
						},
						ServicePortName: port.Name,
					},
				})
			}
		}
	}
	gateways := []*model.Gateway{}
	for _, gateway := range serviceList.NetworkGatewayEndpoints {
		gateways = append(gateways, &model.Gateway{
			Addr: gateway.Hostname,
			Port: uint32(gateway.Port),
		})
	}
	autoAllocateIPs(services)
	c.storeLock.Lock()
	c.serviceStore = services
	c.instanceStore = instances
	c.gatewayStore = gateways
	c.storeLock.Unlock()
}

func autoAllocateIPs(services []*model.Service) []*model.Service {
	// i is everything from 240.240.0.(j) to 240.240.255.(j)
	// j is everything from 240.240.(i).1 to 240.240.(i).254
	// we can capture this in one integer variable.
	// given X, we can compute i by X/255, and j is X%255
	// To avoid allocating 240.240.(i).255, if X % 255 is 0, increment X.
	// For example, when X=510, the resulting IP would be 240.240.2.0 (invalid)
	// So we bump X to 511, so that the resulting IP is 240.240.2.1
	maxIPs := 255 * 255 // are we going to exceeed this limit by processing 64K services?
	x := 0
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.Address == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() &&
			svc.Resolution != model.Passthrough {
			x++
			if x%255 == 0 {
				x++
			}
			if x >= maxIPs {
				log.Errorf("out of IPs to allocate for service entries")
				return services
			}
			thirdOctet := x / 255
			fourthOctet := x % 255
			svc.AutoAllocatedAddress = fmt.Sprintf("240.241.%d.%d", thirdOctet, fourthOctet)
		}
	}
	return services
}

// Services lists services
func (c *Controller) Services() ([]*model.Service, error) {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	return c.serviceStore, nil
}

// GetService retrieves a service by hostname if exists
func (c *Controller) GetService(hostname host.Name) (*model.Service, error) {
	var errs error
	var out *model.Service
	return out, errs
}

// NetworkGateways merges the service-based cross-network gateways from each registry.
func (c *Controller) NetworkGateways() map[string][]*model.Gateway {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	gws := map[string][]*model.Gateway{}
	gws[c.networkName] = c.gatewayStore
	return gws
}

// InstancesByPort retrieves instances for a service on a given port that match
// any of the supplied labels. All instances match an empty label list.
func (c *Controller) InstancesByPort(svc *model.Service, port int, labels labels.Collection) []*model.ServiceInstance {
	instances := []*model.ServiceInstance{}
	c.storeLock.RLock()
	for _, instanceList := range c.instanceStore {
		for _, instance := range instanceList {
			if instance.Service == svc && instance.ServicePort.Port == port {
				instances = append(instances, instance.DeepCopy())
			}
		}
	}
	c.storeLock.RUnlock()
	return instances
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) []*model.ServiceInstance {
	return make([]*model.ServiceInstance, 0)
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Collection {
	return labels.Collection{}
}

// Run starts all the controllers
func (c *Controller) Run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			log.Info("Federation Controller terminated")
			break
		default:
		}
		svcList := c.pollServices()
		if svcList != nil {
			c.convertServices(svcList)
			c.storeLock.RLock()
			for hostname, instanceList := range c.instanceStore {
				endpoints := []*model.IstioEndpoint{}
				for _, instance := range instanceList {
					endpoints = append(endpoints, instance.Endpoint)
				}
				c.xdsUpdater.SvcUpdate(c.clusterID, string(hostname), instanceList[0].Service.Attributes.Namespace, model.EventUpdate)
				c.xdsUpdater.EDSUpdate(c.clusterID, string(hostname), instanceList[0].Service.Attributes.Namespace, endpoints)
			}
			c.storeLock.RUnlock()
		}
		<-time.After(c.resyncPeriod)
	}
}

// HasSynced returns true when all registries have synced
func (c *Controller) HasSynced() bool {
	return true
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	// TODO
	return nil
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) error {
	// TODO
	return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation.
// The returned list contains all SPIFFE based identities that backs the service.
// This method also expand the results from different registries based on the mesh config trust domain aliases.
// To retain such trust domain expansion behavior, the xDS server implementation should wrap any (even if single)
// service registry by this aggreated one.
// For example,
// - { "spiffe://cluster.local/bar@iam.gserviceaccount.com"}; when annotation is used on corresponding workloads.
// - { "spiffe://cluster.local/ns/default/sa/foo" }; normal kubernetes cases
// - { "spiffe://cluster.local/ns/default/sa/foo", "spiffe://trust-domain-alias/ns/default/sa/foo" };
//   if the trust domain alias is configured.
func (c *Controller) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	return []string{}
}
