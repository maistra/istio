//go:build integ
// +build integ

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

package ambient

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
)

// GatewayConformanceInputs defines inputs to the gateway conformance test.
// The upstream build requires using `testing.T` types, which we cannot pass using our framework.
// To workaround this, we set up the inputs it TestMain.
type GatewayConformanceInputs struct {
	Client  kube.CLIClient
	Cleanup bool
}

var gatewayConformanceInputs GatewayConformanceInputs

// defined in sigs.k8s.io/gateway-api/conformance/base/manifests.yaml
var conformanceNamespaces = []string{
	"gateway-conformance-infra",
	"gateway-conformance-app-backend",
	"gateway-conformance-web-backend",
	"gateway-conformance-mesh",
}

var skippedTests = map[string]string{}

func TestGatewayConformance(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient.gateway").
		Run(func(ctx framework.TestContext) {
			// Precreate the GatewayConformance namespaces, and apply the Image Pull Secret to them.
			if ctx.Settings().Image.PullSecret != "" {
				for _, ns := range conformanceNamespaces {
					namespace.Claim(ctx, namespace.Config{
						Prefix: ns,
						Inject: false,
					})
				}
			}

			mapper, _ := gatewayConformanceInputs.Client.UtilFactory().ToRESTMapper()
			rc, _ := gatewayConformanceInputs.Client.UtilFactory().RESTClient()
			c, err := client.New(gatewayConformanceInputs.Client.RESTConfig(), client.Options{
				Scheme: kube.IstioScheme,
				Mapper: mapper,
			})
			if err != nil {
				t.Fatal(err)
			}

			opts := suite.Options{
				Client:               c,
				RestConfig:           gatewayConformanceInputs.Client.RESTConfig(),
				RESTClient:           rc,
				GatewayClassName:     "istio",
				Debug:                scopes.Framework.DebugEnabled(),
				CleanupBaseResources: gatewayConformanceInputs.Cleanup,
				SupportedFeatures:    suite.AllFeatures,
				NamespaceLabels: map[string]string{
					constants.DataplaneMode: "ambient",
				},
			}

			ctx.Cleanup(func() {
				if !ctx.Failed() {
					return
				}
				if ctx.Settings().CIMode {
					for _, ns := range conformanceNamespaces {
						namespace.Dump(ctx, ns)
					}
				}
			})
			csuite := suite.New(opts)
			csuite.Setup(t)

			// remove the dataplane mode label from the gateway-conformance-infra namespace
			// so that the ingress gateway doesn't get captured
			ns, err := namespace.Claim(ctx, namespace.Config{
				Prefix: "gateway-conformance-infra",
				Inject: false,
			})
			if err != nil {
				t.Fatal(err)
			}
			ns.RemoveLabel(constants.DataplaneMode)

			// create a waypoint for mesh conformance
			setupWaypoints(ctx, namespace.Static("gateway-conformance-mesh"), "default")

			for _, ct := range tests.ConformanceTests {
				t.Run(ct.ShortName, func(t *testing.T) {
					if reason, f := skippedTests[ct.ShortName]; f {
						t.Skip(reason)
					}
					ct.Run(t, csuite)
				})
			}
		})
}
