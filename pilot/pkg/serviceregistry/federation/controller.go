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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff"
	corev1 "k8s.io/api/core/v1"
	v1 "maistra.io/api/federation/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/servicemesh/federation/common"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/istio/pkg/servicemesh/federation/status"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

var (
	_ model.ServiceDiscovery   = &Controller{}
	_ model.Controller         = &Controller{}
	_ serviceregistry.Instance = &Controller{}
)

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	model.NetworkGatewaysHandler

	remote               v1.ServiceMeshPeerRemote
	egressService        string
	egressName           string
	discoveryURL         string
	discoveryServiceName string
	useDirectCalls       bool
	namespace            string
	localClusterID       cluster.ID
	localNetworkID       network.ID
	clusterID            cluster.ID
	networkID            network.ID
	resyncPeriod         time.Duration
	backoffPolicy        *backoff.ExponentialBackOff

	logger *log.Scope

	kubeClient    kube.Client
	statusHandler status.Handler
	configStore   model.ConfigStoreController
	xdsUpdater    model.XDSUpdater

	localDomainSuffix   string
	defaultDomainSuffix string
	importNameMapper    common.NameMapper
	defaultLocality     *v1.ImportedServiceLocality
	importLocality      *v1.ImportedServiceLocality
	locality            *v1.ImportedServiceLocality

	storeLock      sync.RWMutex
	imports        map[federationmodel.ServiceKey]*existingImport
	serviceStore   map[host.Name]*model.Service
	instanceStore  map[host.Name][]*model.ServiceInstance
	gatewayStore   []model.NetworkGateway
	egressGateways []model.NetworkGateway
	egressSAs      []string

	lastMessage *federationmodel.ServiceListMessage
	started     int32

	cancelWatch context.CancelFunc
	watchEvents chan *federationmodel.WatchEvent

	peerConfigGeneration   int64
	importConfigGeneration int64
}

type existingImport struct {
	*federationmodel.ServiceMessage
	localName federationmodel.ServiceKey
}

type Options struct {
	DomainSuffix   string
	LocalClusterID string
	LocalNetwork   string
	ClusterID      string
	Network        string
	KubeClient     kube.Client
	StatusHandler  status.Handler
	ConfigStore    model.ConfigStoreController
	XDSUpdater     model.XDSUpdater
	ResyncPeriod   time.Duration
}

func defaultDomainSuffixForMesh(mesh *v1.ServiceMeshPeer) string {
	return fmt.Sprintf("svc.%s-imports.local", mesh.Name)
}

func localDomainSuffix(domainSuffix string) string {
	if domainSuffix == "" {
		return "svc.cluster.local"
	}
	return "svc." + domainSuffix
}

func mergeLocality(locality *v1.ImportedServiceLocality, defaults *v1.ImportedServiceLocality) *v1.ImportedServiceLocality {
	merged := v1.ImportedServiceLocality{}
	if defaults == nil {
		defaults = &v1.ImportedServiceLocality{}
		if locality == nil {
			// If default and imported locality are all empty, return nil
			// Otherwise, return imported locality.
			return nil
		}
	}
	if locality != nil {
		merged = *locality
	}
	if merged.Subzone == "" && defaults.Subzone != "" {
		merged.Subzone = defaults.Subzone
	}
	if merged.Zone == "" && defaults.Zone != "" {
		merged.Zone = defaults.Zone
	}
	if merged.Region == "" && defaults.Region != "" {
		merged.Region = defaults.Region
	}
	if merged.Subzone != "" && (merged.Zone == "" || merged.Region == "") {
		// cannot have subzone specified without region and zone
		return nil
	} else if merged.Zone != "" && merged.Region == "" {
		// cannot have zone specified without region
		return nil
	}
	return &merged
}

// NewController creates a new Aggregate controller
func NewController(opt Options, mesh *v1.ServiceMeshPeer, importConfig *v1.ImportedServiceSet) *Controller {
	backoffPolicy := backoff.NewExponentialBackOff()
	backoffPolicy.MaxElapsedTime = 0
	localDomainSuffix := localDomainSuffix(opt.DomainSuffix)
	c := &Controller{
		localClusterID:    cluster.ID(opt.LocalClusterID),
		localNetworkID:    network.ID(opt.LocalNetwork),
		clusterID:         cluster.ID(opt.ClusterID),
		networkID:         network.ID(opt.Network),
		localDomainSuffix: localDomainSuffix,
		imports:           map[federationmodel.ServiceKey]*existingImport{},
		serviceStore:      map[host.Name]*model.Service{},
		instanceStore:     map[host.Name][]*model.ServiceInstance{},
		kubeClient:        opt.KubeClient,
		statusHandler:     opt.StatusHandler,
		configStore:       opt.ConfigStore,
		xdsUpdater:        opt.XDSUpdater,
		resyncPeriod:      opt.ResyncPeriod,
		backoffPolicy:     backoffPolicy,
		logger:            common.Logger.WithLabels("component", "federation-registry"),
		watchEvents:       make(chan *federationmodel.WatchEvent),
	}
	c.UpdatePeerConfig(mesh)
	c.UpdateImportConfig(importConfig)
	return c
}

// UpdatePeerConfig updates all settings derived from the ServiceMeshPeer resource.
// It will also restart the watch if the controller is already running.
func (c *Controller) UpdatePeerConfig(peerConfig *v1.ServiceMeshPeer) {
	if func() bool {
		c.storeLock.RLock()
		defer c.storeLock.RUnlock()
		return c.peerConfigGeneration == peerConfig.Generation
	}() {
		return
	}

	restartWatch := func() bool {
		c.storeLock.Lock()
		defer c.storeLock.Unlock()

		restartWatch := false

		c.discoveryServiceName = common.DiscoveryServiceHostname(peerConfig)
		c.defaultDomainSuffix = defaultDomainSuffixForMesh(peerConfig)
		c.namespace = peerConfig.Namespace
		c.useDirectCalls = peerConfig.Spec.Security.AllowDirectOutbound
		if c.egressName != peerConfig.Spec.Gateways.Egress.Name {
			c.egressName = peerConfig.Spec.Gateways.Egress.Name
			c.egressService = fmt.Sprintf("%s.%s.%s",
				peerConfig.Spec.Gateways.Egress.Name, c.namespace, c.localDomainSuffix)
			c.discoveryURL = fmt.Sprintf("%s://%s:%d", common.DiscoveryScheme, c.egressService, common.DefaultDiscoveryPort)
			restartWatch = true
		}

		if !reflect.DeepEqual(c.remote, peerConfig.Spec.Remote) {
			c.remote = peerConfig.Spec.Remote
			restartWatch = true
		}

		c.peerConfigGeneration = peerConfig.Generation

		return restartWatch
	}()
	if c.hasStarted() && restartWatch {
		c.RestartWatch()
	}
}

// UpdateImportConfig updates the import rules that are used to select services for
// import into the mesh and rewrite their names.
func (c *Controller) UpdateImportConfig(importConfig *v1.ImportedServiceSet) {
	if func() bool {
		if importConfig == nil {
			return false
		}
		c.storeLock.RLock()
		defer c.storeLock.RUnlock()
		return c.importConfigGeneration == importConfig.Generation
	}() {
		return
	}
	func() {
		c.storeLock.Lock()
		defer c.storeLock.Unlock()
		c.importNameMapper = common.NewServiceImporter(importConfig, nil, c.defaultDomainSuffix, c.localDomainSuffix)
		if importConfig == nil {
			c.importLocality = nil
		} else {
			c.importLocality = importConfig.Spec.Locality
			c.importConfigGeneration = importConfig.Generation
		}
		c.locality = mergeLocality(c.importLocality, c.defaultLocality)
	}()
}

func (c *Controller) Cluster() cluster.ID {
	return c.clusterID
}

func (c *Controller) Provider() provider.ID {
	return provider.Federation
}

func (c *Controller) pollServices() (*federationmodel.ServiceListMessage, error) {
	url := c.discoveryURL + "/v1/services/"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: '%s': %s", url, err)
	}
	req.Header.Add("discovery-service", c.discoveryServiceName)
	req.Header.Add("remote", fmt.Sprint(common.RemoteChecksum(c.remote)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to GET URL: '%s': %s", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}

	respBytes := []byte{}
	_, err = resp.Body.Read(respBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from URL '%s': %s", url, err)
	}

	var serviceList federationmodel.ServiceListMessage
	err = json.NewDecoder(resp.Body).Decode(&serviceList)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response bytes: %s", err)
	}
	return &serviceList, nil
}

func (c *Controller) convertExportedService(s *federationmodel.ServiceMessage) (*model.Service, []*model.ServiceInstance) {
	serviceVisibility := visibility.Private
	if c.useDirectCalls {
		serviceVisibility = visibility.Public
	}
	serviceName := fmt.Sprintf("%s.%s.%s.remote", s.Name, s.Namespace, c.clusterID)
	return c.createService(createServiceOptions{
		service:           s,
		serviceName:       serviceName,
		serviceNamespace:  c.namespace,
		hostname:          s.Hostname,
		clusterID:         c.clusterID,
		networkID:         c.networkID,
		serviceVisibility: serviceVisibility,
		networkGateways:   c.gatewayStore,
		sas:               s.ServiceAccounts,
	})
}

func (c *Controller) convertToLocalService(s *federationmodel.ServiceMessage,
	importedName federationmodel.ServiceKey,
) (*model.Service, []*model.ServiceInstance) {
	serviceName := fmt.Sprintf("%s.%s.%s.local", importedName.Name, importedName.Namespace, c.clusterID)
	serviceNamespace := importedName.Namespace
	if !strings.HasSuffix(importedName.Hostname, c.localDomainSuffix) {
		// not importing as a local service
		serviceNamespace = c.namespace
	}
	// XXX: make this configurable
	serviceVisibility := visibility.Public
	return c.createService(createServiceOptions{
		service:           s,
		serviceName:       serviceName,
		serviceNamespace:  serviceNamespace,
		hostname:          importedName.Hostname,
		clusterID:         c.clusterID,
		networkID:         c.localNetworkID,
		serviceVisibility: serviceVisibility,
		networkGateways:   c.egressGateways,
		sas:               c.egressSAs,
		locality:          c.locality,
	})
}

type createServiceOptions struct {
	service           *federationmodel.ServiceMessage
	serviceName       string
	serviceNamespace  string
	hostname          string
	clusterID         cluster.ID
	networkID         network.ID
	serviceVisibility visibility.Instance
	networkGateways   []model.NetworkGateway
	sas               []string
	locality          *v1.ImportedServiceLocality
}

func (c *Controller) createService(opts createServiceOptions) (*model.Service, []*model.ServiceInstance) {
	var instances []*model.ServiceInstance
	svc := &model.Service{
		Attributes: model.ServiceAttributes{
			ServiceRegistry: provider.Federation,
			Name:            opts.serviceName,
			Namespace:       opts.serviceNamespace,
			ExportTo: map[visibility.Instance]bool{
				opts.serviceVisibility: true,
			},
			Labels: labels.Instance{
				label.TopologyCluster.Name:           opts.clusterID.String(),
				label.TopologyNetwork.Name:           opts.networkID.String(),
				model.IstioCanonicalServiceLabelName: opts.serviceName,
			},
		},
		CreationTime:    time.Now(),
		Resolution:      model.ClientSideLB,
		DefaultAddress:  constants.UnspecifiedIP,
		Hostname:        host.Name(opts.hostname),
		Ports:           model.PortList{},
		ServiceAccounts: append([]string(nil), opts.sas...),
	}
	for _, port := range opts.service.ServicePorts {
		svc.Ports = append(svc.Ports, &model.Port{
			Name:     port.Name,
			Port:     port.Port,
			Protocol: protocol.Instance(port.Protocol),
		})
	}

	baseWorkloadName := opts.serviceName
	if !strings.Contains(opts.serviceName, c.clusterID.String()) {
		baseWorkloadName = fmt.Sprintf("%s-%s", opts.serviceName, c.clusterID)
	}
	localityLabel := ""
	if opts.locality != nil && opts.locality.Region != "" {
		localityLabel = fmt.Sprintf("%s/%s/%s", opts.locality.Region, opts.locality.Zone, opts.locality.Subzone)
	}
	for _, port := range svc.Ports {
		for gatewayIndex, networkGateway := range opts.networkGateways {
			c.logger.Debugf("adding endpoint for imported service: addr=%s, port=%d, host=%s",
				networkGateway.Addr, networkGateway.Port, svc.Hostname)
			instance := &model.ServiceInstance{
				Service:     svc,
				ServicePort: port,
				Endpoint: &model.IstioEndpoint{
					Address:      networkGateway.Addr,
					EndpointPort: networkGateway.Port,
					Labels: labels.Instance{
						label.TopologyCluster.Name:                   opts.clusterID.String(),
						label.TopologyNetwork.Name:                   opts.networkID.String(),
						model.IstioCanonicalServiceLabelName:         opts.serviceName,
						model.IstioCanonicalServiceRevisionLabelName: opts.clusterID.String(),
					},
					Network: opts.networkID,
					Locality: model.Locality{
						ClusterID: opts.clusterID,
					},
					ServicePortName: port.Name,
					TLSMode:         model.IstioMutualTLSModeLabel,
					Namespace:       opts.serviceNamespace,
					WorkloadName:    fmt.Sprintf("%s-%d", baseWorkloadName, gatewayIndex),
				},
			}
			if opts.locality != nil {
				instance.Endpoint.Labels[label.TopologySubzone.Name] = opts.locality.Subzone
				instance.Endpoint.Labels[corev1.LabelZoneFailureDomainStable] = opts.locality.Zone
				instance.Endpoint.Labels[corev1.LabelZoneRegionStable] = opts.locality.Region
				instance.Endpoint.Locality.Label = localityLabel
			}
			instances = append(instances, instance)
		}
	}
	return svc, instances
}

func (c *Controller) gatewayForNetworkAddress() []model.NetworkGateway {
	var gateways []model.NetworkGateway
	remotePort := c.remote.ServicePort
	if remotePort == 0 {
		remotePort = common.DefaultFederationPort
	}
	for _, address := range c.remote.Addresses {
		addrs, err := c.getIPAddrsForHostOrIP(address)
		if err != nil {
			c.logger.Errorf("error resolving IP addr for federation network %s: %v", address, err)
		} else {
			c.logger.Debugf("adding gateway %s endpoints for cluster %s", address, c.clusterID)
			for _, ip := range addrs {
				c.logger.Debugf("adding gateway %s endpoint %s for cluster %s", address, ip, c.clusterID)
				gateways = append(gateways, model.NetworkGateway{
					Addr: ip,
					Port: uint32(remotePort),
				})
			}
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

// store has to be Lock()ed
func (c *Controller) getImportNameForService(exportedService *model.Service) *federationmodel.ServiceKey {
	// XXX: integrate ServiceImports CRD functionality here
	// for now, hardcoding values for all services
	return c.importNameMapper.NameForService(exportedService)
}

func (c *Controller) updateGateways(serviceList *federationmodel.ServiceListMessage) {
	c.gatewayStore = c.gatewayForNetworkAddress()
	if serviceList != nil {
		for _, gateway := range serviceList.NetworkGatewayEndpoints {
			c.gatewayStore = append(c.gatewayStore, model.NetworkGateway{
				Addr: gateway.Hostname,
				Port: uint32(gateway.Port),
			})
		}
	}
	c.egressGateways, c.egressSAs = c.getEgressServiceAddrs()
}

func (c *Controller) getEgressServiceAddrs() ([]model.NetworkGateway, []string) {
	endpoints, err := common.EndpointsForService(c.kubeClient, c.egressName, c.namespace)
	if err != nil {
		c.logger.Errorf("unabled to retrieve endpoints for federation egress gateway %s: %s", c.egressName, err)
		return nil, nil
	}
	serviceAccountByIP := map[string]string{}
	if !c.useDirectCalls {
		serviceAccountByIP, err = common.ServiceAccountsForService(c.kubeClient, c.egressName, c.namespace)
		if err != nil {
			c.logger.Warnf("unable to retrieve ServiceAccount information for imported services accessed through %s: %s",
				c.egressName, err)
		}
	}
	var addrs []model.NetworkGateway
	var sas []string
	for _, subset := range endpoints.Subsets {
		for index, address := range subset.Addresses {
			if subset.Ports[index].Port == common.DefaultFederationPort {
				ips, err := c.getIPAddrsForHostOrIP(address.IP)
				if err != nil {
					c.logger.Errorf("error converting to IP address from %s: %s", address.IP, err)
					continue
				}
				for _, ip := range ips {
					addrs = append(addrs, model.NetworkGateway{
						Addr: ip,
						Port: uint32(common.DefaultFederationPort),
					})
					sas = append(sas, serviceAccountByIP[ip])
				}
			}
		}
	}
	return addrs, sas
}

func (c *Controller) convertServices(serviceList *federationmodel.ServiceListMessage) {
	oldImports := c.imports
	c.imports = map[federationmodel.ServiceKey]*existingImport{}
	allUpdatedConfigs := map[model.ConfigKey]struct{}{}
	for _, s := range serviceList.Services {
		var updatedConfigs map[model.ConfigKey]struct{}
		var err error
		if existing, update := oldImports[s.ServiceKey]; update {
			if updatedConfigs, err = c.updateService(s, existing); err != nil {
				// XXX: just log for now, we can't really recover
				c.logger.Errorf("error updating configuration for federated service %+v from mesh %s: %s", s.ServiceKey, c.clusterID, err)
			}
		} else {
			if updatedConfigs, err = c.addService(s); err != nil {
				// XXX: just log for now, we can't really recover
				c.logger.Errorf("error adding configuration for federated service %+v from mesh %s: %s", s.ServiceKey, c.clusterID, err)
			}
		}
		for key, value := range updatedConfigs {
			allUpdatedConfigs[key] = value
		}
	}
	for key, existing := range oldImports {
		if _, exists := c.imports[key]; !exists {
			updatedConfigs := c.deleteService(&federationmodel.ServiceMessage{ServiceKey: key}, existing)
			c.statusHandler.ImportRemoved(key.Hostname)
			for key, value := range updatedConfigs {
				allUpdatedConfigs[key] = value
			}
		}
	}
	if len(allUpdatedConfigs) > 0 {
		c.logger.Debugf("pushing XDS config for services: %+v", allUpdatedConfigs)
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: allUpdatedConfigs,
		})
	}
}

func (c *Controller) autoAllocateIPs(serviceStore map[host.Name]*model.Service) []*model.Service {
	var services []*model.Service
	// i is everything from 240.241.0.(j) to 240.241.255.(j)
	// j is everything from 240.241.(i).1 to 240.241.(i).254
	// we can capture this in one integer variable.
	// given X, we can compute i by X/255, and j is X%255
	// To avoid allocating 240.241.(i).255, if X % 255 is 0, increment X.
	// For example, when X=510, the resulting IP would be 240.241.2.0 (invalid)
	// So we bump X to 511, so that the resulting IP is 240.241.2.1
	maxIPs := 255 * 255 // are we going to exceeed this limit by processing 64K services?
	x := 0
	for _, svc := range serviceStore {
		services = append(services, svc)
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
				c.logger.Errorf("could not allocate VIP for %s: out of IPs to allocate for service entries", svc.Hostname)
				continue
			}
			thirdOctet := x / 255
			fourthOctet := x % 255
			svc.AutoAllocatedIPv4Address = fmt.Sprintf("240.241.%d.%d", thirdOctet, fourthOctet)
		}
	}
	return services
}

// Services lists services
func (c *Controller) Services() []*model.Service {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	return c.autoAllocateIPs(c.serviceStore)
}

// GetService retrieves a service by hostname if exists
func (c *Controller) GetService(hostname host.Name) *model.Service {
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	if svc, found := c.serviceStore[hostname]; found {
		return svc
	}
	return nil
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
func (c *Controller) InstancesByPort(svc *model.Service, port int, _ labels.Instance) []*model.ServiceInstance {
	instances := []*model.ServiceInstance{}
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	for _, instance := range c.instanceStore[svc.Hostname] {
		if instance.ServicePort.Port == port {
			instances = append(instances, instance.DeepCopy())
		}
	}
	return instances
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(_ *model.Proxy) []*model.ServiceInstance {
	return make([]*model.ServiceInstance, 0)
}

func (c *Controller) GetProxyWorkloadLabels(_ *model.Proxy) labels.Instance {
	return labels.Instance{}
}

// Run starts all the controllers
func (c *Controller) Run(stop <-chan struct{}) {
	refreshTicker := time.NewTicker(c.resyncPeriod)
	defer refreshTicker.Stop()
	c.startWatch()
	atomic.StoreInt32(&c.started, 1)
	for {
		select {
		case <-stop:
			c.logger.Info("Federation Controller terminated")
			c.stopWatch()
			c.stop()
			return
		case e := <-c.watchEvents:
			c.logger.Debugf("watch event received: %v", e)
			c.handleEvent(e)
		case <-refreshTicker.C:
			c.logger.Debugf("performing full resync for cluster %s", c.clusterID)
			_ = c.resync()
		}
	}
}

func (c *Controller) RestartWatch() {
	c.logger.Infof("restarting watch for cluster %s", c.clusterID)
	c.stopWatch()
	c.startWatch()
}

func (c *Controller) stop() {
	atomic.StoreInt32(&c.started, 0)
}

func (c *Controller) hasStarted() bool {
	return atomic.LoadInt32(&c.started) == 1
}

func (c *Controller) handleEvent(e *federationmodel.WatchEvent) {
	if e.Service == nil || c.lastMessage == nil {
		checksum := c.resync()
		if checksum != e.Checksum {
			// this shouldn't happen
			c.logger.Error("checksum mismatch after resync")
		}
		return
	}

	c.statusHandler.WatchEventReceived()

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
		c.logger.Errorf("unknown Action from federation watch: %s", e.Action)
		return
	}

	lastReceivedMessage.Checksum = lastReceivedMessage.GenerateChecksum()
	if lastReceivedMessage.Checksum != e.Checksum {
		c.logger.Warnf("checksums don't match. resyncing")
		unlockIt = false
		c.storeLock.Unlock()
		c.resync()
		return
	}

	c.lastMessage = &lastReceivedMessage

	existing := c.imports[e.Service.ServiceKey]
	var updatedConfigs map[model.ConfigKey]struct{}
	var err error
	switch e.Action {
	case federationmodel.ActionAdd:
		if updatedConfigs, err = c.addService(e.Service); err != nil {
			// XXX: just log for now, we can't really recover
			c.logger.Errorf("error adding configuration for federated service %+v from mesh %s: %s", e.Service.ServiceKey, c.clusterID, err)
		}
	case federationmodel.ActionUpdate:
		if updatedConfigs, err = c.updateService(e.Service, existing); err != nil {
			// XXX: just log for now, we can't really recover
			c.logger.Errorf("error updating configuration for federated service %+v from mesh %s: %s", e.Service.ServiceKey, c.clusterID, err)
		}
	case federationmodel.ActionDelete:
		updatedConfigs = c.deleteService(e.Service, existing)
		c.statusHandler.ImportRemoved(e.Service.Hostname)
	}
	if len(updatedConfigs) > 0 {
		c.logger.Debugf("pushing XDS config for services: %+v", updatedConfigs)
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
func (c *Controller) addService(service *federationmodel.ServiceMessage) (map[model.ConfigKey]struct{}, error) {
	c.logger.Debugf("handling new exported service %+v", service.ServiceKey)

	importedName := c.getImportNameForService(&model.Service{
		Hostname:   host.Name(service.Hostname),
		Attributes: model.ServiceAttributes{Name: service.Name, Namespace: service.Namespace},
	})
	if importedName == nil {
		c.statusHandler.ImportAdded(federationmodel.ServiceKey{}, service.Hostname)
		c.logger.Debugf("skipping import of service %+v, as it does not match an import filter", service.ServiceKey)
		return nil, nil
	}

	c.logger.Debugf("adding import for service %+v as %+v", service.ServiceKey, *importedName)
	updatedConfigs := map[model.ConfigKey]struct{}{}
	exportedService, exportedInstances := c.convertExportedService(service)
	localService, localInstances := c.convertToLocalService(service, *importedName)

	if err := c.createRoutingResources(service.ServiceKey, *importedName); err != nil {
		return nil, err
	}

	c.imports[service.ServiceKey] = &existingImport{ServiceMessage: service, localName: *importedName}

	c.addServiceToStore(exportedService, exportedInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      kind.ServiceEntry,
		Name:      string(exportedService.Hostname),
		Namespace: exportedService.Attributes.Namespace,
	}] = struct{}{}

	c.addServiceToStore(localService, localInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      kind.ServiceEntry,
		Name:      string(localService.Hostname),
		Namespace: localService.Attributes.Namespace,
	}] = struct{}{}

	c.statusHandler.ImportAdded(*importedName, service.Hostname)

	return updatedConfigs, nil
}

// store has to be Lock()ed
func (c *Controller) updateService(service *federationmodel.ServiceMessage, existing *existingImport) (map[model.ConfigKey]struct{}, error) {
	c.logger.Debugf("handling update for exported service %+v", service.ServiceKey)

	// XXX: exported service name is name.namespace, while it's namespace is c.namespace
	importedName := c.getImportNameForService(&model.Service{
		Hostname:   host.Name(service.Hostname),
		Attributes: model.ServiceAttributes{Name: service.Name, Namespace: service.Namespace},
	})
	if importedName == nil {
		c.statusHandler.ImportUpdated(federationmodel.ServiceKey{}, service.Hostname)
		if existing != nil {
			c.logger.Debugf("deleting import for service %+v, as it no longer matches an import filter", service.ServiceKey)
			return c.deleteService(service, existing), nil
		}
		c.logger.Debugf("skipping import for service %+v, as it does not match any import filter", service.ServiceKey)
		return nil, nil
	}

	updatedConfigs := map[model.ConfigKey]struct{}{}
	exportedService, exportedInstances := c.convertExportedService(service)
	localService, localInstances := c.convertToLocalService(service, *importedName)

	if existing == nil {
		// this may have been previously filtered out
		c.logger.Debugf("importing service %+v as %+v", service.ServiceKey, *importedName)
		if err := c.createRoutingResources(service.ServiceKey, *importedName); err != nil {
			return nil, err
		}
		// TODO: optimize service and instance updates
	} else if importedName.Hostname != existing.localName.Hostname {
		c.logger.Debugf("service %+v has been reimported as %+v (was %+v)", service.ServiceKey, *importedName, existing.localName)
		// update the routing
		if err := c.createRoutingResources(service.ServiceKey, *importedName); err != nil {
			return nil, err
		}

		// delete the old imported service
		c.removeServiceFromStore(existing.localName)
		updatedConfigs[model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      existing.localName.Hostname,
			Namespace: existing.Namespace,
		}] = struct{}{}
	}

	// TODO: be smart and see if anything changed, so we don't push unnecessarily

	c.imports[service.ServiceKey] = &existingImport{ServiceMessage: service, localName: *importedName}

	c.updateServiceInStore(exportedService, exportedInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      kind.ServiceEntry,
		Name:      string(exportedService.Hostname),
		Namespace: exportedService.Attributes.Namespace,
	}] = struct{}{}

	c.updateServiceInStore(localService, localInstances)
	updatedConfigs[model.ConfigKey{
		Kind:      kind.ServiceEntry,
		Name:      string(localService.Hostname),
		Namespace: localService.Attributes.Namespace,
	}] = struct{}{}

	c.statusHandler.ImportUpdated(*importedName, service.Hostname)

	return updatedConfigs, nil
}

// store has to be Lock()ed
func (c *Controller) deleteService(service *federationmodel.ServiceMessage, existing *existingImport) map[model.ConfigKey]struct{} {
	if existing != nil {
		c.logger.Debugf("deleting import for service %+v, known locally as %+v", service.ServiceKey, existing.localName)
	} else {
		c.logger.Debugf("deleting import for service %+v, with unknown local name", service.ServiceKey)
	}
	_ = c.deleteRoutingResources(service.ServiceKey)
	updatedConfigs := map[model.ConfigKey]struct{}{}

	delete(c.imports, service.ServiceKey)

	svc := c.removeServiceFromStore(service.ServiceKey)
	if svc != nil {
		updatedConfigs[model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      service.Hostname,
			Namespace: svc.Attributes.Namespace,
		}] = struct{}{}
	}

	if existing != nil {
		c.removeServiceFromStore(existing.localName)
		if svc != nil {
			updatedConfigs[model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      existing.localName.Hostname,
				Namespace: svc.Attributes.Namespace,
			}] = struct{}{}
		}
	}

	return updatedConfigs
}

func (c *Controller) addServiceToStore(service *model.Service, instances []*model.ServiceInstance) {
	// Check existing services in the serviceStore, those can be created before importing services from a federation peer.
	eventType := model.EventAdd
	for i, s := range c.serviceStore {
		if s.Hostname == service.Hostname {
			c.serviceStore[i] = service
			c.logger.Debugf("Update service %s to the registry", service.Hostname)
			eventType = model.EventUpdate
			break
		}
	}

	c.serviceStore[service.Hostname] = service
	c.instanceStore[service.Hostname] = instances
	c.updateXDS(string(service.Hostname), service.Attributes.Namespace, instances, eventType)
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
		c.logger.Warnf("trying to update unknown service %s, adding it to the registry", service.Hostname)
		c.serviceStore[service.Hostname] = service
		eventType = model.EventAdd
	}
	c.instanceStore[service.Hostname] = instances
	c.updateXDS(string(service.Hostname), service.Attributes.Namespace, instances, eventType)
}

func (c *Controller) removeServiceFromStore(service federationmodel.ServiceKey) *model.Service {
	svc := c.serviceStore[host.Name(service.Hostname)]
	if svc != nil {
		delete(c.serviceStore, host.Name(service.Hostname))
		delete(c.instanceStore, host.Name(service.Hostname))
		c.xdsUpdater.SvcUpdate(model.ShardKeyFromRegistry(c), service.Hostname, svc.Attributes.Namespace, model.EventDelete)
		c.xdsUpdater.EDSCacheUpdate(model.ShardKeyFromRegistry(c), service.Hostname, svc.Attributes.Namespace, nil)
	}
	return svc
}

func (c *Controller) startWatch() {
	var ctx context.Context
	var cancelCtx context.CancelFunc
	ctx, cancelCtx = context.WithCancel(context.Background())
	c.cancelWatch = cancelCtx
	go func() {
		defer func() {
			cancelCtx() // cleans up resources when watch is closed gracefully
			c.logger.Info("watch stopped")
		}()
		for ctx.Err() != context.Canceled {
			c.logger.Info("starting watch")
			err := c.watch(ctx, c.watchEvents)
			if err != nil {
				if err == context.Canceled {
					c.logger.Info("watch cancelled")
					return
				}
				c.logger.Errorf("watch failed: %s", err)

				// sleep, but still honor context cancellation
				select {
				case <-ctx.Done():
					c.logger.Info("watch cancelled")
					return
				case <-time.After(c.backoffPolicy.NextBackOff()):
					// start watch again after backoff
				}
			} else {
				c.backoffPolicy.Reset()
				c.logger.Info("watch closed")
			}
		}
	}()
}

func (c *Controller) stopWatch() {
	if c.cancelWatch != nil {
		c.cancelWatch()
	}
}

func (c *Controller) watch(ctx context.Context, eventCh chan *federationmodel.WatchEvent) error {
	c.statusHandler.WatchInitiated()
	url := c.discoveryURL + "/v1/watch"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		c.logger.Errorf("Failed to create request: '%s': %s", url, err)
		return nil
	}
	req.Header.Add("discovery-service", c.discoveryServiceName)
	req.Header.Add("remote", fmt.Sprint(common.RemoteChecksum(c.remote)))
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	defer func() {
		status := ""
		if resp != nil {
			status = resp.Status
		} else if err != nil {
			status = err.Error()
		}
		c.statusHandler.WatchTerminated(status)
	}()
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}

	c.statusHandler.Watching()

	// connection was established successfully. reset backoffPolicy and resync
	c.backoffPolicy.Reset()
	c.resync()

	dec := json.NewDecoder(resp.Body)
	defer resp.Body.Close()
	for {
		var e federationmodel.WatchEvent
		err := dec.Decode(&e)
		if err != nil {
			if err == io.EOF {
				return nil // server closed the connection gracefully
			}
			return err
		}
		eventCh <- &e
	}
}

func (c *Controller) resync() uint64 {
	c.storeLock.Lock()
	defer c.storeLock.Unlock()
	c.logger.Debugf("performing full resync")
	var err error
	var svcList *federationmodel.ServiceListMessage
	err = retry.UntilSuccess(func() error {
		svcList, err = c.pollServices()
		return err
	}, retry.Delay(time.Second), retry.Timeout(2*time.Minute))
	if err != nil {
		c.logger.Warnf("resync failed: %s", err)
		return 0
	}
	c.updateGateways(svcList)
	if svcList != nil {
		c.convertServices(svcList)
		c.statusHandler.FullSyncComplete()
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
//   - { "spiffe://cluster.local/bar@iam.gserviceaccount.com"}; when annotation is used on corresponding workloads.
//   - { "spiffe://cluster.local/ns/default/sa/foo" }; normal kubernetes cases
//   - { "spiffe://cluster.local/ns/default/sa/foo", "spiffe://trust-domain-alias/ns/default/sa/foo" };
//     if the trust domain alias is configured.
func (c *Controller) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	if svc == nil {
		return nil
	}
	c.storeLock.RLock()
	defer c.storeLock.RUnlock()
	// we don't have different workloads for different ports, so we just need to
	// return the SAs associated with our service.
	if ourSVC, ok := c.serviceStore[svc.Hostname]; ok {
		return append([]string(nil), ourSVC.ServiceAccounts...)
	}
	return nil
}
