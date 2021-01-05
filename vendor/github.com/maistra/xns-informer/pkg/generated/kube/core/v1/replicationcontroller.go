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

package v1

import (
	"context"
	time "time"

	internalinterfaces "github.com/maistra/xns-informer/pkg/generated/kube/internalinterfaces"
	informers "github.com/maistra/xns-informer/pkg/informers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	kubernetes "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	cache "k8s.io/client-go/tools/cache"
)

// ReplicationControllerInformer provides access to a shared informer and lister for
// ReplicationControllers.
type ReplicationControllerInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.ReplicationControllerLister
}

type replicationControllerInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespaces       informers.NamespaceSet
}

// NewReplicationControllerInformer constructs a new informer for ReplicationController type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewReplicationControllerInformer(client kubernetes.Interface, namespaces informers.NamespaceSet, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredReplicationControllerInformer(client, namespaces, resyncPeriod, indexers, nil)
}

// NewFilteredReplicationControllerInformer constructs a new informer for ReplicationController type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredReplicationControllerInformer(client kubernetes.Interface, namespaces informers.NamespaceSet, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	newInformer := func(namespace string) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					if tweakListOptions != nil {
						tweakListOptions(&options)
					}
					return client.CoreV1().ReplicationControllers(namespace).List(context.TODO(), options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					if tweakListOptions != nil {
						tweakListOptions(&options)
					}
					return client.CoreV1().ReplicationControllers(namespace).Watch(context.TODO(), options)
				},
			},
			&corev1.ReplicationController{},
			resyncPeriod,
			indexers,
		)
	}

	return informers.NewMultiNamespaceInformer(namespaces, resyncPeriod, newInformer)
}

func (f *replicationControllerInformer) defaultInformer(client kubernetes.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredReplicationControllerInformer(client, f.namespaces, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *replicationControllerInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&corev1.ReplicationController{}, f.defaultInformer)
}

func (f *replicationControllerInformer) Lister() v1.ReplicationControllerLister {
	return v1.NewReplicationControllerLister(f.Informer().GetIndexer())
}
