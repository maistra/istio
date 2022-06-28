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

package servicemesh

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

func TestSMMR(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			namespaceGateway := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "gateway", Inject: true}).Name()
			namespaceA := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "a", Inject: true}).Name()
			namespaceB := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "b", Inject: true}).Name()
			applyGatewayOrFail(ctx, namespaceGateway, "a", "b")
			applyVirtualServiceOrFail(ctx, namespaceA, namespaceGateway, "a")
			applyVirtualServiceOrFail(ctx, namespaceB, namespaceGateway, "b")

			if err := maistra.CreateServiceMeshMemberRoll(ctx, namespaceGateway, namespaceA); err != nil {
				ctx.Fatalf("failed to create ServiceMeshMemberRoll: %s", err)
			}
			verifyThatIngressHasVirtualHostForMember(ctx, "a", 1)

			if err := maistra.AddMemberToServiceMesh(ctx, namespaceB); err != nil {
				ctx.Fatalf("failed to add member to ServiceMeshMemberRoll: %s", err)
			}
			verifyThatIngressHasVirtualHostForMember(ctx, "a", 2)
			verifyThatIngressHasVirtualHostForMember(ctx, "b", 2)
		})
}

func verifyThatIngressHasVirtualHostForMember(ctx framework.TestContext, expectedMemberName string, expectedVirtualHostsNum int) {
	expectedGatewayRouteName := "http.8080"
	expectedVirtualHostName := fmt.Sprintf("%s.maistra.io:80", expectedMemberName)

	retry.UntilSuccessOrFail(ctx, func() error {
		podName, err := getPodName(ctx, "istio-system", "istio-ingressgateway")
		if err != nil {
			return err
		}
		routes := getRoutesFromProxy(ctx, podName, "istio-system", expectedGatewayRouteName)
		if len(routes) != 1 {
			return fmt.Errorf("expected to find exactly 1 route '%s', got %d", expectedGatewayRouteName, len(routes))
		}

		virtualHostsNum := len(routes[0].VirtualHosts)
		if virtualHostsNum != expectedVirtualHostsNum {
			// TODO: log virtual host names
			return fmt.Errorf("expected to find exactly %d virtual hosts, got %d", expectedVirtualHostsNum, virtualHostsNum)
		}

		for _, virtualHost := range routes[0].VirtualHosts {
			if virtualHost.Name == expectedVirtualHostName {
				return nil
			}
		}
		return fmt.Errorf("expected virtual host '%s' was not found", expectedVirtualHostName)
	}, retry.Timeout(10*time.Second))
}

type RouteConfig struct {
	Name         string         `json:"name"`
	VirtualHosts []*VirtualHost `json:"virtualHosts"`
}

type VirtualHost struct {
	Name string `json:"name"`
}

func getRoutesFromProxy(ctx framework.TestContext, pod, namespace, routeName string) []*RouteConfig {
	istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	stdout, stderr, err := istioCtl.Invoke([]string{
		"proxy-config", "routes", fmt.Sprintf("%s.%s", pod, namespace), "--name", routeName, "-o", "json",
	})
	if err != nil || stderr != "" {
		ctx.Fatalf("failed to execute command 'istioctl proxy-config': %s: %v", stderr, err)
	}

	routes := make([]*RouteConfig, 0)
	if err := json.Unmarshal([]byte(stdout), &routes); err != nil {
		ctx.Fatalf("failed to unmarshall routes: %v", err)
	}

	return routes
}

func getPodName(ctx framework.TestContext, namespace, appName string) (string, error) {
	pods, err := ctx.Clusters().Default().PodsForSelector(context.TODO(), namespace, fmt.Sprintf("app=%s", appName))
	if err != nil {
		return "", fmt.Errorf("failed to get %s pod from namespace %s: %v", appName, namespace, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("list of received %s pods from namespace %s is empty", appName, namespace)
	}
	return pods.Items[0].Name, nil
}

func applyGatewayOrFail(ctx framework.TestContext, ns string, hosts ...string) {
	gwYAML := `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: common-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
`
	for _, host := range hosts {
		gwYAML += fmt.Sprintf("    - %s.maistra.io\n", host)
	}
	// retry because of flaky validation webhook
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().YAML(ns, gwYAML).Apply()
	}, retry.Timeout(3*time.Second))
}

func applyVirtualServiceOrFail(ctx framework.TestContext, ns, gatewayNs, virtualServiceName string) {
	vsYAML := fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: %s
spec:
  hosts:
  - "%s.maistra.io"
  gateways:
  - %s/common-gateway
  http:
  - route:
    - destination:
        host: localhost
        port:
          number: 8080
`, virtualServiceName, virtualServiceName, gatewayNs)
	// retry because of flaky validation webhook
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().YAML(ns, vsYAML).Apply()
	}, retry.Timeout(3*time.Second))
}
