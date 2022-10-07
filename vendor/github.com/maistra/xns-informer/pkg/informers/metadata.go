package informers

import (
	"context"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatalister"
	"k8s.io/client-go/tools/cache"
)

// MetadataSharedInformerFactory provides access to shared informers and listers for metadata client.
type MetadataSharedInformerFactory interface {
	Start(stopCh <-chan struct{})
	SetNamespaces(namespaces []string)
	ForResource(gvr schema.GroupVersionResource) informers.GenericInformer
	WaitForCacheSync(stopCh <-chan struct{}) map[schema.GroupVersionResource]bool
}

// NewMetadataSharedInformerFactory constructs a new instance of metadataSharedInformerFactory for all namespaces.
func NewMetadataSharedInformerFactory(client metadata.Interface, defaultResync time.Duration) MetadataSharedInformerFactory {
	namespaces := NewNamespaceSet(metav1.NamespaceAll)
	return NewFilteredMetadataSharedInformerFactory(client, defaultResync, namespaces, nil)
}

// NewFilteredMetadataSharedInformerFactory constructs a new instance of metadataSharedInformerFactory.
// Listers obtained via this factory will be subject to the same filters as specified here.
func NewFilteredMetadataSharedInformerFactory(client metadata.Interface, defaultResync time.Duration, namespaces NamespaceSet, tweakListOptions TweakListOptionsFunc) MetadataSharedInformerFactory {
	return &metadataSharedInformerFactory{
		client:           client,
		defaultResync:    defaultResync,
		namespaces:       namespaces,
		informers:        map[schema.GroupVersionResource]informers.GenericInformer{},
		startedInformers: make(map[schema.GroupVersionResource]bool),
		tweakListOptions: tweakListOptions,
	}
}

type metadataSharedInformerFactory struct {
	client        metadata.Interface
	defaultResync time.Duration
	namespaces    NamespaceSet

	lock      sync.Mutex
	informers map[schema.GroupVersionResource]informers.GenericInformer
	// startedInformers is used for tracking which informers have been started.
	// This allows Start() to be called multiple times safely.
	startedInformers map[schema.GroupVersionResource]bool
	tweakListOptions TweakListOptionsFunc
}

var _ MetadataSharedInformerFactory = &metadataSharedInformerFactory{}

func (f *metadataSharedInformerFactory) ForResource(gvr schema.GroupVersionResource) informers.GenericInformer {
	f.lock.Lock()
	defer f.lock.Unlock()

	key := gvr
	informer, exists := f.informers[key]
	if exists {
		return informer
	}

	informer = NewFilteredMetadataInformer(f.client, gvr, f.namespaces, f.defaultResync, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
	f.informers[key] = informer

	return informer
}

// SetNamespaces updates the set of namespaces for all current and future informers.
func (f *metadataSharedInformerFactory) SetNamespaces(namespaces []string) {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.namespaces.SetNamespaces(namespaces)
}

// Start initializes all requested informers.
func (f *metadataSharedInformerFactory) Start(stopCh <-chan struct{}) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for informerType, informer := range f.informers {
		if !f.startedInformers[informerType] {
			go informer.Informer().Run(stopCh)
			f.startedInformers[informerType] = true
		}
	}
}

// WaitForCacheSync waits for all started informers' cache were synced.
func (f *metadataSharedInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) map[schema.GroupVersionResource]bool {
	informers := func() map[schema.GroupVersionResource]cache.SharedIndexInformer {
		f.lock.Lock()
		defer f.lock.Unlock()

		informers := map[schema.GroupVersionResource]cache.SharedIndexInformer{}
		for informerType, informer := range f.informers {
			if f.startedInformers[informerType] {
				informers[informerType] = informer.Informer()
			}
		}
		return informers
	}()

	res := map[schema.GroupVersionResource]bool{}
	for informType, informer := range informers {
		res[informType] = cache.WaitForCacheSync(stopCh, informer.HasSynced)
	}
	return res
}

// NewFilteredMetadataInformer constructs a new informer for a metadata type.
func NewFilteredMetadataInformer(client metadata.Interface, gvr schema.GroupVersionResource, namespaces NamespaceSet, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions TweakListOptionsFunc) informers.GenericInformer {
	newInformer := func(namespace string) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					if tweakListOptions != nil {
						tweakListOptions(&options)
					}
					return client.Resource(gvr).Namespace(namespace).List(context.TODO(), options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					if tweakListOptions != nil {
						tweakListOptions(&options)
					}
					return client.Resource(gvr).Namespace(namespace).Watch(context.TODO(), options)
				},
			},
			&metav1.PartialObjectMetadata{},
			resyncPeriod,
			indexers,
		)
	}

	return &metadataInformer{
		gvr:      gvr,
		informer: NewMultiNamespaceInformer(namespaces, resyncPeriod, newInformer),
	}
}

type metadataInformer struct {
	informer cache.SharedIndexInformer
	gvr      schema.GroupVersionResource
}

var _ informers.GenericInformer = &metadataInformer{}

func (d *metadataInformer) Informer() cache.SharedIndexInformer {
	return d.informer
}

func (d *metadataInformer) Lister() cache.GenericLister {
	return metadatalister.NewRuntimeObjectShim(metadatalister.New(d.informer.GetIndexer(), d.gvr))
}
