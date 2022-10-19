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

package common

import (
	"testing"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

func TestFormatResourceName(t *testing.T) {
	testCases := []struct {
		name                 string
		prefix               string
		mesh                 cluster.ID
		source               federationmodel.ServiceKey
		expectedResourceName string
	}{
		{
			name:   "standard mesh name + standard namespace + standard service name",
			prefix: "fed-exp",
			mesh:   "mesh",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo",
				Name:      "productpage",
			},
			expectedResourceName: "fed-exp-productpage-bookinfo-mesh",
		},
		{
			name:   "long mesh name",
			prefix: "fed-imp",
			mesh:   "mesh-production",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo",
				Name:      "productpage",
			},
			expectedResourceName: "fed-imp-productpage-bookinfo-mesh-production",
		},
		{
			name:   "long namespace",
			prefix: "fed-exp",
			mesh:   "mesh",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo-production",
				Name:      "productpage",
			},
			expectedResourceName: "fed-exp-productpage-bookinfo-production-mesh",
		},
		{
			name:   "long mesh name + long namespace + long service name",
			prefix: "fed-imp",
			mesh:   "test-remote",
			source: federationmodel.ServiceKey{
				Namespace: "bookinfo-production",
				Name:      "productpage-rest-private",
			},
			expectedResourceName: "fed-imp-productpage-rest-private-bookinfo-production-te160043b8",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resourceName := FormatResourceName(tc.prefix, tc.mesh.String(),
				tc.source.Namespace, tc.source.Name)
			if !labels.IsDNS1123Label(resourceName) {
				t.Fatalf("Not a valid RFC 1123 label. Current length of %s: %d (should be <= %d)",
					resourceName, len(resourceName), labels.DNS1123LabelMaxLength)
			}
			if resourceName != tc.expectedResourceName {
				t.Fatalf("%s not equals to the expected resource name %s",
					resourceName, tc.expectedResourceName)
			}
		})
	}
}
