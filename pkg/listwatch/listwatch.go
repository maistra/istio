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

	"github.com/pkg/errors"
	"istio.io/istio/pkg/servicemesh/controller"
	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("mlw", "mlw", 0)
)

type NamespaceListerWatcher interface {
	cache.ListerWatcher
	controller.MemberRollListener
}

// MultiNamespaceListerWatcher takes a list of namespaces,
// cache.ListerWatcher generator func, an Object and a Duration
// and returns a single cache.ListerWatcher capable of operating on multiple namespaces.
func MultiNamespaceListerWatcher(namespaces []string, f func(string) cache.ListerWatcher,
	exampleObject runtime.Object, resyncPeriod time.Duration) NamespaceListerWatcher {
	return newMultiListerWatcher(namespaces, f, exampleObject, resyncPeriod)
}

type listerWatcherState int

const (
	created listerWatcherState = iota
	listing
	listed
	watching
)

// multiListerWatcher encapsulates multiple informers and aggregates their content through a single channel
type multiListerWatcher struct {
	namespaces    []string
	f             func(string) cache.ListerWatcher
	state         listerWatcherState
	result        chan watch.Event
	exampleObject runtime.Object
	resyncPeriod  time.Duration
	stopped       chan struct{}
	events        chan *watch.Event
	activeMap     map[string]*listerInformer
	drainingMap   map[string]*listerInformer
	drained       chan string
	listingMap    map[string]*runtime.Object
	lock          sync.RWMutex
}

func newMultiListerWatcher(namespaces []string, f func(string) cache.ListerWatcher, exampleObject runtime.Object,
	resyncPeriod time.Duration) *multiListerWatcher {

	return &multiListerWatcher{
		namespaces:    append(namespaces[:0:0], namespaces...),
		f:             f,
		state:         created,
		exampleObject: exampleObject,
		resyncPeriod:  resyncPeriod,
	}
}

// Proxy the event on behalf of each namespace
func (mlw *multiListerWatcher) handleEvents(event *watch.Event) {
	select {
	case <-mlw.stopped:
	case mlw.events <- event:
	}
}

// Proxy the drained notification on behalf of each namespace
func (mlw *multiListerWatcher) handleDrained(namespace string) {
	select {
	case <-mlw.stopped:
	case mlw.drained <- namespace:
	}
}

// Must hold the lock around this function,  should not be called when in created state
// create a new lister informer for a specific namespace
func (mlw *multiListerWatcher) addNamespace(namespace string) *listerInformer {
	return newListerInformer(namespace, mlw.f, mlw.exampleObject, mlw.resyncPeriod, mlw.handleEvents, mlw.handleDrained)
}

// Must hold the lock around this function
func (mlw *multiListerWatcher) drainNamespace(namespace string) {
	// If the namespace is in the active map then move it to the draining map and invokc drain
	if li, ok := mlw.activeMap[namespace]; ok {
		mlw.drainingMap[namespace] = li
		delete(mlw.activeMap, namespace)
		li.drain()
	}
}

// Handle the update of namespaces
// Must hold the lock around this function
func (mlw *multiListerWatcher) updateNamespaces(namespaces []string) []string {
	activeMap := make(map[string]*listerInformer)
	for _, namespace := range namespaces {
		if li, ok := mlw.activeMap[namespace]; ok {
			// If we are already watching the namespace then move it to the new map
			activeMap[namespace] = li
			delete(mlw.activeMap, namespace)
		} else if dli, ok := mlw.drainingMap[namespace]; ok {
			// If we are already draining the namespace then move it to the new map
			// Once the drain has completed a new list informer will be created
			// to enumerate the namespace contents at that time
			activeMap[namespace] = dli
			delete(mlw.drainingMap, namespace)
		} else {
			// We are watching a new namespace, create the underlying informer and add to the map
			activeMap[namespace] = mlw.addNamespace(namespace)
		}
	}

	// Drain all remaining namespaces in the map, the informer will be moved into the draining map
	drainedNamespaces := make([]string, 0, len(mlw.activeMap))
	for namespace := range mlw.activeMap {
		drainedNamespaces = append(drainedNamespaces, namespace)
		mlw.drainNamespace(namespace)
	}

	mlw.activeMap = activeMap
	return drainedNamespaces
}

// Update the set of namespaces being tracked
func (mlw *multiListerWatcher) UpdateNamespaces(namespaces []string) {
	scope.Debugf("Namespaces updated: %s", namespaces)
	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	mlwNamespaces := append(namespaces[:0:0], namespaces...)

	if mlw.state == listed || mlw.state == watching {
		mlw.updateNamespaces(mlwNamespaces)
	}
	mlw.namespaces = mlwNamespaces
}

// Wait for all the underlying caches to sync, this means that they
// will all have performed a List and sent sufficient events through
// this layer to ensure Listed events are returned
func (mlw *multiListerWatcher) waitForCacheSync() bool {
	return cache.WaitForCacheSync(mlw.stopped, func() bool {
		mlw.lock.RLock()
		defer mlw.lock.RUnlock()
		for _, liValue := range mlw.activeMap {
			if !liValue.hasSynced() {
				return false
			}
		}
		return true
	})
}

// List implements the ListerWatcher interface.
// It combines the output of each synced informer into
// a single result.
func (mlw *multiListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	scope.Debugf("List() called with ResourceVersion '%s'", options.ResourceVersion)

	stopped := make(chan struct{})

	err := func() error {
		mlw.lock.Lock()
		defer mlw.lock.Unlock()

		if mlw.state != created {
			return errors.Errorf("Unexpected state, expected to be in %v but am in %v", created, mlw.state)
		}

		var (
			result      = make(chan watch.Event)
			events      = make(chan *watch.Event)
			activeMap   = make(map[string]*listerInformer)
			drainingMap = make(map[string]*listerInformer)
			drained     = make(chan string)
			listingMap  = make(map[string]*runtime.Object)
		)
		mlw.result = result
		mlw.stopped = stopped
		mlw.events = events
		mlw.activeMap = activeMap
		mlw.drainingMap = drainingMap
		mlw.drained = drained
		mlw.listingMap = listingMap

		mlw.state = listing

		// goroutine for processing events from the individual namespace caches
		// and forwarding them through to the top level, aggregated cache
		go func() {
			defer close(result)
			for {
				select {
				case <-stopped:
					return
				case nextEvent := <-events:
					key, err := cache.MetaNamespaceKeyFunc(nextEvent.Object)
					if err == nil {
						sendResult := func() bool {
							mlw.lock.Lock()
							defer mlw.lock.Unlock()
							// If we are still listing then we track the resources in the listing map so the List
							// function can return them as a result
							if mlw.state == listing {
								if nextEvent.Type == watch.Added || nextEvent.Type == watch.Modified {
									mlw.listingMap[key] = &nextEvent.Object
								} else if nextEvent.Type == watch.Deleted {
									delete(mlw.listingMap, key)
								}
								return false
							}
							// otherwise we send the result through the channel to the watcher
							return true
						}()
						if sendResult {
							select {
							case <-stopped:
								break
							case result <- *nextEvent:
							}
						}
					}
				}
			}
		}()

		// goroutine for processing drained notifications sent after the underlying
		// cache has issued all appropriate Delete events
		go func() {
			for {
				select {
				case <-stopped:
					return
				case drainedNamespace := <-drained:
					func() {
						mlw.lock.Lock()
						defer mlw.lock.Unlock()
						// If we are active (listed/watching) then check to see
						// if the namespace has been added back into the active map
						if mlw.state == listed || mlw.state == watching {
							if _, ok := mlw.drainingMap[drainedNamespace]; ok {
								// namespace is really removed, delete it from the draining map
								delete(mlw.drainingMap, drainedNamespace)
							} else if _, ok := mlw.activeMap[drainedNamespace]; ok {
								// namespace has been moved back to the active map, create a new informer
								mlw.activeMap[drainedNamespace] = mlw.addNamespace(drainedNamespace)
							}
						}
					}()
				}
			}
		}()

		// process all namespaces currently being listed, creating informers for each namespace
		mlw.updateNamespaces(mlw.namespaces)
		return nil
	}()

	if err != nil {
		mlw.Stop()
		return nil, err
	}

	// Wait for all the underlying informers to sync
	mlw.waitForCacheSync()
	// Retrieve the listingMap containing all received events, after this point
	// the events will go to the event channel
	listingMap := func() map[string]*runtime.Object {
		mlw.lock.Lock()
		defer mlw.lock.Unlock()
		mlw.state = listed
		listingMap := mlw.listingMap
		mlw.listingMap = nil
		return listingMap
	}()

	l := v1.List{}
	for _, item := range listingMap {
		l.Items = append(l.Items, runtime.RawExtension{Object: *item})
	}
	l.ListMeta.ResourceVersion = "0"

	return &l, nil
}

// Watch implements the ListerWatcher interface.
// It returns a watch.Interface implementation which aggregates the events from
// each individual namespace Informer into a single result chan.
func (mlw *multiListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	scope.Debugf("Watch() called with ResourceVersion '%s'", options.ResourceVersion)

	mlw.lock.Lock()
	defer mlw.lock.Unlock()
	if mlw.state != listed {
		mlw.stopInternal()
		return nil, errors.Errorf("Unexpected state, expected to be in %v but am in %v", listed, mlw.state)
	}

	mlw.state = watching

	return mlw, nil
}

// ResultChan implements the watch.Interface interface.
func (mlw *multiListerWatcher) ResultChan() <-chan watch.Event {
	return mlw.result
}

// Stop implements the watch.Interface interface.
// Can safely be called more than once.
func (mlw *multiListerWatcher) Stop() {
	mlw.lock.Lock()
	defer mlw.lock.Unlock()
	mlw.stopInternal()
}

// This must be called with lock held.
func (mlw *multiListerWatcher) stopInternal() {
	select {
	case <-mlw.stopped:
		// nothing to do, we are already stopped
	default:
		for _, li := range mlw.activeMap {
			li.stop()
		}
		close(mlw.stopped)
		mlw.activeMap = nil
		mlw.drainingMap = nil
		mlw.state = created
	}
}
