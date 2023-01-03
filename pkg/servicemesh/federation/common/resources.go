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
	"reflect"

	coreinformersv1 "k8s.io/client-go/informers/core/v1"
	maistrainformersfederationv1 "maistra.io/api/client/informers/externalversions/federation/v1"
	maistraclient "maistra.io/api/client/versioned"
	maistraxnsinformer "maistra.io/api/client/xnsinformer"

	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
)

type ResourceManager interface {
	MaistraClientSet() maistraclient.Interface
	ConfigMapInformer() coreinformersv1.ConfigMapInformer
	EndpointsInformer() coreinformersv1.EndpointsInformer
	PodInformer() coreinformersv1.PodInformer
	ServiceInformer() coreinformersv1.ServiceInformer
	PeerInformer() maistrainformersfederationv1.ServiceMeshPeerInformer
	ExportsInformer() maistrainformersfederationv1.ExportedServiceSetInformer
	ImportsInformer() maistrainformersfederationv1.ImportedServiceSetInformer
	Start(stopCh <-chan struct{})
	HasSynced() bool
	WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool
}

func NewResourceManager(opts ControllerOptions, mrc memberroll.MemberRollController) (ResourceManager, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	var informerFactory maistraxnsinformer.SharedInformerFactory
	// Currently, we only watch istio system namespace for MeshFederation resources, which is why this block is disabled.
	if mrc != nil && false {
		informerFactory = maistraxnsinformer.NewSharedInformerFactoryWithOptions(opts.MaistraCS, opts.ResyncPeriod, maistraxnsinformer.WithNamespaces())
		mrc.Register(informerFactory, "federation")
	} else {
		informerFactory = maistraxnsinformer.NewSharedInformerFactoryWithOptions(opts.MaistraCS, opts.ResyncPeriod, maistraxnsinformer.WithNamespaces(opts.Namespace))
	}
	rm := &resourceManager{
		mcs:  opts.MaistraCS,
		kc:   opts.KubeClient,
		inff: informerFactory,
		cmi:  opts.KubeClient.KubeInformer().Core().V1().ConfigMaps(),
		ei:   opts.KubeClient.KubeInformer().Core().V1().Endpoints(),
		pi:   opts.KubeClient.KubeInformer().Core().V1().Pods(),
		si:   opts.KubeClient.KubeInformer().Core().V1().Services(),
		smpi: informerFactory.Federation().V1().ServiceMeshPeers(),
		sei:  informerFactory.Federation().V1().ExportedServiceSets(),
		sii:  informerFactory.Federation().V1().ImportedServiceSets(),
	}
	// create the informers now, so they're registered with the factory
	rm.cmi.Informer()
	rm.ei.Informer()
	rm.pi.Informer()
	rm.si.Informer()
	rm.smpi.Informer()
	rm.sei.Informer()
	rm.sii.Informer()
	return rm, nil
}

type resourceManager struct {
	mcs  maistraclient.Interface
	kc   kube.Client
	inff maistraxnsinformer.SharedInformerFactory
	cmi  coreinformersv1.ConfigMapInformer
	ei   coreinformersv1.EndpointsInformer
	pi   coreinformersv1.PodInformer
	si   coreinformersv1.ServiceInformer
	smpi maistrainformersfederationv1.ServiceMeshPeerInformer
	sei  maistrainformersfederationv1.ExportedServiceSetInformer
	sii  maistrainformersfederationv1.ImportedServiceSetInformer
}

var _ ResourceManager = (*resourceManager)(nil)

func (rm *resourceManager) MaistraClientSet() maistraclient.Interface {
	return rm.mcs
}

func (rm *resourceManager) Start(stopCh <-chan struct{}) {
	rm.inff.Start(stopCh)
}

func (rm *resourceManager) HasSynced() bool {
	return rm.cmi.Informer().HasSynced() &&
		rm.ei.Informer().HasSynced() &&
		rm.pi.Informer().HasSynced() &&
		rm.si.Informer().HasSynced() &&
		rm.smpi.Informer().HasSynced() &&
		rm.sei.Informer().HasSynced() &&
		rm.sii.Informer().HasSynced()
}

func (rm *resourceManager) WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool {
	return rm.inff.WaitForCacheSync(stopCh)
}

func (rm *resourceManager) ConfigMapInformer() coreinformersv1.ConfigMapInformer {
	return rm.cmi
}

func (rm *resourceManager) EndpointsInformer() coreinformersv1.EndpointsInformer {
	return rm.ei
}

func (rm *resourceManager) PodInformer() coreinformersv1.PodInformer {
	return rm.pi
}

func (rm *resourceManager) ServiceInformer() coreinformersv1.ServiceInformer {
	return rm.si
}

func (rm *resourceManager) PeerInformer() maistrainformersfederationv1.ServiceMeshPeerInformer {
	return rm.smpi
}

func (rm *resourceManager) ExportsInformer() maistrainformersfederationv1.ExportedServiceSetInformer {
	return rm.sei
}

func (rm *resourceManager) ImportsInformer() maistrainformersfederationv1.ImportedServiceSetInformer {
	return rm.sii
}
