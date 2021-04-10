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
	"errors"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

var _ watch.Interface = &multiListerWatcher{}

func checkError(t *testing.T, msg string, err error, args ...interface{}) {
	if err != nil {
		t.Errorf(msg, args...)
	}
}

//nolint:unparam
func setupMultiWatch(t *testing.T, namespaces []string, exampleObject runtime.Object,
	resyncPeriod time.Duration) (map[string]*watch.FakeWatcher, *sync.Mutex, *multiListerWatcher) {
	n := len(namespaces)
	ws := make(map[string]*watch.FakeWatcher, n)
	wsLock := sync.Mutex{}

	mlw := newMultiListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				l.ListMeta.ResourceVersion = "0"
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				wsLock.Lock()
				defer wsLock.Unlock()
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	}, exampleObject, resyncPeriod)

	if _, err := mlw.List(metav1.ListOptions{}); err != nil {
		t.Errorf("failed to invoke List: %v", err)
	}

	if _, err := mlw.Watch(metav1.ListOptions{}); err != nil {
		t.Errorf("failed to create new multiWatch: %v", err)
	}
	return ws, &wsLock, mlw
}

func TestMultiWatchResultChan(t *testing.T) {
	ws, wsLock, m := setupMultiWatch(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, &v1.Pod{}, 0)
	defer m.Stop()
	var events []watch.Event
	var wg sync.WaitGroup
	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
		for n, w := range ws {
			wg.Add(1)
			go func(n string, w *watch.FakeWatcher) {
				w.Add(createPod(n, "pod", 1))
			}(n, w)
		}
	}()
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
	ws, wsLock, m := setupMultiWatch(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, &v1.Pod{}, 0)
	m.Stop()
	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
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
	}()
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

func TestMultiWatchResultChannelStop(t *testing.T) {
	ws, wsLock, m := setupMultiWatch(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, &v1.Pod{}, 0)

	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
		ws["1"].Stop()
		ws["2"].Stop()
	}()
	select {
	case _, running := <-m.ResultChan():
		if !running {
			t.Errorf("expected multiWatch chan to remain open")
		}
	case <-time.After(5 * time.Second):
		break
	}
	m.Stop()

	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
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
	}()
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
	lock    sync.Mutex
}

func (m *mockListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	return nil, nil
}

func (m *mockListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return m, nil
}

func (m *mockListerWatcher) Stop() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.stopped = true
}

func (m *mockListerWatcher) isStopped() bool {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.stopped
}

func (m *mockListerWatcher) ResultChan() <-chan watch.Event {
	return m.evCh
}

func TestRacyMultiWatch(t *testing.T) {
	evCh := make(chan watch.Event)
	lw := &mockListerWatcher{evCh: evCh}
	testNamespace := "testNamespace"

	namespaces := []string{testNamespace}

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
	}, &v1.Pod{}, 0)

	if _, err := mlw.List(metav1.ListOptions{}); err != nil {
		t.Error(err)
		return
	}

	if _, err := mlw.Watch(metav1.ListOptions{}); err != nil {
		t.Error(err)
		return
	}

	// this will not block, as the informers process this asynchronously
	evCh <- watch.Event{
		Type:   watch.Added,
		Object: createPod(testNamespace, "pod", 1),
	}

	if got := <-mlw.ResultChan(); got.Type != watch.Added {
		t.Errorf("expected %v, got %s", watch.Added, got.Type)
		return
	}

	// Enqueue event, do not dequeue it.
	// In conjunction with go test -race this asserts
	// if there is a race between stopping and dispatching an event
	evCh <- watch.Event{
		Type:   watch.Modified,
		Object: createPod(testNamespace, "pod", 1),
	}
	mlw.Stop()

	time.Sleep(time.Second * 1)
	if got := lw.isStopped(); got != true {
		t.Errorf("expected watcher to be closed true, got %t", got)
	}

	// some reentrant calls, should be non-blocking
	mlw.Stop()
	mlw.Stop()
}

func TestUpdatingNamespaceAfterWatch(t *testing.T) {
	namespaces1 := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	ws, wsLock, m := setupMultiWatch(t, namespaces1, &v1.Pod{}, 0)

	if len(m.activeMap) != len(namespaces1) {
		t.Errorf("Expected activeMap to be of size %d, found %d", len(namespaces1), len(m.activeMap))
		return
	}

	namespaces2 := []string{"1", "2", "3", "4", "5"}
	m.UpdateNamespaces(namespaces2)

	// Should remain open with no errors
	select {
	case _, running := <-m.ResultChan():
		if !running {
			t.Errorf("expected multiWatch chan to remain open")
		}
	case <-time.After(5 * time.Second):
		break
	}

	select {
	case <-m.stopped:
		t.Errorf("Stop has been called unexpectedly")
	default:
		m.Stop()
	}

	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
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
	}()
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

	if len(m.activeMap) != len(namespaces2) {
		t.Errorf("Expected activeMap to be of size %d, found %d", len(namespaces2), len(m.activeMap))
		return
	}

	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
		var running int
		for _, w := range ws {
			if !w.IsStopped() {
				running++
			}
		}
		if running != len(ws) {
			t.Errorf("expected %d watchers to be running but got %d", len(ws), running)
		}
	}()
	m.Stop()
}

func TestUpdatingNamespaceAfterListed(t *testing.T) {
	namespaces1 := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	ws := make(map[string]*watch.FakeWatcher)
	wsLock := sync.Mutex{}

	m := newMultiListerWatcher(namespaces1, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				wsLock.Lock()
				defer wsLock.Unlock()
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	}, &v1.Pod{}, 0)

	if _, err := m.List(metav1.ListOptions{}); err != nil {
		t.Errorf("failed to invoke List: %v", err)
	}

	if len(m.activeMap) != len(namespaces1) {
		t.Errorf("Expected activeMap to be of size %d, found %d", len(namespaces1), len(m.activeMap))
		return
	}

	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
		if len(ws) != len(namespaces1) {
			t.Errorf("Expected ws to be of size 0, found %d", len(ws))
			return
		}
	}()

	namespaces2 := []string{"1", "2", "3", "4", "5"}
	m.UpdateNamespaces(namespaces2)

	time.Sleep(5 * time.Second)

	var stopped1 int
	for _, w := range ws {
		if w.IsStopped() {
			stopped1++
		}
	}
	expected := len(namespaces1) - len(namespaces2)
	if stopped1 != expected {
		t.Errorf("expected %d watchers to be stopped but got %d", expected, stopped1)
	}

	w, err := m.Watch(metav1.ListOptions{})
	checkError(t, "Received unexpected error invoking Watch, %v", err)

	select {
	case got := <-w.ResultChan():
		if got.Type != watch.Error {
			t.Errorf("Unexpected type on result channel, retrieved type %s but exected %s", got.Type, watch.Error)
		}
	case <-time.After(5 * time.Second):
	}

	select {
	case <-m.stopped:
		t.Errorf("Stop has been called unexpectedly")
	default:
		w.Stop()
	}

	time.Sleep(5 * time.Second)

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
	wsLock := sync.Mutex{}

	m := newMultiListerWatcher(namespaces1, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				wsLock.Lock()
				defer wsLock.Unlock()
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	}, &v1.Pod{}, 0)

	if len(m.activeMap) != 0 {
		t.Errorf("Expected activeMap to be of size 0, found %d", len(m.activeMap))
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

	if len(m.activeMap) != len(namespaces2) {
		t.Errorf("Expected activeMap to be of size %d, found %d", len(namespaces2), len(m.activeMap))
		return
	}

	func() {
		wsLock.Lock()
		defer wsLock.Unlock()
		var running int
		for _, w := range ws {
			if !w.IsStopped() {
				running++
			}
		}
		if running != len(ws) {
			t.Errorf("expected %d watchers to be running but got %d", len(ws), running)
		}
	}()
	m.Stop()
}

func TestMultiWatchDoesNotDeadlock(t *testing.T) {
	namespaces := []string{"1", "2", "3", "4", "5"}

	ws := make(map[string]*watch.FakeWatcher)
	wsLock := sync.Mutex{}
	errorRaised := make(chan struct{})

	m := newMultiListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				l := metav1.List{}
				return &l, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if namespace == "4" {
					defer func() { errorRaised <- struct{}{} }()
					return nil, errors.New("deliberate error forced")
				}
				wsLock.Lock()
				defer wsLock.Unlock()
				w := watch.NewFake()
				ws[namespace] = w
				return w, nil
			},
		}
	}, &v1.Pod{}, 0)

	if _, err := m.List(metav1.ListOptions{}); err != nil {
		t.Errorf("Error while listing: %v", err)
	}

	// We wait for two notifications on errorRaised so we know the Watch has been retried
	<-errorRaised
	<-errorRaised

	// We shouldn't get an error, the underlying cache will retry on watch
	if _, err := m.Watch(metav1.ListOptions{}); err != nil {
		t.Errorf("Did not expect an error while watching, received %v", err)
	}

	timeout := time.NewTimer(2 * time.Second)

	listChan := make(chan struct{})
	go func() {
		// Make sure concurrent Lists do not deadlock
		if _, err := m.List(metav1.ListOptions{}); err == nil {
			t.Error("Expected to receive an error for List while in watching state")
		}
		listChan <- struct{}{}
	}()

	select {
	case <-timeout.C:
		t.Error("Deadlock while calling List()")
	case <-listChan:
	}
	m.Stop()
}

func getObjectMap(t *testing.T, list runtime.Object) map[string]*runtime.Object {
	runtimeObjects, err := meta.ExtractList(list)
	checkError(t, "Unexpected error extracing list, %v", err)

	objectMap := make(map[string]*runtime.Object)
	for _, runtimeObject := range runtimeObjects {
		key, err := cache.MetaNamespaceKeyFunc(runtimeObject)
		checkError(t, "Unexpected error retrieving the key, %v, %v", err, runtimeObject)
		objectMap[key] = &runtimeObject
	}
	return objectMap
}

//nolint:unparam
func checkNamespaceObjects(t *testing.T, objectMap map[string]*runtime.Object,
	namespacePrefix string, firstNamespace int, lastNamespace int,
	podPrefix string, firstPod int, lastPod int) {
	count := firstNamespace
	for count <= lastNamespace {
		namespace := getNamespace(namespacePrefix, count)
		podCount := firstPod
		for podCount <= lastPod {
			key := getPodKey(namespace, podPrefix, podCount)
			_, ok := objectMap[key]
			if !ok {
				t.Errorf("Could not find resource %v", key)
			}
			podCount++
		}
		count++
	}
}

func TestInformerBehaviour(t *testing.T) {
	podPrefix := "pod"
	namespacePrefix := "ns"
	w := make(map[string]*watch.RaceFreeFakeWatcher)
	numNamespaces := 200
	numListPods := 20

	var testNamespaces []string = nil
	count := 1
	for count <= numNamespaces {
		namespace := getNamespace(namespacePrefix, count)
		testNamespaces = append(testNamespaces, namespace)
		w[namespace] = watch.NewRaceFreeFake()
		count++
	}

	mlw := newMultiListerWatcher(testNamespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return createPodList(namespace, podPrefix, numListPods), nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return w[namespace], nil
			},
		}
	}, &v1.Pod{}, 0)

	if activeMapLen := len(mlw.activeMap); activeMapLen != 0 {
		t.Errorf("Expected activeMap to be of zero length, length is %v", activeMapLen)
	}

	if drainingMapLen := len(mlw.drainingMap); drainingMapLen != 0 {
		t.Errorf("Expected drainingMap to be of zero length, length is %v", drainingMapLen)
	}

	if listingMapLen := len(mlw.listingMap); listingMapLen != 0 {
		t.Errorf("Expected listingMap to be of zero length, length is %v", listingMapLen)
	}

	list, err := mlw.List(metav1.ListOptions{})
	checkError(t, "Unexpected error listing mlw, %v", err)

	if listLen := meta.LenList(list); listLen != numListPods*numNamespaces {
		t.Errorf("Expected to receive %d pods, actual count was %v", numListPods*numNamespaces, listLen)
	}

	objectMap := getObjectMap(t, list)

	checkNamespaceObjects(t, objectMap, namespacePrefix, 1, numNamespaces,
		podPrefix, 1, numListPods)

	watcher, err := mlw.Watch(metav1.ListOptions{})
	checkError(t, "Unexpected error invoking Watch, %v", err)

	results := watcher.ResultChan()

	podID := numListPods + 1
	for _, namespace := range testNamespaces {
		pod := createPod(namespace, podPrefix, podID)
		w[namespace].Add(pod)
		checkEvent(t, results, watch.Added, getPodKey(namespace, podPrefix, podID))
		w[namespace].Add(pod)
		checkEvent(t, results, watch.Modified, getPodKey(namespace, podPrefix, podID))
		w[namespace].Modify(pod)
		checkEvent(t, results, watch.Modified, getPodKey(namespace, podPrefix, podID))
		w[namespace].Delete(pod)
		checkEvent(t, results, watch.Deleted, getPodKey(namespace, podPrefix, podID))

		count++
	}

	testNamespaces2 := testNamespaces[1:]
	mlw.UpdateNamespaces(testNamespaces2)
	objectMap = getEventsAsObjectMap(t, results, watch.Deleted, numListPods)
	checkNamespaceObjects(t, objectMap, namespacePrefix, 1, 1,
		podPrefix, 1, numListPods)

	// Now test re-adding, should see deletes as they drain followed by adds when resuming
	deletedNamespace := getNamespace(namespacePrefix, 2)
	func() {
		mlw.lock.Lock()
		defer mlw.lock.Unlock()
		testNamespaces3 := testNamespaces2[1:]

		drainedNamespaces := mlw.updateNamespaces(testNamespaces3)
		if len(drainedNamespaces) != 1 {
			t.Errorf("Expected only one namespace to be drained, received %d", len(drainedNamespaces))
		}
		if drainedNamespaces[0] != deletedNamespace {
			t.Errorf("Expected to see %s as the deleted namespace, instead recevied %v", deletedNamespace, drainedNamespaces[0])
		}
		_, ok := mlw.drainingMap[deletedNamespace]
		if !ok {
			t.Errorf("Expected to see namespace %v in the drainingMap", deletedNamespace)
		}
		_, ok = mlw.activeMap[deletedNamespace]
		if ok {
			t.Errorf("Did not expect to see namespace %v in the activeMap", deletedNamespace)
		}
		// add back within the same lock
		drainedNamespaces = mlw.updateNamespaces(testNamespaces2)
		if len(drainedNamespaces) != 0 {
			t.Errorf("Expected no namespaces to be drained, received %d", len(drainedNamespaces))
		}
		_, ok = mlw.drainingMap[deletedNamespace]
		if ok {
			t.Errorf("Did not expect to see namespace %v in the drainingMap", deletedNamespace)
		}
		_, ok = mlw.activeMap[deletedNamespace]
		if !ok {
			t.Errorf("Expected to see namespace %v in the activeMap", deletedNamespace)
		}
	}()

	objectMap = getEventsAsObjectMap(t, results, watch.Deleted, numListPods)
	checkNamespaceObjects(t, objectMap, namespacePrefix, 2, 2,
		podPrefix, 1, numListPods)

	objectMap = getEventsAsObjectMap(t, results, watch.Added, numListPods)
	checkNamespaceObjects(t, objectMap, namespacePrefix, 2, 2,
		podPrefix, 1, numListPods)
	mlw.Stop()
}

func TestErrorBehaviour(t *testing.T) {
	podPrefix := "pod"
	namespacePrefix := "ns"
	w := make(map[string]*watch.RaceFreeFakeWatcher)
	numNamespaces := 200
	numListPods := 20

	var testNamespaces []string = nil
	count := 1
	for count <= numNamespaces {
		namespace := getNamespace(namespacePrefix, count)
		testNamespaces = append(testNamespaces, namespace)
		w[namespace] = watch.NewRaceFreeFake()
		count++
	}

	listChan := make(chan string, numNamespaces)

	mlw := newMultiListerWatcher(testNamespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				listChan <- namespace
				w[namespace].Reset()
				return createPodList(namespace, podPrefix, numListPods), nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return w[namespace], nil
			},
		}
	}, &v1.Pod{}, 0)
	list, err := mlw.List(metav1.ListOptions{})
	checkError(t, "Unexpected error listing mlw, %v", err)

	if listLen := meta.LenList(list); listLen != numListPods*numNamespaces {
		t.Errorf("Expected to receive %d pods, actual count was %v", numListPods*numNamespaces, listLen)
	}

	// make sure we have a list for each namespace
	count = 0
	for count < numNamespaces {
		select {
		case <-listChan:
		case <-time.After(5 * time.Second):
			t.Errorf("Unexpected timeout waiting for list to occur")
		}
		count++
	}

	// make sure no lists are outstanding
	select {
	case <-listChan:
		t.Errorf("Unexpected list occurred")
	case <-time.After(5 * time.Second):
	}

	watcher, err := mlw.Watch(metav1.ListOptions{})
	checkError(t, "Unexpected error invoking Watch, %v", err)

	results := watcher.ResultChan()

	// send error
	errorNamespace := getNamespace(namespacePrefix, 1)
	// &Status{Status:Failure,Message:too old resource version: 6437976 (6438283),Reason:Expired,Details:nil,Code:410,}
	w[errorNamespace].Error(&metav1.Status{
		Status:  "Failure",
		Message: "too old resource version: XXXX",
		Reason:  metav1.StatusReasonExpired,
		Code:    410,
	})

	// check for list on the namespace
	select {
	case namespace := <-listChan:
		if namespace != errorNamespace {
			t.Errorf("Expected a repeated list for namespace %v but received %v", errorNamespace, namespace)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("Unexpected timeout waiting for list to occur")
	}

	// We should receive modified updates for errorNamespace pods
	count = 0
	metaAccessor := meta.NewAccessor()
	for count < numListPods {
		select {
		case event := <-results:
			if event.Type != watch.Modified {
				t.Errorf("Expected %v event type but received %v", watch.Modified, event.Type)
			}
			eventNamespace, _ := metaAccessor.Namespace(event.Object)
			if eventNamespace != errorNamespace {
				t.Errorf("Expected namespace %v but received %v", errorNamespace, eventNamespace)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("Expected modified event for existing pod, count is %v", count)
		}
		count++
	}

	// make sure no lists are outstanding
	select {
	case namespace := <-listChan:
		t.Errorf("Unexpected list occurred for namespace %v", namespace)
	case <-time.After(5 * time.Second):
	}

	// We should receive no new updates from the result channel
	select {
	case event := <-results:
		t.Errorf("Unexpected event occurred %v", event)
	case <-time.After(5 * time.Second):
	}

	// double check events are still occurring
	podID := numListPods + 1
	for _, namespace := range testNamespaces {
		pod := createPod(namespace, podPrefix, podID)
		w[namespace].Add(pod)
		checkEvent(t, results, watch.Added, getPodKey(namespace, podPrefix, podID))
		w[namespace].Add(pod)
		checkEvent(t, results, watch.Modified, getPodKey(namespace, podPrefix, podID))
		w[namespace].Modify(pod)
		checkEvent(t, results, watch.Modified, getPodKey(namespace, podPrefix, podID))
		w[namespace].Delete(pod)
		checkEvent(t, results, watch.Deleted, getPodKey(namespace, podPrefix, podID))

		count++
	}
}

func checkEvent(t *testing.T, events <-chan watch.Event, eventType watch.EventType, expectedKey string) {
	event := <-events
	if event.Type != eventType {
		t.Errorf("Expected event type %v but received %v", eventType, event.Type)
	}
	key, err := cache.MetaNamespaceKeyFunc(event.Object)
	checkError(t, "Unexpected error retrieving the key, %v, %v", err, event)
	if expectedKey != key {
		t.Errorf("Unexpected pod key %v, expected key was %v", key, expectedKey)
	}
}

func getEventsAsObjectMap(t *testing.T, events <-chan watch.Event, eventType watch.EventType,
	numEvents int) map[string]*runtime.Object {
	count := 0
	objectMap := make(map[string]*runtime.Object)
	for count < numEvents {
		event := <-events
		if event.Type != eventType {
			t.Errorf("Expected event type %v but received %v", eventType, event.Type)
		}
		runtimeObject := event.Object
		key, err := cache.MetaNamespaceKeyFunc(runtimeObject)
		checkError(t, "Unexpected error retrieving the key, %v, %v", err, runtimeObject)
		objectMap[key] = &runtimeObject

		count++
	}
	return objectMap
}
