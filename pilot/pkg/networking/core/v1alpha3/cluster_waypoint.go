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
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/spiffe"
)

// buildInternalUpstreamCluster builds a single endpoint cluster to the internal listener.
func buildInternalUpstreamCluster(name string, internalListener string) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   util.BuildInternalEndpoint(internalListener, nil),
		},
		TransportSocket: util.DefaultInternalUpstreamTransportSocket,
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: passthroughHttpProtocolOptions,
		},
	}
}

var (
	MainInternalCluster = buildInternalUpstreamCluster(MainInternalName, MainInternalName)
	EncapCluster        = buildInternalUpstreamCluster(EncapClusterName, ConnectOriginate)
)

func (configgen *ConfigGeneratorImpl) buildInboundHBONEClusters() *cluster.Cluster {
	return MainInternalCluster
}

func (configgen *ConfigGeneratorImpl) buildWaypointInboundClusters(
	cb *ClusterBuilder,
	proxy *model.Proxy,
	push *model.PushContext,
	svcs map[host.Name]*model.Service,
) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	// Creates "main_internal" cluster to route to the main internal listener.
	// Creates "encap" cluster to route to the encap listener.
	clusters = append(clusters, MainInternalCluster, EncapCluster)
	// Creates per-VIP load balancing upstreams.
	clusters = append(clusters, cb.buildWaypointInboundVIP(proxy, svcs)...)
	// Upstream of the "encap" listener.
	clusters = append(clusters, cb.buildWaypointConnectOriginate(proxy, push))

	for _, c := range clusters {
		if c.TransportSocket != nil && c.TransportSocketMatches != nil {
			log.Errorf("invalid cluster, multiple matches: %v", c.Name)
		}
	}
	return clusters
}

// `inbound-vip||hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildWaypointInboundVIPCluster(svc *model.Service, port model.Port, subset string) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, subset, svc.Hostname, port.Port)

	clusterType := cluster.Cluster_EDS
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, nil,
		model.TrafficDirectionInbound, &port, nil, nil)

	// Ensure VIP cluster has services metadata for stats filter usage
	im := getOrCreateIstioMetadata(localCluster.cluster)
	im.Fields["services"] = &structpb.Value{
		Kind: &structpb.Value_ListValue{
			ListValue: &structpb.ListValue{
				Values: []*structpb.Value{},
			},
		},
	}
	svcMetaList := im.Fields["services"].GetListValue()
	svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(svc))

	// no TLS, we are just going to internal address
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = util.TunnelHostInternalUpstreamTransportSocket
	maybeApplyEdsConfig(localCluster.cluster)
	return localCluster
}

// `inbound-vip|protocol|hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildWaypointInboundVIP(proxy *model.Proxy, svcs map[host.Name]*model.Service) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}

	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "tcp").build())
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "http").build())
			}
			cfg := cb.sidecarScope.DestinationRule(model.TrafficDirectionInbound, proxy, svc.Hostname).GetRule()
			if cfg != nil {
				destinationRule := cfg.Spec.(*networking.DestinationRule)
				for _, ss := range destinationRule.Subsets {
					if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
						clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "tcp/"+ss.Name).build())
					}
					if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
						clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "http/"+ss.Name).build())
					}
				}
			}
		}
	}
	return clusters
}

// CONNECT origination cluster
func (cb *ClusterBuilder) buildWaypointConnectOriginate(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	// Restrict upstream SAN to waypoint scope.
	scope := proxy.WaypointScope()
	m := &matcher.StringMatcher{}
	if scope.ServiceAccount != "" {
		m.MatchPattern = &matcher.StringMatcher_Exact{
			Exact: spiffe.MustGenSpiffeURI(scope.Namespace, scope.ServiceAccount),
		}
	} else {
		m.MatchPattern = &matcher.StringMatcher_Prefix{
			Prefix: spiffe.URIPrefix + spiffe.GetTrustDomain() + "/ns/" + scope.Namespace + "/sa/",
		}
	}
	return cb.buildConnectOriginate(proxy, push, m)
}

func (cb *ClusterBuilder) buildConnectOriginate(proxy *model.Proxy, push *model.PushContext, uriSanMatcher *matcher.StringMatcher) *cluster.Cluster {
	ctx := buildCommonConnectTLSContext(proxy, push)
	validationCtx := ctx.GetCombinedValidationContext().DefaultValidationContext
	if uriSanMatcher != nil {
		validationCtx.MatchTypedSubjectAltNames = append(validationCtx.MatchTypedSubjectAltNames, &tls.SubjectAltNameMatcher{
			SanType: tls.SubjectAltNameMatcher_URI,
			Matcher: uriSanMatcher,
		})
	}
	return &cluster.Cluster{
		Name:                          ConnectOriginate,
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                durationpb.New(2 * time.Second),
		CleanupInterval:               durationpb.New(60 * time.Second),
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: ctx,
			})},
		},
	}
}

func h2connectUpgrade() map[string]*anypb.Any {
	return map[string]*anypb.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			UpstreamProtocolOptions: &http.HttpProtocolOptions_ExplicitHttpConfig_{ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: &core.Http2ProtocolOptions{
						AllowConnect: true,
					},
				},
			}},
		}),
	}
}
