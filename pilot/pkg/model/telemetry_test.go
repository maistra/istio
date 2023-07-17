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

package model

import (
	"strings"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wasmfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/envoy/extensions/stats"
	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	jsonTextProvider = &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-json",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
						Labels: &structpb.Struct{},
					},
				},
			},
		},
	}

	textFormattersProvider = &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-text-formatters",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text{
						Text: "%REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%\n",
					},
				},
			},
		},
	}

	jsonFormattersProvider = &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-json-formatters",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: "/dev/stdout",
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
						Labels: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"req1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
								"req2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key2:val1)%"}},
								"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
								"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
							},
						},
					},
				},
			},
		},
	}

	defaultJSONLabelsOut = &fileaccesslog.FileAccessLog{
		Path: "/dev/stdout",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: EnvoyJSONLogFormatIstio,
				},
			},
		},
	}

	formattersJSONLabelsOut = &fileaccesslog.FileAccessLog{
		Path: "/dev/stdout",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Formatters: []*core.TypedExtensionConfig{
					reqWithoutQueryFormatter,
					metadataFormatter,
				},
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"req1": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key1:val1)%"}},
							"req2": {Kind: &structpb.Value_StringValue{StringValue: "%REQ_WITHOUT_QUERY(key2:val1)%"}},
							"key1": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(CLUSTER:istio)%"}},
							"key2": {Kind: &structpb.Value_StringValue{StringValue: "%METADATA(UPSTREAM_HOST:istio)%"}},
						},
					},
				},
			},
		},
	}

	formattersTextLabelsOut = &fileaccesslog.FileAccessLog{
		Path: "/dev/stdout",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Formatters: []*core.TypedExtensionConfig{
					reqWithoutQueryFormatter,
					metadataFormatter,
				},
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: "%REQ_WITHOUT_QUERY(key1:val1)% REQ_WITHOUT_QUERY(key2:val1)% %METADATA(UPSTREAM_HOST:istio)% %METADATA(CLUSTER:istio)%\n",
						},
					},
				},
			},
		},
	}
)

func createTestTelemetries(configs []config.Config, t *testing.T) (*Telemetries, *PushContext) {
	t.Helper()

	store := &telemetryStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	m := mesh.DefaultMeshConfig()

	m.ExtensionProviders = append(m.ExtensionProviders, jsonTextProvider, textFormattersProvider, jsonFormattersProvider)

	environment := &Environment{
		ConfigStore: store,
		Watcher:     mesh.NewFixedWatcher(m),
	}
	telemetries := getTelemetries(environment)

	ctx := NewPushContext()
	ctx.Mesh = m
	return telemetries, ctx
}

func newTelemetry(ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Telemetry,
			Name:             "default",
			Namespace:        ns,
		},
		Spec: spec,
	}
}

type telemetryStore struct {
	ConfigStore

	data []struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}
}

func (ts *telemetryStore) add(cfg config.Config) {
	ts.data = append(ts.data, struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}{
		typ: cfg.GroupVersionKind,
		ns:  cfg.Namespace,
		cfg: cfg,
	})
}

func (ts *telemetryStore) Schemas() collection.Schemas {
	return collection.SchemasFor()
}

func (ts *telemetryStore) Get(_ config.GroupVersionKind, _, _ string) *config.Config {
	return nil
}

func (ts *telemetryStore) List(typ config.GroupVersionKind, namespace string) []config.Config {
	var configs []config.Config
	for _, data := range ts.data {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs
}

func newTracingConfig(providerName string, disabled bool) *TracingConfig {
	return &TracingConfig{
		ClientSpec: TracingSpec{
			Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: providerName},
			Disabled:                     disabled,
			UseRequestIDForTraceSampling: true,
		},
		ServerSpec: TracingSpec{
			Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: providerName},
			Disabled:                     disabled,
			UseRequestIDForTraceSampling: true,
		},
	}
}

const (
	reportingEnabled  = false
	reportingDisabled = !reportingEnabled
)

func TestTracing(t *testing.T) {
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
	}
	envoy := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "envoy",
					},
				},
			},
		},
	}
	stackdriver := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "stackdriver",
					},
				},
			},
		},
	}
	empty := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{{}},
	}
	disabled := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				DisableSpanReporting: &wrappers.BoolValue{Value: true},
			},
		},
	}
	overidesA := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				RandomSamplingPercentage: &wrappers.DoubleValue{Value: 50.0},
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"foo": {},
					"bar": {},
				},
				UseRequestIdForTraceSampling: &wrappers.BoolValue{Value: false},
			},
		},
	}
	overidesB := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				RandomSamplingPercentage: &wrappers.DoubleValue{Value: 80.0},
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"foo": {},
					"baz": {},
				},
				UseRequestIdForTraceSampling: &wrappers.BoolValue{Value: true},
			},
		},
	}
	overridesWithDefaultSampling := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				CustomTags: map[string]*tpb.Tracing_CustomTag{
					"foo": {},
					"baz": {},
				},
			},
		},
	}
	nonExistant := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "custom-provider",
					},
				},
			},
		},
	}
	clientSideSampling := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Match: &tpb.Tracing_TracingSelector{
					Mode: tpb.WorkloadMode_CLIENT,
				},
				Providers: []*tpb.ProviderRef{
					{
						Name: "stackdriver",
					},
				},
				RandomSamplingPercentage: &wrappers.DoubleValue{Value: 99.9},
			},
		},
	}
	serverSideDisabled := &tpb.Telemetry{
		Tracing: []*tpb.Tracing{
			{
				Match: &tpb.Tracing_TracingSelector{
					Mode: tpb.WorkloadMode_SERVER,
				},
				DisableSpanReporting: &wrappers.BoolValue{Value: true},
			},
		},
	}

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		defaultProviders []string
		want             *TracingConfig
	}{
		{
			"empty",
			nil,
			sidecar,
			nil,
			nil,
		},
		{
			"default provider only",
			nil,
			sidecar,
			[]string{"envoy"},
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"provider only",
			[]config.Config{newTelemetry("istio-system", envoy)},
			sidecar,
			nil,
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"override default",
			[]config.Config{newTelemetry("istio-system", envoy)},
			sidecar,
			[]string{"stackdriver"},
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"override namespace",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", stackdriver)},
			sidecar,
			nil,
			newTracingConfig("stackdriver", reportingEnabled),
		},
		{
			"empty config inherits",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", empty)},
			sidecar,
			nil,
			newTracingConfig("envoy", reportingEnabled),
		},
		{
			"disable config",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", disabled)},
			sidecar,
			nil,
			newTracingConfig("envoy", reportingDisabled),
		},
		{
			"disable default",
			[]config.Config{newTelemetry("default", disabled)},
			sidecar,
			[]string{"envoy"},
			newTracingConfig("envoy", reportingDisabled),
		},
		{
			"non existing",
			[]config.Config{newTelemetry("default", nonExistant)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{Disabled: true, UseRequestIDForTraceSampling: true},
				ServerSpec: TracingSpec{Disabled: true, UseRequestIDForTraceSampling: true},
			},
		},
		{
			"overrides",
			[]config.Config{newTelemetry("istio-system", overidesA)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 50.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"bar": {},
					},
					UseRequestIDForTraceSampling: false,
				}, ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 50.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"bar": {},
					},
					UseRequestIDForTraceSampling: false,
				},
			},
		},
		{
			"overrides with default sampling",
			[]config.Config{newTelemetry("istio-system", overridesWithDefaultSampling)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 0.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
				}, ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 0.0,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
				},
			},
		},
		{
			"multi overrides",
			[]config.Config{
				newTelemetry("istio-system", overidesA),
				newTelemetry("default", overidesB),
			},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 80,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
				},
				ServerSpec: TracingSpec{
					Provider:                 &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					RandomSamplingPercentage: 80,
					CustomTags: map[string]*tpb.Tracing_CustomTag{
						"foo": {},
						"baz": {},
					},
					UseRequestIDForTraceSampling: true,
				},
			},
		},
		{
			"client-only override",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", clientSideSampling)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider: &meshconfig.MeshConfig_ExtensionProvider{
						Name: "stackdriver",
						Provider: &meshconfig.MeshConfig_ExtensionProvider_Stackdriver{
							Stackdriver: &meshconfig.MeshConfig_ExtensionProvider_StackdriverProvider{},
						},
					},
					RandomSamplingPercentage:     99.9,
					UseRequestIDForTraceSampling: true,
				},
				ServerSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					UseRequestIDForTraceSampling: true,
				},
			},
		},
		{
			"server-only override",
			[]config.Config{newTelemetry("istio-system", envoy), newTelemetry("default", serverSideDisabled)},
			sidecar,
			[]string{"envoy"},
			&TracingConfig{
				ClientSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					UseRequestIDForTraceSampling: true,
				},
				ServerSpec: TracingSpec{
					Provider:                     &meshconfig.MeshConfig_ExtensionProvider{Name: "envoy"},
					Disabled:                     true,
					UseRequestIDForTraceSampling: true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, _ := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders.Tracing = tt.defaultProviders
			got := telemetry.Tracing(tt.proxy)
			if got != nil && got.ServerSpec.Provider != nil {
				// We don't match on this, just the name for test simplicity
				got.ServerSpec.Provider.Provider = nil
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestTelemetryFilters(t *testing.T) {
	overrides := []*tpb.MetricsOverrides{{
		Match: &tpb.MetricSelector{
			MetricMatch: &tpb.MetricSelector_Metric{
				Metric: tpb.MetricSelector_REQUEST_COUNT,
			},
		},
		TagOverrides: map[string]*tpb.MetricsOverrides_TagOverride{
			"remove": {
				Operation: tpb.MetricsOverrides_TagOverride_REMOVE,
			},
			"add": {
				Operation: tpb.MetricsOverrides_TagOverride_UPSERT,
				Value:     "bar",
			},
		},
	}}
	sidecar := &Proxy{
		ConfigNamespace: "default",
		Labels:          map[string]string{"app": "test"},
		Metadata:        &NodeMetadata{Labels: map[string]string{"app": "test"}},
	}
	emptyPrometheus := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
			},
		},
	}
	overridesPrometheus := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
				Overrides: overrides,
			},
		},
	}
	reportingInterval := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers:         []*tpb.ProviderRef{{Name: "prometheus"}},
				ReportingInterval: durationpb.New(15 * time.Second),
			},
		},
	}
	overridesInterval := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers:         []*tpb.ProviderRef{{Name: "prometheus"}},
				ReportingInterval: durationpb.New(10 * time.Second),
			},
		},
	}
	emptyStackdriver := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
			},
		},
	}
	overridesStackdriver := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
				Overrides: overrides,
			},
		},
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
				Filter: &tpb.AccessLogging_Filter{
					Expression: `response.code >= 500 && response.code <= 800`,
				},
			},
		},
	}
	overridesEmptyProvider := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Overrides: overrides,
			},
		},
	}
	sdLogging := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{{Name: "stackdriver"}},
			},
		},
	}
	emptyLogging := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{},
		},
	}
	disabledAllMetrics := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Overrides: []*tpb.MetricsOverrides{{
					Match: &tpb.MetricSelector{
						MetricMatch: &tpb.MetricSelector_Metric{
							Metric: tpb.MetricSelector_ALL_METRICS,
						},
					},
					Disabled: &wrappers.BoolValue{
						Value: true,
					},
				}},

				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
			},
		},
	}
	disabledAllMetricsImplicit := &tpb.Telemetry{
		Metrics: []*tpb.Metrics{
			{
				Overrides: []*tpb.MetricsOverrides{{
					Disabled: &wrappers.BoolValue{
						Value: true,
					},
				}},

				Providers: []*tpb.ProviderRef{{Name: "prometheus"}},
			},
		},
	}
	stackdriverDisabled := &tpb.Telemetry{
		AccessLogging: []*tpb.AccessLogging{
			{
				Providers: []*tpb.ProviderRef{
					{
						Name: "stackdriver",
					},
				},
				Disabled: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
	}

	cfg := `{"metrics":[{"dimensions":{"add":"bar"},"name":"requests_total","tags_to_remove":["remove"]}]}`

	tests := []struct {
		name             string
		cfgs             []config.Config
		proxy            *Proxy
		class            networking.ListenerClass
		protocol         networking.ListenerProtocol
		defaultProviders *meshconfig.MeshConfig_DefaultProviders
		want             map[string]string
	}{
		{
			"empty",
			nil,
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-prometheus",
			[]config.Config{newTelemetry("istio-system", disabledAllMetrics)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-prometheus-implicit",
			[]config.Config{newTelemetry("istio-system", disabledAllMetricsImplicit)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-then-empty",
			[]config.Config{
				newTelemetry("istio-system", disabledAllMetrics),
				newTelemetry("default", emptyPrometheus),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{},
		},
		{
			"disabled-then-overrides",
			[]config.Config{
				newTelemetry("istio-system", disabledAllMetrics),
				newTelemetry("default", overridesPrometheus),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			"default prometheus",
			[]config.Config{newTelemetry("istio-system", emptyPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": "{}",
			},
		},
		{
			"default provider prometheus",
			[]config.Config{},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			map[string]string{
				"istio.stats": "{}",
			},
		},
		{
			"prometheus overrides",
			[]config.Config{newTelemetry("istio-system", overridesPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			"prometheus overrides TCP",
			[]config.Config{newTelemetry("istio-system", overridesPrometheus)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolTCP,
			nil,
			map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			"reporting-interval",
			[]config.Config{newTelemetry("istio-system", reportingInterval)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": `{"tcp_reporting_duration":"15s"}`,
			},
		},
		{
			"override-interval",
			[]config.Config{
				newTelemetry("istio-system", reportingInterval),
				newTelemetry("default", overridesInterval),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": `{"tcp_reporting_duration":"10s"}`,
			},
		},
		{
			"empty stackdriver",
			[]config.Config{newTelemetry("istio-system", emptyStackdriver)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"overrides stackdriver",
			[]config.Config{newTelemetry("istio-system", overridesStackdriver)},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"access_logging_filter_expression":"response.code >= 500 && response.code <= 800",` +
					`"metric_expiry_duration":"3600s","metrics_overrides":{"client/request_count":{"tag_overrides":{"add":"bar"}}}}`,
			},
		},
		{
			"namespace empty merge",
			[]config.Config{
				newTelemetry("istio-system", emptyPrometheus),
				newTelemetry("default", emptyStackdriver),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"namespace overrides merge without provider",
			[]config.Config{
				newTelemetry("istio-system", emptyPrometheus),
				newTelemetry("default", overridesEmptyProvider),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			"namespace overrides merge with default provider",
			[]config.Config{
				newTelemetry("default", overridesEmptyProvider),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			map[string]string{
				"istio.stats": cfg,
			},
		},
		{
			"namespace overrides default provider",
			[]config.Config{
				newTelemetry("default", emptyStackdriver),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{Metrics: []string{"prometheus"}},
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"stackdriver logging",
			[]config.Config{
				newTelemetry("default", sdLogging),
			},
			sidecar,
			networking.ListenerClassSidecarOutbound,
			networking.ListenerProtocolHTTP,
			nil,
			map[string]string{
				"istio.stackdriver": `{"access_logging":"ERRORS_ONLY","metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"stackdriver logging default provider",
			[]config.Config{
				newTelemetry("default", emptyLogging),
			},
			sidecar,
			networking.ListenerClassSidecarInbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{AccessLogging: []string{"stackdriver"}},
			map[string]string{
				"istio.stackdriver": `{"disable_host_header_fallback":true,"access_logging":"FULL","metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"stackdriver default for all",
			[]config.Config{},
			sidecar,
			networking.ListenerClassSidecarInbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{
				Metrics:       []string{"stackdriver"},
				AccessLogging: []string{"stackdriver"},
			},
			map[string]string{
				"istio.stackdriver": `{"disable_host_header_fallback":true,"access_logging":"FULL","metric_expiry_duration":"3600s"}`,
			},
		},
		{
			"disable stackdriver",
			[]config.Config{newTelemetry("istio-system", stackdriverDisabled)},
			sidecar,
			networking.ListenerClassSidecarInbound,
			networking.ListenerProtocolHTTP,
			&meshconfig.MeshConfig_DefaultProviders{
				Metrics:       []string{"stackdriver"},
				AccessLogging: []string{"stackdriver"},
			},
			map[string]string{
				"istio.stackdriver": `{"disable_server_access_logging":true,"disable_host_header_fallback":true,"metric_expiry_duration":"3600s"}`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			telemetry, _ := createTestTelemetries(tt.cfgs, t)
			telemetry.meshConfig.DefaultProviders = tt.defaultProviders
			got := telemetry.telemetryFilters(tt.proxy, tt.class, tt.protocol)
			res := map[string]string{}
			http, ok := got.([]*hcm.HttpFilter)
			if ok {
				for _, f := range http {
					if strings.HasSuffix(f.GetTypedConfig().GetTypeUrl(), "/stats.PluginConfig") {
						w := &stats.PluginConfig{}
						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfgJSON, _ := protomarshal.MarshalProtoNames(w)
						res[f.GetName()] = string(cfgJSON)
					} else {
						w := &httpwasm.Wasm{}

						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfg := &wrappers.StringValue{}
						if err := w.GetConfig().GetConfiguration().UnmarshalTo(cfg); err != nil {
							t.Fatal(err)
						}
						if _, dupe := res[f.GetName()]; dupe {
							t.Fatalf("duplicate filter found: %v", f.GetName())
						}
						res[f.GetName()] = cfg.GetValue()
					}
				}
			}
			tcp, ok := got.([]*listener.Filter)
			if ok {
				for _, f := range tcp {
					if strings.HasSuffix(f.GetTypedConfig().GetTypeUrl(), "/stats.PluginConfig") {
						w := &stats.PluginConfig{}
						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfgJSON, _ := protomarshal.MarshalProtoNames(w)
						res[f.GetName()] = string(cfgJSON)
					} else {
						w := &wasmfilter.Wasm{}

						if err := f.GetTypedConfig().UnmarshalTo(w); err != nil {
							t.Fatal(err)
						}
						cfg := &wrappers.StringValue{}
						if err := w.GetConfig().GetConfiguration().UnmarshalTo(cfg); err != nil {
							t.Fatal(err)
						}
						if _, dupe := res[f.GetName()]; dupe {
							t.Fatalf("duplicate filter found: %v", f.GetName())
						}
						res[f.GetName()] = cfg.GetValue()
					}
				}
			}
			if diff := cmp.Diff(res, tt.want); diff != "" {
				t.Errorf("got diff: %v", diff)
			}
		})
	}
}

func TestGetInterval(t *testing.T) {
	cases := []struct {
		name              string
		input, defaultVal time.Duration
		expected          *durationpb.Duration
	}{
		{
			name:       "return nil",
			input:      0,
			defaultVal: 0,
			expected:   nil,
		},
		{
			name:       "return input",
			input:      1 * time.Second,
			defaultVal: 0,
			expected:   durationpb.New(1 * time.Second),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := getInterval(tc.input, tc.defaultVal)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
