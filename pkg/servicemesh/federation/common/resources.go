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

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	maistrainformersfederationv1 "maistra.io/api/client/informers/externalversions/federation/v1"
	maistraclient "maistra.io/api/client/versioned"
	maistraxnsinformer "maistra.io/api/client/xnsinformer"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
)

type ResourceManager interface {
	MaistraClientSet() maistraclient.Interface
	ConfigMapInformer() kclient.Client[*corev1.ConfigMap]
	EndpointSliceInformer() kclient.Client[*discoveryv1.EndpointSlice]
	PodInformer() kclient.Client[*corev1.Pod]
	ServiceInformer() kclient.Client[*corev1.Service]
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
	//nolint:revive
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
		cmi:  kclient.NewFiltered[*corev1.ConfigMap](opts.KubeClient, kclient.Filter{}),
		esi:  kclient.NewFiltered[*discoveryv1.EndpointSlice](opts.KubeClient, kclient.Filter{}),
		pi: kclient.NewFiltered[*corev1.Pod](opts.KubeClient, kclient.Filter{
			ObjectTransform: kube.StripPodUnusedFields,
		}),
		si:   kclient.NewFiltered[*corev1.Service](opts.KubeClient, kclient.Filter{}),
		smpi: informerFactory.Federation().V1().ServiceMeshPeers(),
		sei:  informerFactory.Federation().V1().ExportedServiceSets(),
		sii:  informerFactory.Federation().V1().ImportedServiceSets(),
	}
	// create the informers now, so they're registered with the factory
	rm.smpi.Informer()
	rm.sei.Informer()
	rm.sii.Informer()
	return rm, nil
}

type resourceManager struct {
	mcs  maistraclient.Interface
	kc   kube.Client
	inff maistraxnsinformer.SharedInformerFactory
	cmi  kclient.Client[*corev1.ConfigMap]
	esi  kclient.Client[*discoveryv1.EndpointSlice]
	pi   kclient.Client[*corev1.Pod]
	si   kclient.Client[*corev1.Service]
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
	return rm.smpi.Informer().HasSynced() &&
		rm.sei.Informer().HasSynced() &&
		rm.sii.Informer().HasSynced() &&
		rm.cmi.HasSynced() &&
		rm.esi.HasSynced() &&
		rm.pi.HasSynced() &&
		rm.si.HasSynced()
}

func (rm *resourceManager) WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool {
	return rm.inff.WaitForCacheSync(stopCh)
}

func (rm *resourceManager) ConfigMapInformer() kclient.Client[*corev1.ConfigMap] {
	return rm.cmi
}

func (rm *resourceManager) EndpointSliceInformer() kclient.Client[*discoveryv1.EndpointSlice] {
	return rm.esi
}

func (rm *resourceManager) PodInformer() kclient.Client[*corev1.Pod] {
	return rm.pi
}

func (rm *resourceManager) ServiceInformer() kclient.Client[*corev1.Service] {
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
