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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/sets"
)

const (
	// ConnectTerminate is the name for the resources associated with the termination of HTTP CONNECT.
	ConnectTerminate = "connect_terminate"

	// MainInternalName is the name for the resources associated with the main (non-tunnel) internal listener.
	MainInternalName = "main_internal"

	// ConnectOriginate is the name for the resources associated with the origination of HTTP CONNECT.
	ConnectOriginate = "connect_originate"

	// EncapClusterName is the name of the cluster used for traffic to the connect_originate listener.
	EncapClusterName = "encap"

	// ConnectUpgradeType is the type of upgrade for HTTP CONNECT.
	ConnectUpgradeType = "CONNECT"
)

// findWaypointResources returns workloads and services associated with the waypoint proxy
func findWaypointResources(node *model.Proxy, push *model.PushContext) ([]*model.WorkloadInfo, map[host.Name]*model.Service) {
	scope := node.WaypointScope()
	workloads := push.WorkloadsForWaypoint(scope)
	return workloads, findWorkloadServices(workloads, push)
}

func findWorkloadServices(workloads []*model.WorkloadInfo, push *model.PushContext) map[host.Name]*model.Service {
	svcs := map[host.Name]*model.Service{}
	for _, wl := range workloads {
		for _, ns := range push.ServiceIndex.HostnameAndNamespace {
			svc := ns[wl.Namespace]
			if svc == nil {
				continue
			}
			if labels.Instance(svc.Attributes.LabelSelectors).Match(wl.Labels) {
				svcs[svc.Hostname] = svc
			}
		}
	}
	return svcs
}

// filterWaypointOutboundServices is used to determine the set of outbound clusters we need to build for waypoints.
// Waypoints typically only have inbound clusters, except in cases where we have a route from
// a service owned by the waypoint to a service not owned by the waypoint.
// It looks at:
// * referencedServices: all services referenced by mesh virtual services
// * waypointServices: all services owned by this waypoint
// * all services
// We want to find any VirtualServices that are from a waypointServices to a non-waypointService
func filterWaypointOutboundServices(
	referencedServices map[string]sets.String,
	waypointServices map[host.Name]*model.Service,
	services []*model.Service,
) []*model.Service {
	outboundServices := sets.New[string]()
	for waypointService := range waypointServices {
		refs := referencedServices[waypointService.String()]
		for ref := range refs {
			// We reference this service. Is it "inbound" for the waypoint or "outbound"?
			ws, f := waypointServices[host.Name(ref)]
			if !f || ws.MeshExternal {
				outboundServices.Insert(ref)
			}
		}
	}
	res := make([]*model.Service, 0, len(outboundServices))
	for _, s := range services {
		if outboundServices.Contains(s.Hostname.String()) {
			res = append(res, s)
		}
	}
	return res
}
