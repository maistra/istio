package informers

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// TweakListOptionsFunc defines the signature of a helper function that allows
// filtering in informers created by shared informer factories.
type TweakListOptionsFunc func(*metav1.ListOptions)

// MultiNamespaceInformer extends the cache.SharedIndexInformer interface with
// methods relevant to cross-namespace informers.
type MultiNamespaceInformer interface {
	cache.SharedIndexInformer

	AddNamespace(namespace string)
	RemoveNamespace(namespace string)
	GetIndexers() map[string]cache.Indexer
}

// NewInformerFunc returns a new informer for a given namespace.
type NewInformerFunc func(namespace string) cache.SharedIndexInformer

// informerData holds a single namespaced informer.
type informerData struct {
	informer cache.SharedIndexInformer
	stopCh   chan struct{}
	started  bool
}

// eventHandlerData holds an event handler and its resync period.
type eventHandlerData struct {
	handler      cache.ResourceEventHandler
	resyncPeriod time.Duration
}

// multiNamespaceInformer satisfies the SharedIndexInformer interface and
// provides an informer that works across a set of namespaces -- though not all
// methods are actually usable.
type multiNamespaceInformer struct {
	informers     map[string]cache.SharedIndexInformer
	stopChans     map[string]chan struct{}
	errorHandler  cache.WatchErrorHandler
	eventHandlers []eventHandlerData
	indexers      []cache.Indexers
	resyncPeriod  time.Duration
	lock          sync.Mutex
	started       bool
	namespaces    NamespaceSet
	newInformer   NewInformerFunc
}

var _ cache.SharedIndexInformer = &multiNamespaceInformer{}

// NewMultiNamespaceInformer returns a new cross-namespace informer.  The given
// NewInformerFunc will be used to craft new single-namespace informers when
// adding namespaces.
func NewMultiNamespaceInformer(namespaces NamespaceSet, resync time.Duration, newInformer NewInformerFunc) MultiNamespaceInformer {
	informer := &multiNamespaceInformer{
		informers:     make(map[string]cache.SharedIndexInformer),
		stopChans:     make(map[string]chan struct{}),
		eventHandlers: make([]eventHandlerData, 0),
		indexers:      make([]cache.Indexers, 0),
		namespaces:    namespaces,
		resyncPeriod:  resync,
		newInformer:   newInformer,
	}

	namespaces.AddHandler(NamespaceSetHandlerFuncs{
		AddFunc:    informer.AddNamespace,
		RemoveFunc: informer.RemoveNamespace,
	})

	return informer
}

// GetController is unimplemented and always returns nil.
func (i *multiNamespaceInformer) GetController() cache.Controller {
	return nil
}

// GetStore returns a new cache.Store providing read-only access to the
// informer's underlying per-namespace caches.
func (i *multiNamespaceInformer) GetStore() cache.Store {
	return NewCacheReader(i)
}

// GetIndexer returns a new cache.Indexer providing read-only access to the
// informer's underlying per-namespace caches.
func (i *multiNamespaceInformer) GetIndexer() cache.Indexer {
	return NewCacheReader(i)
}

// LastSyncResourceVersion always returns an empty string at the moment.
func (i *multiNamespaceInformer) LastSyncResourceVersion() string {
	return "" // TODO: What's the most correct thing here?
}

// SetWatchErrorHandler sets the error handler for the informer's underlying
// watches.  The handler will also be added for any new namespaces added later.
// This must be called before the first time the informer is started.
func (i *multiNamespaceInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.errorHandler = handler

	for _, informer := range i.informers {
		if err := informer.SetWatchErrorHandler(handler); err != nil {
			return err
		}
	}

	return nil
}

// AddNamespace adds the given namespace to the informer.  This is a no-op if an
// informer for this namespace already exists.  You must call one of the run
// functions and wait for the caches to sync before the new informer is useful.
// This is usually done via a factory with Start() and WaitForCacheSync().
func (i *multiNamespaceInformer) AddNamespace(namespace string) {
	i.lock.Lock()
	defer i.lock.Unlock()

	// If an informer for this namespace already exists, this is a no-op.
	if _, ok := i.informers[namespace]; ok {
		return
	}

	informer := i.newInformer(namespace)

	// Add indexers to the new informer.
	for _, idx := range i.indexers {
		informer.AddIndexers(idx)
	}

	// Add event handlers to the new informer.
	for _, handler := range i.eventHandlers {
		informer.AddEventHandlerWithResyncPeriod(
			handler.handler,
			handler.resyncPeriod,
		)
	}

	// Add watch error handler.
	if i.errorHandler != nil {
		if err := informer.SetWatchErrorHandler(i.errorHandler); err != nil {
			klog.Errorf("Failed to set watch error handler for namespace %q: %v",
				namespace, err)
		}
	}

	stopCh := make(chan struct{})

	i.informers[namespace] = informer
	i.stopChans[namespace] = stopCh

	if i.started {
		go informer.Run(stopCh)
	}

	klog.V(4).Infof("Added informer for namespace: %q", namespace)
}

// RemoveNamespace stops and deletes the informer for the given namespace.
func (i *multiNamespaceInformer) RemoveNamespace(namespace string) {
	i.lock.Lock()
	defer i.lock.Unlock()

	informer, ok := i.informers[namespace]

	// If there is no informer for this namespace, this is a no-op.
	if !ok {
		return
	}

	close(i.stopChans[namespace]) // Stop the informer.

	// Send delete events for everything in the store.
	for _, obj := range informer.GetStore().List() {
		for _, h := range i.eventHandlers {
			h.handler.OnDelete(obj)
		}
	}

	delete(i.stopChans, namespace)
	delete(i.informers, namespace)

	klog.V(4).Infof("Removed informer for namespace: %q", namespace)
}

// Run starts all informers and waits for the stop channel to close.
func (i *multiNamespaceInformer) Run(stopCh <-chan struct{}) {
	func() {
		i.lock.Lock()
		defer i.lock.Unlock()

		for namespace, informer := range i.informers {
			go informer.Run(i.stopChans[namespace])
		}

		i.started = true
	}()

	<-stopCh // Block until stopCh is closed.

	i.lock.Lock()
	defer i.lock.Unlock()

	for namespace := range i.informers {
		// Close and recreate the channel.
		close(i.stopChans[namespace])
		i.stopChans[namespace] = make(chan struct{})
	}

	i.started = false
}

// AddEventHandler adds the given handler to each namespaced informer.
func (i *multiNamespaceInformer) AddEventHandler(handler cache.ResourceEventHandler) {
	i.AddEventHandlerWithResyncPeriod(handler, i.resyncPeriod)
}

// AddEventHandlerWithResyncPeriod adds the given handler with a resync period
// to each namespaced informer.  The handler will also be added to any informers
// created later as namespaces are added.
func (i *multiNamespaceInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.eventHandlers = append(i.eventHandlers, eventHandlerData{
		handler:      handler,
		resyncPeriod: resyncPeriod,
	})

	for _, informer := range i.informers {
		informer.AddEventHandlerWithResyncPeriod(handler, resyncPeriod)
	}
}

// AddIndexers adds the given indexers to each namespaced informer.
func (i *multiNamespaceInformer) AddIndexers(indexers cache.Indexers) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.indexers = append(i.indexers, indexers)

	for _, informer := range i.informers {
		err := informer.AddIndexers(indexers)
		if err != nil {
			return err
		}
	}

	return nil
}

// HasSynced checks if each namespaced informer has synced.
func (i *multiNamespaceInformer) HasSynced() bool {
	i.lock.Lock()
	defer i.lock.Unlock()

	if !i.namespaces.Initialized() {
		return false
	}

	for _, informer := range i.informers {
		if synced := informer.HasSynced(); !synced {
			return false
		}
	}

	return true
}

// GetIndexers returns a map of namespaces to their cache.Indexer.
func (i *multiNamespaceInformer) GetIndexers() map[string]cache.Indexer {
	i.lock.Lock()
	defer i.lock.Unlock()

	res := make(map[string]cache.Indexer, len(i.informers))
	for namespace, informer := range i.informers {
		res[namespace] = informer.GetIndexer()
	}

	return res
}
