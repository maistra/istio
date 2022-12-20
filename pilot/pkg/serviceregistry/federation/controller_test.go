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

	v1 "maistra.io/api/federation/v1"
)

// TestMergeLocality tests the federation mergeLocality function
func TestMergeLocality(t *testing.T) {
	remoteLocality := v1.ImportedServiceLocality{
		Region:  "region1",
		Zone:    "zone1",
		Subzone: "subzone1",
	}
	merged := mergeLocality(&remoteLocality, nil)
	if merged.Region != "region1" {
		t.Fatalf("Federation controller should initialize importConfig locality region. Actual %v, expected %v",
			merged.Region, "region1")
	}
	if merged.Zone != "zone1" {
		t.Fatalf("Federation controller should initialize importConfig locality zone. Actual %v, expected %v",
			merged.Zone, "zone1")
	}
	if merged.Subzone != "subzone1" {
		t.Fatalf("Federation controller should initialize importConfig locality subzone Actual %v, expected %v",
			merged.Subzone, "subzone1")
	}

	updatedLocality := v1.ImportedServiceLocality{
		Region:  "region2",
		Zone:    "zone2",
		Subzone: "subzone2",
	}
	merged = mergeLocality(&updatedLocality, &remoteLocality)
	if merged.Region != "region2" {
		t.Fatalf("Federation controller should update importConfig locality region. Actual %v, expected %v",
			merged.Region, "region2")
	}
	if merged.Zone != "zone2" {
		t.Fatalf("Federation controller should update importConfig locality zone. Actual %v, expected %v",
			merged.Zone, "zone2")
	}
	if merged.Subzone != "subzone2" {
		t.Fatalf("Federation controller should update importConfig locality subzone Actual %v, expected %v",
			merged.Subzone, "subzone2")
	}
}
