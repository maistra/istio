/*
Copyright Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by xns-informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	internalinterfaces "github.com/maistra/xns-informer/pkg/generated/kube/internalinterfaces"
	informers "github.com/maistra/xns-informer/pkg/informers"
	storagev1alpha1 "k8s.io/api/storage/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	kubernetes "k8s.io/client-go/kubernetes"
	v1alpha1 "k8s.io/client-go/listers/storage/v1alpha1"
	cache "k8s.io/client-go/tools/cache"
)

// CSIStorageCapacityInformer provides access to a shared informer and lister for
// CSIStorageCapacities.
type CSIStorageCapacityInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.CSIStorageCapacityLister
}

type cSIStorageCapacityInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespaces       informers.NamespaceSet
}

// NewCSIStorageCapacityInformer constructs a new informer for CSIStorageCapacity type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewCSIStorageCapacityInformer(client kubernetes.Interface, namespaces informers.NamespaceSet, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredCSIStorageCapacityInformer(client, namespaces, resyncPeriod, indexers, nil)
}

// NewFilteredCSIStorageCapacityInformer constructs a new informer for CSIStorageCapacity type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredCSIStorageCapacityInformer(client kubernetes.Interface, namespaces informers.NamespaceSet, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	newInformer := func(namespace string) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
					if tweakListOptions != nil {
						tweakListOptions(&options)
					}
					return client.StorageV1alpha1().CSIStorageCapacities(namespace).List(context.TODO(), options)
				},
				WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
					if tweakListOptions != nil {
						tweakListOptions(&options)
					}
					return client.StorageV1alpha1().CSIStorageCapacities(namespace).Watch(context.TODO(), options)
				},
			},
			&storagev1alpha1.CSIStorageCapacity{},
			resyncPeriod,
			indexers,
		)
	}

	return informers.NewMultiNamespaceInformer(namespaces, resyncPeriod, newInformer)
}

func (f *cSIStorageCapacityInformer) defaultInformer(client kubernetes.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredCSIStorageCapacityInformer(client, f.namespaces, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *cSIStorageCapacityInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&storagev1alpha1.CSIStorageCapacity{}, f.defaultInformer)
}

func (f *cSIStorageCapacityInformer) Lister() v1alpha1.CSIStorageCapacityLister {
	return v1alpha1.NewCSIStorageCapacityLister(f.Informer().GetIndexer())
}
