//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package api

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
	"istio.io/istio/tests/integration/telemetry/common"
)

const (
	imageName      = "istio-testing/wasm/header-injector"
	injectedHeader = "x-resp-injection"
	wasmConfigFile = "testdata/wasm-filter.yaml"
)

type wasmTestConfigs struct {
	desc            string
	name            string
	policy          string
	tag             string
	upstreamVersion string
	expectedVersion string
	testHostname    string
}

var generation = 0

func mapTagToVersionOrFail(t framework.TestContext, tag, version string) {
	t.Helper()
	if err := registry.SetupTagMap(map[string]string{
		imageName + ":" + tag: version,
	}); err != nil {
		t.Fatalf("failed to setup the tag map: %v", err)
	}
}

func applyAndTestWasmWithOCI(ctx framework.TestContext, c wasmTestConfigs) {
	applyAndTestCustomWasmConfigWithOCI(ctx, c, wasmConfigFile)
}

func applyAndTestCustomWasmConfigWithOCI(ctx framework.TestContext, c wasmTestConfigs, path string) {
	ctx.NewSubTest("OCI_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			generation++
		}()
		mapTagToVersionOrFail(t, c.tag, c.upstreamVersion)
		wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), imageName, c.tag)
		if err := installWasmExtension(t, c.name, wasmModuleURL, c.policy, fmt.Sprintf("g-%d", generation), path); err != nil {
			t.Fatalf("failed to install WasmPlugin: %v", err)
		}
		if c.testHostname != "" {
			sendTrafficToHostname(t, check.ResponseHeader(injectedHeader, c.expectedVersion), c.testHostname)
		} else {
			sendTraffic(t, check.ResponseHeader(injectedHeader, c.expectedVersion))
		}
	})
}

func resetWasm(ctx framework.TestContext, pluginName string) {
	ctx.NewSubTest("Delete WasmPlugin " + pluginName).Run(func(t framework.TestContext) {
		if err := uninstallWasmExtension(t, pluginName, wasmConfigFile); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, ""), retry.Converge(2))
	})
}

func resetCustomWasmConfig(ctx framework.TestContext, pluginName, path string) {
	ctx.NewSubTest("Delete WasmPlugin " + pluginName).Run(func(t framework.TestContext) {
		if err := uninstallWasmExtension(t, pluginName, path); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, ""), retry.Converge(2))
	})
}

func TestImagePullPolicy(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.image-pull-policy").
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "initial creation with latest",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module")
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present and policy is IfNotPresent, so should not pull",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present. But policy is default and tag is latest, so pull the image",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
			resetWasm(t, "wasm-test-module")

			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "initial creation with 0.0.1",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
		})
}

func applyWasmConfig(ctx framework.TestContext, ns string, args map[string]any, path string) error {
	return ctx.ConfigIstio().EvalFile(ns, args, path).Apply()
}

func installWasmExtension(ctx framework.TestContext, pluginName, wasmModuleURL, imagePullPolicy, pluginVersion, path string) error {
	args := map[string]any{
		"WasmPluginName":    pluginName,
		"TestWasmModuleURL": wasmModuleURL,
		"WasmPluginVersion": pluginVersion,
		"TargetAppName":     common.GetTarget().(echo.Instances).NamespacedName().Name,
		"TargetGatewayName": common.GetTarget().(echo.Instances).ServiceName() + "-gateway",
	}

	if len(imagePullPolicy) != 0 {
		args["ImagePullPolicy"] = imagePullPolicy
	}

	if err := applyWasmConfig(ctx, common.GetAppNamespace().Name(), args, path); err != nil {
		return err
	}

	return nil
}

func uninstallWasmExtension(ctx framework.TestContext, pluginName, path string) error {
	args := map[string]any{
		"WasmPluginName": pluginName,
	}
	if err := ctx.ConfigIstio().EvalFile(common.GetAppNamespace().Name(), args, path).Delete(); err != nil {
		return err
	}
	return nil
}

func sendTraffic(ctx framework.TestContext, checker echo.Checker, options ...retry.Option) {
	ctx.Helper()
	if len(common.GetClientInstances()) == 0 {
		ctx.Fatal("there is no client")
	}
	cltInstance := common.GetClientInstances()[0]

	defaultOptions := []retry.Option{retry.Delay(100 * time.Millisecond), retry.Timeout(200 * time.Second)}
	httpOpts := echo.CallOptions{
		To: common.GetTarget(),
		Port: echo.Port{
			Name: "http",
		},
		HTTP: echo.HTTP{
			Path:   "/path",
			Method: "GET",
		},
		Count: 1,
		Retry: echo.Retry{
			Options: append(defaultOptions, options...),
		},
		Check: checker,
	}

	_ = cltInstance.CallOrFail(ctx, httpOpts)
}

func sendTrafficToHostname(ctx framework.TestContext, checker echo.Checker, hostname string, options ...retry.Option) {
	ctx.Helper()
	if len(common.GetClientInstances()) == 0 {
		ctx.Fatal("there is no client")
	}
	cltInstance := common.GetClientInstances()[0]

	defaultOptions := []retry.Option{retry.Delay(100 * time.Millisecond), retry.Timeout(200 * time.Second)}
	httpOpts := echo.CallOptions{
		Address: hostname,
		Port: echo.Port{
			Name:        "http",
			ServicePort: 80,
			Protocol:    protocol.HTTP,
		},
		HTTP: echo.HTTP{
			Path:    "/path",
			Method:  "GET",
			Headers: headers.New().WithHost(fmt.Sprintf("%s.com", common.GetTarget().ServiceName())).Build(),
		},
		Count: 1,
		Retry: echo.Retry{
			Options: append(defaultOptions, options...),
		},
		Check: checker,
	}

	_ = cltInstance.CallOrFail(ctx, httpOpts)
}

func applyAndTestWasmWithHTTP(ctx framework.TestContext, c wasmTestConfigs) {
	applyAndTestCustomWasmConfigWithHTTP(ctx, c, wasmConfigFile)
}

func applyAndTestCustomWasmConfigWithHTTP(ctx framework.TestContext, c wasmTestConfigs, path string) {
	ctx.NewSubTest("HTTP_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			generation++
		}()
		mapTagToVersionOrFail(t, c.tag, c.upstreamVersion)
		// registry-redirector will redirect to the gzipped tarball of the first layer with this request.
		// The gzipped tarball should have a wasm module.
		wasmModuleURL := fmt.Sprintf("http://%v/layer/v1/%v:%v", registry.Address(), imageName, c.tag)
		t.Logf("Trying to get a wasm file from %v", wasmModuleURL)
		if err := installWasmExtension(t, c.name, wasmModuleURL, c.policy, fmt.Sprintf("g-%d", generation), path); err != nil {
			t.Fatalf("failed to install WasmPlugin: %v", err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, c.expectedVersion))
	})
}

// TestTargetRef vs workloadSelector for gateways
func TestGatewaySelection(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(t)
			args := map[string]any{
				"To": common.GetTarget().(echo.Instances),
			}
			t.ConfigIstio().EvalFile(common.GetAppNamespace().Name(), args, "testdata/gateway-api.yaml").ApplyOrFail(t)
			applyAndTestCustomWasmConfigWithOCI(t, wasmTestConfigs{
				desc:            "initial creation with latest for a gateway",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
				testHostname:    fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", common.GetTarget().ServiceName(), common.GetAppNamespace().Name()),
			}, "testdata/gateway-wasm-filter.yaml")

			resetCustomWasmConfig(t, "wasm-test-module", "testdata/gateway-wasm-filter.yaml")
		})
}

// TestImagePullPolicyWithHTTP tests pulling Wasm Binary via HTTP and ImagePullPolicy.
func TestImagePullPolicyWithHTTP(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.image-pull-policy").
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "initial creation with 0.0.1",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-http")
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-http")
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
		})
}

// TestBadWasmRemoteLoad tests that bad Wasm remote load configuration won't affect service.
// The test will set up an echo client and server, test echo ping works fine. Then apply a
// Wasm filter which has a bad URL link, which will result as module download failure. After that,
// verifies that echo ping could still work. The test also verifies that metrics are properly
// recorded for module downloading failure and nack on ECDS update.
func TestBadWasmRemoteLoad(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			badWasmTestHelper(t, "testdata/bad-filter.yaml", false, true)
		})
}

// TestBadWasmWithFailOpen is basically the same with TestBadWasmRemoteLoad except
// it tests with "fail_open = true". To test the fail_open, the target pod is restarted
// after applying the Wasm filter.
// At this moment, there is no "fail_open" option in WasmPlugin API. So, we test it using
// EnvoyFilter. When WasmPlugin has a "fail_open" option in the API plane, we need to change
// this test to use the WasmPlugin API
func TestBadWasmWithFailOpen(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			// since this case is for "fail_open=true", ecds is not rejected.
			badWasmTestHelper(t, "testdata/bad-wasm-envoy-filter-fail-open.yaml", true, false)
		})
}

func badWasmTestHelper(t framework.TestContext, filterConfigPath string, restartTarget bool, ecdsShouldReject bool) {
	t.Helper()
	// Test bad wasm remote load in only one cluster.
	// There is no need to repeat the same testing logic in multiple clusters.
	to := match.Cluster(t.Clusters().Default()).FirstOrFail(t, common.GetClientInstances())
	// Verify that echo server could return 200
	common.SendTrafficOrFail(t, to)
	t.Log("echo server returns OK, apply bad wasm remote load filter.")

	// Apply bad filter config
	t.Logf("use config in %s.", filterConfigPath)
	t.ConfigIstio().File(common.GetAppNamespace().Name(), filterConfigPath).ApplyOrFail(t)
	if restartTarget {
		target := match.Cluster(t.Clusters().Default()).FirstOrFail(t, common.GetTarget().Instances())
		if err := target.Restart(); err != nil {
			t.Fatalf("failed to restart the target pod: %v", err)
		}
	}

	// Wait until there is agent metrics for wasm download failure
	retry.UntilSuccessOrFail(t, func() error {
		q := prometheus.Query{Metric: "istio_agent_wasm_remote_fetch_count", Labels: map[string]string{"result": "download_failure"}}
		c := to.Config().Cluster
		if _, err := util.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
			util.PromDiff(t, common.GetPromInstance(), c, q)
			return err
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

	if ecdsShouldReject && t.Clusters().Default().IsPrimary() { // Only check istiod if running locally (i.e., not an external control plane)
		// Verify that istiod has a stats about rejected ECDS update
		// pilot_total_xds_rejects{type="type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"}
		retry.UntilSuccessOrFail(t, func() error {
			q := prometheus.Query{Metric: "pilot_total_xds_rejects", Labels: map[string]string{"type": "ecds"}}
			c := to.Config().Cluster
			if _, err := util.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
				util.PromDiff(t, common.GetPromInstance(), c, q)
				return err
			}
			return nil
		}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
	}

	t.Log("got istio_agent_wasm_remote_fetch_count metric in prometheus, bad wasm filter is applied, send request to echo server again.")

	// Verify that echo server could still return 200
	common.SendTrafficOrFail(t, to)

	t.Log("echo server still returns ok after bad wasm filter is applied.")
}
