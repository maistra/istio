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

package federation

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/tests/integration/servicemesh"
)

var i istio.Instance

// GetIstioInstance gets Istio instance.
func GetIstioInstance() *istio.Instance {
	return &i
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireMinClusters(2).
		Setup(servicemesh.ApplyServiceMeshCRDs).
		Setup(istio.Setup(GetIstioInstance(), setupConfig)).
		Run()
}

func TestFederation(t *testing.T) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Run(func(ctx framework.TestContext) {
			err := CreateServiceMeshPeers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			primary := ctx.Clusters().GetByName("primary")
			secondary := ctx.Clusters().GetByName("cross-network-primary")
			primaryNamespace := servicemesh.CreateNamespace(ctx, primary, "bookinfo")
			secondaryNamespace := servicemesh.CreateNamespace(ctx, secondary, "bookinfo")
			SetupExportsAndImports(ctx, primaryNamespace)
			servicemesh.InstallBookinfo(ctx, primary, primaryNamespace)
			servicemesh.InstallSleep(ctx, secondary, secondaryNamespace)
			checkConnectivity(ctx, secondary, secondaryNamespace)
		})
}
