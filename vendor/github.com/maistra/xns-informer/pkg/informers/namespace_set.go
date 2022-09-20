package informers

import (
	"sort"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/maistra/xns-informer/pkg/internal/sets"
)

// NamespaceSetHandler handles add and remove events for namespace sets.
type NamespaceSetHandler interface {
	OnAdd(namespace string)
	OnRemove(namespace string)
}

// NamespaceSetHandlerFuncs is a helper for implementing NamespaceSetHandler.
type NamespaceSetHandlerFuncs struct {
	AddFunc    func(namespace string)
	RemoveFunc func(namespace string)
}

// OnAdd calls AddFunc if it is non-nil.
func (h NamespaceSetHandlerFuncs) OnAdd(namespace string) {
	if h.AddFunc != nil {
		h.AddFunc(namespace)
	}
}

// OnRemove calls RemoveFunc if it is non-nil.
func (h NamespaceSetHandlerFuncs) OnRemove(namespace string) {
	if h.RemoveFunc != nil {
		h.RemoveFunc(namespace)
	}
}

// NamespaceSet represents a dynamic set of namespaces.  The set can be updated
// with SetNamespaces, and handlers can be added with AddHandler that will
// respond to addition or removal of individual namespaces.
type NamespaceSet interface {
	// Initialized returns true if SetNamespaces() has been called at least once
	Initialized() bool
	SetNamespaces(namespaces ...string)
	AddHandler(handler NamespaceSetHandler)
	Contains(namespace string) bool
	List() []string
}

type namespaceSet struct {
	initialized bool
	lock        sync.Mutex
	namespaces  sets.Set
	handlers    []NamespaceSetHandler
}

// NewNamespaceSet returns a new NamespaceSet.
func NewNamespaceSet() NamespaceSet {
	return &namespaceSet{}
}

// Contains indicates whether the given namespace is in the set.
func (n *namespaceSet) Contains(namespace string) bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	return n.namespaces.Contains(namespace)
}

// List returns the set as a sort slice of strings.
func (n *namespaceSet) List() []string {
	n.lock.Lock()
	defer n.lock.Unlock()

	namespaces := n.namespaces.UnsortedList()
	sort.Strings(namespaces)
	return namespaces
}

// Initialized returns true after SetNamespaces is called at least once.
func (n *namespaceSet) Initialized() bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	return n.initialized
}

// SetNamespaces replaces the set of namespaces.
func (n *namespaceSet) SetNamespaces(namespaces ...string) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if !n.initialized {
		n.initialized = true
	}

	newNamespaceSet := sets.NewSet(namespaces...)

	// If the set of namespaces, includes metav1.NamespaceAll, then it
	// only makes sense to track that.
	if newNamespaceSet.Contains(metav1.NamespaceAll) {
		newNamespaceSet = sets.NewSet(metav1.NamespaceAll)
	}

	klog.V(2).Infof("SetNamespaces: %q", newNamespaceSet.UnsortedList())

	// Call OnRemove handlers.
	for namespace := range n.namespaces.Difference(newNamespaceSet) {
		klog.V(2).Infof("Calling remove funcs for: %q", namespace)
		for _, h := range n.handlers {
			h.OnRemove(namespace)
		}
	}

	// Call OnAdd handlers.
	for namespace := range newNamespaceSet.Difference(n.namespaces) {
		klog.V(2).Infof("Calling add funcs for: %q", namespace)
		for _, h := range n.handlers {
			h.OnAdd(namespace)
		}
	}

	n.namespaces = newNamespaceSet
}

// AddHandler adds a handler for add and remove events.
func (n *namespaceSet) AddHandler(handler NamespaceSetHandler) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.handlers = append(n.handlers, handler)

	for ns := range n.namespaces {
		handler.OnAdd(ns)
	}
}
