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
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var i istio.Instance

func TestMain(m *testing.M) {
	// do not change order of setup functions
	framework.
		NewSuite(m).
		RequireMaxClusters(1).
		Setup(maistra.ApplyServiceMeshCRDs).
		Setup(istio.Setup(&i, nil)).
		Setup(maistra.Install).
		Run()
}
