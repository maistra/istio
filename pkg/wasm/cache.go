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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"istio.io/pkg/log"
)

var wasmLog = log.RegisterScope("wasm", "", 0)

const (
	// DefaultWasmModulePurgeInterval is the default interval for periodic stale Wasm module clean up.
	DefaultWasmModulePurgeInterval = 10 * time.Minute

	// DefaultWasmModuleExpiry is the default duration for least recently touched Wasm module to become stale.
	DefaultWasmModuleExpiry = 24 * time.Hour
)

// Cache models a Wasm module cache.
type Cache interface {
	Get(url, checksum string, timeout time.Duration) (string, error)
	Cleanup()
}

// LocalFileCache for downloaded Wasm modules. Currently it stores the Wasm module as local file.
type LocalFileCache struct {
	// Map from Wasm module checksum to cache entry.
	modules map[cacheKey]*cacheEntry

	// http fetcher fetches Wasm module with HTTP get.
	httpFetcher *HTTPFetcher

	// directory path used to store Wasm module.
	dir string

	// mux is needed because stale Wasm module files will be purged periodically.
	mux sync.Mutex

	// Duration for stale Wasm module purging.
	purgeInterval    time.Duration
	wasmModuleExpiry time.Duration

	// stopChan currently is only used by test
	stopChan chan struct{}
}

var _ Cache = &LocalFileCache{}

type cacheKey struct {
	downloadURL string
	checksum    string
}

// cacheEntry contains information about a Wasm module cache entry.
type cacheEntry struct {
	// File path to the downloaded wasm modules.
	modulePath string

	// Last time that this local Wasm module is referenced.
	last time.Time
}

// NewLocalFileCache create a new Wasm module cache which downloads and stores Wasm module files locally.
func NewLocalFileCache(dir string, purgeInterval, moduleExpiry time.Duration) *LocalFileCache {
	cache := &LocalFileCache{
		httpFetcher:      NewHTTPFetcher(),
		modules:          make(map[cacheKey]*cacheEntry),
		dir:              dir,
		purgeInterval:    purgeInterval,
		wasmModuleExpiry: moduleExpiry,
		stopChan:         make(chan struct{}),
	}
	go func() {
		cache.purge()
	}()
	return cache
}

// Get returns path the local Wasm module file.
func (c *LocalFileCache) Get(downloadURL, checksum string, timeout time.Duration) (string, error) {
	// Construct Wasm cache key with downloading URL and provided checksum of the module.
	key := cacheKey{
		downloadURL: downloadURL,
		checksum:    checksum,
	}

	// First check if the cache entry is already downloaded.
	if modulePath := c.getEntry(key); modulePath != "" {
		return modulePath, nil
	}

	// If not, fetch images.
	u, err := url.Parse(downloadURL)
	if err != nil {
		return "", fmt.Errorf("fail to parse Wasm module fetch url: %s", downloadURL)
	}

	// Byte array of Wasm binary.
	var b []byte
	// Hex-Encoded sha256 checksum of binary.
	var dChecksum string
	switch u.Scheme {
	case "http", "https":
		// Download the Wasm module with http fetcher.
		b, err = c.httpFetcher.Fetch(downloadURL, timeout)
		if err != nil {
			wasmRemoteFetchCount.With(resultTag.Value(downloadFailure)).Increment()
			return "", err
		}

		// Get sha256 checksum and check if it is the same as provided one.
		sha := sha256.Sum256(b)
		dChecksum = hex.EncodeToString(sha[:])
		if checksum != "" && dChecksum != checksum {
			wasmRemoteFetchCount.With(resultTag.Value(checksumMismatch)).Increment()
			return "", fmt.Errorf("module downloaded from %v has checksum %v, which does not match: %v", downloadURL, dChecksum, checksum)
		}
	case "oci":
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		// TODO: support imagePullSecret and pass it to ImageFetcherOption.
		fetcher := NewImageFetcher(ctx, ImageFetcherOption{})
		b, err = fetcher.Fetch(u.Host+u.Path, checksum)
		if err != nil {
			if errors.Is(err, errWasmOCIImageDigestMismatch) {
				wasmRemoteFetchCount.With(resultTag.Value(checksumMismatch)).Increment()
			} else {
				wasmRemoteFetchCount.With(resultTag.Value(downloadFailure)).Increment()
			}
			return "", fmt.Errorf("could not fetch Wasm OCI image: %v", err)
		}
		sha := sha256.Sum256(b)
		dChecksum = hex.EncodeToString(sha[:])
	default:
		return "", fmt.Errorf("unsupported Wasm module downloading URL scheme: %v", u.Scheme)
	}

	if !isValidWasmBinary(b) {
		wasmRemoteFetchCount.With(resultTag.Value(fetchFailure)).Increment()
		return "", fmt.Errorf("fetched Wasm binary from %s is invalid", downloadURL)
	}

	wasmRemoteFetchCount.With(resultTag.Value(fetchSuccess)).Increment()

	key.checksum = dChecksum
	f := filepath.Join(c.dir, fmt.Sprintf("%s.wasm", dChecksum))

	if err := c.addEntry(key, b, f); err != nil {
		return "", err
	}
	return f, nil
}

// Cleanup closes background Wasm module purge routine.
func (c *LocalFileCache) Cleanup() {
	close(c.stopChan)
}

func (c *LocalFileCache) addEntry(key cacheKey, wasmModule []byte, f string) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Check if the module has already been added. If so, avoid writing the file again.
	if ce, ok := c.modules[key]; ok {
		// Update last touched time.
		ce.last = time.Now()
		return nil
	}

	// Materialize the Wasm module into a local file. Use checksum as name of the module.
	if err := os.WriteFile(f, wasmModule, 0o644); err != nil {
		return err
	}

	ce := cacheEntry{
		modulePath: f,
		last:       time.Now(),
	}
	c.modules[key] = &ce
	wasmCacheEntries.Record(float64(len(c.modules)))
	return nil
}

func (c *LocalFileCache) getEntry(key cacheKey) string {
	modulePath := ""
	cacheHit := false
	c.mux.Lock()
	defer c.mux.Unlock()
	if ce, ok := c.modules[key]; ok {
		// Update last touched time.
		ce.last = time.Now()
		modulePath = ce.modulePath
		cacheHit = true
	}
	wasmCacheLookupCount.With(hitTag.Value(strconv.FormatBool(cacheHit))).Increment()
	return modulePath
}

// Purge periodically clean up the stale Wasm modules local file and the cache map.
func (c *LocalFileCache) purge() {
	ticker := time.NewTicker(c.purgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mux.Lock()
			for k, m := range c.modules {
				if m.expired(c.wasmModuleExpiry) {
					// The module has not be touched for expiry duration, delete it from the map as well as the local dir.
					if err := os.Remove(m.modulePath); err != nil {
						wasmLog.Errorf("failed to purge Wasm module %v: %v", m.modulePath, err)
					} else {
						delete(c.modules, k)
						wasmLog.Debugf("successfully removed stale Wasm module %v", m.modulePath)
					}
				}
			}
			wasmCacheEntries.Record(float64(len(c.modules)))
			c.mux.Unlock()
		case <-c.stopChan:
			// Currently this will only happen in test.
			return
		}
	}
}

// Expired returns true if the module has not been touched for Wasm module Expiry.
func (ce *cacheEntry) expired(expiry time.Duration) bool {
	now := time.Now()
	return now.Sub(ce.last) > expiry
}

var wasmMagicNumber = []byte{0x00, 0x61, 0x73, 0x6d}

func isValidWasmBinary(in []byte) bool {
	// Wasm file header is 8 bytes (magic number + version).
	return len(in) >= 8 && bytes.Equal(in[:4], wasmMagicNumber)
}
