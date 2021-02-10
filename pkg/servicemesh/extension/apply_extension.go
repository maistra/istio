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

package extension

import (
	"fmt"

	xdslistener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"istio.io/istio/istioctl/pkg/authz"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	maistramodel "istio.io/istio/pkg/servicemesh/model"
)

const (
	DefaultCacheCluster = "outbound|80||mec.istio-system.svc.cluster.local"
	defaultRuntime      = "envoy.wasm.runtime.v8"
)

var (
	// CacheCluster is the Envoy cluster that is used to retrieve WASM filters from
	CacheCluster = ""
	// Runtime sets the WASM runtime to use for extensions
	Runtime = defaultRuntime
)

// ApplyListenerPatches adds extensions to listener filterChains
func ApplyListenerPatches(
	listener *xdslistener.Listener,
	proxy *model.Proxy,
	push *model.PushContext,
	patchAll bool,
) *xdslistener.Listener {
	if listener == nil {
		return nil
	}
	extensionsMap := push.Extensions(proxy)

	relevantFilterChains := []string{}
	for _, si := range proxy.ServiceInstances {
		relevantFilterChains = append(relevantFilterChains, fmt.Sprintf("%s_%d", si.Endpoint.Address, si.Endpoint.EndpointPort))
	}

	for fcIndex, fc := range listener.FilterChains {
		// copy extensions map
		extensions := make(map[v1alpha1.FilterPhase][]*maistramodel.ExtensionWrapper)
		for k, v := range extensionsMap {
			extensions[k] = []*maistramodel.ExtensionWrapper{}
			extensions[k] = append(extensions[k], v...)
		}
		if !patchAll {
			isRelevant := false
			for _, relevant := range relevantFilterChains {
				if fc.Name == relevant {
					isRelevant = true
				}
			}
			if !isRelevant {
				continue
			}
		}
		var hcm *hcm_filter.HttpConnectionManager
		var hcmIndex int
		for i, f := range fc.Filters {
			if f.Name == "envoy.http_connection_manager" {
				if hcm = authz.GetHTTPConnectionManager(f); hcm != nil {
					hcmIndex = i
					break
				}
			}
		}
		if hcm == nil {
			continue
		}
		newHTTPFilters := make([]*hcm_filter.HttpFilter, 0)
		for _, httpFilter := range hcm.GetHttpFilters() {
			switch httpFilter.Name {
			case "envoy.filters.http.jwt_authn":
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthN)
				newHTTPFilters = append(newHTTPFilters, httpFilter)
			case "istio_authn":
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthN)
				newHTTPFilters = append(newHTTPFilters, httpFilter)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthN)
			case "envoy.filters.http.rbac":
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthN)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthN)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthZ)
				newHTTPFilters = append(newHTTPFilters, httpFilter)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthZ)
			case "istio.stats":
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthN)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthN)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthZ)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthZ)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreStats)
				newHTTPFilters = append(newHTTPFilters, httpFilter)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostStats)
			case "envoy.router":
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthN)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthN)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreAuthZ)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostAuthZ)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePreStats)
				newHTTPFilters = popAppend(newHTTPFilters, extensions, v1alpha1.FilterPhasePostStats)
				newHTTPFilters = append(newHTTPFilters, httpFilter)
			default:
				newHTTPFilters = append(newHTTPFilters, httpFilter)
			}
		}
		hcm.HttpFilters = newHTTPFilters
		fc.Filters[hcmIndex] = &xdslistener.Filter{
			Name:       xdsutil.HTTPConnectionManager,
			ConfigType: &xdslistener.Filter_TypedConfig{TypedConfig: util.MessageToAny(hcm)},
		}
		listener.FilterChains[fcIndex] = fc
	}
	return listener
}

func ApplyListenerListPatches(
	listeners []*xdslistener.Listener,
	proxy *model.Proxy,
	push *model.PushContext,
	patchAll bool,
) (out []*xdslistener.Listener) {
	for _, listener := range listeners {
		out = append(out, ApplyListenerPatches(listener, proxy, push, patchAll))
	}
	return out
}

func popAppend(list []*hcm_filter.HttpFilter,
	filterMap map[v1alpha1.FilterPhase][]*maistramodel.ExtensionWrapper,
	phase v1alpha1.FilterPhase) []*hcm_filter.HttpFilter {
	for _, ext := range filterMap[phase] {
		list = append(list, toEnvoyHTTPFilter(ext))
	}
	filterMap[phase] = []*maistramodel.ExtensionWrapper{}
	return list
}

func toEnvoyHTTPFilter(extension *maistramodel.ExtensionWrapper) *hcm_filter.HttpFilter {
	return &hcm_filter.HttpFilter{
		Name: "envoy.filters.http.wasm",
		ConfigType: &hcm_filter.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"config": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
						"name":          {Kind: &structpb.Value_StringValue{StringValue: extension.Name}},
						"rootId":        {Kind: &structpb.Value_StringValue{StringValue: extension.Name + "_root"}},
						"configuration": {Kind: &structpb.Value_StringValue{StringValue: extension.Config}},
						"vmConfig": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
							"code": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
								"remote": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
									"httpUri": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
										"uri":     {Kind: &structpb.Value_StringValue{StringValue: extension.FilterURL}},
										"cluster": {Kind: &structpb.Value_StringValue{StringValue: CacheCluster}},
										"timeout": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
											"seconds": {Kind: &structpb.Value_NumberValue{NumberValue: 30}},
										}}}},
									}}}},
									"sha256": {Kind: &structpb.Value_StringValue{StringValue: extension.SHA256}},
									"retryPolicy": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
										"numRetries": {Kind: &structpb.Value_NumberValue{NumberValue: 2}},
									}}}},
								}}}},
							}}}},
							"runtime": {Kind: &structpb.Value_StringValue{StringValue: Runtime}},
						}}}},
					}}}},
				},
			}),
		},
	}
}
