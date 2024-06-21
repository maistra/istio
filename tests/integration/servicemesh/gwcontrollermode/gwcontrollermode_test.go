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

package gwcontrollermode

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	iGatewayController, i istio.Instance

	appNs     namespace.Instance
	ingressNs namespace.Instance
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Setup(namespace.Setup(&appNs, namespace.Config{Prefix: "app"})).
		Setup(namespace.Setup(&ingressNs, namespace.Config{Prefix: "gateway-controller"})).
		Setup(istio.Setup(&i, setupMeshInstance)).
		Setup(istio.Setup(&iGatewayController, setupGatewayControllerInstance)).
		Run()
}

func VirtualServiceTest(t framework.TestContext) {
	waitForIstiodToSettle(t, "istio-system")
	waitForIstiodToSettle(t, ingressNs.Name())
	t.ConfigIstio().YAML(appNs.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: traffic-weights
spec:
  hosts:
    - c
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25
`).ApplyOrFail(t)
	retry.UntilSuccessOrFail(t, func() error {
		return expectVirtualServiceStatus(t, "traffic-weights", appNs, true)
	}, retry.Timeout(time.Second*5))

	if hasLogged(t, ingressNs.Name(), "traffic-weights") {
		t.Fatal("gateway-controller instance reconciled the VirtualService")
	}
	if !hasLogged(t, "istio-system", "traffic-weights") {
		t.Fatal("mesh instance did not reconcile the VirtualService")
	}
}

func KubernetesServiceTest(t framework.TestContext) {
	waitForIstiodToSettle(t, "istio-system")
	waitForIstiodToSettle(t, ingressNs.Name())
	t.ConfigIstio().YAML(appNs.Name(), `
apiVersion: v1
kind: Service
metadata:
  name: hello-service
spec:
  ports:
  - port: 80
    name: tcp
    protocol: TCP
  type: NodePort
`).ApplyOrFail(t)
	waitForIstiodToSettle(t, "istio-system")
	waitForIstiodToSettle(t, ingressNs.Name())
	if !hasLogged(t, ingressNs.Name(), "hello-service") {
		t.Fatal("gateway-controller instance did not reconcile the Service")
	}
	if !hasLogged(t, "istio-system", "hello-service") {
		t.Fatal("mesh instance did not reconcile the Service")
	}
}

func TestGatewayControllerMode(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("vs").Run(func(t framework.TestContext) {
				VirtualServiceTest(t)
			})
			t.NewSubTest("svc").Run(func(t framework.TestContext) {
				KubernetesServiceTest(t)
			})
		})
}

func setupMeshInstance(ctx resource.Context, cfg *istio.Config) {
	cfg.ControlPlaneValues = `
values:
  global:
    istiod:
      enableAnalysis: true
  pilot:
    env:
      PILOT_ENABLE_STATUS: true
      PILOT_ENABLE_CONFIG_DISTRIBUTION_TRACKING: true
`
}

func setupGatewayControllerInstance(ctx resource.Context, cfg *istio.Config) {
	cfg.OperatorOptions = map[string]string{
		"revision": "gateway-controller",
	}
	cfg.ControlPlaneValues = fmt.Sprintf(`
values:
  meshConfig:
    discoverySelectors:
    - matchLabels:
        test: test
  global:
    istioNamespace: %s
    caCertConfigMapName: ossm-ca-root-cert
    revision: gateway-controller
  pilot:
    env:
      PILOT_GATEWAY_API_DEFAULT_GATEWAYCLASS: openshift
      PILOT_GATEWAY_API_CONTROLLER_NAME: openshift.io/gateway-controller
      PILOT_ENABLE_GATEWAY_CONTROLLER_MODE: true
`, ingressNs.Name())
}

func hasLogged(t framework.TestContext, namespace string, keyword string) bool {
	kclient := t.AllClusters()[0].(kube.CLIClient)
	istioPods, err := kclient.GetIstioPods(t.Context(), namespace, v1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to retrieve istiod pods: %v", err)
	}
	for _, p := range istioPods {
		if strings.HasPrefix(p.Name, "istiod") {
			log, err := kclient.PodLogs(t.Context(), p.Name, p.Namespace, "discovery", false)
			if err != nil {
				t.Fatalf("failed to retrieve pod logs: %v", err)
			}
			if strings.Contains(log, keyword) {
				return true
			}
		}
	}
	return false
}

func expectVirtualServiceStatus(t framework.TestContext, name string, ns namespace.Instance, hasError bool) error {
	c := t.Clusters().Default()

	x, err := c.Istio().NetworkingV1alpha3().VirtualServices(ns.Name()).Get(context.TODO(), name, v1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected test failure: can't get virtualservice: %v", err)
	}

	status := &x.Status

	if hasError {
		if len(status.ValidationMessages) < 1 {
			return fmt.Errorf("expected validation messages to exist, but got nothing")
		}
		found := false
		for _, validation := range status.ValidationMessages {
			if validation.Type.Code == msg.ReferencedResourceNotFound.Code() {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("expected error %v to exist", msg.ReferencedResourceNotFound.Code())
		}
	} else if status.ValidationMessages != nil && len(status.ValidationMessages) > 0 {
		return fmt.Errorf("expected no validation messages, but got %d", len(status.ValidationMessages))
	}

	if len(status.Conditions) < 1 {
		return fmt.Errorf("expected conditions to exist, but got nothing")
	}
	found := false
	for _, condition := range status.Conditions {
		if condition.Type == "Reconciled" {
			found = true
			if condition.Status != "True" {
				return fmt.Errorf("expected Reconciled to be true but was %v", condition.Status)
			}
		}
	}
	if !found {
		return fmt.Errorf("expected Reconciled condition to exist, but got %v", status.Conditions)
	}
	return nil
}

func waitForIstiodToSettle(t framework.TestContext, namespace string) {
	kclient := t.AllClusters()[0].(kube.CLIClient)
	istioPods, err := kclient.GetIstioPods(t.Context(), namespace, v1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to retrieve istiod pods: %v", err)
	}
	for _, p := range istioPods {
		if strings.HasPrefix(p.Name, "istiod") {
			lastLength := 0
			stableAttempts := 0
			retry.UntilOrFail(t, func() bool {
				log, err := kclient.PodLogs(t.Context(), p.Name, p.Namespace, "discovery", false)
				if err != nil {
					t.Fatalf("failed to retrieve pod logs: %v", err)
				}
				currentLength := len(log)
				if lastLength == currentLength {
					stableAttempts++
				} else {
					lastLength = currentLength
					stableAttempts = 0
				}
				if stableAttempts >= 5 {
					return true
				}
				return false
			}, retry.Timeout(5*time.Second), retry.Delay(100*time.Millisecond))
		}
	}
}
