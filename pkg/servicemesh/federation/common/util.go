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
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/mitchellh/hashstructure/v2"
	corev1 "k8s.io/api/core/v1"
	kubelabels "k8s.io/apimachinery/pkg/labels"
	coreinformersv1 "k8s.io/client-go/informers/core/v1"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/spiffe"
)

const hashLength = 8

func ContainsPort(ports []corev1.EndpointPort, expectedPort int32) bool {
	for _, p := range ports {
		if p.Port == expectedPort {
			return true
		}
	}
	return false
}

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
func EndpointsForService(endpointsInformer coreinformersv1.EndpointsInformer, name, namespace string) (*corev1.Endpoints, error) {
	return endpointsInformer.Lister().Endpoints(namespace).Get(name)
}

// ServiceAccountsForService returns a list of service account names used by all
// pods implementing the service.
func ServiceAccountsForService(
	serviceInformer coreinformersv1.ServiceInformer, podInformer coreinformersv1.PodInformer, name, namespace string,
) (map[string]string, error) {
	serviceAccountByIP := map[string]string{}
	service, err := serviceInformer.Lister().Services(namespace).Get(name)
	if err != nil {
		return serviceAccountByIP, err
	}

	pods, err := podInformer.Lister().Pods(namespace).List(kubelabels.Set(service.Spec.Selector).AsSelector())
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

// hashResourceName applies a sha256 on the host and truncates it to the first n char
func hashResourceName(name string, n int) string {
	hash := sha256.Sum256([]byte(name))
	return (hex.EncodeToString(hash[:]))[:n]
}

// FormatResourceName returns the imported/exported resource name
// checking if the length of the formatted name < DNS1123LabelMaxLength
// If not returns the truncated formatted name suffixed by its hash part
func FormatResourceName(prefix, meshName, namespace, serviceName string) string {
	resourceName := fmt.Sprintf("%s-%s-%s-%s", prefix, serviceName, namespace, meshName)
	if len(resourceName) > labels.DNS1123LabelMaxLength {
		resourceName = resourceName[:labels.DNS1123LabelMaxLength-hashLength] + hashResourceName(resourceName, hashLength)
	}
	return resourceName
}
