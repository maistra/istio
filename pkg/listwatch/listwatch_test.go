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

package listwatch

import (
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

var _ watch.Interface = &multiListerWatcher{}

func setupMultiWatch(t *testing.T, namespaces []string, rvs map[string]string) (map[string]*watch.FakeWatcher, *multiListerWatcher) {
	n := len(namespaces)
	ws := make(map[string]*watch.FakeWatcher, n)

	mlw := newMultiListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				l.ListMeta.ResourceVersion = rvs[namespace]
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	})

	if _, err := mlw.List(metav1.ListOptions{}); err != nil {
		t.Fatalf("failed to invoke List: %v", err)
	}

	if err := mlw.newMultiWatch(rvs, metav1.ListOptions{}); err != nil {
		t.Fatalf("failed to create new multiWatch: %v", err)
	}
	return ws, mlw
}

func TestNewMultiWatch(t *testing.T) {
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("newMultiWatch should not panic when number of resource versions is less than ListerWatchers; got: %v", r)
			}
		}()
		// Create a multiWatch from 2 ListerWatchers but only pass 1 resource version.
		_, _ = setupMultiWatch(t, []string{"1", "2"}, map[string]string{"1": "resource1"})
	}()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("newMultiWatch should not panic when number of resource versions matches ListerWatchers; got: %v", r)
			}
		}()
		// Create a multiWatch from 2 ListerWatchers and pass 2 resource versions.
		_, _ = setupMultiWatch(t, []string{"1", "2"}, map[string]string{"1": "resource1", "2": "resource2"})
	}()
}

func TestMultiWatchResultChan(t *testing.T) {
	ws, m := setupMultiWatch(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, map[string]string{})
	defer m.Stop()
	var events []watch.Event
	var wg sync.WaitGroup
	for _, w := range ws {
		w := w
		wg.Add(1)
		go func() {
			w.Add(&runtime.Unknown{})
		}()
	}
	go func() {
		for {
			event, ok := <-m.ResultChan()
			if !ok {
				break
			}
			events = append(events, event)
			wg.Done()
		}
	}()
	wg.Wait()
	if len(events) != len(ws) {
		t.Errorf("expected %d events but got %d", len(ws), len(events))
	}
}

func TestMultiWatchStop(t *testing.T) {
	ws, m := setupMultiWatch(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, map[string]string{})
	m.Stop()
	var stopped int
	for _, w := range ws {
		_, running := <-w.ResultChan()
		if !running && w.IsStopped() {
			stopped++
		}
	}
	if stopped != len(ws) {
		t.Errorf("expected %d watchers to be stopped but got %d", len(ws), stopped)
	}
	select {
	case <-m.stopped:
		// all good, watcher is closed, proceed
	default:
		t.Error("expected multiWatch to be stopped")
	}
	_, running := <-m.ResultChan()
	if running {
		t.Errorf("expected multiWatch chan to be closed")
	}
}

type mockListerWatcher struct {
	evCh    chan watch.Event
	stopped bool
}

func (m *mockListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	return nil, nil
}

func (m *mockListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return m, nil
}

func (m *mockListerWatcher) Stop() {
	m.stopped = true
}

func (m *mockListerWatcher) ResultChan() <-chan watch.Event {
	return m.evCh
}

func TestRacyMultiWatch(t *testing.T) {
	evCh := make(chan watch.Event)
	lw := &mockListerWatcher{evCh: evCh}

	namespaces := []string{"namespace"}
	rsv := map[string]string{"namespace": "foo"}

	mlw := newMultiListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := &metav1.List{}
				l.ResourceVersion = "foo"
				return l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return lw, nil
			},
		}
	})

	if _, err := mlw.List(metav1.ListOptions{}); err != nil {
		t.Error(err)
		return
	}

	if err := mlw.newMultiWatch(rsv, metav1.ListOptions{}); err != nil {
		t.Error(err)
		return
	}

	// this will not block, as newMultiWatch started a goroutine,
	// receiving that event and block on the dispatching it there.
	evCh <- watch.Event{
		Type: "foo",
	}

	if got := <-mlw.ResultChan(); got.Type != "foo" {
		t.Errorf("expected foo, got %s", got.Type)
		return
	}

	// Enqueue event, do not dequeue it.
	// In conjunction with go test -race this asserts
	// if there is a race between stopping and dispatching an event
	evCh <- watch.Event{
		Type: "bar",
	}
	mlw.Stop()

	if got := lw.stopped; got != true {
		t.Errorf("expected watcher to be closed true, got %t", got)
	}

	// some reentrant calls, should be non-blocking
	mlw.Stop()
	mlw.Stop()
}

func TestUpdatingNamespaceAfterWatch(t *testing.T) {
	namespaces1 := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	ws, m := setupMultiWatch(t, namespaces1, map[string]string{})

	if len(m.lwMap) != len(namespaces1) {
		t.Errorf("Expected lwMap to be of size %d, found %d", len(namespaces1), len(m.lwMap))
		return
	}

	namespaces2 := []string{"1", "2", "3", "4", "5"}
	m.UpdateNamespaces(namespaces2)

	if got := <-m.ResultChan(); got.Type != watch.Error {
		t.Errorf("Unexpected type on result channel, retrieved type %s but exected %s", got.Type, watch.Error)
		return
	}

	select {
	case <-m.stopped:
		t.Errorf("Stop has been called unexpectedly")
	default:
		m.Stop()
	}

	var stopped int
	for _, w := range ws {
		_, running := <-w.ResultChan()
		if !running && w.IsStopped() {
			stopped++
		}
	}
	if stopped != len(ws) {
		t.Errorf("expected %d watchers to be stopped but got %d", len(ws), stopped)
	}
	select {
	case <-m.stopped:
		// all good, watcher is closed, proceed
	default:
		t.Error("expected multiWatch to be stopped")
	}
	if _, running := <-m.ResultChan(); running {
		t.Errorf("expected multiWatch chan to be closed")
	}

	for n := range ws {
		delete(ws, n)
	}

	if _, err := m.List(metav1.ListOptions{}); err != nil {
		t.Errorf("Received unexpected error invoking List, %v", err)
		return
	}

	if _, err := m.Watch(metav1.ListOptions{}); err != nil {
		t.Errorf("Received unexpected error invoking Watch, %v", err)
		return
	}

	if len(m.lwMap) != len(namespaces2) {
		t.Errorf("Expected lwMap to be of size %d, found %d", len(namespaces2), len(m.lwMap))
		return
	}

	var running int
	for _, w := range ws {
		if !w.IsStopped() {
			running++
		}
	}
	if running != len(ws) {
		t.Errorf("expected %d watchers to be running but got %d", len(ws), running)
	}
}

func TestUpdatingNamespaceAfterListed(t *testing.T) {
	namespaces1 := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	ws := make(map[string]*watch.FakeWatcher)

	m := newMultiListerWatcher(namespaces1, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	})

	if _, err := m.List(metav1.ListOptions{}); err != nil {
		t.Fatalf("failed to invoke List: %v", err)
	}

	if len(m.lwMap) != len(namespaces1) {
		t.Errorf("Expected lwMap to be of size %d, found %d", len(namespaces1), len(m.lwMap))
		return
	}

	if len(ws) != 0 {
		t.Errorf("Expected ws to be of size 0, found %d", len(ws))
		return
	}

	namespaces2 := []string{"1", "2", "3", "4", "5"}
	m.UpdateNamespaces(namespaces2)

	w, err := m.Watch(metav1.ListOptions{})
	if err != nil {
		t.Errorf("Received unexpected error invoking Watch, %v", err)
		return
	}

	if got := <-w.ResultChan(); got.Type != watch.Error {
		t.Errorf("Unexpected type on result channel, retrieved type %s but exected %s", got.Type, watch.Error)
		return
	}

	select {
	case <-m.stopped:
		t.Errorf("Stop has been called unexpectedly")
	default:
		w.Stop()
	}

	var stopped int
	for _, w := range ws {
		if w.IsStopped() {
			stopped++
		}
	}
	if stopped != len(ws) {
		t.Errorf("expected %d watchers to be stopped but got %d", len(ws), stopped)
	}
}

func TestUpdatingNamespaceAfterCreated(t *testing.T) {
	namespaces1 := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	ws := make(map[string]*watch.FakeWatcher)

	m := newMultiListerWatcher(namespaces1, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	})

	if len(m.lwMap) != 0 {
		t.Errorf("Expected lwMap to be of size 0, found %d", len(m.lwMap))
		return
	}

	if len(ws) != 0 {
		t.Errorf("Expected ws to be of size 0, found %d", len(ws))
		return
	}

	namespaces2 := []string{"1", "2", "3", "4", "5"}
	m.UpdateNamespaces(namespaces2)

	if _, err := m.List(metav1.ListOptions{}); err != nil {
		t.Errorf("Received unexpected error invoking List, %v", err)
		return
	}

	if _, err := m.Watch(metav1.ListOptions{}); err != nil {
		t.Errorf("Received unexpected error invoking Watch, %v", err)
		return
	}

	if len(m.lwMap) != len(namespaces2) {
		t.Errorf("Expected lwMap to be of size %d, found %d", len(namespaces2), len(m.lwMap))
		return
	}

	var running int
	for _, w := range ws {
		if !w.IsStopped() {
			running++
		}
	}
	if running != len(ws) {
		t.Errorf("expected %d watchers to be running but got %d", len(ws), running)
	}
}
