//go:build integ
// +build integ

//
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

package router

import (
	"context"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
)

func InstallOpenShiftRouter(ctx resource.Context) error {
	c := ctx.Clusters().Default()
	openshiftIngressNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-ingress",
		},
	}
	if _, err := c.Kube().CoreV1().Namespaces().Create(context.Background(), openshiftIngressNs, metav1.CreateOptions{}); err != nil {
		return err
	}
	if err := c.ApplyYAMLFiles("", filepath.Join(env.IstioSrc, "tests/integration/servicemesh/router/testdata/route_crd.yaml")); err != nil {
		return err
	}
	if err := c.ApplyYAMLFiles("", filepath.Join(env.IstioSrc, "tests/integration/servicemesh/router/testdata/router.yaml")); err != nil {
		return err
	}
	if err := c.ApplyYAMLFiles("", filepath.Join(env.IstioSrc, "tests/integration/servicemesh/router/testdata/router_rbac.yaml")); err != nil {
		return err
	}
	return nil
}
