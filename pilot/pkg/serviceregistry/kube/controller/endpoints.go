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

package controller

import (
	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/kclient"
)

type endpointsController struct {
	endpoints kclient.Client[*v1.Endpoints]
	c         *Controller
}

var _ kubeEndpointsController = &endpointsController{}

func newEndpointsController(c *Controller) *endpointsController {
	endpoints := kclient.NewFiltered[*v1.Endpoints](c.client, kclient.Filter{ObjectFilter: c.opts.GetFilter()})
	out := &endpointsController{
		endpoints: endpoints,
		c:         c,
	}
	registerHandlers[*v1.Endpoints](c, endpoints, "Endpoints", out.onEvent, endpointsEqual)
	return out
}

func (e *endpointsController) HasSynced() bool {
	return e.endpoints.HasSynced()
}

func (e *endpointsController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance {
	eps := e.endpoints.List(proxy.Metadata.Namespace, klabels.Everything())
	var out []*model.ServiceInstance
	for _, ep := range eps {
		instances := endpointServiceInstances(c, ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func endpointServiceInstances(c *Controller, endpoints *v1.Endpoints, proxy *model.Proxy) []*model.ServiceInstance {
	var out []*model.ServiceInstance

	for _, svc := range c.servicesForNamespacedName(config.NamespacedName(endpoints)) {
		pod := c.pods.getPodByProxy(proxy)
		builder := NewEndpointBuilder(c, pod)

		discoverabilityPolicy := c.exports.EndpointDiscoverabilityPolicy(svc)

		for _, ss := range endpoints.Subsets {
			for _, port := range ss.Ports {
				svcPort, exists := svc.Ports.Get(port.Name)
				if !exists {
					continue
				}

				// consider multiple IP scenarios
				for _, ip := range proxy.IPAddresses {
					if hasProxyIP(ss.Addresses, ip) || hasProxyIP(ss.NotReadyAddresses, ip) {
						istioEndpoint := builder.buildIstioEndpoint(ip, port.Port, svcPort.Name, discoverabilityPolicy, model.Healthy)
						out = append(out, &model.ServiceInstance{
							Endpoint:    istioEndpoint,
							ServicePort: svcPort,
							Service:     svc,
						})
					}

					if hasProxyIP(ss.NotReadyAddresses, ip) {
						if c.opts.Metrics != nil {
							c.opts.Metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy.ID, "")
						}
					}
				}
			}
		}
	}

	return out
}

func (e *endpointsController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int) []*model.ServiceInstance {
	ep := e.endpoints.Get(svc.Attributes.Name, svc.Attributes.Namespace)
	if ep == nil {
		return nil
	}
	discoverabilityPolicy := c.exports.EndpointDiscoverabilityPolicy(svc)

	// Locate all ports in the actual service
	svcPort, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil
	}
	var out []*model.ServiceInstance
	for _, ss := range ep.Subsets {
		out = append(out, e.buildServiceInstances(ep, ss, ss.Addresses, svc, discoverabilityPolicy, svcPort, model.Healthy)...)
		if features.SendUnhealthyEndpoints.Load() {
			out = append(out, e.buildServiceInstances(ep, ss, ss.NotReadyAddresses, svc, discoverabilityPolicy, svcPort, model.UnHealthy)...)
		}
	}
	return out
}

func (e *endpointsController) sync(name, ns string, event model.Event, filtered bool) error {
	if name != "" {
		ep := e.endpoints.Get(name, ns)
		if ep == nil {
			return nil
		}
		return e.onEvent(nil, ep, model.EventAdd)
	}
	var err *multierror.Error
	var endpoints []*v1.Endpoints
	if filtered {
		endpoints = e.endpoints.List(ns, klabels.Everything())
	} else {
		endpoints = e.endpoints.ListUnfiltered(ns, klabels.Everything())
	}
	log.Debugf("syncing %d endpoints", len(endpoints))
	for _, s := range endpoints {
		err = multierror.Append(err, e.onEvent(nil, s, event))
	}
	return err.ErrorOrNil()
}

func (e *endpointsController) onEvent(_, ep *v1.Endpoints, event model.Event) error {
	return processEndpointEvent(e.c, e, ep.Name, ep.Namespace, event, ep)
}

func (e *endpointsController) forgetEndpoint(endpoint any) map[host.Name][]*model.IstioEndpoint {
	ep := endpoint.(*v1.Endpoints)
	key := config.NamespacedName(ep)
	for _, ss := range ep.Subsets {
		for _, ea := range ss.Addresses {
			e.c.pods.endpointDeleted(key, ea.IP)
		}
	}
	return make(map[host.Name][]*model.IstioEndpoint)
}

func (e *endpointsController) buildIstioEndpoints(endpoint any, host host.Name) []*model.IstioEndpoint {
	var endpoints []*model.IstioEndpoint
	ep := endpoint.(*v1.Endpoints)

	discoverabilityPolicy := e.c.exports.EndpointDiscoverabilityPolicy(e.c.GetService(host))

	for _, ss := range ep.Subsets {
		endpoints = append(endpoints, e.buildIstioEndpointFromAddress(ep, ss, ss.Addresses, host, discoverabilityPolicy, model.Healthy)...)
		if features.SendUnhealthyEndpoints.Load() {
			endpoints = append(endpoints, e.buildIstioEndpointFromAddress(ep, ss, ss.NotReadyAddresses, host, discoverabilityPolicy, model.UnHealthy)...)
		}
	}
	return endpoints
}

func (e *endpointsController) buildServiceInstances(ep *v1.Endpoints, ss v1.EndpointSubset, endpoints []v1.EndpointAddress,
	svc *model.Service, discoverabilityPolicy model.EndpointDiscoverabilityPolicy,
	svcPort *model.Port, health model.HealthStatus,
) []*model.ServiceInstance {
	var out []*model.ServiceInstance
	for _, ea := range endpoints {
		pod, expectedPod := getPod(e.c, ea.IP, &metav1.ObjectMeta{Name: ep.Name, Namespace: ep.Namespace}, ea.TargetRef, svc.Hostname)
		if pod == nil && expectedPod {
			continue
		}

		builder := NewEndpointBuilder(e.c, pod)

		// identify the port by name. K8S EndpointPort uses the service port name
		for _, port := range ss.Ports {
			if port.Name == "" || // 'name optional if single port is defined'
				svcPort.Name == port.Name {
				istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, svcPort.Name, discoverabilityPolicy, model.Healthy)
				istioEndpoint.HealthStatus = health
				out = append(out, &model.ServiceInstance{
					Endpoint:    istioEndpoint,
					ServicePort: svcPort,
					Service:     svc,
				})
			}
		}
	}
	return out
}

func (e *endpointsController) buildIstioEndpointFromAddress(ep *v1.Endpoints, ss v1.EndpointSubset, endpoints []v1.EndpointAddress,
	host host.Name, discoverabilityPolicy model.EndpointDiscoverabilityPolicy, health model.HealthStatus,
) []*model.IstioEndpoint {
	var istioEndpoints []*model.IstioEndpoint
	for _, ea := range endpoints {
		pod, expectedPod := getPod(e.c, ea.IP, &metav1.ObjectMeta{Name: ep.Name, Namespace: ep.Namespace}, ea.TargetRef, host)
		if pod == nil && expectedPod {
			continue
		}
		builder := NewEndpointBuilder(e.c, pod)
		// EDS and ServiceEntry use name for service port - ADS will need to map to numbers.
		for _, port := range ss.Ports {
			istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, port.Name, discoverabilityPolicy, health)
			istioEndpoints = append(istioEndpoints, istioEndpoint)
		}
	}
	return istioEndpoints
}

func (e *endpointsController) buildIstioEndpointsWithService(name, namespace string, host host.Name, _ bool) []*model.IstioEndpoint {
	ep := e.endpoints.Get(name, namespace)
	if ep == nil {
		log.Debugf("endpoints(%s, %s) not found", name, namespace)
		return nil
	}

	return e.buildIstioEndpoints(ep, host)
}

func (e *endpointsController) getServiceNamespacedName(ep any) types.NamespacedName {
	endpoint := ep.(*v1.Endpoints)
	return config.NamespacedName(endpoint)
}

// endpointsEqual returns true if the two endpoints are the same in aspects Pilot cares about
// This currently means only looking at "Ready" endpoints
func endpointsEqual(a, b *v1.Endpoints) bool {
	if len(a.Subsets) != len(b.Subsets) {
		return false
	}
	for i := range a.Subsets {
		if !portsEqual(a.Subsets[i].Ports, b.Subsets[i].Ports) {
			return false
		}
		if !addressesEqual(a.Subsets[i].Addresses, b.Subsets[i].Addresses) {
			return false
		}
	}
	return true
}
