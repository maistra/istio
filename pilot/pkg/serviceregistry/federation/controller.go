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
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/servicemesh/federation/common"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/pkg/log"
)

var (
	_ model.ServiceDiscovery   = &Controller{}
	_ model.Controller         = &Controller{}
	_ serviceregistry.Instance = &Controller{}
)

var logger = log.RegisterScope("federation-registry", "federation-registry", 0)

const (
	federationPort = common.FederationPort
)

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	networkAddress string
	egressService  string
	egressName     string
	discoveryURL   string
	useDirectCalls bool
	networkID      network.ID
	namespace      string
	clusterID      cluster.ID
	resyncPeriod   time.Duration
	backoffPolicy  *backoff.ExponentialBackOff

	configStore model.ConfigStoreCache
	xdsUpdater  model.XDSUpdater

	storeLock     sync.RWMutex
	imports       map[federationmodel.ServiceKey]*existingImport
	serviceStore  []*model.Service
	instanceStore map[host.Name][]*model.ServiceInstance
	gatewayStore  []model.NetworkGateway

	lastMessage *federationmodel.ServiceListMessage
	stopped     int32
}

type existingImport struct {
	*federationmodel.ServiceMessage
	localName federationmodel.ServiceKey
}

type Options struct {
	NetworkAddress string
	EgressService  string
	EgressName     string
	UseDirectCalls bool
	NetworkName    string
	ClusterID      string
	Namespace      string
	ConfigStore    model.ConfigStoreCache
	XDSUpdater     model.XDSUpdater
	ResyncPeriod   time.Duration
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	backoffPolicy := backoff.NewExponentialBackOff()
	backoffPolicy.MaxElapsedTime = 0
	return &Controller{
		discoveryURL:   fmt.Sprintf("%s://%s:%d", common.DiscoveryScheme, opt.EgressService, common.DefaultDiscoveryPort),
		egressService:  opt.EgressService,
		egressName:     opt.EgressName,
		networkAddress: opt.NetworkAddress,
		useDirectCalls: opt.UseDirectCalls,
		networkID:      network.ID(opt.NetworkName),
		namespace:      opt.Namespace,
		clusterID:      cluster.ID(opt.ClusterID),
		imports:        map[federationmodel.ServiceKey]*existingImport{},
		instanceStore:  map[host.Name][]*model.ServiceInstance{},
		configStore:    opt.ConfigStore,
		xdsUpdater:     opt.XDSUpdater,
		resyncPeriod:   opt.ResyncPeriod,
		backoffPolicy:  backoffPolicy,
	}
}

func (c *Controller) Cluster() cluster.ID {
	return c.clusterID
}

func (c *Controller) Provider() provider.ID {
	return provider.Federation
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

func (c *Controller) convertExportedService(s *federationmodel.ServiceMessage) (*model.Service, []*model.ServiceInstance) {
	serviceVisibility := visibility.Private
	if c.useDirectCalls {
		serviceVisibility = visibility.Public
	}
	serviceName := fmt.Sprintf("%s.%s.remote", s.Name, s.Namespace)
	return c.createService(s, serviceName, s.Hostname, c.clusterID, c.networkID, serviceVisibility, c.gatewayStore)
}

func (c *Controller) convertToLocalService(s *federationmodel.ServiceMessage,
	importedName federationmodel.ServiceKey) (*model.Service, []*model.ServiceInstance) {
	serviceName := fmt.Sprintf("%s.%s.local", s.Name, s.Namespace)
	egressAddrs, err := c.getIPAddrsForHostOrIP(c.egressService)
	gateways := make([]model.NetworkGateway, len(egressAddrs))
	if err == nil {
		for index, addr := range egressAddrs {
			gateways[index] = model.NetworkGateway{
				Addr: addr,
				Port: federationPort,
			}
		}
	} else {
		logger.Errorf("could not get address for egress gateway: %s", err)
	}
	network := network.ID("")
	// XXX: make this configurable
	serviceVisibility := visibility.Public
	return c.createService(s, serviceName, importedName.Hostname, c.clusterID, network, serviceVisibility, gateways)
}

func (c *Controller) createService(s *federationmodel.ServiceMessage, serviceName, hostname string, clusterID cluster.ID, network network.ID,
	serviceVisibility visibility.Instance, networkGateways []model.NetworkGateway) (*model.Service, []*model.ServiceInstance) {
	instances := []*model.ServiceInstance{}
	svc := &model.Service{
		Attributes: model.ServiceAttributes{
			ServiceRegistry: provider.Federation,
			Name:            serviceName, // simple name in the form name.namespace of the exported service
			Namespace:       c.namespace, // the federation namespace
			ExportTo: map[visibility.Instance]bool{
				serviceVisibility: true,
			},
		},
		Resolution:      model.ClientSideLB,
		DefaultAddress:  constants.UnspecifiedIP,
		Hostname:        host.Name(hostname),
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
					Address:      networkGateway.Addr,
					EndpointPort: networkGateway.Port,
					Network:      network,
					Locality: model.Locality{
						ClusterID: clusterID,
					},
					ServicePortName: port.Name,
					TLSMode:         model.IstioMutualTLSModeLabel,
				},
			})
		}
	}
	return svc, instances
}

func (c *Controller) gatewayForNetworkAddress() []model.NetworkGateway {
	var gateways []model.NetworkGateway
	addrs, err := c.getIPAddrsForHostOrIP(c.networkAddress)
	if err != nil {
		logger.Errorf("error resolving IP addr for federation network %s: %v", c.networkAddress, err)
	} else {
		logger.Debugf("adding gateway %s endpoints for cluster %s", c.networkAddress, c.clusterID)
		for _, ip := range addrs {
			logger.Debugf("adding gateway %s endpoint %s for cluster %s", c.networkAddress, ip, c.clusterID)
			gateways = append(gateways, model.NetworkGateway{
				Addr: ip,
				Port: federationPort,
			})
		}
	}
	return gateways
}

func (c *Controller) getIPAddrsForHostOrIP(host string) ([]string, error) {
	gwIP := net.ParseIP(host)
	if gwIP != nil {
		return []string{gwIP.String()}, nil
	}
	return net.LookupHost(host)
}

func (c *Controller) getImportNameForService(name federationmodel.ServiceKey) federationmodel.ServiceKey {
	// XXX: integrate ServiceImports CRD functionality here
	// for now, hardcoding values for all services
	return federationmodel.ServiceKey{
		Name:      fmt.Sprintf("%s.%s", name.Name, name.Namespace),
		Namespace: c.namespace,
		Hostname:  fmt.Sprintf("%s.%s.svc.%s.local", name.Name, name.Namespace, c.clusterID),
	}
}

func (c *Controller) convertServices(serviceList *federationmodel.ServiceListMessage) {
	c.gatewayStore = c.gatewayForNetworkAddress()
	for _, gateway := range serviceList.NetworkGatewayEndpoints {
		c.gatewayStore = append(c.gatewayStore, model.NetworkGateway{
			Addr: gateway.Hostname,
			Port: uint32(gateway.Port),
		})
	}

	oldImports := c.imports
	c.imports = map[federationmodel.ServiceKey]*existingImport{}
	allUpdatedConfigs := map[model.ConfigKey]struct{}{}
	for _, s := range serviceList.Services {
		var updatedConfigs map[model.ConfigKey]struct{}
		var err error
		importName := c.getImportNameForService(s.ServiceKey)
		if existing, update := oldImports[s.ServiceKey]; update {
			if updatedConfigs, err = c.updateService(s, existing, importName); err != nil {
				// XXX: just log for now, we can't really recover
				logger.Errorf("error updating configuration for federated service %+v from mesh %s: %s", s.ServiceKey, c.clusterID, err)
			}
		} else if importName.Hostname != "" {
			if updatedConfigs, err = c.addService(s, importName); err != nil {
				// XXX: just log for now, we can't really recover
				logger.Errorf("error adding configuration for federated service %+v from mesh %s: %s", s.ServiceKey, c.clusterID, err)
			}
		}
		for key, value := range updatedConfigs {
			allUpdatedConfigs[key] = value
		}
	}
	for key, existing := range oldImports {
		if _, exists := c.imports[key]; !exists {
			updatedConfigs := c.deleteService(&federationmodel.ServiceMessage{ServiceKey: key}, existing)
			for key, value := range updatedConfigs {
				allUpdatedConfigs[key] = value
			}
		}
	}
	if len(allUpdatedConfigs) > 0 {
		logger.Debugf("pushing XDS config for services: %+v", allUpdatedConfigs)
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: allUpdatedConfigs,
		})
	}
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
		if svc.DefaultAddress == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() &&
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
func (c *Controller) GetService(hostname host.Name) *model.Service {
	var out *model.Service
	return out
}

// NetworkGateways merges the service-based cross-network gateways from each registry.
func (c *Controller) NetworkGateways() []model.NetworkGateway {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	return c.gatewayStore
}

func (c *Controller) MCSServices() []model.MCSServiceInfo {
	return nil // TODO: Implement
}

// InstancesByPort retrieves instances for a service on a given port that match
// any of the supplied labels. All instances match an empty label list.
func (c *Controller) InstancesByPort(svc *model.Service, port int, labels labels.Collection) []*model.ServiceInstance {
	instances := []*model.ServiceInstance{}
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	for _, instanceList := range c.instanceStore {
		for _, instance := range instanceList {
			if instance.Service == svc && instance.ServicePort.Port == port {
				instances = append(instances, instance.DeepCopy())
			}
		}
	}
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
	if e.Service == nil || c.lastMessage == nil {
		checksum := c.resync()
		if checksum != e.Checksum {
			// this shouldn't happen
			logger.Error("checksum mismatch after resync")
		}
		return
	}

	unlockIt := true
	c.storeLock.Lock()
	defer func() {
		if unlockIt {
			c.storeLock.Unlock()
		}
	}()
	// verify we're up to date
	lastReceivedMessage := *c.lastMessage
	switch e.Action {
	case federationmodel.ActionAdd:
		lastReceivedMessage.Services = append(c.lastMessage.Services, e.Service)
	case federationmodel.ActionUpdate:
		for i, s := range lastReceivedMessage.Services {
			if s.Name == e.Service.Name {
				lastReceivedMessage.Services[i] = e.Service
				break
			}
		}
	case federationmodel.ActionDelete:
		for i, s := range lastReceivedMessage.Services {
			if s.Name == e.Service.Name {
				lastReceivedMessage.Services[i] = c.lastMessage.Services[len(c.lastMessage.Services)-1]
				lastReceivedMessage.Services = c.lastMessage.Services[:len(c.lastMessage.Services)-1]
				break
			}
		}
	default:
		logger.Errorf("unknown Action from federation watch: %s", e.Action)
		return
	}

	lastReceivedMessage.Checksum = lastReceivedMessage.GenerateChecksum()
	if lastReceivedMessage.Checksum != e.Checksum {
		logger.Warnf("checksums don't match. resyncing")
		unlockIt = false
		c.storeLock.Unlock()
		c.resync()
		return
	}

	c.lastMessage = &lastReceivedMessage

	importedName := c.getImportNameForService(e.Service.ServiceKey)
	existing := c.imports[e.Service.ServiceKey]
	var updatedConfigs map[model.ConfigKey]struct{}
	var err error
	switch e.Action {
	case federationmodel.ActionAdd:
		if importedName.Hostname != "" {
			if updatedConfigs, err = c.addService(e.Service, importedName); err != nil {
				// XXX: just log for now, we can't really recover
				logger.Errorf("error adding configuration for federated service %+v from mesh %s: %s", e.Service.ServiceKey, c.clusterID, err)
			}
		}
	case federationmodel.ActionUpdate:
		if importedName.Hostname == "" {
			if existing != nil {
				updatedConfigs = c.deleteService(e.Service, existing)
			}
		} else {
			if updatedConfigs, err = c.updateService(e.Service, existing, importedName); err != nil {
				// XXX: just log for now, we can't really recover
				logger.Errorf("error updating configuration for federated service %+v from mesh %s: %s", e.Service.ServiceKey, c.clusterID, err)
			}
		}
	case federationmodel.ActionDelete:
		updatedConfigs = c.deleteService(e.Service, existing)
	}
	if len(updatedConfigs) > 0 {
		logger.Debugf("pushing XDS config for services: %+v", updatedConfigs)
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: updatedConfigs,
		})
	}
}

// store has to be Lock()ed
func (c *Controller) updateXDS(hostname, namespace string, instances []*model.ServiceInstance, event model.Event) {
	endpoints := []*model.IstioEndpoint{}
	for _, instance := range instances {
		endpoints = append(endpoints, instance.Endpoint)
	}
	c.xdsUpdater.SvcUpdate(model.ShardKeyFromRegistry(c), hostname, namespace, event)
	c.xdsUpdater.EDSCacheUpdate(model.ShardKeyFromRegistry(c), hostname, namespace, endpoints)
}

// store has to be Lock()ed
func (c *Controller) addService(service *federationmodel.ServiceMessage, importName federationmodel.ServiceKey) (map[model.ConfigKey]struct{}, error) {
	logger.Debugf("adding exported service %+v, known locally as %+v", service.ServiceKey, importName)
	updatedConfigs := map[model.ConfigKey]struct{}{}
	exportedService, exportedInstances := c.convertExportedService(service)
	localService, localInstances := c.convertToLocalService(service, importName)

	if err := c.createRoutingResources(service.ServiceKey, importName); err != nil {
		return nil, err
	}

	c.imports[service.ServiceKey] = &existingImport{ServiceMessage: service, localName: importName}

	c.addServiceToStore(exportedService, exportedInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      gvk.ServiceEntry,
		Name:      string(exportedService.Hostname),
		Namespace: c.namespace,
	}] = struct{}{}

	c.addServiceToStore(localService, localInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      gvk.ServiceEntry,
		Name:      string(localService.Hostname),
		Namespace: c.namespace,
	}] = struct{}{}

	return updatedConfigs, nil
}

// store has to be Lock()ed
func (c *Controller) updateService(service *federationmodel.ServiceMessage, existing *existingImport,
	importName federationmodel.ServiceKey) (map[model.ConfigKey]struct{}, error) {
	logger.Debugf("updating exported service %+v, known locally as %+v", service.ServiceKey, importName)
	updatedConfigs := map[model.ConfigKey]struct{}{}
	exportedService, exportedInstances := c.convertExportedService(service)
	localService, localInstances := c.convertToLocalService(service, importName)

	if existing != nil && importName.Hostname != existing.localName.Hostname {
		if err := c.createRoutingResources(service.ServiceKey, importName); err != nil {
			return nil, err
		}
		// TODO: optimize service and instance updates
	}

	// TODO: be smart and see if anything changed, so we don't push unnecessarily

	c.imports[service.ServiceKey] = &existingImport{ServiceMessage: service, localName: importName}

	c.updateServiceInStore(exportedService, exportedInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      gvk.ServiceEntry,
		Name:      string(exportedService.Hostname),
		Namespace: c.namespace,
	}] = struct{}{}

	c.updateServiceInStore(localService, localInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      gvk.ServiceEntry,
		Name:      string(localService.Hostname),
		Namespace: c.namespace,
	}] = struct{}{}

	return updatedConfigs, nil
}

// store has to be Lock()ed
func (c *Controller) deleteService(service *federationmodel.ServiceMessage, existing *existingImport) map[model.ConfigKey]struct{} {
	if existing != nil {
		logger.Debugf("deleting exported service %+v, known locally as %+v", service.ServiceKey, existing.localName)
	} else {
		logger.Debugf("deleting exported service %+v, with unknown locally name", service.ServiceKey)
	}
	_ = c.deleteRoutingResources(service.ServiceKey)
	updatedConfigs := map[model.ConfigKey]struct{}{}

	delete(c.imports, service.ServiceKey)

	c.removeServiceFromStore(service.ServiceKey)
	updatedConfigs[model.ConfigKey{
		Kind:      gvk.ServiceEntry,
		Name:      service.Hostname,
		Namespace: c.namespace,
	}] = struct{}{}

	if existing != nil {
		c.removeServiceFromStore(existing.localName)
		updatedConfigs[model.ConfigKey{
			Kind:      gvk.ServiceEntry,
			Name:      existing.localName.Hostname,
			Namespace: c.namespace,
		}] = struct{}{}
	}

	return updatedConfigs
}

func (c *Controller) addServiceToStore(service *model.Service, instances []*model.ServiceInstance) {
	c.serviceStore = append(c.serviceStore, service)
	c.instanceStore[service.Hostname] = instances
	c.updateXDS(string(service.Hostname), service.Attributes.Namespace, instances, model.EventAdd)
}

func (c *Controller) updateServiceInStore(service *model.Service, instances []*model.ServiceInstance) {
	eventType := model.EventUpdate
	found := false
	for i, s := range c.serviceStore {
		if s.Hostname == service.Hostname {
			c.serviceStore[i] = service
			found = true
			break
		}
	}
	if !found {
		logger.Warnf("trying to update unknown service %s, adding it to the registry", service.Hostname)
		c.serviceStore = append(c.serviceStore, service)
		eventType = model.EventAdd
	}
	c.instanceStore[service.Hostname] = instances
	c.updateXDS(string(service.Hostname), service.Attributes.Namespace, instances, eventType)
}

func (c *Controller) removeServiceFromStore(service federationmodel.ServiceKey) {
	for i, s := range c.serviceStore {
		if s.Hostname == host.Name(service.Hostname) {
			c.serviceStore[i] = c.serviceStore[len(c.serviceStore)-1]
			c.serviceStore = c.serviceStore[:len(c.serviceStore)-1]
			break
		}
	}
	delete(c.instanceStore, host.Name(service.Hostname))
	c.xdsUpdater.SvcUpdate(model.ShardKeyFromRegistry(c), service.Hostname, c.namespace, model.EventDelete)
	c.xdsUpdater.EDSCacheUpdate(model.ShardKeyFromRegistry(c), service.Hostname, c.namespace, nil)
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
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	logger.Debugf("performing full resync")
	svcList := c.pollServices()
	if svcList != nil {
		c.convertServices(svcList)
		return svcList.Checksum
	}
	return 0
}

// HasSynced always returns true so not to stall istiod on a broken federation connection
func (c *Controller) HasSynced() bool {
	return true
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) {
	// TODO
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) {
	// TODO
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
