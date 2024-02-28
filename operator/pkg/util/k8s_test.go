// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"sigs.k8s.io/yaml"

	pkgAPI "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	o1 = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      pilotCertProvider: kubernetes
`
	o2 = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      pilotCertProvider: istiod
`
	o3 = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
`
)

func TestValidateIOPCAConfig(t *testing.T) {
	var err error

	tests := []struct {
		major        string
		minor        string
		expErr       bool
		operatorYaml string
	}{
		{
			major:        "1",
			minor:        "16",
			expErr:       false,
			operatorYaml: o1,
		},
		{
			major:        "1",
			minor:        "22",
			expErr:       true,
			operatorYaml: o1,
		},
		{
			major:        "1",
			minor:        "23",
			expErr:       false,
			operatorYaml: o2,
		},
		{
			major:        "1",
			minor:        "24",
			expErr:       false,
			operatorYaml: o3,
		},
	}

	for i, tt := range tests {
		k8sClient := kube.NewFakeClient()
		k8sClient.Kube().Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
			Major: tt.major,
			Minor: tt.minor,
		}
		op := &pkgAPI.IstioOperator{}
		err = yaml.Unmarshal([]byte(tt.operatorYaml), op)
		if err != nil {
			t.Fatalf("Failure in test case %v. Error %s", i, err)
		}
		err = ValidateIOPCAConfig(k8sClient, op)
		if !tt.expErr && err != nil {
			t.Fatalf("Failure in test case %v. Expected No Error. Got %s", i, err)
		} else if tt.expErr && err == nil {
			t.Fatalf("Failure in test case %v. Expected Error. Got No error", i)
		}
	}
}

func TestPrometheusPathAndPort(t *testing.T) {
	cases := []struct {
		pod  *v1.Pod
		path string
		port int
	}{
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "case-1",
					Annotations: map[string]string{
						"prometheus.io/path": "/metrics",
						"prometheus.io/port": "15020",
					},
				},
			},
			path: "/metrics",
			port: 15020,
		},
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "case-2",
					Annotations: map[string]string{
						"prometheus.io.path": "/metrics",
						"prometheus.io.port": "15020",
					},
				},
			},
			path: "/metrics",
			port: 15020,
		},
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "case-3",
					Annotations: map[string]string{
						"prometheus-io/path": "/metrics",
						"prometheus-io/port": "15020",
					},
				},
			},
			path: "/metrics",
			port: 15020,
		},
	}

	for _, tc := range cases {
		t.Run(tc.pod.Name, func(t *testing.T) {
			path, port, err := PrometheusPathAndPort(tc.pod)
			assert.NoError(t, err)
			assert.Equal(t, tc.path, path)
			assert.Equal(t, tc.port, port)
		})
	}
}
