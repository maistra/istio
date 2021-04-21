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
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/servicemesh/federation/common"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/pkg/log"
)

var _ model.ServiceDiscovery = &Controller{}
var _ model.Controller = &Controller{}
var _ serviceregistry.Instance = &Controller{}

var logger = log.RegisterScope("federation-registry", "federation-registry", 0)

const (
	federationPort = common.FederationPort
)

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	meshHolder     mesh.Holder
	networkAddress string
	discoveryURL   string
	networkName    string
	namespace      string
	clusterID      string
	resyncPeriod   time.Duration
	backoffPolicy  *backoff.ExponentialBackOff

	xdsUpdater model.XDSUpdater

	storeLock     sync.RWMutex
	serviceStore  []*model.Service
	instanceStore map[host.Name][]*model.ServiceInstance
	gatewayStore  []*model.Gateway

	lastMessage *federationmodel.ServiceListMessage
	stopped     int32
}

type Options struct {
	MeshHolder     mesh.Holder
	NetworkAddress string
	EgressService  string
	NetworkName    string
	ClusterID      string
	Namespace      string
	XDSUpdater     model.XDSUpdater
	ResyncPeriod   time.Duration
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	backoffPolicy := backoff.NewExponentialBackOff()
	backoffPolicy.MaxElapsedTime = 0
	return &Controller{
		meshHolder:     opt.MeshHolder,
		discoveryURL:   fmt.Sprintf("%s://%s:%d", common.DiscoveryScheme, opt.EgressService, common.DefaultDiscoveryPort),
		networkAddress: opt.NetworkAddress,
		networkName:    opt.NetworkName,
		namespace:      opt.Namespace,
		clusterID:      opt.ClusterID,
		xdsUpdater:     opt.XDSUpdater,
		resyncPeriod:   opt.ResyncPeriod,
		backoffPolicy:  backoffPolicy,
	}
}

func (c *Controller) Cluster() string {
	return c.clusterID
}

func (c *Controller) Provider() serviceregistry.ProviderID {
	return serviceregistry.Federation
}

func (c *Controller) NetworkAddress() string {
	return c.networkAddress
}

func (c *Controller) pollServices() *federationmodel.ServiceListMessage {
	url := c.discoveryURL + "/services/"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		logger.Errorf("Failed to create request: '%s': %s", url, err)
		return nil
	}
	req.Header.Add("discovery-address", c.networkAddress)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf("Failed to GET URL: '%s': %s", url, err)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		logger.Errorf("status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
		return nil
	}

	respBytes := []byte{}
	_, err = resp.Body.Read(respBytes)
	if err != nil {
		logger.Errorf("Failed to read response body from URL '%s': %s", url, err)
		return nil
	}

	var serviceList federationmodel.ServiceListMessage
	err = json.NewDecoder(resp.Body).Decode(&serviceList)
	if err != nil {
		logger.Errorf("Failed to unmarshal response bytes: %s", err)
		return nil
	}
	return &serviceList
}

func (c *Controller) convertService(s *federationmodel.ServiceMessage, networkGateways []*model.Gateway) (*model.Service, []*model.ServiceInstance) {
	instances := []*model.ServiceInstance{}
	serviceName := fmt.Sprintf("%s.%s", s.Name, s.Namespace)
	svc := &model.Service{
		Attributes: model.ServiceAttributes{
			ServiceRegistry: string(serviceregistry.Federation),
			Name:            serviceName, // simple name in the form name.namespace of the exported service
			Namespace:       c.namespace, // the federation namespace
			UID:             fmt.Sprintf("istio://%s/services/%s", c.namespace, serviceName),
		},
		Resolution:      model.ClientSideLB,
		Address:         constants.UnspecifiedIP,
		Hostname:        host.Name(s.Hostname),
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

	for _, port := range svc.Ports {
		for _, networkGateway := range networkGateways {
			instances = append(instances, &model.ServiceInstance{
				Service:     svc,
				ServicePort: port,
				Endpoint: &model.IstioEndpoint{
					// XXX: in any mode, this should be set to the exported service host
					// this assumes that istio will route this to the ip associated with
					// the network gateway
					// it looks like istio will swap this address out with the address of the network gateway
					Address: networkGateway.Addr,
					// XXX: as above, this should be set to the actual service port
					EndpointPort: networkGateway.Port,
					Network:      c.networkName,
					Locality: model.Locality{
						ClusterID: c.clusterID,
					},
					ServicePortName: port.Name,
				},
			})
		}
	}
	return svc, instances
}

func (c *Controller) gatewayForNetworkAddress() []*model.Gateway {
	var gateways []*model.Gateway
	gwIP := net.ParseIP(c.networkAddress)
	if gwIP == nil {
		addrs, err := net.LookupHost(c.networkAddress)
		if err != nil {
			logger.Errorf("error resolving host for federation network %s: %v", c.networkAddress, err)
		} else {
			logger.Debugf("adding gateway %s endpoints for cluster %s", c.networkAddress, c.clusterID)
			for _, ip := range addrs {
				logger.Debugf("adding gateway %s endpoint %s for cluster %s", c.networkAddress, ip, c.clusterID)
				gateways = append(gateways, &model.Gateway{
					Addr: ip,
					Port: federationPort,
				})
			}
		}
	} else {
		logger.Debugf("adding gateway %s for cluster %s", gwIP.String(), c.clusterID)
		gateways = append(gateways, &model.Gateway{
			Addr: gwIP.String(),
		})
	}
	return gateways
}

func (c *Controller) convertServices(serviceList *federationmodel.ServiceListMessage) {
	services := []*model.Service{}
	instances := make(map[host.Name][]*model.ServiceInstance)
	gateways := c.gatewayForNetworkAddress()
	for _, gateway := range serviceList.NetworkGatewayEndpoints {
		gateways = append(gateways, &model.Gateway{
			Addr: gateway.Hostname,
			Port: uint32(gateway.Port),
		})
	}
	for _, s := range serviceList.Services {
		svc, instanceList := c.convertService(s, gateways)
		services = append(services, svc)
		instances[svc.Hostname] = instanceList
	}
	c.storeLock.Lock()
	c.serviceStore = services
	c.instanceStore = instances
	c.gatewayStore = gateways
	c.lastMessage = serviceList
	c.storeLock.Unlock()
}

func autoAllocateIPs(services []*model.Service) []*model.Service {
	// i is everything from 240.241.0.(j) to 240.241.255.(j)
	// j is everything from 240.241.(i).1 to 240.241.(i).254
	// we can capture this in one integer variable.
	// given X, we can compute i by X/255, and j is X%255
	// To avoid allocating 240.241.(i).255, if X % 255 is 0, increment X.
	// For example, when X=510, the resulting IP would be 240.241.2.0 (invalid)
	// So we bump X to 511, so that the resulting IP is 240.241.2.1
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
				logger.Errorf("out of IPs to allocate for service entries")
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
	return autoAllocateIPs(c.serviceStore), nil
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
	eventCh := make(chan *federationmodel.WatchEvent)
	refreshTicker := time.NewTicker(c.resyncPeriod)
	defer refreshTicker.Stop()
	c.resync()
	go func() {
		for !c.hasStopped() {
			logger.Info("starting watch")
			err := c.watch(eventCh, stop)
			if err != nil {
				logger.Errorf("watch failed: %s", err)
				time.Sleep(c.backoffPolicy.NextBackOff())
			} else {
				return
			}
		}
	}()
	for {
		select {
		case <-stop:
			logger.Info("Federation Controller terminated")
			c.stop()
			return
		case e := <-eventCh:
			logger.Debugf("watch event received: %s service %s", e.Action, e.Service.Name)
			c.handleEvent(e)
		case <-refreshTicker.C:
			logger.Debugf("performing full resync for cluster %s", c.clusterID)
			_ = c.resync()
		}
	}
}

func (c *Controller) stop() {
	atomic.StoreInt32(&c.stopped, 1)
}

func (c *Controller) hasStopped() bool {
	return atomic.LoadInt32(&c.stopped) != 0
}

func (c *Controller) handleEvent(e *federationmodel.WatchEvent) {
	if e.Service == nil {
		checksum := c.resync()
		if checksum != e.Checksum {
			// this shouldn't happen
			logger.Error("checksum mismatch after resync")
		}
		return
	}
	c.storeLock.RLock()
	gateways := c.gatewayStore
	c.storeLock.RUnlock()
	svc, instances := c.convertService(e.Service, gateways)

	// verify we're up to date
	lastReceivedMessage := c.lastMessage
	if e.Action == federationmodel.ActionAdd {
		lastReceivedMessage.Services = append(c.lastMessage.Services, e.Service)
	} else if e.Action == federationmodel.ActionUpdate {
		for i, s := range lastReceivedMessage.Services {
			if s.Name == e.Service.Name {
				lastReceivedMessage.Services[i] = e.Service
				break
			}
		}
	} else if e.Action == federationmodel.ActionDelete {
		for i, s := range lastReceivedMessage.Services {
			if s.Name == e.Service.Name {
				lastReceivedMessage.Services[i] = c.lastMessage.Services[len(c.lastMessage.Services)-1]
				lastReceivedMessage.Services = c.lastMessage.Services[:len(c.lastMessage.Services)-1]
				break
			}
		}
	}
	lastReceivedMessage.Checksum = lastReceivedMessage.GenerateChecksum()
	if lastReceivedMessage.Checksum != e.Checksum {
		logger.Warnf("checksums don't match. resyncing")
		c.resync()
		return
	}

	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	c.lastMessage = lastReceivedMessage

	endpoints := []*model.IstioEndpoint{}
	action := model.EventAdd
	if e.Action == federationmodel.ActionAdd {
		c.serviceStore = append(c.serviceStore, svc)
		c.instanceStore[svc.Hostname] = instances
		for _, instance := range instances {
			endpoints = append(endpoints, instance.Endpoint)
		}
	} else if e.Action == federationmodel.ActionUpdate {
		action = model.EventUpdate
		c.serviceStore = append(c.serviceStore, svc)
		c.instanceStore[svc.Hostname] = instances
		for _, instance := range instances {
			endpoints = append(endpoints, instance.Endpoint)
		}
	} else if e.Action == federationmodel.ActionDelete {
		action = model.EventDelete
		for i, s := range c.serviceStore {
			if s.Hostname == svc.Hostname {
				c.serviceStore[i] = c.serviceStore[len(c.serviceStore)-1]
				c.serviceStore = c.serviceStore[:len(c.serviceStore)-1]
				break
			}
		}
		delete(c.instanceStore, svc.Hostname)
	}
	c.xdsUpdater.SvcUpdate(c.clusterID, string(svc.Hostname), svc.Attributes.Namespace, action)
	c.xdsUpdater.EDSUpdate(c.clusterID, string(svc.Hostname), svc.Attributes.Namespace, endpoints)
	c.xdsUpdater.ConfigUpdate(&model.PushRequest{
		Full: true,
	})
}

func (c *Controller) watch(eventCh chan *federationmodel.WatchEvent, stopCh <-chan struct{}) error {
	url := c.discoveryURL + "/watch"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		logger.Errorf("Failed to create request: '%s': %s", url, err)
		return nil
	}
	req.Header.Add("discovery-address", c.networkAddress)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}

	// connection was established successfully. reset backoffPolicy
	c.backoffPolicy.Reset()

	dec := json.NewDecoder(resp.Body)
	for {
		select {
		case <-stopCh:
			return nil
		default:
		}
		var e federationmodel.WatchEvent
		err := dec.Decode(&e)
		if err != nil {
			return err
		}
		eventCh <- &e
	}
}

func (c *Controller) resync() uint64 {
	svcList := c.pollServices()
	if svcList != nil {
		c.convertServices(svcList)
		c.storeLock.RLock()
		defer c.storeLock.RUnlock()
		for hostname, instanceList := range c.instanceStore {
			endpoints := []*model.IstioEndpoint{}
			for _, instance := range instanceList {
				endpoints = append(endpoints, instance.Endpoint)
			}
			c.xdsUpdater.SvcUpdate(c.clusterID, string(hostname), instanceList[0].Service.Attributes.Namespace, model.EventUpdate)
			c.xdsUpdater.EDSUpdate(c.clusterID, string(hostname), instanceList[0].Service.Attributes.Namespace, endpoints)
		}
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full: true,
		})
		return svcList.Checksum
	}
	return 0
}

// HasSynced always returns true so not to stall istiod on a broken federation connection
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
