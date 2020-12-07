// Copyright Red Hat, Inc.
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

package server

import (
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestHTTPServer(t *testing.T) {
	testCases := []struct {
		name           string
		requestPath    string
		filename       string
		fileContent    []byte
		expectedStatus int
	}{
		{
			name:           "fail_validUUIDNotFound",
			requestPath:    "71e7c506-24df-11eb-89f5-482ae3492105",
			expectedStatus: 404,
		},
		{
			name:           "pass_validUUIDFound",
			requestPath:    "72e7c506-24df-11eb-89f5-482ae3492105",
			fileContent:    []byte("all good"),
			expectedStatus: 200,
		},
		{
			name:           "fail_invalidUUID",
			requestPath:    "73e7c50624df11eb89f5482ae3492105",
			expectedStatus: 404,
		},
		{
			name:           "fail_ignore_directories",
			requestPath:    "test/74e7c506-24df-11eb-89f5-482ae3492105",
			filename:       "test/74e7c506-24df-11eb-89f5-482ae3492105",
			expectedStatus: 404,
		},
		{
			name:           "pass_ignore_directories",
			requestPath:    "test/asd/75e7c506-24df-11eb-89f5-482ae3492105",
			filename:       "75e7c506-24df-11eb-89f5-482ae3492105",
			fileContent:    []byte("test"),
			expectedStatus: 200,
		},
	}
	tmpDir, err := ioutil.TempDir("", "servertest")
	if err != nil {
		t.Fatalf("failed to create temp dir: %s", err)
	}
	defer func() {
		err = os.RemoveAll(tmpDir)
		if err != nil {
			t.Fatalf("Failed to remove temp directory %s", tmpDir)
		}
	}()

	server := NewHTTPServer(50505, tmpDir)
	baseURL := "http://127.0.0.1:50505/"
	stopChan := make(<-chan struct{})
	server.Start(stopChan)
	// wait a bit until the server is up
	time.Sleep(time.Millisecond * 250)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filename := path.Join(tmpDir, tc.filename)
			if tc.fileContent != nil {
				if tc.filename == "" {
					filename = path.Join(tmpDir, tc.requestPath)
				}
				if _, err := os.Stat(filepath.Dir(filename)); err != nil {
					t.Logf("Directory %s does not exist, creating", filepath.Dir(filename))
					os.MkdirAll(filepath.Dir(filename), os.ModePerm)
				}
				t.Logf("Writing to file %s", filename)
				err := ioutil.WriteFile(filename, tc.fileContent, os.ModePerm)
				if err != nil {
					t.Errorf("%s", err)
				}
			}

			url := baseURL + tc.requestPath
			t.Logf("Sending GET request to %s", url)
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("failed to GET %s: %s", url, err)
			}
			if resp != nil && resp.StatusCode != tc.expectedStatus {
				t.Fatalf("expected StatusCode %d but got %d", tc.expectedStatus, resp.StatusCode)
			}
			if resp != nil && tc.fileContent != nil {
				os.Remove(filename)
				defer resp.Body.Close()
				body, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("failed to read body: %s", err)
				}
				if !cmp.Equal(body, tc.fileContent) {
					t.Fatalf("response didn't match. Expected %v but got %v", tc.fileContent, body)
				}
			}
		})
	}
}
