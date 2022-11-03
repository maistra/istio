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

package ior

import (
	"testing"

	"istio.io/istio/pkg/config/labels"
)

func TestGetRouteName(t *testing.T) {
	testCases := []struct {
		name                  string
		controlPlaneNamespace string
		gatewayNamespace      string
		gatewayName           string
		host                  string
		expectedRouteName     string
	}{
		{
			name:                  "standard control plane namespace + standard gateway namespace + standard gateway name",
			controlPlaneNamespace: "istio-system",
			gatewayNamespace:      "bookinfo",
			gatewayName:           "bookinfo-gateway",
			host:                  "*",
			expectedRouteName:     "bookinfo-bookinfo-gateway-684888c0ebb17f37",
		},
		{
			name:                  "standard control plane namespace + long gateway namespace + standard gateway name",
			controlPlaneNamespace: "istio-system",
			gatewayNamespace:      "bookinfo-servicemesh-prod",
			gatewayName:           "bookinfo-gateway",
			host:                  "maistra.io",
			expectedRouteName:     "bookinfo-servicemesh-prod-bookinfo-gateway-788df67",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			routeName := getRouteName(tc.gatewayNamespace, tc.gatewayName, tc.host, tc.controlPlaneNamespace)
			if !labels.IsDNS1123Label(routeName) {
				t.Fatalf("Not a valid RFC 1123 label. Current length of %s: %d (should be <= %d)",
					routeName, len(routeName), labels.DNS1123LabelMaxLength)
			}
			if routeName != tc.expectedRouteName {
				t.Fatalf("%s not equals to the expected resource name %s",
					routeName, tc.expectedRouteName)
			}
		})
	}
}
