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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pilot/pkg/model"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

func setHostname(name *federationmodel.ServiceKey, domainSuffix string) {
	if name == nil {
		return
	}
	name.Hostname = fmt.Sprintf("%s.%s.%s", name.Name, name.Namespace, domainSuffix)
}

type NameMapper interface {
	NameForService(svc *model.Service) *federationmodel.ServiceKey
}

type nameMatcher struct {
	domainSuffix string
	match        v1.ServiceName
	alias        *v1.ServiceName
}

var _ NameMapper = (*nameMatcher)(nil)

func newNameMatcher(mapping *v1.ServiceNameMapping, domainSuffix string) NameMapper {
	var alias *v1.ServiceName
	// if it's nil or matches anything, it may as well be nil
	if mapping.Alias == nil ||
		((mapping.Alias.Namespace == v1.MatchAny || mapping.Alias.Namespace == "") &&
			(mapping.Alias.Name == v1.MatchAny || mapping.Alias.Name == "")) {
		alias = nil
	} else {
		alias = mapping.Alias.DeepCopy()
	}
	return &nameMatcher{
		domainSuffix: domainSuffix,
		match:        mapping.ServiceName,
		alias:        alias,
	}
}

func (m *nameMatcher) NameForService(svc *model.Service) *federationmodel.ServiceKey {
	if (m.match.Namespace == "" || m.match.Namespace == v1.MatchAny || m.match.Namespace == svc.Attributes.Namespace) &&
		(m.match.Name == "" || m.match.Name == v1.MatchAny || m.match.Name == svc.Attributes.Name) {
		name := &federationmodel.ServiceKey{}
		if m.alias == nil {
			name.Namespace = svc.Attributes.Namespace
			name.Name = svc.Attributes.Name
		} else {
			if m.alias.Namespace == v1.MatchAny || m.alias.Namespace == "" {
				name.Namespace = svc.Attributes.Namespace
			} else {
				name.Namespace = m.alias.Namespace
			}
			if m.alias.Name == v1.MatchAny || m.alias.Name == "" {
				name.Name = svc.Attributes.Name
			} else {
				name.Name = m.alias.Name
			}
		}
		setHostname(name, m.domainSuffix)
		return name
	}
	return nil
}

type labelMatcher struct {
	domainSuffix string
	namespace    string
	selector     labels.Selector
	aliases      []NameMapper
}

var _ NameMapper = (*labelMatcher)(nil)

func newLabelMatcher(labelSelector *v1.ServiceImportExportLabelSelector, domainSuffix string) (NameMapper, error) {
	selector, err := metav1.LabelSelectorAsSelector(&labelSelector.Selector)
	if err != nil {
		return nil, err
	}
	aliases := make([]NameMapper, len(labelSelector.Aliases))
	for index, alias := range labelSelector.Aliases {
		aliases[index] = newNameMatcher(&alias, domainSuffix)
	}
	return &labelMatcher{
		domainSuffix: domainSuffix,
		namespace:    labelSelector.Namespace,
		selector:     selector,
		aliases:      aliases,
	}, nil
}

func (m *labelMatcher) NameForService(svc *model.Service) *federationmodel.ServiceKey {
	if (m.namespace == "" || m.namespace == v1.MatchAny || m.namespace == svc.Attributes.Namespace) &&
		m.selector.Matches(labels.Set(svc.Attributes.Labels)) {
		for _, alias := range m.aliases {
			if name := alias.NameForService(svc); name != nil {
				setHostname(name, m.domainSuffix)
				return name
			}
		}
		// if there's no alias, we return the original service name
		name := &federationmodel.ServiceKey{
			Namespace: svc.Attributes.Namespace,
			Name:      svc.Attributes.Name,
		}
		setHostname(name, m.domainSuffix)
		return name
	}
	return nil
}
