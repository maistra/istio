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
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/telemetry/common"
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		// TODO: Remove this restriction once we validate our prometheus helm chart version is high enough
		Label(label.IPv4). // https://github.com/istio/istio/issues/35915
		Setup(istio.Setup(common.GetIstioInstance(), setupConfig)).
		Setup(func(ctx resource.Context) error {
			i, err := istio.Get(ctx)
			if err != nil {
				return err
			}
			return ctx.ConfigIstio().YAML(i.Settings().SystemNamespace, `
apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: mesh-default
spec:
  metrics:
  - providers:
    - name: prometheus
`).Apply()
		}).
		Setup(common.TestSetup).
		Setup(testRegistrySetup).
		Run()
}

func setupConfig(c resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["meshConfig.accessLogFile"] = "" // disabled accesslog, will enabled by TelemetryAPI later
	cfg.Values["telemetry.v2.enabled"] = "false"
}
