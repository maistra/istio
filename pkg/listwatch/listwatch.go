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
	"fmt"
	"strings"
	"sync"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/servicemesh/controller"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
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
)

type listerWatcher struct {
	lw      cache.ListerWatcher
	stopper func()
}

type multiListerWatcher struct {
	namespaces []string
	f          func(string) cache.ListerWatcher
	state      listerWatcherState
	result     chan watch.Event
	stopped    chan struct{}
	lwMap      map[string]*listerWatcher
	lock       sync.Mutex
}

type combinedEvent struct {
	namespace string
	event     watch.Event
}

func newMultiListerWatcher(namespaces []string, f func(string) cache.ListerWatcher) *multiListerWatcher {
	return &multiListerWatcher{
		namespaces: append(namespaces[:0:0], namespaces...),
		f:          f,
		state:      created,
	}
}

// Update the set of namespaces being tracked
func (mlw *multiListerWatcher) UpdateNamespaces(namespaces []string) {
	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	mlw.namespaces = append(namespaces[:0:0], namespaces...)

	switch mlw.state {
	case listed:
		mlw.state = errorOnWatch
	case watching:
		mlw.reportUpdatedNamespaceError()
	}
}

// Report error on result channel and close, state changes to created
// Must be called with lock held
func (mlw *multiListerWatcher) reportUpdatedNamespaceError() {
	go func(result chan watch.Event) {
		select {
		case <-result:
			return
		case result <- watch.Event{Type: watch.Error, Object: &metav1.Status{
			Status:  metav1.StatusFailure,
			Reason:  metav1.StatusReasonExpired,
			Message: "Namespaces Updated",
		}}:
			return
		}
	}(mlw.result)
	mlw.state = created
}

// List implements the ListerWatcher interface.
// It combines the output of the List method of every ListerWatcher into
// a single result.
func (mlw *multiListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	l := metav1.List{}
	var resourceVersions []string

	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	if mlw.state == watching {
		mlw.Stop()
	}

	mlw.lwMap = make(map[string]*listerWatcher)

	mlw.state = listed

	for _, n := range mlw.namespaces {
		lws := &listerWatcher{lw: mlw.f(n)}
		mlw.lwMap[n] = lws
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
		resourceVersions = append(resourceVersions, fmt.Sprintf("%s=%s", n, metaObj.GetResourceVersion()))
	}
	// Combine the resource versions so that the composite Watch method can
	// distribute appropriate versions to each underlying Watch func.
	l.ListMeta.ResourceVersion = strings.Join(resourceVersions, "/")

	return &l, nil
}

// Watch implements the ListerWatcher interface.
// It returns a watch.Interface that combines the output from the
// watch.Interface of every cache.ListerWatcher into a single result chan.
func (mlw *multiListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	resourceVersions := make(map[string]string)

	// Allow resource versions to be "".
	if options.ResourceVersion != "" {
		rvs := strings.Split(options.ResourceVersion, "/")
		for _, nrv := range rvs {
			equalsIndex := strings.IndexByte(nrv, '=')
			if equalsIndex == -1 {
				log.Infof("Received unexpected resource version %s", nrv)
			} else {
				namespace := nrv[:equalsIndex]
				resourceVersion := nrv[equalsIndex+1:]
				resourceVersions[namespace] = resourceVersion
			}
		}
	}
	if err := mlw.newMultiWatch(resourceVersions, options); err != nil {
		return nil, err
	}
	return mlw, nil
}

// newMultiWatch returns a new multiWatch or an error if one of the underlying
// Watch funcs errored. The length of []cache.ListerWatcher and []string must
// match.
func (mlw *multiListerWatcher) newMultiWatch(resourceVersions map[string]string, options metav1.ListOptions) error {
	mlw.lock.Lock()
	defer mlw.lock.Unlock()

	var (
		result  = make(chan watch.Event)
		stopped = make(chan struct{})
		wg      sync.WaitGroup
		combinedResult  = make(chan *combinedEvent)
	)
	mlw.result = result
	mlw.stopped = stopped

	if mlw.state == errorOnWatch {
		mlw.reportUpdatedNamespaceError()
		return nil
	}

	mlw.state = watching

	wg.Add(len(mlw.lwMap)+1)

	go func() {
		defer wg.Done()
		resourceVersions := make(map[string]string)
		for {
			var event *combinedEvent
			select {
			case event = <- combinedResult:
			case <-stopped:
				return
			}

			resultEvent := *event.event.DeepCopy()
			resultNamespace := event.namespace
			if resultEvent.Type != watch.Error {
				metaObj, err := meta.Accessor(resultEvent.Object)
				if err != nil {
					log.Infof("Unable to identify watch event, ignoring resource version")
				} else {
					resourceVersions[resultNamespace] = fmt.Sprintf("%s=%s", resultNamespace, metaObj.GetResourceVersion())
					var versions []string
					for _, resourceVersion := range resourceVersions {
						versions = append(versions, resourceVersion)
					}
					newResourceVersion := strings.Join(versions, "/")
					metaObj.SetResourceVersion(newResourceVersion)
				}
			}

			select {
			case result <- resultEvent:
			case <-stopped:
				return
			}
		}
	}()

	for n, lws := range mlw.lwMap {
		o := options.DeepCopy()
		o.ResourceVersion = resourceVersions[n]
		w, err := lws.lw.Watch(*o)
		if err != nil {
			return err
		}

		go func(namespace string) {
			defer wg.Done()

			for {
				event, ok := <-w.ResultChan()
				if !ok {
					return
				}

				select {
				case combinedResult <- &combinedEvent{namespace, event}:
				case <-stopped:
					return
				}
			}
		}(n)
		lws.stopper = w.Stop
	}

	// result chan must be closed,
	// once all event sender goroutines exited.
	go func() {
		wg.Wait()
		close(combinedResult)
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
	}
}
