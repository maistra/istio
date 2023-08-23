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

package crdclient

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/exp/slices"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	metadatafake "k8s.io/client-go/metadata/fake"

	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	clientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func makeClient(t *testing.T, schemas collection.Schemas) (model.ConfigStoreController, kube.CLIClient) {
	fake := kube.NewFakeClient()
	for _, s := range schemas.All() {
		createCRD(t, fake, s)
	}
	stop := test.NewStop(t)
	config, err := New(fake, Option{
		EnableCRDScan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go config.Run(stop)
	fake.RunAndWait(stop)
	kube.WaitForCacheSync(stop, config.HasSynced)
	return config, fake
}

// Ensure that the client can run without CRDs present
func TestClientNoCRDs(t *testing.T) {
	schema := collection.NewSchemasBuilder().MustAdd(collections.Sidecar).Build()
	store, _ := makeClient(t, schema)
	retry.UntilOrFail(t, store.HasSynced, retry.Timeout(time.Second))
	r := collections.VirtualService
	configMeta := config.Meta{
		Name:             "name",
		Namespace:        "ns",
		GroupVersionKind: r.GroupVersionKind(),
	}
	pb, err := r.NewInstance()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Create(config.Config{
		Meta: configMeta,
		Spec: pb,
	}); err != nil {
		t.Fatalf("Create => got %v", err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		l := store.List(r.GroupVersionKind(), configMeta.Namespace)
		if len(l) != 0 {
			return fmt.Errorf("expected no items returned for unknown CRD")
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))
	retry.UntilOrFail(t, func() bool {
		return store.Get(r.GroupVersionKind(), configMeta.Name, configMeta.Namespace) == nil
	}, retry.Message("expected no items returned for unknown CRD"), retry.Timeout(time.Second*5), retry.Converge(5))
}

// Ensure that the client can run without CRDs present, but then added later
func TestClientDelayedCRDs(t *testing.T) {
	schema := collection.NewSchemasBuilder().MustAdd(collections.Sidecar).Build()
	store, fake := makeClient(t, schema)
	retry.UntilOrFail(t, store.HasSynced, retry.Timeout(time.Second))
	r := collections.VirtualService

	// Create a virtual service
	configMeta := config.Meta{
		Name:             "name",
		Namespace:        "ns",
		GroupVersionKind: r.GroupVersionKind(),
	}
	pb, err := r.NewInstance()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(config.Config{
		Meta: configMeta,
		Spec: pb,
	}); err != nil {
		t.Fatalf("Create => got %v", err)
	}

	retry.UntilSuccessOrFail(t, func() error {
		l := store.List(r.GroupVersionKind(), configMeta.Namespace)
		if len(l) != 0 {
			return fmt.Errorf("expected no items returned for unknown CRD")
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))

	createCRD(t, fake, r)

	retry.UntilSuccessOrFail(t, func() error {
		l := store.List(r.GroupVersionKind(), configMeta.Namespace)
		if len(l) != 1 {
			return fmt.Errorf("expected items returned")
		}
		return nil
	}, retry.Timeout(time.Second*10), retry.Converge(5))
}

// CheckIstioConfigTypes validates that an empty store can do CRUD operators on all given types
func TestClient(t *testing.T) {
	store, _ := makeClient(t, collections.PilotGatewayAPI().Union(collections.Kube))
	configName := "test"
	configNamespace := "test-ns"
	timeout := retry.Timeout(time.Millisecond * 200)
	for _, r := range collections.PilotGatewayAPI().All() {
		name := r.Kind()
		t.Run(name, func(t *testing.T) {
			configMeta := config.Meta{
				GroupVersionKind: r.GroupVersionKind(),
				Name:             configName,
			}
			if !r.IsClusterScoped() {
				configMeta.Namespace = configNamespace
			}

			pb, err := r.NewInstance()
			if err != nil {
				t.Fatal(err)
			}

			if _, err := store.Create(config.Config{
				Meta: configMeta,
				Spec: pb,
			}); err != nil {
				t.Fatalf("Create(%v) => got %v", name, err)
			}
			// Kubernetes is eventually consistent, so we allow a short time to pass before we get
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.Meta, configMeta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			}, timeout)

			// Validate it shows up in List
			retry.UntilSuccessOrFail(t, func() error {
				cfgs := store.List(r.GroupVersionKind(), configMeta.Namespace)
				if len(cfgs) != 1 {
					return fmt.Errorf("expected 1 config, got %v", len(cfgs))
				}
				for _, cfg := range cfgs {
					if !reflect.DeepEqual(cfg.Meta, configMeta) {
						return fmt.Errorf("get(%v) => got %v", name, cfg)
					}
				}
				return nil
			}, timeout)

			// check we can update object metadata
			annotations := map[string]string{
				"foo": "bar",
			}
			configMeta.Annotations = annotations
			if _, err := store.Update(config.Config{
				Meta: configMeta,
				Spec: pb,
			}); err != nil {
				t.Errorf("Unexpected Error in Update -> %v", err)
			}
			if r.StatusKind() != "" {
				stat, err := r.Status()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.UpdateStatus(config.Config{
					Meta:   configMeta,
					Status: stat,
				}); err != nil {
					t.Errorf("Unexpected Error in Update -> %v", err)
				}
			}
			var cfg *config.Config
			// validate it is updated
			retry.UntilSuccessOrFail(t, func() error {
				cfg = store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.Meta, configMeta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			})

			// check we can patch items
			var patchedCfg config.Config
			if _, err := store.(*Client).Patch(*cfg, func(cfg config.Config) (config.Config, types.PatchType) {
				cfg.Annotations["fizz"] = "buzz"
				patchedCfg = cfg
				return cfg, types.JSONPatchType
			}); err != nil {
				t.Errorf("unexpected err in Patch: %v", err)
			}
			// validate it is updated
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.Meta, patchedCfg.Meta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			})

			// Check we can remove items
			if err := store.Delete(r.GroupVersionKind(), configName, configNamespace, nil); err != nil {
				t.Fatalf("failed to delete: %v", err)
			}
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configNamespace)
				if cfg != nil {
					return fmt.Errorf("get(%v) => got %v, expected item to be deleted", name, cfg)
				}
				return nil
			}, timeout)
		})
	}

	t.Run("update status", func(t *testing.T) {
		r := collections.WorkloadGroup
		name := "name1"
		namespace := "bar"
		cfgMeta := config.Meta{
			GroupVersionKind: r.GroupVersionKind(),
			Name:             name,
		}
		if !r.IsClusterScoped() {
			cfgMeta.Namespace = namespace
		}
		pb := &v1alpha3.WorkloadGroup{Probe: &v1alpha3.ReadinessProbe{PeriodSeconds: 6}}
		if _, err := store.Create(config.Config{
			Meta: cfgMeta,
			Spec: config.Spec(pb),
		}); err != nil {
			t.Fatalf("Create bad: %v", err)
		}

		retry.UntilSuccessOrFail(t, func() error {
			cfg := store.Get(r.GroupVersionKind(), name, cfgMeta.Namespace)
			if cfg == nil {
				return fmt.Errorf("cfg shouldnt be nil :(")
			}
			if !reflect.DeepEqual(cfg.Meta, cfgMeta) {
				return fmt.Errorf("something is deeply wrong....., %v", cfg.Meta)
			}
			return nil
		})

		stat := &v1alpha1.IstioStatus{
			Conditions: []*v1alpha1.IstioCondition{
				{
					Type:    "Health",
					Message: "heath is badd",
				},
			},
		}

		if _, err := store.UpdateStatus(config.Config{
			Meta:   cfgMeta,
			Spec:   config.Spec(pb),
			Status: config.Status(stat),
		}); err != nil {
			t.Errorf("bad: %v", err)
		}

		retry.UntilSuccessOrFail(t, func() error {
			cfg := store.Get(r.GroupVersionKind(), name, cfgMeta.Namespace)
			if cfg == nil {
				return fmt.Errorf("cfg cant be nil")
			}
			if !reflect.DeepEqual(cfg.Status, stat) {
				return fmt.Errorf("status %v does not match %v", cfg.Status, stat)
			}
			return nil
		})
	})
}

// TestClientInitialSyncSkipsOtherRevisions tests that the initial sync skips objects from other
// revisions.
func TestClientInitialSyncSkipsOtherRevisions(t *testing.T) {
	fake := kube.NewFakeClient()
	for _, s := range collections.Istio.All() {
		createCRD(t, fake, s)
	}

	// Populate the client with some ServiceEntrys such that 1/3 are in the default revision and
	// 2/3 are in different revisions.
	labels := []map[string]string{
		nil,
		{"istio.io/rev": "canary"},
		{"istio.io/rev": "prod"},
	}
	var expectedCfgs []config.Config
	for i := 0; i < 9; i++ {
		selectedLabels := labels[i%len(labels)]
		obj := &clientnetworkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-service-entry-%d", i),
				Namespace: "test",
				Labels:    selectedLabels,
			},
			Spec: v1alpha3.ServiceEntry{},
		}

		clienttest.NewWriter[*clientnetworkingv1alpha3.ServiceEntry](t, fake).Create(obj)
		// Only SEs from the default revision should generate events.
		if selectedLabels == nil {
			expectedCfgs = append(expectedCfgs, TranslateObject(obj, gvk.ServiceEntry, ""))
		}
	}

	// Create a config store with a handler that records add events
	store, err := New(fake, Option{})
	assert.NoError(t, err)

	var cfgsAdded []config.Config
	store.RegisterEventHandler(
		gvk.ServiceEntry,
		func(old config.Config, curr config.Config, event model.Event) {
			if event != model.EventAdd {
				t.Fatalf("unexpected event: %v", event)
			}
			cfgsAdded = append(cfgsAdded, curr)
		},
	)

	stop := test.NewStop(t)
	fake.RunAndWait(stop)
	go store.Run(stop)

	kube.WaitForCacheSync(stop, store.HasSynced)

	// The order of the events doesn't matter, so sort the two slices so the ordering is consistent
	sortFunc := func(a, b config.Config) bool {
		return a.Key() < b.Key()
	}
	slices.SortFunc(cfgsAdded, sortFunc)
	slices.SortFunc(expectedCfgs, sortFunc)

	assert.Equal(t, expectedCfgs, cfgsAdded)
}

func createCRD(t test.Failer, client kube.Client, r resource.Schema) {
	t.Helper()
	crd := &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", r.Plural(), r.Group()),
		},
	}
	clienttest.NewWriter[*v1.CustomResourceDefinition](t, client).Create(crd)

	// Metadata client fake is not kept in sync, so if using a fake client update that as well
	fmc, ok := client.Metadata().(*metadatafake.FakeMetadataClient)
	if !ok {
		return
	}
	fmg := fmc.Resource(gvr.CustomResourceDefinition)
	fmd, ok := fmg.(metadatafake.MetadataClient)
	if !ok {
		return
	}
	if _, err := fmd.CreateFake(&metav1.PartialObjectMetadata{
		TypeMeta:   crd.TypeMeta,
		ObjectMeta: crd.ObjectMeta,
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestClientSync(t *testing.T) {
	obj := &clientnetworkingv1alpha3.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service-entry",
			Namespace: "test",
		},
		Spec: v1alpha3.ServiceEntry{},
	}
	fake := kube.NewFakeClient()
	clienttest.NewWriter[*clientnetworkingv1alpha3.ServiceEntry](t, fake).Create(obj)
	for _, s := range collections.Pilot.All() {
		createCRD(t, fake, s)
	}
	stop := test.NewStop(t)
	c, err := New(fake, Option{})
	assert.NoError(t, err)

	events := atomic.NewInt64(0)
	c.RegisterEventHandler(gvk.ServiceEntry, func(c config.Config, c2 config.Config, event model.Event) {
		events.Inc()
	})
	go c.Run(stop)
	fake.RunAndWait(stop)
	kube.WaitForCacheSync(stop, c.HasSynced)
	// This MUST have been called by the time HasSynced returns true
	assert.Equal(t, events.Load(), 1)
}
