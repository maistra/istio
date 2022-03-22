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

package wasm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Wasm header = magic number (4 bytes) + Wasm spec version (4 bytes).
var wasmHeader = append(wasmMagicNumber, []byte{0x1, 0x00, 0x00, 0x00}...)

func TestWasmCache(t *testing.T) {
	// Setup http server.
	tsNumRequest := 0
	httpData := append(wasmHeader, []byte("data")...)
	invalidHTTPData := []byte("invalid binary")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tsNumRequest++
		if r.URL.Path == "/different-url" {
			w.Write(append(httpData, []byte("different data")...))
		} else if r.URL.Path == "/invalid-wasm-header" {
			w.Write(invalidHTTPData)
		} else {
			w.Write(httpData)
		}
	}))
	defer ts.Close()
	httpDataSha := sha256.Sum256(httpData)
	httpDataCheckSum := hex.EncodeToString(httpDataSha[:])
	invalidHTTPDataSha := sha256.Sum256(invalidHTTPData)
	invalidHTTPDataCheckSum := hex.EncodeToString(invalidHTTPDataSha[:])

	// Set up a fake registry for OCI images.
	tos := httptest.NewServer(registry.New())
	defer tos.Close()
	ou, err := url.Parse(tos.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantOCIDockerBinaryChecksum, dockerImageDigest, invalidOCIImageDigest := setupOCIRegistry(t, ou.Host)

	// Calculate cachehit sum.
	cacheHitSha := sha256.Sum256([]byte("cachehit"))
	cacheHitSum := hex.EncodeToString(cacheHitSha[:])

	cases := []struct {
		name                 string
		initialCachedModules map[cacheKey]cacheEntry
		fetchURL             string
		purgeInterval        time.Duration
		wasmModuleExpiry     time.Duration
		checkPurgeTimeout    time.Duration
		checksum             string // Hex-encoded string.
		requestTimeout       time.Duration
		wantFileName         string
		wantErrorMsgPrefix   string
		wantServerReqNum     int
	}{
		{
			name:                 "cache miss",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             ts.URL,
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             httpDataCheckSum,
			wantFileName:         fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantServerReqNum:     1,
		},
		{
			name: "cache hit",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: cacheHitSum}: {modulePath: "test.wasm"},
			},
			fetchURL:         ts.URL,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			checksum:         cacheHitSum,
			wantFileName:     "test.wasm",
			wantServerReqNum: 0,
		},
		{
			name:                 "invalid scheme",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             "foo://abc",
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             httpDataCheckSum,
			wantFileName:         fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantErrorMsgPrefix:   "unsupported Wasm module downloading URL scheme: foo",
			wantServerReqNum:     0,
		},
		{
			name:                 "download failure",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             "https://dummyurl",
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			wantErrorMsgPrefix:   "wasm module download failed, last error: Get \"https://dummyurl\"",
			wantServerReqNum:     0,
		},
		{
			name:                 "wrong checksum",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             ts.URL,
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             "wrongchecksum\n",
			wantErrorMsgPrefix:   fmt.Sprintf("module downloaded from %v has checksum %s, which does not match", ts.URL, httpDataCheckSum),
			wantServerReqNum:     1,
		},
		{
			// this might be common error in user configuration, that url was updated, but not checksum.
			// Test that downloading still proceeds and error returns.
			name: "different url same checksum",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			fetchURL:           ts.URL + "/different-url",
			purgeInterval:      DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:   DefaultWasmModuleExpiry,
			checksum:           httpDataCheckSum,
			wantErrorMsgPrefix: fmt.Sprintf("module downloaded from %v/different-url has checksum", ts.URL),
			wantServerReqNum:   1,
		},
		{
			name: "invalid wasm header",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			fetchURL:           ts.URL + "/invalid-wasm-header",
			purgeInterval:      DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:   DefaultWasmModuleExpiry,
			checksum:           invalidHTTPDataCheckSum,
			wantErrorMsgPrefix: fmt.Sprintf("fetched Wasm binary from %s is invalid", ts.URL+"/invalid-wasm-header"),
			wantServerReqNum:   1,
		},
		{
			name: "purge on expiry",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			fetchURL:          ts.URL,
			purgeInterval:     1 * time.Millisecond,
			wasmModuleExpiry:  1 * time.Millisecond,
			checkPurgeTimeout: 5 * time.Second,
			checksum:          httpDataCheckSum,
			wantFileName:      fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantServerReqNum:  1,
		},
		{
			name:                 "fetch oci without digest",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host),
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			requestTimeout:       time.Second * 10,
			wantFileName:         fmt.Sprintf("%s.wasm", wantOCIDockerBinaryChecksum),
		},
		{
			name:                 "fetch oci with digest",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host),
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			requestTimeout:       time.Second * 10,
			checksum:             dockerImageDigest,
			wantFileName:         fmt.Sprintf("%s.wasm", wantOCIDockerBinaryChecksum),
		},
		{
			name:                 "fetch oci timed out",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/invalid", ou.Host),
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			requestTimeout:       0, // Cause timeout immediately.
			wantErrorMsgPrefix:   fmt.Sprintf("could not fetch Wasm OCI image: could not fetch image: Get \"https://%s/v2/\"", ou.Host),
		},
		{
			name:                 "fetch oci with wrong digest",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host),
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			requestTimeout:       time.Second * 10,
			checksum:             "wrongdigest",
			wantErrorMsgPrefix: fmt.Sprintf(
				"could not fetch Wasm OCI image: fetched image's digest does not match the expected one: got %s, but want wrongdigest", dockerImageDigest,
			),
		},
		{
			name:                 "fetch invalid oci",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/invalid", ou.Host),
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             invalidOCIImageDigest,
			requestTimeout:       time.Second * 10,
			wantErrorMsgPrefix: `could not fetch Wasm OCI image: the given image is in invalid format as an OCI image: 2 errors occurred:
	* could not parse as compat variant: invalid media type application/vnd.oci.image.layer.v1.tar (expect application/vnd.oci.image.layer.v1.tar+gzip)
	* could not parse as oci variant: number of layers must be 2 but got 1`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cache := NewLocalFileCache(tmpDir, c.purgeInterval, c.wasmModuleExpiry)
			defer close(cache.stopChan)
			tsNumRequest = 0

			var cacheHitKey *cacheKey
			initTime := time.Now()
			cache.mux.Lock()
			for k, m := range c.initialCachedModules {
				filePath := filepath.Join(tmpDir, m.modulePath)
				err := os.WriteFile(filePath, []byte("data/\n"), 0o644)
				if err != nil {
					t.Fatalf("failed to write initial wasm module file %v", err)
				}
				cache.modules[cacheKey{downloadURL: k.downloadURL, checksum: k.checksum}] =
					&cacheEntry{modulePath: filePath, last: time.Now()}
			}
			cache.mux.Unlock()

			if c.checkPurgeTimeout > 0 {
				moduleDeleted := false
				for start := time.Now(); time.Since(start) < c.checkPurgeTimeout; {
					// Check existence of module files. files should be deleted before timing out.
					if files, err := os.ReadDir(tmpDir); err == nil && len(files) == 0 {
						moduleDeleted = true
						break
					}
				}
				if !moduleDeleted {
					t.Fatalf("Wasm modules are not purged before purge timeout")
				}
			}

			gotFilePath, gotErr := cache.Get(c.fetchURL, c.checksum, c.requestTimeout)
			if cacheHitKey != nil {
				cache.mux.Lock()
				if entry, ok := cache.modules[*cacheHitKey]; ok && entry.last == initTime {
					t.Errorf("Wasm module cache entry's last access time not updated after get operation, key: %v", *cacheHitKey)
				}
				cache.mux.Unlock()
			}
			wantFilePath := filepath.Join(tmpDir, c.wantFileName)
			if c.wantErrorMsgPrefix != "" {
				if gotErr == nil {
					t.Errorf("Wasm module cache lookup got no error, want error prefix `%v`", c.wantErrorMsgPrefix)
				} else if !strings.HasPrefix(gotErr.Error(), c.wantErrorMsgPrefix) {
					t.Errorf("Wasm module cache lookup got error `%v`, want error prefix `%v`", gotErr, c.wantErrorMsgPrefix)
				}
			} else if gotFilePath != wantFilePath {
				t.Errorf("Wasm module local file path got %v, want %v", gotFilePath, wantFilePath)
				if gotErr != nil {
					t.Errorf("got unexpected error %v", gotErr)
				}
			}
			if c.wantServerReqNum != tsNumRequest {
				t.Errorf("test server request number got %v, want %v", tsNumRequest, c.wantServerReqNum)
			}
		})
	}
}

func setupOCIRegistry(t *testing.T, host string) (wantBinaryCheckSum, dockerImageDigest, invalidOCIImageDigest string) {
	// Push *compat* variant docker image (others are well tested in imagefetcher's test and the behavior is consistent).
	ref := fmt.Sprintf("%s/test/valid/docker:v0.1.0", host)
	binary := append(wasmHeader, []byte("this is wasm plugin")...)

	// Create docker layer.
	l, err := newMockLayer(types.DockerLayer,
		map[string][]byte{"plugin.wasm": binary})
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
	if err != nil {
		t.Fatal(err)
	}

	// Set manifest type.
	manifest, err := img.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.MediaType = types.DockerManifestSchema2

	// Push image to the registry.
	err = crane.Push(img, ref)
	if err != nil {
		t.Fatal(err)
	}

	// Calculate sum
	sha := sha256.Sum256(binary)
	wantBinaryCheckSum = hex.EncodeToString(sha[:])
	d, _ := img.Digest()
	dockerImageDigest = d.Hex

	// Finally push the invalid image.
	ref = fmt.Sprintf("%s/test/invalid", host)
	l, err = newMockLayer(types.OCIUncompressedLayer, map[string][]byte{"not-wasm.txt": []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
	img2, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
	if err != nil {
		t.Fatal(err)
	}

	// Set manifest type so it will pass the docker parsing branch.
	manifest, err = img2.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.MediaType = "no-docker"

	d, _ = img2.Digest()
	invalidOCIImageDigest = d.Hex

	// Push image to the registry.
	err = crane.Push(img2, ref)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestWasmCacheMissChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewLocalFileCache(tmpDir, DefaultWasmModulePurgeInterval, DefaultWasmModuleExpiry)
	defer close(cache.stopChan)

	gotNumRequest := 0
	binary1 := append(wasmHeader, 1)
	binary2 := append(wasmHeader, 2)
	// Create a test server which returns 0 for the first two calls, and returns 1 for the following calls.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotNumRequest <= 1 {
			w.Write(binary1)
		} else {
			w.Write(binary2)
		}
		gotNumRequest++
	}))
	defer ts.Close()
	wantFilePath1 := filepath.Join(tmpDir, fmt.Sprintf("%x.wasm", sha256.Sum256(binary1)))
	wantFilePath2 := filepath.Join(tmpDir, fmt.Sprintf("%x.wasm", sha256.Sum256(binary2)))

	// Get wasm module three times, since checksum is not specified, it will be fetched from module server every time.
	// 1st time
	gotFilePath, err := cache.Get(ts.URL, "", 0)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath1 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath1)
	}

	// 2nd time
	gotFilePath, err = cache.Get(ts.URL, "", 0)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath1 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath1)
	}

	// 3rd time
	gotFilePath, err = cache.Get(ts.URL, "", 0)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath2 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath2)
	}

	wantNumRequest := 3
	if gotNumRequest != wantNumRequest {
		t.Errorf("wasm download call got %v want %v", gotNumRequest, wantNumRequest)
	}
}
