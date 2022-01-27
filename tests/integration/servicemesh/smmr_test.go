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

package servicemesh

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
)

var i istio.Instance

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(ApplyServiceMeshCRDs).
		Setup(istio.Setup(&i, nil)).
		Run()
}

func TestSMMR(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			cluster := ctx.Clusters().Default()
			gatewayNamespace := CreateNamespace(ctx, cluster, "gateway")
			httpbinNamespace := CreateNamespace(ctx, cluster, "httpbin")
			sleepNamespace := CreateNamespace(ctx, cluster, "sleep")
			configureMemberRollNameInIstiod(ctx, cluster)
			createServiceMeshMemberRoll(ctx, cluster, gatewayNamespace, httpbinNamespace)
			applyGateway(ctx, gatewayNamespace)
			applyVirtualService(ctx, httpbinNamespace, gatewayNamespace, "httpbin")
			applyVirtualService(ctx, sleepNamespace, gatewayNamespace, "sleep")
			time.Sleep(5 * time.Second)
			checkIfIngressHasConfiguredRouteWithVirtualHost(ctx, cluster, 1, "httpbin")
			checkIfIngressHasConfiguredRouteWithoutVirtualHost(ctx, cluster, "sleep")
			addNamespaceToServiceMeshMemberRoll(ctx, cluster, sleepNamespace)
			time.Sleep(5 * time.Second)
			checkIfIngressHasConfiguredRouteWithVirtualHost(ctx, cluster, 2, "httpbin")
			checkIfIngressHasConfiguredRouteWithVirtualHost(ctx, cluster, 2, "sleep")
		})
}
