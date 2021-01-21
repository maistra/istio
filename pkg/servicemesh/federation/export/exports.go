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

package export

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
)

type ServiceExporter struct {
	exportConfig        []matcher
	defaultExportConfig []matcher
}

func NewServiceExporter(exportConfig *v1alpha1.ServiceExports, defaultConfig *ServiceExporter) *ServiceExporter {
	var defaultExportConfig []matcher
	if defaultConfig != nil && len(defaultConfig.defaultExportConfig) > 0 {
		defaultExportConfig = append([]matcher(nil), defaultConfig.exportConfig...)
	}
	return &ServiceExporter{
		exportConfig:        convertServiceExportsToExportConfig(exportConfig),
		defaultExportConfig: defaultExportConfig,
	}
}

func convertServiceExportsToExportConfig(serviceExports *v1alpha1.ServiceExports) []matcher {
	if serviceExports == nil {
		return nil
	}
	var exportConfig []matcher
	for _, rule := range serviceExports.Spec.Exports {
		switch rule.Type {
		case v1alpha1.LabelSelectorType:
			if rule.LabelSelector == nil {
				// XXX: log error?  this should be caught in validation
				continue
			}
			if matcher, err := newLabelMatcher(rule.LabelSelector); err != nil {
				// XXX: log error?  this should be caught in validation
				continue
			} else {
				exportConfig = append(exportConfig, matcher)
			}
		case v1alpha1.NameSelectorType:
			if rule.NameSelector == nil {
				// XXX: log error?  this should be caught in validation
				continue
			}
			exportConfig = append(exportConfig, newNameMatcher(rule.NameSelector))
		default:
			// unknown selector type
			// XXX: log error?  this should be caught in validation
		}
	}
	return exportConfig
}

func (se *ServiceExporter) ExportedNameForService(svc *model.Service) *v1alpha1.ServiceName {
	// don't reexport federated services
	if svc.Attributes.ServiceRegistry == provider.Federation || svc.MeshExternal {
		return nil
	}
	for _, matcher := range se.exportConfig {
		if name := matcher.exportedNameForService(svc); name != nil {
			return name
		}
	}
	for _, matcher := range se.defaultExportConfig {
		if name := matcher.exportedNameForService(svc); name != nil {
			return name
		}
	}
	return nil
}

type matcher interface {
	exportedNameForService(svc *model.Service) *v1alpha1.ServiceName
}

type nameMatcher struct {
	match v1alpha1.ServiceName
	alias *v1alpha1.ServiceName
}

var _ matcher = (*nameMatcher)(nil)

func newNameMatcher(mapping *v1alpha1.ServiceNameMapping) matcher {
	var alias *v1alpha1.ServiceName
	// if it's nil or matches anything, it may as well be nil
	if mapping.Alias == nil ||
		((mapping.Alias.Namespace == v1alpha1.MatchAny || mapping.Alias.Namespace == "") &&
			(mapping.Alias.Name == v1alpha1.MatchAny || mapping.Alias.Name == "")) {
		alias = nil
	} else {
		alias = &v1alpha1.ServiceName{}
		*alias = *mapping.Alias
	}
	return &nameMatcher{
		match: mapping.Name,
		alias: alias,
	}
}

func (m *nameMatcher) exportedNameForService(svc *model.Service) *v1alpha1.ServiceName {
	if (m.match.Namespace == "" || m.match.Namespace == v1alpha1.MatchAny || m.match.Namespace == svc.Attributes.Namespace) &&
		(m.match.Name == "" || m.match.Name == v1alpha1.MatchAny || m.match.Name == svc.Attributes.Name) {
		name := &v1alpha1.ServiceName{}
		if m.alias == nil {
			name.Namespace = svc.Attributes.Namespace
			name.Name = svc.Attributes.Name
		} else {
			if m.alias.Namespace == v1alpha1.MatchAny || m.alias.Namespace == "" {
				name.Namespace = svc.Attributes.Namespace
			} else {
				name.Namespace = m.alias.Namespace
			}
			if m.alias.Name == v1alpha1.MatchAny || m.alias.Name == "" {
				name.Name = svc.Attributes.Name
			} else {
				name.Name = m.alias.Name
			}
		}
		return name
	}
	return nil
}

type labelMatcher struct {
	namespace string
	selector  labels.Selector
	aliases   []matcher
}

var _ matcher = (*labelMatcher)(nil)

func newLabelMatcher(labelSelector *v1alpha1.ServiceExportLabelSelector) (matcher, error) {
	selector, err := metav1.LabelSelectorAsSelector(&labelSelector.Selector)
	if err != nil {
		return nil, err
	}
	aliases := make([]matcher, len(labelSelector.Aliases))
	for index, alias := range labelSelector.Aliases {
		aliases[index] = newNameMatcher(&alias)
	}
	return &labelMatcher{
		namespace: labelSelector.Namespace,
		selector:  selector,
		aliases:   aliases,
	}, nil
}

func (m *labelMatcher) exportedNameForService(svc *model.Service) *v1alpha1.ServiceName {
	if (m.namespace == "" || m.namespace == v1alpha1.MatchAny || m.namespace == svc.Attributes.Namespace) &&
		m.selector.Matches(labels.Set(svc.Attributes.Labels)) {
		for _, alias := range m.aliases {
			if name := alias.exportedNameForService(svc); name != nil {
				return name
			}
		}
		// if there's no alias, we return the original service name
		return &v1alpha1.ServiceName{
			Namespace: svc.Attributes.Namespace,
			Name:      svc.Attributes.Name,
		}
	}
	return nil
}
