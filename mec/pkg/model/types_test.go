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

package model

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestStringToImageRef(t *testing.T) {
	testCases := []struct {
		input  string
		output *ImageRef
	}{
		{
			input: "registry.io/corp/repo:latest",
			output: &ImageRef{
				Repository: "repo",
				Hub:        "registry.io/corp",
				Tag:        "latest",
			},
		},
		{
			input: "registry.io/corp/repo@sha256:41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
			output: &ImageRef{
				Repository: "repo",
				Hub:        "registry.io/corp",
				SHA256:     "41af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
			},
		},
		{
			input:  "registry.io/corp/repo@sha256:af286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
			output: nil,
		},
		{
			input:  "registry.io/corp/repo@sha256:invaliddc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
			output: nil,
		},
		{
			input:  "registry.io/corp/repo@sha256:41@f286dc0b172ed2f1ca934fd2278de4a1192302ffa07087cea2682e7d372e3",
			output: nil,
		},
		{
			input: "registry.io:8080/corp/repo:latest",
			output: &ImageRef{
				Repository: "repo",
				Hub:        "registry.io:8080/corp",
				Tag:        "latest",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := StringToImageRef(tc.input)
			if !cmp.Equal(result, tc.output) {
				t.Errorf("result not as expected, -got +want:\n%s", cmp.Diff(result, tc.output))
			}
			if result != nil && result.String() != tc.input {
				t.Errorf("ImageRef.String() conversion failed, got: %s want: %s", result.String(), tc.input)
			}
		})
	}
}
