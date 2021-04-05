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
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

func getNamespace(prefix string, id int) string {
	return fmt.Sprintf("%v-%v", prefix, id)
}

func getPodName(prefix string, id int) string {
	return fmt.Sprintf("%v-%v", prefix, id)
}

func getPodKey(namespace string, prefix string, id int) string {
	if namespace != "" {
		return fmt.Sprintf("%v/%v", namespace, getPodName(prefix, id))
	}
	return getPodName(prefix, id)
}

func createPod(namespace string, prefix string, id int) *v1.Pod {
	return &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "",
			APIVersion: "",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getPodName(prefix, id),
			Namespace: namespace,
		},
		Spec: v1.PodSpec{},
	}
}

func createPodList(namespace string, prefix string, count int) *v1.PodList {
	l := v1.PodList{}
	index := 1
	for index <= count {
		l.Items = append(l.Items, *createPod(namespace, prefix, index))
		index++
	}
	return &l
}

func checkEvents(t *testing.T, namespace string, events chan *watch.Event, eventType watch.EventType, prefix string, first int, last int) {
	index := first
	for index <= last {
		select {
		case nextEvent := <-events:
			if nextEvent.Type != eventType {
				t.Fatalf("Expected event type %v but received %v", eventType, nextEvent.Type)
			}
			pod := nextEvent.Object.(*v1.Pod)
			if pod.Namespace != namespace {
				t.Fatalf("Expected pod namespace %v but received %v", namespace, pod.Namespace)
			}
			expectedPodName := getPodName(prefix, index)
			if pod.Name != expectedPodName {
				t.Fatalf("Expected pod name %v but received %v", expectedPodName, pod.Name)
			}
		case <-time.After(time.Minute):
			t.Fatalf("Expected to receive an event for index %v but did not receive anything", index)
		}
		index++
	}
}

func TestListInformer(t *testing.T) {
	testNamespace := "test_namespace"
	events := make(chan *watch.Event, 100)
	drained := make(chan string)
	prefix := "pod"
	unblock := make(chan struct{})
	synced := make(chan struct{})
	w := watch.NewRaceFreeFake()
	numListPods := 20

	li := newListerInformer(testNamespace, func(string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				<-unblock
				return createPodList(testNamespace, prefix, numListPods), nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				synced <- struct{}{}
				<-unblock
				return w, nil
			},
		}
	}, &v1.Pod{}, 0, func(event *watch.Event) {
		events <- event
	}, func(namespace string) {
		drained <- namespace
	})

	if li.hasSynced() {
		t.Error("Expected hasSynced to return false before List returns")
	}
	unblock <- struct{}{}

	// Check we get pod events added
	checkEvents(t, testNamespace, events, watch.Added, prefix, 1, numListPods)

	<-synced
	// Now check synced

	if !li.hasSynced() {
		t.Error("Expected hasSynced to return true after List returns")
	}

	// unblock Wait
	unblock <- struct{}{}

	nextPodID := numListPods + 1
	// Check added events
	w.Add(createPod(testNamespace, prefix, nextPodID))
	checkEvents(t, testNamespace, events, watch.Added, prefix, nextPodID, nextPodID)

	// Check Modified events
	w.Modify(createPod(testNamespace, prefix, nextPodID))
	checkEvents(t, testNamespace, events, watch.Modified, prefix, nextPodID, nextPodID)

	// Check Adding an existing one results in a modify event
	w.Add(createPod(testNamespace, prefix, nextPodID))
	checkEvents(t, testNamespace, events, watch.Modified, prefix, nextPodID, nextPodID)

	// Check Deleted events
	w.Delete(createPod(testNamespace, prefix, nextPodID))
	checkEvents(t, testNamespace, events, watch.Deleted, prefix, nextPodID, nextPodID)

	li.drain()
	select {
	case drainedNamespace := <-drained:
		if testNamespace != drainedNamespace {
			t.Fatalf("Expected namespace notification for %v on drained channel, received %v",
				testNamespace, drainedNamespace)
		}
	case <-time.After(time.Minute):
		t.Fatalf("Did not receive notification from draining channel")
	}
	drainedPods := make(map[string]string)
	close(events)

	for event, ok := <-events; ok; {
		if watch.Deleted != event.Type {
			t.Fatalf("Expected to see a %v event but received %v", watch.Deleted, event.Type)
		}
		name := event.Object.(*v1.Pod).Name
		drainedPods[name] = name
		event, ok = <-events
	}
	if len(drainedPods) != numListPods {
		t.Fatalf("Expected to see %v pods, but received %v", numListPods, len(drainedPods))
	}

	index := 1
	for index <= numListPods {
		name := getPodName(prefix, index)
		if _, ok := drainedPods[name]; !ok {
			t.Fatalf("Expected to see information about pod %v but it is not part of the drain", name)
		}
		index++
	}
}
