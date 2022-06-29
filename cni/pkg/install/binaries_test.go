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

package install

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyBinaries(t *testing.T) {
	cases := []struct {
		name           string
		srcFiles       map[string]string // {filename: contents, ...}
		existingFiles  map[string]string // {filename: contents, ...}
		expectedFiles  map[string]string // {filename: contents. ...}
		updateBinaries bool
		skipBinaries   []string
		prefix         string
	}{
		{
			name:          "basic",
			srcFiles:      map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			expectedFiles: map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
		},
		{
			name:           "update binaries",
			updateBinaries: true,
			srcFiles:       map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			existingFiles:  map[string]string{"istio-cni": "cni000", "istio-iptables": "iptables111"},
			expectedFiles:  map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
		},
		{
			name:           "don't update binaries",
			updateBinaries: false,
			srcFiles:       map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			existingFiles:  map[string]string{"istio-cni": "cni000", "istio-iptables": "iptables111"},
			expectedFiles:  map[string]string{"istio-cni": "cni000", "istio-iptables": "iptables111"},
		},
		{
			name:          "skip binaries",
			skipBinaries:  []string{"istio-iptables"},
			srcFiles:      map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			expectedFiles: map[string]string{"istio-cni": "cni111"},
		},
		{
			name:          "binaries prefix",
			prefix:        "prefix-",
			srcFiles:      map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			expectedFiles: map[string]string{"prefix-istio-cni": "cni111", "prefix-istio-iptables": "iptables111"},
		},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srcDir, err := ioutil.TempDir("", fmt.Sprintf("test-case-%d-src-", i))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.RemoveAll(srcDir); err != nil {
					t.Fatal(err)
				}
			}()
			for filename, contents := range c.srcFiles {
				err := ioutil.WriteFile(filepath.Join(srcDir, filename), []byte(contents), os.ModePerm)
				if err != nil {
					t.Fatal(err)
				}
			}

			targetDir, err := ioutil.TempDir("", fmt.Sprintf("test-case-%d-target-", i))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.RemoveAll(targetDir); err != nil {
					t.Fatal(err)
				}
			}()
			for filename, contents := range c.existingFiles {
				err := ioutil.WriteFile(filepath.Join(targetDir, filename), []byte(contents), os.ModePerm)
				if err != nil {
					t.Fatal(err)
				}
			}

			err = copyBinaries(srcDir, []string{targetDir}, c.updateBinaries, c.skipBinaries, c.prefix)
			if err != nil {
				t.Fatal(err)
			}

			for filename, expectedContents := range c.expectedFiles {
				contents, err := ioutil.ReadFile(filepath.Join(targetDir, filename))
				if err != nil {
					t.Fatal(err)
				}
				if string(contents) != expectedContents {
					t.Fatalf("target file contents don't match source file; actual: %s", string(contents))
				}
			}
		})
	}
}
