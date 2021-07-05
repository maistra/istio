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

package controller

import (
	"reflect"
	"testing"
)

func TestSetSeedNamespaces(t *testing.T) {
	testNamespaces := []string{"test-5", "test-4", "test-3", "test-2", "test-1"}
	smmrc := &serviceMeshMemberRollController{
		namespace: "istio-system",
	}

	copyTestNamespaces := append(testNamespaces[:0:0], testNamespaces...)
	smmrc.setSeedNamespaces(testNamespaces)

	if !reflect.DeepEqual(testNamespaces, copyTestNamespaces) {
		t.Error("Input seed namespace slice has been modified")
	}
}
