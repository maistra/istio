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

package common

import (
	"fmt"

	"github.com/mitchellh/hashstructure/v2"
	corev1 "k8s.io/api/core/v1"
	kubelabels "k8s.io/apimachinery/pkg/labels"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/spiffe"
)

// DiscoveryServiceHostname returns the hostname used to represent a remote's
// discovery service in the local mesh.
func DiscoveryServiceHostname(instance *v1.ServiceMeshPeer) string {
	return fmt.Sprintf("discovery.%s.svc.%s.local", instance.Namespace, instance.Name)
}

// DefaultFederationCARootResourceName is the default name used for the resource
// containing the root CA for a remote mesh.
func DefaultFederationCARootResourceName(instance *v1.ServiceMeshPeer) string {
	return fmt.Sprintf("%s-ca-root-cert", instance.Name)
}

// EndpointsForService returns the Endpoints for the named service.
func EndpointsForService(client kube.Client, name, namespace string) (*corev1.Endpoints, error) {
	return client.KubeInformer().Core().V1().Endpoints().Lister().Endpoints(namespace).Get(name)
}

// ServiceAccountsForService returns a list of service account names used by all
// pods implementing the service.
func ServiceAccountsForService(client kube.Client, name, namespace string) (map[string]string, error) {
	serviceAccountByIP := map[string]string{}
	service, err := client.KubeInformer().Core().V1().Services().Lister().Services(namespace).Get(name)
	if err != nil {
		return serviceAccountByIP, err
	}

	pods, err := client.KubeInformer().Core().V1().Pods().Lister().Pods(namespace).
		List(kubelabels.Set(service.Spec.Selector).AsSelector())
	if err != nil {
		return serviceAccountByIP, err
	}

	for _, pod := range pods {
		if pod.Status.PodIP == "" {
			continue
		}
		sa := pod.Spec.ServiceAccountName
		if sa == "" {
			sa = "default"
		}
		sa = spiffe.MustGenSpiffeURI(namespace, sa)
		serviceAccountByIP[pod.Status.PodIP] = sa
		Logger.Debugf("using ServiceAccount %s for gateway pod %s/%s", sa, pod.Namespace, pod.Name)
	}
	return serviceAccountByIP, nil
}

func RemoteChecksum(remote v1.ServiceMeshPeerRemote) uint64 {
	checksum, err := hashstructure.Hash(remote, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return 0
	}
	return checksum
}
