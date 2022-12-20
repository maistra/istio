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
	"sync"

	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

type ServiceExporter struct {
	mu            sync.RWMutex
	domainSuffix  string
	exportConfig  []NameMapper
	defaultMapper NameMapper
}

var _ NameMapper = (*ServiceExporter)(nil)

func NewServiceExporter(exportConfig *v1.ExportedServiceSet, defaultMapper *ServiceExporter, domainSuffix string) *ServiceExporter {
	return &ServiceExporter{
		domainSuffix:  domainSuffix,
		exportConfig:  convertServiceExportsToNameMapper(exportConfig, domainSuffix),
		defaultMapper: defaultMapper,
	}
}

func convertServiceExportsToNameMapper(serviceExports *v1.ExportedServiceSet, domainSuffix string) []NameMapper {
	if serviceExports == nil {
		return nil
	}
	var exportConfig []NameMapper
	for index, rule := range serviceExports.Spec.ExportRules {
		switch rule.Type {
		case v1.LabelSelectorType:
			if rule.LabelSelector == nil {
				Logger.Errorf("skipping rule %d in ServiceExports %s/%s: null labelSelector", index, serviceExports.Namespace, serviceExports.Name)
				continue
			}
			var matcher NameMapper
			var err error
			if matcher, err = newLabelMatcher(rule.LabelSelector, domainSuffix); err != nil {
				Logger.Errorf("skipping rule %d in ServiceExports %s/%s: error creating matcher: %s",
					index, serviceExports.Namespace, serviceExports.Name, err)
				continue
			}
			exportConfig = append(exportConfig, matcher)
		case v1.NameSelectorType:
			if rule.NameSelector == nil {
				Logger.Errorf("skipping rule %d in ServiceExports %s/%s: null nameSelector", index, serviceExports.Namespace, serviceExports.Name)
				continue
			}
			exportConfig = append(exportConfig, newNameMatcher(rule.NameSelector, domainSuffix))
		default:
			// unknown selector type
			Logger.Errorf("skipping rule %d in ServiceExports %s/%s: unknown selector type %s",
				rule.Type, index, serviceExports.Namespace, serviceExports.Name)
		}
	}
	return exportConfig
}

func (se *ServiceExporter) NameForService(svc *model.Service) *federationmodel.ServiceKey {
	if se == nil {
		return nil
	}
	// don't reexport federated services
	if svc.Attributes.ServiceRegistry == provider.Federation || svc.MeshExternal {
		return nil
	}
	se.mu.RLock()
	defer se.mu.RUnlock()
	for _, matcher := range se.exportConfig {
		if name := matcher.NameForService(svc); name != nil {
			return name
		}
	}
	if se.defaultMapper != nil {
		if name := se.defaultMapper.NameForService(svc); name != nil {
			return name
		}
	}
	return nil
}

func (se *ServiceExporter) UpdateDefaultMapper(defaults NameMapper) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.defaultMapper = defaults
}
