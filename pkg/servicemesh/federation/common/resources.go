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

	maistrainformersfederationv1 "maistra.io/api/client/informers/externalversions/federation/v1"
	maistraclient "maistra.io/api/client/versioned"
	maistraxnsinformer "maistra.io/api/client/xnsinformer"

	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
)

type ResourceManager interface {
	MaistraClientSet() maistraclient.Interface
	KubeClient() kube.Client
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
		informerFactory = maistraxnsinformer.NewSharedInformerFactoryWithOptions(opts.MaistraCS, opts.ResyncPeriod)
		mrc.Register(informerFactory, "federation")
	} else {
		informerFactory = maistraxnsinformer.NewSharedInformerFactoryWithOptions(opts.MaistraCS, opts.ResyncPeriod, maistraxnsinformer.WithNamespaces(opts.Namespace))
	}
	rm := &resourceManager{
		mcs:  opts.MaistraCS,
		kc:   opts.KubeClient,
		inff: informerFactory,
		pi:   informerFactory.Federation().V1().ServiceMeshPeers(),
		sei:  informerFactory.Federation().V1().ExportedServiceSets(),
		sii:  informerFactory.Federation().V1().ImportedServiceSets(),
	}
	// create the informers now, so they're registered with the factory
	rm.pi.Informer()
	rm.sei.Informer()
	rm.sii.Informer()
	return rm, nil
}

type resourceManager struct {
	mcs  maistraclient.Interface
	kc   kube.Client
	inff maistraxnsinformer.SharedInformerFactory
	pi   maistrainformersfederationv1.ServiceMeshPeerInformer
	sei  maistrainformersfederationv1.ExportedServiceSetInformer
	sii  maistrainformersfederationv1.ImportedServiceSetInformer
}

var _ ResourceManager = (*resourceManager)(nil)

func (rm *resourceManager) MaistraClientSet() maistraclient.Interface {
	return rm.mcs
}

func (rm *resourceManager) KubeClient() kube.Client {
	return rm.kc
}

func (rm *resourceManager) Start(stopCh <-chan struct{}) {
	rm.inff.Start(stopCh)
}

func (rm *resourceManager) HasSynced() bool {
	return rm.pi.Informer().HasSynced() && rm.sei.Informer().HasSynced() && rm.sii.Informer().HasSynced()
}

func (rm *resourceManager) WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool {
	return rm.inff.WaitForCacheSync(stopCh)
}

func (rm *resourceManager) PeerInformer() maistrainformersfederationv1.ServiceMeshPeerInformer {
	return rm.pi
}

func (rm *resourceManager) ExportsInformer() maistrainformersfederationv1.ExportedServiceSetInformer {
	return rm.sei
}

func (rm *resourceManager) ImportsInformer() maistrainformersfederationv1.ImportedServiceSetInformer {
	return rm.sii
}
