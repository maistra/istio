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

package ior

import (
	"strings"
	"time"

	"istio.io/istio/pkg/kube"
)

// KubeClient is an extension of `kube.Client` with auxiliary functions for IOR
type KubeClient interface {
	IsRouteSupported() bool
	GetActualClient() kube.Client
	GetHandleEventTimeout() time.Duration
}

type kubeClient struct {
	client kube.Client
}

// NewKubeClient creates the IOR version of KubeClient
func NewKubeClient(client kube.Client) KubeClient {
	return &kubeClient{client: client}
}

func (c *kubeClient) IsRouteSupported() bool {
	_, s, _ := c.client.Kube().Discovery().ServerGroupsAndResources()
	// This may fail if any api service is down, but the result will still be populated, so we skip the error
	for _, res := range s {
		for _, api := range res.APIResources {
			if api.Kind == "Route" && strings.HasPrefix(res.GroupVersion, "route.openshift.io/") {
				return true
			}
		}
	}
	return false
}

func (c *kubeClient) GetActualClient() kube.Client {
	return c.client
}

func (c *kubeClient) GetHandleEventTimeout() time.Duration {
	return 10 * time.Second
}
