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

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

func TestCreateResourceName(t *testing.T) {
	testCases := []struct {
		name                 string
		mesh                 cluster.ID
		source               federationmodel.ServiceKey
		expectedResourceName string
	}{
		{
			name: "standard mesh name + standard namespace + standard service name",
			mesh: "mesh",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo",
				Name:      "productpage",
			},
			expectedResourceName: "fed-imp-productpage-bookinfo-mesh",
		},
		{
			name: "long mesh name",
			mesh: "myveryveryverylongmesh",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo",
				Name:      "productpage",
			},
			expectedResourceName: "fed-imp-productpage-bookinfo-myveryveryverylongmesh",
		},
		{
			name: "long namespace",
			mesh: "mesh",
			source: federationmodel.ServiceKey{
				Namespace: "bigbigbigbigbookinfo",
				Name:      "productpage",
			},
			expectedResourceName: "fed-imp-productpage-bigbigbigbigbookinfo-mesh",
		},
		{
			name: "long service name",
			mesh: "mesh",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo",
				Name:      "productpage-rest-private",
			},
			expectedResourceName: "fed-imp-productpage-rest-private-bookinfo-mesh",
		},
		{
			name: "long mesh name + long namespace + long service name => truncated + hashed",
			mesh: "test-remote",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo-production",
				Name:      "productpage-rest-private",
			},
			expectedResourceName: "fed-imp-productpage-rest-private-bookinfo-production-te160043b8",
		},
	}
	for _, tc := range testCases {
		resourceName := createResourceName(tc.mesh, tc.source)
		if len(resourceName) > labels.DNS1123LabelMaxLength {
			t.Fatalf("%s\n%s length: %d, should be < 63", tc.name,
				resourceName, len(resourceName))
		}
		if resourceName != tc.expectedResourceName {
			t.Fatalf("%s\n%s not equals to the expected resource name %s", tc.name,
				resourceName, tc.expectedResourceName)
		}
	}
}
