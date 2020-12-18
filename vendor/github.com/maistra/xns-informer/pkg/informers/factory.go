package informers

import (
	"context"
	"sync"
	"time"

	"github.com/maistra/xns-informer/pkg/internal/sets"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

// SharedInformerFactory provides shared informers for any resource type and
// works across a set of namespaces, which can be updated at any time.
type SharedInformerFactory interface {
	Start(stopCh <-chan struct{})
	ForResource(gvr schema.GroupVersionResource, opts ResourceOptions) informers.GenericInformer
	WaitForCacheSync(stopCh <-chan struct{}) bool
	SetNamespaces(namespaces []string)
	GetScheme() *runtime.Scheme
}

// ResourceOptions represents optional parameters to the ForResource method.
type ResourceOptions struct {
	ClusterScoped      bool
	ListWatchConverter *ListWatchConverter
}

// SharedInformerOption is a functional option for a SharedInformerFactory.
type SharedInformerOption func(*multiNamespaceInformerFactory) *multiNamespaceInformerFactory

// WithScheme sets a custom scheme for a SharedInformerFactory.
func WithScheme(scheme *runtime.Scheme) SharedInformerOption {
	return func(factory *multiNamespaceInformerFactory) *multiNamespaceInformerFactory {
		factory.scheme = scheme
		return factory
	}
}

// WithNamespaces sets the namespaces for a SharedInformerFactory.
func WithNamespaces(namespaces []string) SharedInformerOption {
	return func(factory *multiNamespaceInformerFactory) *multiNamespaceInformerFactory {
		factory.SetNamespaces(namespaces)
		return factory
	}
}

// WithTweakListOptions sets list options for a SharedInformerFactory.
func WithTweakListOptions(tweakListOptions dynamicinformer.TweakListOptionsFunc) SharedInformerOption {
	return func(factory *multiNamespaceInformerFactory) *multiNamespaceInformerFactory {
		factory.tweakListOptions = tweakListOptions
		return factory
	}
}

// WithCustomResyncConfig sets custom resync period for certain resources.
func WithCustomResyncConfig(config map[schema.GroupVersionResource]time.Duration) SharedInformerOption {
	return func(factory *multiNamespaceInformerFactory) *multiNamespaceInformerFactory {
		for gvr, resyncPeriod := range config {
			factory.customResync[gvr] = resyncPeriod
		}
		return factory
	}
}

// Informers are cached by resource version and whether their backing stores are
// expected to contain unstructured or structured objects.
type informersCacheKey struct {
	resource     schema.GroupVersionResource
	unstructured bool
}

type informersCache map[informersCacheKey]*multiNamespaceGenericInformer

// multiNamespaceInformerFactory provides a dynamic informer factory that
// creates informers which track changes across a set of namespaces.
type multiNamespaceInformerFactory struct {
	client           dynamic.Interface
	scheme           *runtime.Scheme
	resyncPeriod     time.Duration
	lock             sync.Mutex
	namespaces       sets.Set
	tweakListOptions dynamicinformer.TweakListOptionsFunc
	customResync     map[schema.GroupVersionResource]time.Duration

	// Cache of previously created informers.
	informers informersCache
}

var _ SharedInformerFactory = &multiNamespaceInformerFactory{}

// NewSharedInformerFactory returns a new cross-namespace shared informer
// factory.  Use SetNamespaces on the resulting factory to configure the set of
// namespaces to be watched.
func NewSharedInformerFactory(client dynamic.Interface, resync time.Duration) SharedInformerFactory {
	return NewSharedInformerFactoryWithOptions(client, resync)
}

// NewSharedInformerFactoryWithOptions constructs a new cross-namespace shared
// informer factory with the given options applied.  You must either supply the
// WithNamespaces option, or call SetNamespaces on the returned factory to
// configure the set of namespaces to be watched.
func NewSharedInformerFactoryWithOptions(client dynamic.Interface, resync time.Duration, options ...SharedInformerOption) SharedInformerFactory {
	factory := &multiNamespaceInformerFactory{
		client:       client,
		scheme:       scheme.Scheme,
		resyncPeriod: resync,
		informers:    make(informersCache),
		customResync: make(map[schema.GroupVersionResource]time.Duration),
	}

	for _, opt := range options {
		factory = opt(factory)
	}

	return factory
}

// GetScheme returns the runtime.Scheme for the factory.
func (f *multiNamespaceInformerFactory) GetScheme() *runtime.Scheme {
	return f.scheme
}

// SetNamespaces sets the list of namespaces the factory and its informers
// track.  Any new namespaces in the given set will be added to all previously
// created informers, and any namespaces that aren't in the new set will be
// removed.  You must call Start() and WaitForCacheSync() after changing the set
// of namespaces.  These are safe to call multiple times.
func (f *multiNamespaceInformerFactory) SetNamespaces(namespaces []string) {
	f.lock.Lock()
	defer f.lock.Unlock()

	newNamespaceSet := sets.NewSet(namespaces...)

	// If the set of namespaces, includes metav1.NamespaceAll, then it
	// only makes sense to create a single informer for that.
	if newNamespaceSet.Contains(metav1.NamespaceAll) {
		newNamespaceSet = sets.NewSet(metav1.NamespaceAll)
	}

	// Remove any namespaces in the current set which aren't in the
	// new set from the existing informers.
	for namespace := range f.namespaces.Difference(newNamespaceSet) {
		for _, i := range f.informers {
			i.informer.RemoveNamespace(namespace)
		}
	}

	f.namespaces = newNamespaceSet

	// Add any new namespaces to existing informers.
	for namespace := range f.namespaces {
		for _, i := range f.informers {
			i.informer.AddNamespace(namespace)
		}
	}
}

// ForResource returns a new cross-namespace informer for the given resource
// type.  If an informer for this resource type has been previously requested,
// it will be returned, otherwise a new one will be created.
func (f *multiNamespaceInformerFactory) ForResource(gvr schema.GroupVersionResource, opts ResourceOptions) informers.GenericInformer {
	f.lock.Lock()
	defer f.lock.Unlock()

	// The key for the informers cached is comprised of the resource type and
	// whether the cache is expected to hold unstructured objects.
	cacheKey := informersCacheKey{
		resource:     gvr,
		unstructured: opts.ListWatchConverter == nil,
	}

	// Return existing informer if found.
	if informer, ok := f.informers[cacheKey]; ok {
		return informer
	}

	// Check for a custom resync period for this resource type.
	resyncPeriod, ok := f.customResync[gvr]
	if !ok {
		resyncPeriod = f.resyncPeriod
	}

	newInformerFunc := func(namespace string) cache.SharedIndexInformer {
		// Namespace argument is ignored for cluster-scoped resources.
		if opts.ClusterScoped {
			namespace = metav1.NamespaceAll
		}

		lw := &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				if f.tweakListOptions != nil {
					f.tweakListOptions(&opts)
				}
				return f.client.Resource(gvr).Namespace(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				if f.tweakListOptions != nil {
					f.tweakListOptions(&opts)
				}
				return f.client.Resource(gvr).Namespace(namespace).Watch(context.TODO(), opts)
			},
		}

		var obj runtime.Object = &unstructured.Unstructured{}

		if opts.ListWatchConverter != nil {
			lw = opts.ListWatchConverter.Wrap(lw)
			obj = opts.ListWatchConverter.NewObject()
		}

		return cache.NewSharedIndexInformer(
			lw, obj,
			resyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
	}

	informer := NewMultiNamespaceInformer(!opts.ClusterScoped, f.resyncPeriod, newInformerFunc)
	lister := cache.NewGenericLister(informer.GetIndexer(), gvr.GroupResource())

	for namespace := range f.namespaces {
		informer.AddNamespace(namespace)
	}

	f.informers[cacheKey] = &multiNamespaceGenericInformer{
		informer: informer,
		lister:   lister,
	}

	return f.informers[cacheKey]
}

// Start starts all of the informers the factory has created to this point.
// They will be stopped when stopCh is closed.  Start is safe to call multiple
// times -- only stopped informers will be started.  This is non-blocking.
func (f *multiNamespaceInformerFactory) Start(stopCh <-chan struct{}) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, i := range f.informers {
		i.informer.NonBlockingRun(stopCh)
	}
}

// WaitForCacheSync waits for all previously started informers caches to sync.
func (f *multiNamespaceInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) bool {
	syncFuncs := func() (syncFuncs []cache.InformerSynced) {
		f.lock.Lock()
		defer f.lock.Unlock()

		for _, i := range f.informers {
			syncFuncs = append(syncFuncs, i.informer.HasSynced)
		}

		return
	}

	return cache.WaitForCacheSync(stopCh, syncFuncs()...)
}
