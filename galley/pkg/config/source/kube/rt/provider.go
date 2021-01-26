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

package rt

import (
	"errors"

	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"istio.io/istio/pkg/config/schema/resource"
	kubelib "istio.io/istio/pkg/kube"
)

var (
	defaultProvider = NewProvider(nil)
)

// DefaultProvider returns a default provider that has no K8s connectivity enabled.
func DefaultProvider() *Provider {
	return defaultProvider
}

// Provider for adapters. It closes over K8s connection-related infrastructure.
type Provider struct {
	kubeClient kubelib.Client
	known      map[string]*Adapter
}

// NewProvider returns a new instance of Provider.
func NewProvider(kubeClient kubelib.Client) *Provider {
	p := &Provider{kubeClient: kubeClient}
	p.initKnownAdapters()

	return p
}

// GetAdapter returns a type for the group/kind. If the type is a well-known type, then the returned type will have
// a specialized implementation. Otherwise, it will be using the dynamic conversion logic.
func (p *Provider) GetAdapter(r resource.Schema) *Adapter {
	if t, found := p.known[asTypesKey(r.Group(), r.Kind())]; found {
		return t
	}

	return p.getDynamicAdapter(r)
}

// GetDynamicResourceInterface returns a dynamic.NamespaceableResourceInterface for the given resource.
func (p *Provider) GetDynamicResourceInterface(r resource.Schema) (dynamic.NamespaceableResourceInterface, error) {
	if p.kubeClient == nil {
		return nil, errors.New("client interfaces was not initialized")
	}

	return p.kubeClient.Dynamic().Resource(kubeSchema.GroupVersionResource{
		Group:    r.Group(),
		Version:  r.Version(),
		Resource: r.Plural(),
	}), nil
}
