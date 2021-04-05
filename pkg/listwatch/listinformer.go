// Copyright 2019 Istio Authors
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

// Provides multiple namespace listerWatcher. This implementation is Largely from
// https://github.com/coreos/prometheus-operator/pkg/listwatch/listwatch.go

package listwatch

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

/*
 * This type adapts the events from the informer into events which would come
 * from a Watch, in this way we can use this as a lower level cache handling
 * individual namespaces to feed a higher level cache aggregating across all
 * the namespaces.
 *
 * This type will also support draining in that once the namespace has been removed
 * from the aggregated cache it will issue DELETED events for each entry within this
 * namespace so that the upper level cache is consistent.
 */
type listerInformer struct {
	namespace  string
	informer   cache.SharedInformer
	lllw       *listLenListerWatcher
	events     func(*watch.Event)
	stopped    chan struct{}
	hasStopped bool
	draining   bool
	drained    func(string)
	lock       sync.RWMutex
}

func newListerInformer(namespace string, f func(string) cache.ListerWatcher, exampleObject runtime.Object,
	resyncPeriod time.Duration, events func(*watch.Event), drained func(string)) *listerInformer {

	lllw := newListLenListerWatcher(f(namespace))
	informer := cache.NewSharedInformer(lllw, exampleObject, resyncPeriod)

	li := &listerInformer{
		namespace: namespace,
		informer:  informer,
		lllw:      lllw,
		events:    events,
		stopped:   make(chan struct{}),
		drained:   drained,
	}
	informer.AddEventHandler(li)
	go informer.Run(li.stopped)
	return li
}

// The lister informer is considered synced if the underlying
// informer has synced *and* we have passed sufficient ADDED events
// to the higher level cache to match the underlying List count.
func (li *listerInformer) hasSynced() bool {
	return li.informer.HasSynced() && li.lllw.hasReachedListCount()
}

func (li *listerInformer) isDraining() bool {
	li.lock.RLock()
	defer li.lock.RUnlock()
	return li.draining
}

func (li *listerInformer) isStopped() bool {
	li.lock.RLock()
	defer li.lock.RUnlock()
	return li.hasStopped
}

func (li *listerInformer) drain() {
	// If we are already draining then there is nothing to do
	shouldDrain := func() bool {
		li.lock.Lock()
		defer li.lock.Unlock()
		if li.draining {
			return false
		}
		li.draining = true
		// We are draining.  Stop the underlying informer so it
		// cannot interfere with our Delete events
		li.stopInformer()
		return true
	}()
	if shouldDrain {
		go func() {
			// Issue Delete events for each entry remaining in the store
			store := li.informer.GetStore()
			resourcesToDrain := store.List()
			for _, resource := range resourcesToDrain {
				li.OnDelete(resource)
			}
			li.lock.Lock()
			defer li.lock.Unlock()
			if !li.hasStopped {
				li.hasStopped = true
				li.drained(li.namespace)
			}
		}()
	}
}

func (li *listerInformer) stopInformer() {
	close(li.stopped)
}

func (li *listerInformer) stop() {
	li.lock.Lock()
	defer li.lock.Unlock()
	li.hasStopped = true
	li.stopInformer()
}

func (li *listerInformer) newWatchEvent(eventType watch.EventType, obj interface{}) *watch.Event {
	return &watch.Event{
		eventType, obj.(runtime.Object),
	}
}

func (li *listerInformer) sendEvent(event *watch.Event) {
	li.lock.RLock()
	defer li.lock.RUnlock()
	if !li.hasStopped {
		li.events(event)
	}
}

// Adapt an OnAdd event into a watch ADDED event
func (li *listerInformer) OnAdd(obj interface{}) {
	li.sendEvent(li.newWatchEvent(watch.Added, obj))
	li.lllw.incAddCount()
}

// Adapt an OnUpdate event into a watch MODIFIED event
func (li *listerInformer) OnUpdate(oldObj, newObj interface{}) {
	li.sendEvent(li.newWatchEvent(watch.Modified, newObj))
}

// Adapt an OnDelete event into a watch DELETED event
func (li *listerInformer) OnDelete(obj interface{}) {
	li.sendEvent(li.newWatchEvent(watch.Deleted, obj))
}
