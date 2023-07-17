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

package appsignals

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestReloadWatcher(t *testing.T) {
	c := make(chan Signal, 5)
	Watch(c)

	// Direct
	Notify("hi", syscall.SIGINT)
	select {
	case v := <-c:
		if v.Source != "hi" {
			t.Fatalf("Expected 'hi' but got: %v", v.Source)
		}
		if v.Signal != syscall.SIGINT {
			t.Fatalf("Expected 'syscall.SIGINT' but got: %v", v.Signal)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Signal
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("Failed to find current process: %v", err)
	}
	err = process.Signal(syscall.SIGUSR1)
	if err != nil {
		t.Fatalf("Failed to send signal: %v", err)
	}
	select {
	case v := <-c:
		if v.Signal != syscall.SIGUSR1 {
			t.Fatalf("Expected 'SIGUSR1' but got: %v", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// File watch
	f, err := os.CreateTemp("", "marker")
	if err != nil {
		t.Fatalf("failed to created tmpfile: %v", err)
	}
	shutdown := make(chan os.Signal, 1)
	err = FileTrigger(f.Name(), syscall.SIGUSR2, shutdown)
	if err != nil {
		t.Fatalf("failed to watch trigger file: %v", err)
	}
	_, err = f.WriteString("touche!")
	if err != nil {
		t.Fatalf("failed to touch trigger file: %v", err)
	}
	select {
	case v := <-c:
		if v.Source != f.Name() {
			t.Fatalf("Expected '%v' but got: %v", f.Name(), v)
		}
		if v.Signal != syscall.SIGUSR2 {
			t.Fatalf("Expected 'SIGUSR1' but got: %v", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Shutdown the filewatcher
	shutdown <- syscall.SIGTERM

	// Check it is shutdown. This is eventually consistent, so keep touching the file until we don't get updates
	for attempt := 0; attempt < 10; attempt++ {
		_, err = f.WriteString("touche!")
		if err != nil {
			t.Fatalf("failed to touch trigger file after watcher stopped: %v", err)
		}
		// No residual
		select {
		case <-c:
			// Got event, so we aren't shutdown...
			time.Sleep(time.Millisecond * 10)
			continue
		case <-time.After(50 * time.Millisecond):
			// Got no event in 50ms
			return
		}
	}
}

func TestBadPath(t *testing.T) {
	err := FileTrigger("XXXXYYYY", syscall.SIGUSR2, nil)
	if err == nil {
		t.Error("Expecting error, got success")
	}
}
