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
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

type ServiceImporter struct {
	mu            sync.RWMutex
	domainSuffix  string
	importConfig  []NameMapper
	defaultMapper NameMapper
}

var _ NameMapper = (*ServiceImporter)(nil)

func NewServiceImporter(importConfig *v1.ImportedServiceSet, defaultMapper NameMapper, defaultDomainSuffix, localDomainSuffix string) *ServiceImporter {
	return &ServiceImporter{
		domainSuffix:  defaultDomainSuffix,
		importConfig:  convertServiceImportsToNameMapper(importConfig, defaultDomainSuffix, localDomainSuffix),
		defaultMapper: defaultMapper,
	}
}

func convertServiceImportsToNameMapper(serviceImports *v1.ImportedServiceSet, defaultDomainSuffix, localDomainSuffix string) []NameMapper {
	if serviceImports == nil {
		return nil
	}
	if serviceImports.Spec.DomainSuffix != "" {
		defaultDomainSuffix = serviceImports.Spec.DomainSuffix
	}
	var importConfig []NameMapper
	for index, rule := range serviceImports.Spec.ImportRules {
		if rule.Type != v1.NameSelectorType {
			Logger.Errorf("skipping rule %d in ServiceImports %s/%s: unknown selector type %s",
				rule.Type, index, serviceImports.Namespace, serviceImports.Name)
			continue
		}
		if rule.NameSelector == nil {
			Logger.Errorf("skipping rule %d in ServiceImports %s/%s: null nameSelector", index, serviceImports.Namespace, serviceImports.Name)
			continue
		}
		ruleDomainSuffix := rule.DomainSuffix
		if rule.ImportAsLocal {
			if rule.NameSelector.Alias == nil || rule.NameSelector.Alias.Namespace == "" || rule.NameSelector.Alias.Namespace == v1.MatchAny {
				Logger.Errorf("skipping rule %d in ServiceImports %s/%s: cannot use importAsLocal without setting a fixed namespace alias",
					index, serviceImports.Namespace, serviceImports.Name)
				continue
			}
			ruleDomainSuffix = localDomainSuffix
		} else {
			if ruleDomainSuffix == "" {
				ruleDomainSuffix = defaultDomainSuffix
			}
			if ruleDomainSuffix == localDomainSuffix {
				Logger.Errorf("skipping rule %d in ServiceImports %s/%s: "+
					"cannot use domainSuffix that matches the cluster domain suffix, use importAsLocal instead",
					index, serviceImports.Namespace, serviceImports.Name)
				continue
			}
		}
		importConfig = append(importConfig, newNameMatcher(rule.NameSelector, ruleDomainSuffix))
	}
	return importConfig
}

func (si *ServiceImporter) NameForService(svc *model.Service) *federationmodel.ServiceKey {
	if si == nil {
		return nil
	}
	si.mu.RLock()
	defer si.mu.RUnlock()
	for _, matcher := range si.importConfig {
		if name := matcher.NameForService(svc); name != nil {
			return name
		}
	}
	if si.defaultMapper != nil {
		return si.defaultMapper.NameForService(svc)
	}
	return nil
}

func (si *ServiceImporter) UpdateDefaultMapper(defaults NameMapper) {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.defaultMapper = defaults
}
