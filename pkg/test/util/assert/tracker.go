// Copyright Istio Authors
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

package assert

import (
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

type Tracker[T comparable] struct {
	t      test.Failer
	mu     sync.Mutex
	events []T
}

// NewTracker builds a tracker which records events that occur
func NewTracker[T comparable](t test.Failer) *Tracker[T] {
	return &Tracker[T]{t: t}
}

// Record that an event occurred.
func (t *Tracker[T]) Record(event T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

// Empty asserts the tracker is empty
func (t *Tracker[T]) Empty() {
	t.t.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) != 0 {
		t.t.Fatalf("unexpected events: %v", t.events)
	}
}

// WaitOrdered waits for an event to happen, in order
func (t *Tracker[T]) WaitOrdered(events ...T) {
	t.t.Helper()
	for _, event := range events {
		retry.UntilSuccessOrFail(t.t, func() error {
			t.mu.Lock()
			defer t.mu.Unlock()
			if len(t.events) == 0 {
				return fmt.Errorf("no events")
			}
			if t.events[0] != event {
				t.t.Fatalf("got events %v, want %v", t.events, event)
			}
			// clear the event
			t.events = t.events[1:]
			return nil
		}, retry.Timeout(time.Second))
	}
	t.Empty()
}
