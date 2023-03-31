//go:build integ
// +build integ

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

// While most of this suite is a copy of the Gateway API-focused tests in
// tests/integration/pilot/ingress_test.go, it performs these tests with
// maistra multi-tenancy enabled and adds a test case for namespace-selectors,
// which are not supported in maistra. Usage of namespace selectors in a
// Gateway resource will be ignored and interpreted like the default case,
// ie only Routes from the same namespace will be taken into account for
// that listener.

package v1beta1

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/tests/integration/servicemesh/gateway-api/common"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var (
	i    istio.Instance
	apps = deployment.SingleNamespaceView{}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireMaxClusters(1).
		Setup(maistra.ApplyServiceMeshCRDs).
		Setup(maistra.CleanupGatewayAPICRDs()).
		Setup(maistra.ApplyGatewayAPICRDs("v1alpha2")).
		Setup(istio.Setup(&i, nil)).
		Setup(deployment.SetupSingleNamespace(&apps)).
		Setup(maistra.Install(maistra.InstallationOptions{EnableGatewayAPI: true})).
		Run()
}

func TestGatewayV1Alpha2(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			common.TestGatewayAPI(t, "v1alpha2", &apps)
		})
}
