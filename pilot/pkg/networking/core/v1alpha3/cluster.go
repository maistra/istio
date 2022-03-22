// Copyright Istio Authors
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

package v1alpha3

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	structpb "google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/gogo"
)

// defaultTransportSocketMatch applies to endpoints that have no security.istio.io/tlsMode label
// or those whose label value does not match "istio"
var defaultTransportSocketMatch = &cluster.Cluster_TransportSocketMatch{
	Name:  "tlsMode-disabled",
	Match: &structpb.Struct{},
	TransportSocket: &core.TransportSocket{
		Name: util.EnvoyRawBufferSocketName,
	},
}

// deltaConfigTypes are used to detect changes and trigger delta calculations. When config updates has ONLY entries
// in this map, then delta calculation is triggered.
var deltaConfigTypes = sets.NewSet(gvk.ServiceEntry.Kind)

// getDefaultCircuitBreakerThresholds returns a copy of the default circuit breaker thresholds for the given traffic direction.
func getDefaultCircuitBreakerThresholds() *cluster.CircuitBreakers_Thresholds {
	return &cluster.CircuitBreakers_Thresholds{
		// DefaultMaxRetries specifies the default for the Envoy circuit breaker parameter max_retries. This
		// defines the maximum number of parallel retries a given Envoy will allow to the upstream cluster. Envoy defaults
		// this value to 3, however that has shown to be insufficient during periods of pod churn (e.g. rolling updates),
		// where multiple endpoints in a cluster are terminated. In these scenarios the circuit breaker can kick
		// in before Pilot is able to deliver an updated endpoint list to Envoy, leading to client-facing 503s.
		MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxConnections:     &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
		TrackRemaining:     true,
	}
}

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
// For outbound: Cluster for each service/subset hostname or cidr with SNI set to service hostname
// Cluster type based on resolution
// For inbound (sidecar only): Cluster for each inbound endpoint port and for each service port
func (configgen *ConfigGeneratorImpl) BuildClusters(proxy *model.Proxy, req *model.PushRequest) ([]*discovery.Resource, model.XdsLogDetails) {
	// In Sotw, we care about all services.
	var services []*model.Service
	if features.FilterGatewayClusterConfig && proxy.Type == model.Router {
		services = req.Push.GatewayServices(proxy)
	} else {
		services = req.Push.Services(proxy)
	}
	return configgen.buildClusters(proxy, req, services)
}

// BuildDeltaClusters generates the deltas (add and delete) for a given proxy. Currently, only service changes are reflected with deltas.
// Otherwise, we fall back onto generating everything.
func (configgen *ConfigGeneratorImpl) BuildDeltaClusters(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource) ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
	// if we can't use delta, fall back to generate all
	if !shouldUseDelta(updates) {
		cl, lg := configgen.BuildClusters(proxy, updates)
		return cl, nil, lg, false
	}
	deletedClusters := make([]string, 0)
	services := make([]*model.Service, 0)
	// In delta, we only care about the services that have changed.
	for key := range updates.ConfigsUpdated {
		// get the service that has changed.
		service := updates.Push.ServiceForHostname(proxy, host.Name(key.Name))
		for _, n := range watched.ResourceNames {
			if isClusterForServiceRemoved(n, key.Name, service) {
				deletedClusters = append(deletedClusters, n)
			}
		}
		if service != nil {
			services = append(services, service)
		}
	}
	clusters, log := configgen.buildClusters(proxy, updates, services)
	return clusters, deletedClusters, log, true
}

func isClusterForServiceRemoved(cluster string, hostName string, svc *model.Service) bool {
	// WatchedResources.ResourceNames will contain the names of the clusters it is subscribed to. We can
	// check with the name of our service (cluster names are in the format outbound|<port>||<hostname>.
	_, _, svcHost, port := model.ParseSubsetKey(cluster)
	if svcHost == host.Name(hostName) {
		// if this service removed, we can conclude that it is a removed cluster.
		if svc == nil {
			return true
		}
		// if this service port is removed, we can conclude that it is a removed cluster.
		if _, exists := svc.Ports.GetByPort(port); !exists {
			return true
		}
	}
	return false
}

// buildClusters builds clusters for the proxy with the services passed.
func (configgen *ConfigGeneratorImpl) buildClusters(proxy *model.Proxy, req *model.PushRequest,
	services []*model.Service) ([]*discovery.Resource, model.XdsLogDetails) {
	clusters := make([]*cluster.Cluster, 0)
	resources := model.Resources{}
	envoyFilterPatches := req.Push.EnvoyFilters(proxy)
	cb := NewClusterBuilder(proxy, req, configgen.Cache)
	instances := proxy.ServiceInstances
	cacheStats := cacheStats{}
	switch proxy.Type {
	case model.SidecarProxy:
		// Setup outbound clusters
		outboundPatcher := clusterPatcher{efw: envoyFilterPatches, pctx: networking.EnvoyFilter_SIDECAR_OUTBOUND}
		ob, cs := configgen.buildOutboundClusters(cb, proxy, outboundPatcher, services)
		cacheStats = cacheStats.merge(cs)
		resources = append(resources, ob...)
		// Add a blackhole and passthrough cluster for catching traffic to unresolved routes
		clusters = outboundPatcher.conditionallyAppend(clusters, nil, cb.buildBlackHoleCluster(), cb.buildDefaultPassthroughCluster())
		clusters = append(clusters, outboundPatcher.insertedClusters()...)

		// Setup inbound clusters
		inboundPatcher := clusterPatcher{efw: envoyFilterPatches, pctx: networking.EnvoyFilter_SIDECAR_INBOUND}
		clusters = append(clusters, configgen.buildInboundClusters(cb, proxy, instances, inboundPatcher)...)
		// Pass through clusters for inbound traffic. These cluster bind loopback-ish src address to access node local service.
		clusters = inboundPatcher.conditionallyAppend(clusters, nil, cb.buildInboundPassthroughClusters()...)
		clusters = append(clusters, inboundPatcher.insertedClusters()...)
	default: // Gateways
		patcher := clusterPatcher{efw: envoyFilterPatches, pctx: networking.EnvoyFilter_GATEWAY}
		ob, cs := configgen.buildOutboundClusters(cb, proxy, patcher, services)
		cacheStats = cacheStats.merge(cs)
		resources = append(resources, ob...)
		// Gateways do not require the default passthrough cluster as they do not have original dst listeners.
		clusters = patcher.conditionallyAppend(clusters, nil, cb.buildBlackHoleCluster())
		if proxy.Type == model.Router && proxy.MergedGateway != nil && proxy.MergedGateway.ContainsAutoPassthroughGateways {
			clusters = append(clusters, configgen.buildOutboundSniDnatClusters(proxy, req, patcher)...)
		}
		clusters = append(clusters, patcher.insertedClusters()...)
	}

	for _, c := range clusters {
		resources = append(resources, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
	}
	resources = cb.normalizeClusters(resources)

	if cacheStats.empty() {
		return resources, model.DefaultXdsLogDetails
	}
	return resources, model.XdsLogDetails{AdditionalInfo: fmt.Sprintf("cached:%v/%v", cacheStats.hits, cacheStats.hits+cacheStats.miss)}
}

func shouldUseDelta(updates *model.PushRequest) bool {
	return updates != nil && allConfigKeysOfType(updates.ConfigsUpdated) && len(updates.ConfigsUpdated) > 0
}

func allConfigKeysOfType(cfgs map[model.ConfigKey]struct{}) bool {
	for k := range cfgs {
		if !deltaConfigTypes.Contains(k.Kind.Kind) {
			return false
		}
	}
	return true
}

type cacheStats struct {
	hits, miss int
}

func (c cacheStats) empty() bool {
	return c.hits == 0 && c.miss == 0
}

func (c cacheStats) merge(other cacheStats) cacheStats {
	return cacheStats{
		hits: c.hits + other.hits,
		miss: c.miss + other.miss,
	}
}

func buildClusterKey(service *model.Service, port *model.Port, cb *ClusterBuilder, proxy *model.Proxy, efKeys []string) *clusterCache {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
	clusterKey := &clusterCache{
		clusterName:     clusterName,
		proxyVersion:    cb.proxyVersion,
		locality:        cb.locality,
		proxyClusterID:  cb.clusterID,
		proxySidecar:    cb.sidecarProxy(),
		networkView:     cb.networkView,
		http2:           port.Protocol.IsHTTP2(),
		downstreamAuto:  cb.sidecarProxy() && util.IsProtocolSniffingEnabledForOutboundPort(port),
		service:         service,
		destinationRule: cb.req.Push.DestinationRule(proxy, service),
		envoyFilterKeys: efKeys,
		metadataCerts:   cb.metadataCerts,
		peerAuthVersion: cb.req.Push.AuthnPolicies.GetVersion(),
		serviceAccounts: cb.req.Push.ServiceAccounts[service.Hostname][port.Port],
	}
	return clusterKey
}

// buildOutboundClusters generates all outbound (including subsets) clusters for a given proxy.
func (configgen *ConfigGeneratorImpl) buildOutboundClusters(cb *ClusterBuilder, proxy *model.Proxy, cp clusterPatcher,
	services []*model.Service) ([]*discovery.Resource, cacheStats) {
	resources := make([]*discovery.Resource, 0)
	efKeys := cp.efw.Keys()
	hit, miss := 0, 0
	for _, service := range services {
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			clusterKey := buildClusterKey(service, port, cb, proxy, efKeys)
			cached, allFound := cb.getAllCachedSubsetClusters(*clusterKey)
			if allFound && !features.EnableUnsafeAssertions {
				hit += len(cached)
				resources = append(resources, cached...)
				continue
			} else {
				miss += len(cached)
			}

			// We have a cache miss, so we will re-generate the cluster and later store it in the cache.
			lbEndpoints := cb.buildLocalityLbEndpoints(clusterKey.networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(cb.proxyType, service)
			defaultCluster := cb.buildDefaultCluster(clusterKey.clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, port, service, nil)
			if defaultCluster == nil {
				continue
			}
			// If stat name is configured, build the alternate stats name.
			if len(cb.req.Push.Mesh.OutboundClusterStatName) != 0 {
				defaultCluster.cluster.AltStatName = util.BuildStatPrefix(cb.req.Push.Mesh.OutboundClusterStatName,
					string(service.Hostname), "", port, &service.Attributes)
			}

			subsetClusters := cb.applyDestinationRule(defaultCluster, DefaultClusterMode, service, port,
				clusterKey.networkView, clusterKey.destinationRule, clusterKey.serviceAccounts)

			if patched := cp.applyResource(nil, defaultCluster.build()); patched != nil {
				resources = append(resources, patched)
				if features.EnableCDSCaching {
					cb.cache.Add(clusterKey, cb.req, patched)
				}
			}
			for _, ss := range subsetClusters {
				if patched := cp.applyResource(nil, ss); patched != nil {
					nk := *clusterKey
					nk.clusterName = ss.Name
					resources = append(resources, patched)
					if features.EnableCDSCaching {
						cb.cache.Add(&nk, cb.req, patched)
					}
				}
			}
		}
	}

	return resources, cacheStats{hits: hit, miss: miss}
}

type clusterPatcher struct {
	efw  *model.EnvoyFilterWrapper
	pctx networking.EnvoyFilter_PatchContext
}

func (p clusterPatcher) applyResource(hosts []host.Name, c *cluster.Cluster) *discovery.Resource {
	cluster := p.apply(hosts, c)
	if cluster == nil {
		return nil
	}
	return &discovery.Resource{Name: cluster.Name, Resource: util.MessageToAny(cluster)}
}

func (p clusterPatcher) apply(hosts []host.Name, c *cluster.Cluster) *cluster.Cluster {
	if !envoyfilter.ShouldKeepCluster(p.pctx, p.efw, c, hosts) {
		return nil
	}
	return envoyfilter.ApplyClusterMerge(p.pctx, p.efw, c, hosts)
}

func (p clusterPatcher) conditionallyAppend(l []*cluster.Cluster, hosts []host.Name, clusters ...*cluster.Cluster) []*cluster.Cluster {
	if !p.hasPatches() {
		return append(l, clusters...)
	}
	for _, c := range clusters {
		if patched := p.apply(hosts, c); patched != nil {
			l = append(l, patched)
		}
	}
	return l
}

func (p clusterPatcher) insertedClusters() []*cluster.Cluster {
	return envoyfilter.InsertedClusters(p.pctx, p.efw)
}

func (p clusterPatcher) hasPatches() bool {
	return p.efw != nil && len(p.efw.Patches[networking.EnvoyFilter_CLUSTER]) > 0
}

// SniDnat clusters do not have any TLS setting, as they simply forward traffic to upstream
// All SniDnat clusters are internal services in the mesh.
// TODO enable cache - there is no blockers here, skipped to simplify the original caching implementation
func (configgen *ConfigGeneratorImpl) buildOutboundSniDnatClusters(proxy *model.Proxy, req *model.PushRequest,
	cp clusterPatcher) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	cb := NewClusterBuilder(proxy, req, nil)

	networkView := proxy.GetNetworkView()

	for _, service := range req.Push.Services(proxy) {
		if service.MeshExternal {
			continue
		}
		destRule := cb.req.Push.DestinationRule(proxy, service)
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			lbEndpoints := cb.buildLocalityLbEndpoints(networkView, service, port.Port, nil)

			// create default cluster
			discoveryType := convertResolution(cb.proxyType, service)

			clusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "",
				service.Hostname, port.Port)
			defaultCluster := cb.buildDefaultCluster(clusterName, discoveryType, lbEndpoints, model.TrafficDirectionOutbound, port, service, nil)
			if defaultCluster == nil {
				continue
			}
			subsetClusters := cb.applyDestinationRule(defaultCluster, SniDnatClusterMode, service, port, networkView, destRule, nil)
			clusters = cp.conditionallyAppend(clusters, nil, defaultCluster.build())
			clusters = cp.conditionallyAppend(clusters, nil, subsetClusters...)
		}
	}

	return clusters
}

func buildInboundLocalityLbEndpoints(bind string, port uint32) []*endpoint.LocalityLbEndpoints {
	if bind == "" {
		return nil
	}
	address := util.BuildAddress(bind, port)
	lbEndpoint := &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: address,
			},
		},
	}
	return []*endpoint.LocalityLbEndpoints{
		{
			LbEndpoints: []*endpoint.LbEndpoint{lbEndpoint},
		},
	}
}

func (configgen *ConfigGeneratorImpl) buildInboundClusters(cb *ClusterBuilder, proxy *model.Proxy, instances []*model.ServiceInstance,
	cp clusterPatcher) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)

	// The inbound clusters for a node depends on whether the node has a SidecarScope with inbound listeners
	// or not. If the node has a sidecarscope with ingress listeners, we only return clusters corresponding
	// to those listeners i.e. clusters made out of the defaultEndpoint field.
	// If the node has no sidecarScope and has interception mode set to NONE, then we should skip the inbound
	// clusters, because there would be no corresponding inbound listeners
	sidecarScope := proxy.SidecarScope
	noneMode := proxy.GetInterceptionMode() == model.InterceptionNone

	_, actualLocalHost := getActualWildcardAndLocalHost(proxy)

	// No user supplied sidecar scope or the user supplied one has no ingress listeners
	if !sidecarScope.HasIngressListener() {
		// We should not create inbound listeners in NONE mode based on the service instances
		// Doing so will prevent the workloads from starting as they would be listening on the same port
		// Users are required to provide the sidecar config to define the inbound listeners
		if noneMode {
			return nil
		}

		clustersToBuild := make(map[int][]*model.ServiceInstance)
		for _, instance := range instances {
			// For service instances with the same port,
			// we still need to capture all the instances on this port, as its required to populate telemetry metadata
			// The first instance will be used as the "primary" instance; this means if we have an conflicts between
			// Services the first one wins
			ep := int(instance.Endpoint.EndpointPort)
			clustersToBuild[ep] = append(clustersToBuild[ep], instance)
		}

		bind := actualLocalHost
		if features.EnableInboundPassthrough {
			bind = ""
		}
		// For each workload port, we will construct a cluster
		for epPort, instances := range clustersToBuild {
			// The inbound cluster port equals to endpoint port.
			localCluster := cb.buildInboundClusterForPortOrUDS(epPort, bind, proxy, instances[0], instances)
			// If inbound cluster match has service, we should see if it matches with any host name across all instances.
			hosts := make([]host.Name, 0, len(instances))
			for _, si := range instances {
				hosts = append(hosts, si.Service.Hostname)
			}
			clusters = cp.conditionallyAppend(clusters, hosts, localCluster.build())
		}
		return clusters
	}

	for _, ingressListener := range sidecarScope.Sidecar.Ingress {
		// LDS would have setup the inbound clusters
		// as inbound|portNumber|portName|Hostname[or]SidecarScopeID
		listenPort := &model.Port{
			Port:     int(ingressListener.Port.Number),
			Protocol: protocol.Parse(ingressListener.Port.Protocol),
			Name:     ingressListener.Port.Name,
		}

		// Set up the endpoint. By default, we set this empty which will use ORIGINAL_DST passthrough.
		// This can be overridden by ingress.defaultEndpoint.
		// * 127.0.0.1: send to localhost
		// * 0.0.0.0: send to INSTANCE_IP
		// * unix:///...: send to configured unix domain socket
		endpointAddress := ""
		port := 0
		if strings.HasPrefix(ingressListener.DefaultEndpoint, model.UnixAddressPrefix) {
			// this is a UDS endpoint. assign it as is
			endpointAddress = ingressListener.DefaultEndpoint
		} else if len(ingressListener.DefaultEndpoint) > 0 {
			// parse the ip, port. Validation guarantees presence of :
			parts := strings.Split(ingressListener.DefaultEndpoint, ":")
			if len(parts) < 2 {
				continue
			}
			var err error
			if port, err = strconv.Atoi(parts[1]); err != nil {
				continue
			}
			if parts[0] == model.PodIPAddressPrefix {
				endpointAddress = cb.proxyIPAddresses[0]
			} else if parts[0] == model.LocalhostAddressPrefix {
				endpointAddress = actualLocalHost
			}
		}

		// Find the service instance that corresponds to this ingress listener by looking
		// for a service instance that matches this ingress port as this will allow us
		// to generate the right cluster name that LDS expects inbound|portNumber|portName|Hostname
		instance := configgen.findOrCreateServiceInstance(instances, ingressListener, sidecarScope.Name, sidecarScope.Namespace)
		instance.Endpoint.Address = endpointAddress
		instance.ServicePort = listenPort
		instance.Endpoint.ServicePortName = listenPort.Name
		instance.Endpoint.EndpointPort = uint32(port)

		localCluster := cb.buildInboundClusterForPortOrUDS(int(ingressListener.Port.Number), endpointAddress, proxy, instance, nil)
		clusters = cp.conditionallyAppend(clusters, []host.Name{instance.Service.Hostname}, localCluster.build())
	}

	return clusters
}

func (configgen *ConfigGeneratorImpl) findOrCreateServiceInstance(instances []*model.ServiceInstance,
	ingressListener *networking.IstioIngressListener, sidecar string, sidecarns string) *model.ServiceInstance {
	for _, realInstance := range instances {
		if realInstance.Endpoint.EndpointPort == ingressListener.Port.Number {
			// We need to create a copy of the instance, as it is modified later while building clusters/listeners.
			return realInstance.DeepCopy()
		}
	}
	// We didn't find a matching instance. Create a dummy one because we need the right
	// params to generate the right cluster name i.e. inbound|portNumber|portName|SidecarScopeID - which is uniformly generated by LDS/CDS.
	return &model.ServiceInstance{
		Service: &model.Service{
			Hostname: host.Name(sidecar + "." + sidecarns),
			Attributes: model.ServiceAttributes{
				Name: sidecar,
				// This will ensure that the right AuthN policies are selected
				Namespace: sidecarns,
			},
		},
		Endpoint: &model.IstioEndpoint{
			EndpointPort: ingressListener.Port.Number,
		},
	}
}

func convertResolution(proxyType model.NodeType, service *model.Service) cluster.Cluster_DiscoveryType {
	switch service.Resolution {
	case model.ClientSideLB:
		return cluster.Cluster_EDS
	case model.DNSLB:
		return cluster.Cluster_STRICT_DNS
	case model.DNSRoundRobinLB:
		return cluster.Cluster_LOGICAL_DNS
	case model.Passthrough:
		// Gateways cannot use passthrough clusters. So fallback to EDS
		if proxyType == model.SidecarProxy {
			if service.Attributes.ServiceRegistry == provider.Kubernetes && features.EnableEDSForHeadless {
				return cluster.Cluster_EDS
			}

			return cluster.Cluster_ORIGINAL_DST
		}
		return cluster.Cluster_EDS
	default:
		return cluster.Cluster_EDS
	}
}

// SelectTrafficPolicyComponents returns the components of TrafficPolicy that should be used for given port.
func selectTrafficPolicyComponents(policy *networking.TrafficPolicy) (
	*networking.ConnectionPoolSettings, *networking.OutlierDetection, *networking.LoadBalancerSettings, *networking.ClientTLSSettings) {
	if policy == nil {
		return nil, nil, nil, nil
	}
	connectionPool := policy.ConnectionPool
	outlierDetection := policy.OutlierDetection
	loadBalancer := policy.LoadBalancer
	tls := policy.Tls

	// Check if CA Certificate should be System CA Certificate
	if features.VerifyCertAtClient && tls != nil && tls.CaCertificates == "" {
		tls.CaCertificates = "system"
	}

	return connectionPool, outlierDetection, loadBalancer, tls
}

// ClusterMode defines whether the cluster is being built for SNI-DNATing (sni passthrough) or not
type ClusterMode string

const (
	// SniDnatClusterMode indicates cluster is being built for SNI dnat mode
	SniDnatClusterMode ClusterMode = "sni-dnat"
	// DefaultClusterMode indicates usual cluster with mTLS et al
	DefaultClusterMode ClusterMode = "outbound"
)

type buildClusterOpts struct {
	mesh             *meshconfig.MeshConfig
	mutable          *MutableCluster
	policy           *networking.TrafficPolicy
	port             *model.Port
	serviceAccounts  []string
	serviceInstances []*model.ServiceInstance
	// Used for traffic across multiple Istio clusters
	// the ingress gateway in a remote cluster will use this value to route
	// traffic to the appropriate service
	istioMtlsSni string
	// This is used when the sidecar is sending simple TLS traffic
	// to endpoints. This is different from the previous SNI
	// because usually in this case the traffic is going to a
	// non-sidecar workload that can only understand the service's
	// hostname in the SNI.
	simpleTLSSni    string
	clusterMode     ClusterMode
	direction       model.TrafficDirection
	meshExternal    bool
	serviceMTLSMode model.MutualTLSMode
	// Indicates the service registry of the cluster being built.
	serviceRegistry provider.ID
	cache           model.XdsCache
}

type upgradeTuple struct {
	meshdefault meshconfig.MeshConfig_H2UpgradePolicy
	override    networking.ConnectionPoolSettings_HTTPSettings_H2UpgradePolicy
}

func applyTCPKeepalive(mesh *meshconfig.MeshConfig, c *cluster.Cluster, settings *networking.ConnectionPoolSettings) {
	// Apply Keepalive config only if it is configured in mesh config or in destination rule.
	if mesh.TcpKeepalive != nil || settings.Tcp.TcpKeepalive != nil {

		// Start with empty tcp_keepalive, which would set SO_KEEPALIVE on the socket with OS default values.
		c.UpstreamConnectionOptions = &cluster.UpstreamConnectionOptions{
			TcpKeepalive: &core.TcpKeepalive{},
		}

		// Apply mesh wide TCP keepalive if available.
		if mesh.TcpKeepalive != nil {
			setKeepAliveSettings(c, mesh.TcpKeepalive)
		}

		// Apply/Override individual attributes with DestinationRule TCP keepalive if set.
		if settings.Tcp.TcpKeepalive != nil {
			setKeepAliveSettings(c, settings.Tcp.TcpKeepalive)
		}
	}
}

func setKeepAliveSettings(cluster *cluster.Cluster, keepalive *networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive) {
	if keepalive.Probes > 0 {
		cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveProbes = &wrappers.UInt32Value{Value: keepalive.Probes}
	}

	if keepalive.Time != nil {
		cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveTime = &wrappers.UInt32Value{Value: uint32(keepalive.Time.Seconds)}
	}

	if keepalive.Interval != nil {
		cluster.UpstreamConnectionOptions.TcpKeepalive.KeepaliveInterval = &wrappers.UInt32Value{Value: uint32(keepalive.Interval.Seconds)}
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyOutlierDetection(c *cluster.Cluster, outlier *networking.OutlierDetection) {
	if outlier == nil {
		return
	}

	out := &cluster.OutlierDetection{}

	// SuccessRate based outlier detection should be disabled.
	out.EnforcingSuccessRate = &wrappers.UInt32Value{Value: 0}

	if e := outlier.Consecutive_5XxErrors; e != nil {
		v := e.GetValue()

		out.Consecutive_5Xx = &wrappers.UInt32Value{Value: v}

		if v > 0 {
			v = 100
		}
		out.EnforcingConsecutive_5Xx = &wrappers.UInt32Value{Value: v}
	}
	if e := outlier.ConsecutiveGatewayErrors; e != nil {
		v := e.GetValue()

		out.ConsecutiveGatewayFailure = &wrappers.UInt32Value{Value: v}

		if v > 0 {
			v = 100
		}
		out.EnforcingConsecutiveGatewayFailure = &wrappers.UInt32Value{Value: v}
	}

	if outlier.Interval != nil {
		out.Interval = gogo.DurationToProtoDuration(outlier.Interval)
	}
	if outlier.BaseEjectionTime != nil {
		out.BaseEjectionTime = gogo.DurationToProtoDuration(outlier.BaseEjectionTime)
	}
	if outlier.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &wrappers.UInt32Value{Value: uint32(outlier.MaxEjectionPercent)}
	}

	if outlier.SplitExternalLocalOriginErrors {
		out.SplitExternalLocalOriginErrors = true
		if outlier.ConsecutiveLocalOriginFailures.GetValue() > 0 {
			out.ConsecutiveLocalOriginFailure = &wrappers.UInt32Value{Value: outlier.ConsecutiveLocalOriginFailures.Value}
			out.EnforcingConsecutiveLocalOriginFailure = &wrappers.UInt32Value{Value: 100}
		}
		// SuccessRate based outlier detection should be disabled.
		out.EnforcingLocalOriginSuccessRate = &wrappers.UInt32Value{Value: 0}
	}

	c.OutlierDetection = out

	// Disable panic threshold by default as its not typically applicable in k8s environments
	// with few pods per service.
	// To do so, set the healthy_panic_threshold field even if its value is 0 (defaults to 50).
	// FIXME: we can't distinguish between it being unset or being explicitly set to 0
	if outlier.MinHealthPercent >= 0 {
		if c.CommonLbConfig == nil {
			c.CommonLbConfig = &cluster.Cluster_CommonLbConfig{}
		}
		c.CommonLbConfig.HealthyPanicThreshold = &xdstype.Percent{Value: float64(outlier.MinHealthPercent)} // defaults to 50
	}
}

func applyLoadBalancer(c *cluster.Cluster, lb *networking.LoadBalancerSettings, port *model.Port,
	locality *core.Locality, proxyLabels map[string]string, meshConfig *meshconfig.MeshConfig) {
	localityLbSetting := loadbalancer.GetLocalityLbSetting(meshConfig.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
	if localityLbSetting != nil {
		if c.CommonLbConfig == nil {
			c.CommonLbConfig = &cluster.Cluster_CommonLbConfig{}
		}
		c.CommonLbConfig.LocalityConfigSpecifier = &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
			LocalityWeightedLbConfig: &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
		}
	}

	// Use locality lb settings from load balancer settings if present, else use mesh wide locality lb settings
	applyLocalityLBSetting(locality, proxyLabels, c, localityLbSetting)

	// apply default round robin lb policy
	c.LbPolicy = cluster.Cluster_ROUND_ROBIN
	if c.GetType() == cluster.Cluster_ORIGINAL_DST {
		c.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
		return
	}

	// Redis protocol must be defaulted with MAGLEV to benefit from client side sharding.
	if features.EnableRedisFilter && port != nil && port.Protocol == protocol.Redis {
		c.LbPolicy = cluster.Cluster_MAGLEV
		return
	}

	if lb == nil {
		return
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).
	switch lb.GetSimple() {
	case networking.LoadBalancerSettings_LEAST_CONN:
		c.LbPolicy = cluster.Cluster_LEAST_REQUEST
	case networking.LoadBalancerSettings_RANDOM:
		c.LbPolicy = cluster.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		c.LbPolicy = cluster.Cluster_ROUND_ROBIN
	case networking.LoadBalancerSettings_PASSTHROUGH:
		c.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
		c.ClusterDiscoveryType = &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST}
	}

	ApplyRingHashLoadBalancer(c, lb)
}

// ApplyRingHashLoadBalancer will set the LbPolicy and create an LbConfig for RING_HASH if  used in LoadBalancerSettings
func ApplyRingHashLoadBalancer(c *cluster.Cluster, lb *networking.LoadBalancerSettings) {
	consistentHash := lb.GetConsistentHash()
	if consistentHash == nil {
		return
	}

	// TODO MinimumRingSize is an int, and zero could potentially be a valid value
	// unable to distinguish between set and unset case currently GregHanson
	// 1024 is the default value for envoy
	minRingSize := &wrappers.UInt64Value{Value: 1024}
	if consistentHash.MinimumRingSize != 0 {
		minRingSize = &wrappers.UInt64Value{Value: consistentHash.GetMinimumRingSize()}
	}
	c.LbPolicy = cluster.Cluster_RING_HASH
	c.LbConfig = &cluster.Cluster_RingHashLbConfig_{
		RingHashLbConfig: &cluster.Cluster_RingHashLbConfig{
			MinimumRingSize: minRingSize,
		},
	}
}

func applyLocalityLBSetting(locality *core.Locality, proxyLabels map[string]string, cluster *cluster.Cluster,
	localityLB *networking.LocalityLoadBalancerSetting) {
	// Failover should only be applied with outlier detection, or traffic will never failover.
	enabledFailover := cluster.OutlierDetection != nil
	if cluster.LoadAssignment != nil {
		// TODO: enable failoverPriority for `STRICT_DNS` cluster type
		loadbalancer.ApplyLocalityLBSetting(cluster.LoadAssignment, nil, locality, proxyLabels, localityLB, enabledFailover)
	}
}

func addTelemetryMetadata(opts buildClusterOpts, service *model.Service, direction model.TrafficDirection, instances []*model.ServiceInstance) {
	if !features.EnableTelemetryLabel {
		return
	}
	if opts.mutable.cluster == nil {
		return
	}
	if direction == model.TrafficDirectionInbound && (opts.serviceInstances == nil ||
		len(opts.serviceInstances) == 0 || opts.port == nil) {
		// At inbound, port and local service instance has to be provided
		return
	}
	if direction == model.TrafficDirectionOutbound && service == nil {
		// At outbound, the service corresponding to the cluster has to be provided.
		return
	}

	im := getOrCreateIstioMetadata(opts.mutable.cluster)

	// Add services field into istio metadata
	im.Fields["services"] = &structpb.Value{
		Kind: &structpb.Value_ListValue{
			ListValue: &structpb.ListValue{
				Values: []*structpb.Value{},
			},
		},
	}

	svcMetaList := im.Fields["services"].GetListValue()

	// Add service related metadata. This will be consumed by telemetry v2 filter for metric labels.
	if direction == model.TrafficDirectionInbound {
		// For inbound cluster, add all services on the cluster port
		have := make(map[host.Name]bool)
		for _, svc := range instances {
			if svc.ServicePort.Port != opts.port.Port {
				// If the service port is different from the the port of the cluster that is being built,
				// skip adding telemetry metadata for the service to the cluster.
				continue
			}
			if _, ok := have[svc.Service.Hostname]; ok {
				// Skip adding metadata for instance with the same host name.
				// This could happen when a service has multiple IPs.
				continue
			}
			svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(svc.Service))
			have[svc.Service.Hostname] = true
		}
	} else if direction == model.TrafficDirectionOutbound {
		// For outbound cluster, add telemetry metadata based on the service that the cluster is built for.
		svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(service))
	}
}

// Insert the original port into the istio metadata. The port is used in BTS delivered from client sidecar to server sidecar.
// Server side car uses this port after de-multiplexed from tunnel.
func addNetworkingMetadata(opts buildClusterOpts, service *model.Service, direction model.TrafficDirection) {
	if opts.mutable == nil || direction == model.TrafficDirectionInbound {
		return
	}
	if service == nil {
		// At outbound, the service corresponding to the cluster has to be provided.
		return
	}

	if port, ok := service.Ports.GetByPort(opts.port.Port); ok {
		im := getOrCreateIstioMetadata(opts.mutable.cluster)

		// Add original_port field into istio metadata
		// Endpoint could override this port but the chance should be small.
		im.Fields["default_original_port"] = &structpb.Value{
			Kind: &structpb.Value_NumberValue{
				NumberValue: float64(port.Port),
			},
		}
	}
}

// Build a struct which contains service metadata and will be added into cluster label.
func buildServiceMetadata(svc *model.Service) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_StructValue{
			StructValue: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					// service fqdn
					"host": {
						Kind: &structpb.Value_StringValue{
							StringValue: string(svc.Hostname),
						},
					},
					// short name of the service
					"name": {
						Kind: &structpb.Value_StringValue{
							StringValue: svc.Attributes.Name,
						},
					},
					// namespace of the service
					"namespace": {
						Kind: &structpb.Value_StringValue{
							StringValue: svc.Attributes.Namespace,
						},
					},
				},
			},
		},
	}
}

func getOrCreateIstioMetadata(cluster *cluster.Cluster) *structpb.Struct {
	if cluster.Metadata == nil {
		cluster.Metadata = &core.Metadata{
			FilterMetadata: map[string]*structpb.Struct{},
		}
	}
	// Create Istio metadata if does not exist yet
	if _, ok := cluster.Metadata.FilterMetadata[util.IstioMetadataKey]; !ok {
		cluster.Metadata.FilterMetadata[util.IstioMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{},
		}
	}
	return cluster.Metadata.FilterMetadata[util.IstioMetadataKey]
}
