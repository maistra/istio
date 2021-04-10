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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

/*
 * This type allows us to track the count of resources returned from the informer's
 * invocation of the List method and compare it with the number of add events which
 * have been sent from the list informer.  We use this comparison to gauge whether
 * the informer has synced and that all events have been passed through to the upper
 * level cache.
 */
type listLenListerWatcher struct {
	listerWatcher cache.ListerWatcher
	listCount     int
	addCount      int
	lock          sync.Mutex
}

func newListLenListerWatcher(listerWatcher cache.ListerWatcher) *listLenListerWatcher {
	return &listLenListerWatcher{
		listerWatcher: listerWatcher,
		listCount:     -1,
	}
}

func (lllw *listLenListerWatcher) resetCounts() {
	lllw.lock.Lock()
	defer lllw.lock.Unlock()
	lllw.listCount = -1
	lllw.addCount = 0
}

func (lllw *listLenListerWatcher) setListCount(listCount int) {
	lllw.lock.Lock()
	defer lllw.lock.Unlock()
	lllw.listCount = listCount
}

func (lllw *listLenListerWatcher) incAddCount() {
	lllw.lock.Lock()
	defer lllw.lock.Unlock()
	if lllw.listCount < 0 || lllw.addCount < lllw.listCount {
		lllw.addCount++
	}
}

func (lllw *listLenListerWatcher) hasReachedListCount() bool {
	lllw.lock.Lock()
	defer lllw.lock.Unlock()
	return lllw.listCount <= lllw.addCount
}

func (lllw *listLenListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	lllw.resetCounts()
	result, err := lllw.listerWatcher.List(options)
	if err != nil {
		return result, err
	}
	lllw.setListCount(meta.LenList(result))
	return result, err
}

func (lllw *listLenListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return lllw.listerWatcher.Watch(options)
}
