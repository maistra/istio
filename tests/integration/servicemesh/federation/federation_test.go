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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var i istio.Instance

// GetIstioInstance gets Istio instance.
func GetIstioInstance() *istio.Instance {
	return &i
}

func TestMain(m *testing.M) {
	// Ignoring staticcheck linter. RequireMinClusters is now deprecated, but we need at least 2 clusters for this
	// test to make it work.
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMinClusters(2).
		Setup(maistra.ApplyServiceMeshCRDs).
		Setup(istio.Setup(GetIstioInstance(), setupConfig)).
		Run()
}

func TestFederation(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			CreateServiceMeshPeersOrFail(ctx)
			primary := ctx.Clusters().GetByName("primary")
			secondary := ctx.Clusters().GetByName("cross-network-primary")
			bookinfoNs := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "bookinfo",
				Inject: true,
			})
			SetupExportsAndImportsOrFail(ctx, bookinfoNs.Name())
			InstallBookinfo(ctx, primary, bookinfoNs.Name())
			InstallSleep(ctx, secondary, bookinfoNs.Name())
			checkConnectivity(ctx, secondary, bookinfoNs.Name())
		})
}
