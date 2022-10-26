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

package v1alpha2

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var (
	i    istio.Instance
	apps = deployment.SingleNamespaceView{}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireMaxClusters(1).
		Setup(maistra.ApplyServiceMeshCRDs).
		Setup(maistra.ApplyGatewayAPICRDs("v1alpha2")).
		Setup(istio.Setup(&i, nil)).
		Setup(deployment.SetupSingleNamespace(&apps)).
		Setup(maistra.Install(maistra.InstallationOptions{EnableGatewayAPI: true})).
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
			memberNamespace := apps.Namespace.Name()
			if err := maistra.ApplyServiceMeshMemberRoll(t, memberNamespace); err != nil {
				t.Fatalf("failed to apply SMMR for namespace %s: %s", memberNamespace, err)
			}
			ingressutil.CreateIngressKubeSecretInNamespace(t, "test-gateway-cert-same", ingressutil.TLS, ingressutil.IngressCredentialA,
				false, apps.Namespace.Name(), t.Clusters().Configs()...)
			ingressutil.CreateIngressKubeSecretInNamespace(t, "test-gateway-cert-cross", ingressutil.TLS, ingressutil.IngressCredentialB,
				false, apps.Namespace.Name(), t.Clusters().Configs()...)
			retry.UntilSuccessOrFail(t, func() error {
				err := t.ConfigIstio().YAML("", fmt.Sprintf(`
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
---`, apps.Namespace.Name())).Apply()
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			retry.UntilSuccessOrFail(t, func() error {
				err := t.ConfigIstio().YAML(apps.Namespace.Name(), `
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
`).Apply()
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			retry.UntilSuccessOrFail(t, func() error {
				err := t.ConfigIstio().YAML(secondaryNamespace.Name(), fmt.Sprintf(`
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
`, apps.Namespace.Name())).Apply()
				return err
			}, retry.Delay(time.Second*10), retry.Timeout(time.Second*90))
			for _, ingr := range istio.IngressesOrFail(t, t) {
				t.NewSubTest(ingr.Cluster().StableName()).Run(func(t framework.TestContext) {
					t.NewSubTest("http").Run(func(t framework.TestContext) {
						paths := []string{"/get", "/get/", "/get/prefix"}
						for _, path := range paths {
							retry.UntilSuccessOrFail(t, func() error {
								_, err := ingr.Call(echo.CallOptions{
									Port: echo.Port{
										Protocol: protocol.HTTP,
									},
									HTTP: echo.HTTP{
										Path: path,
										Headers: http.Header{
											"Host": {"my.domain.example"},
										},
									},
									Check: check.OK(),
								})
								if err != nil {
									return fmt.Errorf("failed to call ingress path %s: %s", path, err)
								}
								return nil
							}, retry.Timeout(10*time.Second), retry.Delay(1*time.Second))
						}
					})
					t.NewSubTest("http-othernamespace").Run(func(t framework.TestContext) {
						paths := []string{"/get", "/get/", "/get/prefix"}
						for _, path := range paths {
							retry.UntilSuccessOrFail(t, func() error {
								_, err := ingr.Call(
									echo.CallOptions{
										Port: echo.Port{
											Protocol: protocol.HTTP,
										},
										HTTP: echo.HTTP{
											Path: path,
											Headers: http.Header{
												"Host": {"secondary.namespace"},
											},
										},
										Check: check.NoErrorAndStatus(404),
									},
								)
								if err != nil {
									return fmt.Errorf("failed to call ingress path %s: %s", path, err)
								}
								return nil
							}, retry.Timeout(10*time.Second), retry.Delay(1*time.Second))
						}
					})
					t.NewSubTest("tcp").Run(func(t framework.TestContext) {
						host, port := ingr.TCPAddress()
						retry.UntilSuccessOrFail(t, func() error {
							_, err := ingr.Call(echo.CallOptions{
								Port: echo.Port{
									Protocol:    protocol.HTTP,
									ServicePort: port,
								},
								Address: host,
								HTTP: echo.HTTP{
									Path: "/",
									Headers: http.Header{
										"Host": {"my.domain.example"},
									},
								},
							})
							if err != nil {
								return fmt.Errorf("failed to establish TCP connection with ingress route: %s", err)
							}
							return nil
						}, retry.Timeout(10*time.Second), retry.Delay(1*time.Second))
					})
					t.NewSubTest("mesh").Run(func(t framework.TestContext) {
						retry.UntilSuccessOrFail(t, func() error {
							_, err := apps.A[0].Call(echo.CallOptions{
								To: apps.B[0],
								Port: echo.Port{
									Protocol: protocol.HTTP,
								},
								HTTP: echo.HTTP{
									Path: "/path",
								},
								Check: check.And(check.OK(), check.RequestHeader("my-added-header", "added-value")),
							})
							if err != nil {
								return fmt.Errorf("failed to execute request to ingress route: %s", err)
							}
							return nil
						}, retry.Timeout(10*time.Second), retry.Delay(1*time.Second))
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
					t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
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
`).ApplyOrFail(t)
					retry.UntilSuccessOrFail(t, func() error {
						_, err := apps.B[0].Call(echo.CallOptions{
							Address: fmt.Sprintf("gateway.%s.svc.cluster.local", apps.Namespace.Name()),
							HTTP: echo.HTTP{
								Headers: http.Header{
									"Host": {"bar.example.com"},
								},
							},
							Port: echo.Port{
								ServicePort: 80,
							},
							Scheme: scheme.HTTP,
							Check:  check.OK(),
						})
						return err
					}, retry.Timeout(10*time.Second), retry.Delay(1*time.Second))
				})
			}
		})
}
