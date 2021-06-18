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

	maistrainformerscorev1 "maistra.io/api/client/informers/externalversions/core/v1"
	maistraclient "maistra.io/api/client/versioned"
	maistraxnsinformer "maistra.io/api/client/xnsinformer"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
)

type ResourceManager interface {
	MaistraClientSet() maistraclient.Interface
	KubeClient() kube.Client
	MeshFederationInformer() maistrainformerscorev1.MeshFederationInformer
	ServiceExportsInformer() maistrainformerscorev1.ServiceExportsInformer
	ServiceImportsInformer() maistrainformerscorev1.ServiceImportsInformer
	DefaultServiceExports(namespace string) (*v1.ServiceExports, error)
	DefaultServiceImports(namespace string) (*v1.ServiceImports, error)
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
		mfi:  informerFactory.Core().V1().MeshFederations(),
		sei:  informerFactory.Core().V1().ServiceExports(),
		sii:  informerFactory.Core().V1().ServiceImports(),
	}
	// create the informers now, so they're registered with the factory
	rm.mfi.Informer()
	rm.sei.Informer()
	rm.sii.Informer()
	return rm, nil
}

type resourceManager struct {
	mcs  maistraclient.Interface
	kc   kube.Client
	inff maistraxnsinformer.SharedInformerFactory
	mfi  maistrainformerscorev1.MeshFederationInformer
	sei  maistrainformerscorev1.ServiceExportsInformer
	sii  maistrainformerscorev1.ServiceImportsInformer
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
	return rm.mfi.Informer().HasSynced() && rm.sei.Informer().HasSynced() && rm.sii.Informer().HasSynced()
}

func (rm *resourceManager) WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool {
	return rm.inff.WaitForCacheSync(stopCh)
}

func (rm *resourceManager) MeshFederationInformer() maistrainformerscorev1.MeshFederationInformer {
	return rm.mfi
}

func (rm *resourceManager) ServiceExportsInformer() maistrainformerscorev1.ServiceExportsInformer {
	return rm.sei
}

func (rm *resourceManager) ServiceImportsInformer() maistrainformerscorev1.ServiceImportsInformer {
	return rm.sii
}

func (rm *resourceManager) DefaultServiceExports(namespace string) (*v1.ServiceExports, error) {
	return rm.sei.Lister().ServiceExports(namespace).Get("default")
}

func (rm *resourceManager) DefaultServiceImports(namespace string) (*v1.ServiceImports, error) {
	return rm.sii.Lister().ServiceImports(namespace).Get("default")
}
