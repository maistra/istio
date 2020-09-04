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

	controller "istio.io/istio/pkg/servicemesh/controller/memberroll"
	"k8s.io/apimachinery/pkg/api/meta"
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

// MultiNamespaceListerWatcher takes a list of namespaces and a
// cache.ListerWatcher generator func and returns a single cache.ListerWatcher
// capable of operating on multiple namespaces.
func MultiNamespaceListerWatcher(namespaces []string, f func(string) cache.ListerWatcher) NamespaceListerWatcher {
	return newMultiListerWatcher(namespaces, f)
}

// multiListerWatcher abstracts several cache.ListerWatchers, allowing them
// to be treated as a single cache.ListerWatcher.

type listerWatcherState int

const (
	created      listerWatcherState = 0
	listed       listerWatcherState = 1
	watching     listerWatcherState = 2
	errorOnWatch listerWatcherState = 3
	stopping     listerWatcherState = 4
)

type listerWatcher struct {
	lw      cache.ListerWatcher
	stopper func()
}

type multiListerWatcher struct {
	namespaces       []string
	f                func(string) cache.ListerWatcher
	state            listerWatcherState
	result           chan watch.Event
	stopped          chan struct{}
	wg               *sync.WaitGroup
	lwMap            map[string]*listerWatcher
	lock             sync.Mutex
	resourceVersions map[string]string
}

func newMultiListerWatcher(namespaces []string, f func(string) cache.ListerWatcher) *multiListerWatcher {
	return &multiListerWatcher{
		namespaces:       append(namespaces[:0:0], namespaces...),
		f:                f,
		state:            created,
		resourceVersions: make(map[string]string, len(namespaces)),
	}
}

// Update the set of namespaces being tracked
func (mlw *multiListerWatcher) UpdateNamespaces(namespaces []string) {
	scope.Debugf("Namespaces updated: %s", namespaces)
	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	mlw.namespaces = append(namespaces[:0:0], namespaces...)

	switch mlw.state {
	case listed:
		mlw.state = errorOnWatch
	case watching:
		mlw.reportError("Namespaces Updated")
	}
}

// Report error on result channel and close, state changes to created
// Must be called with lock held
func (mlw *multiListerWatcher) reportError(message string) {
	if mlw.state != watching {
		return
	}
	mlw.state = stopping
	mlw.wg.Add(1)
	go func(result chan watch.Event, stopped chan struct{}, wg *sync.WaitGroup) {
		defer wg.Done()
		select {
		case <-stopped:
			return
		case result <- watch.Event{Type: watch.Error, Object: &metav1.Status{
			Status:  metav1.StatusFailure,
			Reason:  metav1.StatusReasonExpired,
			Message: message,
		}}:
			return
		}
	}(mlw.result, mlw.stopped, mlw.wg)
}

// List implements the ListerWatcher interface.
// It combines the output of the List method of every ListerWatcher into
// a single result.
func (mlw *multiListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	l := metav1.List{}
	scope.Debugf("List() called with ResourceVersion '%s'", options.ResourceVersion)
	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	if mlw.state == watching || mlw.state == stopping {
		mlw.stopInternal()
	}

	mlw.lwMap = make(map[string]*listerWatcher)

	mlw.state = listed

	for _, n := range mlw.namespaces {
		lws := &listerWatcher{lw: mlw.f(n)}
		mlw.lwMap[n] = lws
		scope.Debugf("-> List() dispatched with ResourceVersion '%s' in namespace %s", options.ResourceVersion, n)
		list, err := lws.lw.List(options)
		if err != nil {
			return nil, err
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return nil, err
		}
		metaObj, err := meta.ListAccessor(list)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			l.Items = append(l.Items, runtime.RawExtension{Object: item.DeepCopyObject()})
		}
		scope.Debugf("-> Storing ResourceVersion '%s' for namespace %s", metaObj.GetResourceVersion(), n)
		mlw.resourceVersions[n] = metaObj.GetResourceVersion()
	}
	// set ResourceVersion to 0
	// after timeout, the reflector will List() again using this ResourceVersion
	// using "0" makes sure we're getting the latest from the cache
	l.ListMeta.ResourceVersion = "0"

	return &l, nil
}

// Watch implements the ListerWatcher interface.
// It returns a watch.Interface that combines the output from the
// watch.Interface of every cache.ListerWatcher into a single result chan.
func (mlw *multiListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	scope.Debugf("Watch() called with ResourceVersion '%s'", options.ResourceVersion)
	if err := mlw.newMultiWatch(options); err != nil {
		return nil, err
	}
	return mlw, nil
}

// newMultiWatch returns a new multiWatch or an error if one of the underlying
// Watch funcs errored.
func (mlw *multiListerWatcher) newMultiWatch(options metav1.ListOptions) error {
	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	var (
		result  = make(chan watch.Event)
		stopped = make(chan struct{})
		wg      sync.WaitGroup
	)
	mlw.result = result
	mlw.stopped = stopped
	mlw.wg = &wg

	if mlw.state == errorOnWatch {
		mlw.state = watching

		mlw.reportError("Namespaces Updated")
		return nil
	}

	mlw.state = watching

	wg.Add(len(mlw.lwMap))

	for n, lws := range mlw.lwMap {
		o := options.DeepCopy()
		// if we have a stored resourceVersion, use that as starting point
		if mlw.resourceVersions[n] != "" {
			o.ResourceVersion = mlw.resourceVersions[n]
		}
		scope.Debugf("-> Watch() dispatched with ResourceVersion '%s' in namespace %s", o.ResourceVersion, n)
		w, err := lws.lw.Watch(*o)
		if err != nil {
			return err
		}

		go func() {
			defer wg.Done()

			for {
				event, ok := <-w.ResultChan()
				if !ok {
					mlw.lock.Lock()
					defer mlw.lock.Unlock()
					mlw.reportError("Underlying Result Channel closed")
					return
				}

				select {
				case result <- event:
				case <-stopped:
					return
				}
			}
		}()
		lws.stopper = w.Stop
	}

	// result chan must be closed,
	// once all event sender goroutines exited.
	go func() {
		wg.Wait()
		close(result)
	}()
	return nil
}

// ResultChan implements the watch.Interface interface.
func (mlw *multiListerWatcher) ResultChan() <-chan watch.Event {
	return mlw.result
}

// Stop implements the watch.Interface interface.
// It stops all of the underlying watch.Interfaces and closes the backing chan.
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
		for _, lw := range mlw.lwMap {
			if lw.stopper != nil {
				lw.stopper()
			}
		}
		close(mlw.stopped)
		mlw.state = created
	}
}
