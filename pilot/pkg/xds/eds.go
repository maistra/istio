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

package xds

import (
	"fmt"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	any "google.golang.org/protobuf/types/known/anypb"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	networking "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/sets"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
)

// UpdateServiceShards will list the endpoints and create the shards.
// This is used to reconcile and to support non-k8s registries (until they migrate).
// Note that aggregated list is expensive (for large numbers) - we want to replace
// it with a model where DiscoveryServer keeps track of all endpoint registries
// directly, and calls them one by one.
func (s *DiscoveryServer) UpdateServiceShards(push *model.PushContext) error {
	registries := s.getNonK8sRegistries()
	// Short circuit now to avoid the call to Services
	if len(registries) == 0 {
		return nil
	}
	// Each registry acts as a shard - we don't want to combine them because some
	// may individually update their endpoints incrementally
	for _, svc := range push.Services(nil) {
		for _, registry := range registries {
			// skip the service in case this svc does not belong to the registry.
			if svc.Attributes.ServiceRegistry != registry.Provider() {
				continue
			}
			endpoints := make([]*model.IstioEndpoint, 0)
			for _, port := range svc.Ports {
				if port.Protocol == protocol.UDP {
					continue
				}

				// This loses track of grouping (shards)
				for _, inst := range registry.InstancesByPort(svc, port.Port, labels.Collection{}) {
					endpoints = append(endpoints, inst.Endpoint)
				}
			}
			shard := model.ShardKeyFromRegistry(registry)
			s.edsCacheUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, endpoints)
		}
	}

	return nil
}

// SvcUpdate is a callback from service discovery when service info changes.
func (s *DiscoveryServer) SvcUpdate(shard model.ShardKey, hostname string, namespace string, event model.Event) {
	// When a service deleted, we should cleanup the endpoint shards and also remove keys from EndpointShardsByService to
	// prevent memory leaks.
	if event == model.EventDelete {
		inboundServiceDeletes.Increment()
		s.deleteService(shard, hostname, namespace)
	} else {
		inboundServiceUpdates.Increment()
	}
}

// EDSUpdate computes destination address membership across all clusters and networks.
// This is the main method implementing EDS.
// It replaces InstancesByPort in model - instead of iterating over all endpoints it uses
// the hostname-keyed map. And it avoids the conversion from Endpoint to ServiceEntry to envoy
// on each step: instead the conversion happens once, when an endpoint is first discovered.
func (s *DiscoveryServer) EDSUpdate(shard model.ShardKey, serviceName string, namespace string,
	istioEndpoints []*model.IstioEndpoint) {
	inboundEDSUpdates.Increment()
	// Update the endpoint shards
	fp := s.edsCacheUpdate(shard, serviceName, namespace, istioEndpoints)
	// Trigger a push
	s.ConfigUpdate(&model.PushRequest{
		Full: fp,
		ConfigsUpdated: map[model.ConfigKey]struct{}{{
			Kind:      gvk.ServiceEntry,
			Name:      serviceName,
			Namespace: namespace,
		}: {}},
		Reason: []model.TriggerReason{model.EndpointUpdate},
	})
}

// EDSCacheUpdate computes destination address membership across all clusters and networks.
// This is the main method implementing EDS.
// It replaces InstancesByPort in model - instead of iterating over all endpoints it uses
// the hostname-keyed map. And it avoids the conversion from Endpoint to ServiceEntry to envoy
// on each step: instead the conversion happens once, when an endpoint is first discovered.
//
// Note: the difference with `EDSUpdate` is that it only update the cache rather than requesting a push
func (s *DiscoveryServer) EDSCacheUpdate(shard model.ShardKey, serviceName string, namespace string,
	istioEndpoints []*model.IstioEndpoint) {
	inboundEDSUpdates.Increment()
	// Update the endpoint shards
	s.edsCacheUpdate(shard, serviceName, namespace, istioEndpoints)
}

// edsCacheUpdate updates EndpointShards data by clusterID, hostname, IstioEndpoints.
// It also tracks the changes to ServiceAccounts. It returns whether a full push
// is needed or incremental push is sufficient.
func (s *DiscoveryServer) edsCacheUpdate(shard model.ShardKey, hostname string, namespace string, istioEndpoints []*model.IstioEndpoint) bool {
	if len(istioEndpoints) == 0 {
		// Should delete the service EndpointShards when endpoints become zero to prevent memory leak,
		// but we should not delete the keys from EndpointShardsByService map - that will trigger
		// unnecessary full push which can become a real problem if a pod is in crashloop and thus endpoints
		// flip flopping between 1 and 0.
		s.deleteEndpointShards(shard, hostname, namespace)
		log.Infof("Incremental push, service %s at shard %v has no endpoints", hostname, shard)
		return false
	}

	fullPush := false
	// Find endpoint shard for this service, if it is available - otherwise create a new one.
	ep, created := s.getOrCreateEndpointShard(hostname, namespace)
	// If we create a new endpoint shard, that means we have not seen the service earlier. We should do a full push.
	if created {
		log.Infof("Full push, new service %s/%s", namespace, hostname)
		fullPush = true
	}

	ep.mutex.Lock()
	ep.Shards[shard] = istioEndpoints
	// Check if ServiceAccounts have changed. We should do a full push if they have changed.
	saUpdated := s.UpdateServiceAccount(ep, hostname)
	// Clear the cache here. While it would likely be cleared later when we trigger a push, a race
	// condition is introduced where an XDS response may be generated before the update, but not
	// completed until after a response after the update. Essentially, we transition from v0 -> v1 ->
	// v0 -> invalidate -> v1. Reverting a change we pushed violates our contract of monotonically
	// moving forward in version. In practice, this is pretty rare and self corrects nearly
	// immediately. However, clearing the cache here has almost no impact on cache performance as we
	// would clear it shortly after anyways.
	s.Cache.Clear(map[model.ConfigKey]struct{}{{
		Kind:      gvk.ServiceEntry,
		Name:      hostname,
		Namespace: namespace,
	}: {}})
	ep.mutex.Unlock()

	// For existing endpoints, we need to do full push if service accounts change.
	if saUpdated {
		log.Infof("Full push, service accounts changed, %v", hostname)
		fullPush = true
	}
	return fullPush
}

func (s *DiscoveryServer) getOrCreateEndpointShard(serviceName, namespace string) (*EndpointShards, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.EndpointShardsByService[serviceName]; !exists {
		s.EndpointShardsByService[serviceName] = map[string]*EndpointShards{}
	}
	if ep, exists := s.EndpointShardsByService[serviceName][namespace]; exists {
		return ep, false
	}
	// This endpoint is for a service that was not previously loaded.
	ep := &EndpointShards{
		Shards:          map[model.ShardKey][]*model.IstioEndpoint{},
		ServiceAccounts: sets.Set{},
	}
	s.EndpointShardsByService[serviceName][namespace] = ep
	// Clear the cache here to avoid race in cache writes (see edsCacheUpdate for details).
	s.Cache.Clear(map[model.ConfigKey]struct{}{{
		Kind:      gvk.ServiceEntry,
		Name:      serviceName,
		Namespace: namespace,
	}: {}})
	return ep, true
}

// deleteEndpointShards deletes matching endpoint shards from EndpointShardsByService map. This is called when
// endpoints are deleted.
func (s *DiscoveryServer) deleteEndpointShards(shard model.ShardKey, serviceName, namespace string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.EndpointShardsByService[serviceName] != nil &&
		s.EndpointShardsByService[serviceName][namespace] != nil {
		epShards := s.EndpointShardsByService[serviceName][namespace]
		epShards.mutex.Lock()
		delete(epShards.Shards, shard)
		epShards.ServiceAccounts = sets.Set{}
		for _, shard := range epShards.Shards {
			for _, ep := range shard {
				if ep.ServiceAccount != "" {
					epShards.ServiceAccounts.Insert(ep.ServiceAccount)
				}
			}
		}
		// Clear the cache here to avoid race in cache writes (see edsCacheUpdate for details).
		s.Cache.Clear(map[model.ConfigKey]struct{}{{
			Kind:      gvk.ServiceEntry,
			Name:      serviceName,
			Namespace: namespace,
		}: {}})
		epShards.mutex.Unlock()
	}
}

// deleteService deletes all service related references from EndpointShardsByService. This is called
// when a service is deleted.
func (s *DiscoveryServer) deleteService(shard model.ShardKey, serviceName, namespace string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.EndpointShardsByService[serviceName] != nil &&
		s.EndpointShardsByService[serviceName][namespace] != nil {
		epShards := s.EndpointShardsByService[serviceName][namespace]
		epShards.mutex.Lock()
		delete(epShards.Shards, shard)
		shardsLen := len(epShards.Shards)
		s.UpdateServiceAccount(epShards, serviceName)
		// Clear the cache here to avoid race in cache writes (see edsCacheUpdate for details).
		s.Cache.Clear(map[model.ConfigKey]struct{}{{
			Kind:      gvk.ServiceEntry,
			Name:      serviceName,
			Namespace: namespace,
		}: {}})
		epShards.mutex.Unlock()
		if shardsLen == 0 {
			delete(s.EndpointShardsByService[serviceName], namespace)
		}
		if len(s.EndpointShardsByService[serviceName]) == 0 {
			delete(s.EndpointShardsByService, serviceName)
		}
	}
}

// UpdateServiceAccount updates the service endpoints' sa when service/endpoint event happens.
// Note: it is not concurrent safe.
func (s *DiscoveryServer) UpdateServiceAccount(shards *EndpointShards, serviceName string) bool {
	oldServiceAccount := shards.ServiceAccounts
	serviceAccounts := sets.Set{}
	for _, epShards := range shards.Shards {
		for _, ep := range epShards {
			if ep.ServiceAccount != "" {
				serviceAccounts.Insert(ep.ServiceAccount)
			}
		}
	}

	if !oldServiceAccount.Equals(serviceAccounts) {
		shards.ServiceAccounts = serviceAccounts
		log.Debugf("Updating service accounts now, svc %v, before service account %v, after %v",
			serviceName, oldServiceAccount, serviceAccounts)
		return true
	}

	return false
}

// llbEndpointAndOptionsForCluster return the endpoints for a cluster
// Initial implementation is computing the endpoints on the flight - caching will be added as needed, based on
// perf tests.
func (s *DiscoveryServer) llbEndpointAndOptionsForCluster(b EndpointBuilder) ([]*LocLbEndpointsAndOptions, error) {
	if b.service == nil {
		// Shouldn't happen here
		log.Debugf("can not find the service for cluster %s", b.clusterName)
		return nil, nil
	}

	// Service resolution type might have changed and Cluster may be still in the EDS cluster list of "Connection.Clusters".
	// This can happen if a ServiceEntry's resolution is changed from STATIC to DNS which changes the Envoy cluster type from
	// EDS to STRICT_DNS or LOGICAL_DNS. When pushEds is called before Envoy sends the updated cluster list via Endpoint request which in turn
	// will update "Connection.Clusters", we might accidentally send EDS updates for STRICT_DNS cluster. This check guards
	// against such behavior and returns nil. When the updated cluster warms up in Envoy, it would update with new endpoints
	// automatically.
	// Gateways use EDS for Passthrough cluster. So we should allow Passthrough here.
	if b.service.Resolution == model.DNSLB || b.service.Resolution == model.DNSRoundRobinLB {
		log.Infof("cluster %s in eds cluster, but its resolution now is updated to %v, skipping it.", b.clusterName, b.service.Resolution)
		return nil, fmt.Errorf("cluster %s in eds cluster", b.clusterName)
	}

	svcPort, f := b.service.Ports.GetByPort(b.port)
	if !f {
		// Shouldn't happen here
		log.Debugf("can not find the service port %d for cluster %s", b.port, b.clusterName)
		return nil, nil
	}

	s.mutex.RLock()
	epShards, f := s.EndpointShardsByService[string(b.hostname)][b.service.Attributes.Namespace]
	s.mutex.RUnlock()
	if !f {
		// Shouldn't happen here
		log.Debugf("can not find the endpointShards for cluster %s", b.clusterName)
		return nil, nil
	}

	return b.buildLocalityLbEndpointsFromShards(epShards, svcPort), nil
}

func (s *DiscoveryServer) generateEndpoints(b EndpointBuilder) *endpoint.ClusterLoadAssignment {
	llbOpts, err := s.llbEndpointAndOptionsForCluster(b)
	if err != nil {
		return buildEmptyClusterLoadAssignment(b.clusterName)
	}

	// Apply the Split Horizon EDS filter, if applicable.
	llbOpts = b.EndpointsByNetworkFilter(llbOpts)

	if model.IsDNSSrvSubsetKey(b.clusterName) {
		// For the SNI-DNAT clusters, we are using AUTO_PASSTHROUGH gateway. AUTO_PASSTHROUGH is intended
		// to passthrough mTLS requests. However, at the gateway we do not actually have any way to tell if the
		// request is a valid mTLS request or not, since its passthrough TLS.
		// To ensure we allow traffic only to mTLS endpoints, we filter out non-mTLS endpoints for these cluster types.
		llbOpts = b.EndpointsWithMTLSFilter(llbOpts)
	}
	llbOpts = b.ApplyTunnelSetting(llbOpts, b.tunnelType)

	l := b.createClusterLoadAssignment(llbOpts)

	// If locality aware routing is enabled, prioritize endpoints or set their lb weight.
	// Failover should only be enabled when there is an outlier detection, otherwise Envoy
	// will never detect the hosts are unhealthy and redirect traffic.
	enableFailover, lb := getOutlierDetectionAndLoadBalancerSettings(b.DestinationRule(), b.port, b.subsetName)
	lbSetting := loadbalancer.GetLocalityLbSetting(b.push.Mesh.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
	if lbSetting != nil {
		// Make a shallow copy of the cla as we are mutating the endpoints with priorities/weights relative to the calling proxy
		l = util.CloneClusterLoadAssignment(l)
		wrappedLocalityLbEndpoints := make([]*loadbalancer.WrappedLocalityLbEndpoints, len(llbOpts))
		for i := range llbOpts {
			wrappedLocalityLbEndpoints[i] = &loadbalancer.WrappedLocalityLbEndpoints{
				IstioEndpoints:      llbOpts[i].istioEndpoints,
				LocalityLbEndpoints: l.Endpoints[i],
			}
		}
		loadbalancer.ApplyLocalityLBSetting(l, wrappedLocalityLbEndpoints, b.locality, b.proxy.Metadata.Labels, lbSetting, enableFailover)
	}
	return l
}

// EdsGenerator implements the new Generate method for EDS, using the in-memory, optimized endpoint
// storage in DiscoveryServer.
type EdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &EdsGenerator{}

// Map of all configs that do not impact EDS
var skippedEdsConfigs = map[config.GroupVersionKind]struct{}{
	gvk.Gateway:               {},
	gvk.VirtualService:        {},
	gvk.WorkloadGroup:         {},
	gvk.AuthorizationPolicy:   {},
	gvk.RequestAuthentication: {},
	gvk.Secret:                {},
}

func edsNeedsPush(updates model.XdsUpdates) bool {
	// If none set, we will always push
	if len(updates) == 0 {
		return true
	}
	for config := range updates {
		if _, f := skippedEdsConfigs[config.Kind]; !f {
			return true
		}
	}
	return false
}

func (eds *EdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource,
	req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !edsNeedsPush(req.ConfigsUpdated) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	var edsUpdatedServices map[string]struct{}
	if !req.Full {
		edsUpdatedServices = model.ConfigNamesOfKind(req.ConfigsUpdated, gvk.ServiceEntry)
	}
	resources := make(model.Resources, 0)
	empty := 0

	cached := 0
	regenerated := 0
	for _, clusterName := range w.ResourceNames {
		if edsUpdatedServices != nil {
			_, _, hostname, _ := model.ParseSubsetKey(clusterName)
			if _, ok := edsUpdatedServices[string(hostname)]; !ok {
				// Cluster was not updated, skip recomputing. This happens when we get an incremental update for a
				// specific Hostname. On connect or for full push edsUpdatedServices will be empty.
				continue
			}
		}
		builder := NewEndpointBuilder(clusterName, proxy, push)
		if marshalledEndpoint, f := eds.Server.Cache.Get(builder); f && !features.EnableUnsafeAssertions {
			// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
			resources = append(resources, marshalledEndpoint)
			cached++
		} else {
			l := eds.Server.generateEndpoints(builder)
			if l == nil {
				continue
			}
			regenerated++

			if len(l.Endpoints) == 0 {
				empty++
			}
			resource := &discovery.Resource{
				Name:     l.ClusterName,
				Resource: util.MessageToAny(l),
			}
			resources = append(resources, resource)
			eds.Server.Cache.Add(builder, req, resource)
		}
	}
	return resources, model.XdsLogDetails{
		Incremental:    len(edsUpdatedServices) != 0,
		AdditionalInfo: fmt.Sprintf("empty:%v cached:%v/%v", empty, cached, cached+regenerated),
	}, nil
}

func getOutlierDetectionAndLoadBalancerSettings(
	destinationRule *networkingapi.DestinationRule,
	portNumber int,
	subsetName string) (bool, *networkingapi.LoadBalancerSettings) {
	if destinationRule == nil {
		return false, nil
	}
	outlierDetectionEnabled := false
	var lbSettings *networkingapi.LoadBalancerSettings

	port := &model.Port{Port: portNumber}
	policy := networking.MergeTrafficPolicy(nil, destinationRule.TrafficPolicy, port)

	for _, subset := range destinationRule.Subsets {
		if subset.Name == subsetName {
			policy = networking.MergeTrafficPolicy(policy, subset.TrafficPolicy, port)
			break
		}
	}

	if policy != nil {
		lbSettings = policy.LoadBalancer
		if policy.OutlierDetection != nil {
			outlierDetectionEnabled = true
		}
	}

	return outlierDetectionEnabled, lbSettings
}

func endpointDiscoveryResponse(loadAssignments []*any.Any, version, noncePrefix string) *discovery.DiscoveryResponse {
	out := &discovery.DiscoveryResponse{
		TypeUrl: v3.EndpointType,
		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject EDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: version,
		Nonce:       nonce(noncePrefix),
		Resources:   loadAssignments,
	}

	return out
}

// cluster with no endpoints
func buildEmptyClusterLoadAssignment(clusterName string) *endpoint.ClusterLoadAssignment {
	return &endpoint.ClusterLoadAssignment{
		ClusterName: clusterName,
	}
}
