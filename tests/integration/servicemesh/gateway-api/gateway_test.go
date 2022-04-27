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

// While most of this suite is a copy of the Gateway API-focused tests in
// tests/integration/pilot/ingress_test.go, it performs these tests with
// maistra multi-tenancy enabled and adds a test case for namespace-selectors,
// which are not supported in maistra. Usage of namespace selectors in a
// Gateway resource will be ignored and interpreted like the default case,
// ie only Routes from the same namespace will be taken into account for
// that listener.

package smmr

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
	"istio.io/istio/tests/integration/servicemesh"
	"istio.io/istio/tests/integration/servicemesh/smmr"
)

var (
	i    istio.Instance
	apps = &common.EchoDeployments{}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(servicemesh.ApplyServiceMeshCRDs).
		Setup(istio.Setup(&i, nil)).
		Setup(func(t resource.Context) error {
			return common.SetupApps(t, i, apps)
		}).
		Setup(smmr.EnableMultiTenancy).
		Run()
}

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			secondaryNamespace := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "secondary",
				Inject: true,
			})
			cluster := t.Clusters().Default()
			smmr.CreateServiceMeshMemberRoll(t, cluster, apps.Namespace.Name())
			ingressutil.CreateIngressKubeSecretInNamespace(t, "test-gateway-cert-same", ingressutil.TLS, ingressutil.IngressCredentialA,
				false, apps.Namespace.Name(), t.Clusters().Configs()...)
			ingressutil.CreateIngressKubeSecretInNamespace(t, "test-gateway-cert-cross", ingressutil.TLS, ingressutil.IngressCredentialB,
				false, apps.Namespace.Name(), t.Clusters().Configs()...)

			retry.UntilSuccessOrFail(t, func() error {
				err := t.Config().ApplyYAML("", fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: GatewayClass
metadata:
  name: istio
spec:
  controllerName: istio.io/gateway-controller
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: gateway
  namespace: istio-system
spec:
  addresses:
  - value: istio-ingressgateway
    type: Hostname
  gatewayClassName: istio
  listeners:
  - name: http
    hostname: "*.domain.example"
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: All
  - name: http-secondary
    hostname: "secondary.namespace"
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        selector:
          matchLabels:
            test: test
  - name: tcp
    port: 31400
    protocol: TCP
    allowedRoutes:
      namespaces:
        from: All
  - name: tls-cross
    hostname: cross-namespace.domain.example
    port: 443
    protocol: HTTPS
    allowedRoutes:
      namespaces:
        from: All
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: test-gateway-cert-cross
        namespace: "%s"
  - name: tls-same
    hostname: same-namespace.domain.example
    port: 443
    protocol: HTTPS
    allowedRoutes:
      namespaces:
        from: All
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: test-gateway-cert-same
---`, apps.Namespace.Name()))
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			retry.UntilSuccessOrFail(t, func() error {
				err := t.Config().ApplyYAML(apps.Namespace.Name(), `
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: http
spec:
  hostnames: ["my.domain.example"]
  parentRefs:
  - name: gateway
    namespace: istio-system
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /get/
    backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: tcp
spec:
  parentRefs:
  - name: gateway
    namespace: istio-system
  rules:
  - backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: b
spec:
  parentRefs:
  - kind: Mesh
    name: istio
  - name: gateway
    namespace: istio-system
  hostnames: ["b"]
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /path
    filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
        - name: my-added-header
          value: added-value
    backendRefs:
    - name: b
      port: 80
`)
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			retry.UntilSuccessOrFail(t, func() error {
				err := t.Config().ApplyYAML(secondaryNamespace.Name(), fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: http
spec:
  hostnames: ["secondary.namespace"]
  parentRefs:
  - name: gateway
    namespace: istio-system
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /get/
    backendRefs:
    - name: b
      namespace: %s
      port: 80
`, apps.Namespace.Name()))
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			for _, ingr := range apps.Ingresses {
				t.NewSubTest(ingr.Cluster().StableName()).Run(func(t framework.TestContext) {
					t.NewSubTest("http").Run(func(t framework.TestContext) {
						paths := []string{"/get", "/get/", "/get/prefix"}
						for _, path := range paths {
							_ = apps.Ingress.CallWithRetryOrFail(t, echo.CallOptions{
								Port: &echo.Port{
									Protocol: protocol.HTTP,
								},
								Path: path,
								Headers: map[string][]string{
									"Host": {"my.domain.example"},
								},
							})
						}
					})
					t.NewSubTest("http-othernamespace").Run(func(t framework.TestContext) {
						paths := []string{"/get", "/get/", "/get/prefix"}
						for _, path := range paths {
							resp, err := apps.Ingress.CallWithRetry(
								echo.CallOptions{
									Port: &echo.Port{
										Protocol: protocol.HTTP,
									},
									Path: path,
									Headers: map[string][]string{
										"Host": {"secondary.namespace"},
									},
								},
								retry.Timeout(time.Second*5),
							)
							if err == nil && resp.CheckCode("404") != nil {
								t.Fatal(fmt.Errorf("expected error but request succeeded:\n%v", resp))
							}
						}
					})
					t.NewSubTest("tcp").Run(func(t framework.TestContext) {
						host, port := apps.Ingress.TCPAddress()
						_ = apps.Ingress.CallWithRetryOrFail(t, echo.CallOptions{
							Port: &echo.Port{
								Protocol:    protocol.HTTP,
								ServicePort: port,
							},
							Address: host,
							Path:    "/",
							Headers: map[string][]string{
								"Host": {"my.domain.example"},
							},
						})
					})
					t.NewSubTest("mesh").Run(func(t framework.TestContext) {
						_ = apps.PodA[0].CallWithRetryOrFail(t, echo.CallOptions{
							Target:    apps.PodB[0],
							PortName:  "http",
							Path:      "/path",
							Validator: echo.And(echo.ExpectOK(), echo.ExpectKey("My-Added-Header", "added-value")),
						})
					})
					t.NewSubTest("status").Run(func(t framework.TestContext) {
						retry.UntilSuccessOrFail(t, func() error {
							gw, err := t.Clusters().Kube().Default().GatewayAPI().GatewayV1alpha2().Gateways("istio-system").Get(
								context.Background(), "gateway", metav1.GetOptions{})
							if err != nil {
								return err
							}
							if s := kstatus.GetCondition(gw.Status.Conditions, string(k8s.GatewayConditionReady)).Status; s != metav1.ConditionTrue {
								return fmt.Errorf("expected status %q, got %q", metav1.ConditionTrue, s)
							}
							return nil
						})
					})
				})
				t.NewSubTest("managed").Run(func(t framework.TestContext) {
					t.Config().ApplyYAMLOrFail(t, apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: http
spec:
  parentRefs:
  - name: gateway
  rules:
  - backendRefs:
    - name: b
      port: 80
`)
					apps.PodB[0].CallWithRetryOrFail(t, echo.CallOptions{
						Port:   &echo.Port{ServicePort: 80},
						Scheme: scheme.HTTP,
						Headers: map[string][]string{
							"Host": {"bar.example.com"},
						},
						Address:   fmt.Sprintf("gateway.%s.svc.cluster.local", apps.Namespace.Name()),
						Validator: echo.ExpectOK(),
					}, retry.Timeout(time.Minute))
				})
			}
		})
}
