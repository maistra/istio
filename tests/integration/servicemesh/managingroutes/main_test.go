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

package managingroutes

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var (
	istioNamespace namespace.Instance

	gatewayTmpl    = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/managingroutes/testdata/gateway.tmpl.yaml")
	virtualSvcTmpl = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/managingroutes/testdata/virtual-service.tmpl.yaml")
)

func TestMain(m *testing.M) {
	// do not change order of setup functions
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMaxClusters(1).
		Setup(maistra.ApplyServiceMeshCRDs).
		Setup(namespace.Setup(&istioNamespace, namespace.Config{Prefix: "istio-system"})).
		Setup(maistra.Install(namespace.Future(&istioNamespace), nil)).
		// We cannot apply restricted RBAC before the control plane installation, because the operator always applies
		// the default RBAC, so we have to remove it and apply after the installation.
		Setup(maistra.RemoveDefaultRBAC).
		Setup(maistra.ApplyRestrictedRBAC(namespace.Future(&istioNamespace))).
		// We cannot disable webhooks in maistra.Install(), because then we would need maistra/istio-operator
		// to properly patch CA bundles in the webhooks. To avoid that problem we restart Istio with disabled webhooks
		// and without roles for managing webhooks once they are already created and patched.
		Setup(maistra.DisableWebhooksAndRestart(namespace.Future(&istioNamespace))).
		Run()
}

func TestManagingGateways(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			namespaceGateway := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "gateway", Inject: true}).Name()
			namespaceA := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "a", Inject: true}).Name()
			namespaceB := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "b", Inject: true}).Name()
			applyGatewayOrFail(ctx, namespaceGateway, "a", "b")
			applyVirtualServiceOrFail(ctx, namespaceA, namespaceGateway, "a")
			applyVirtualServiceOrFail(ctx, namespaceB, namespaceGateway, "b")

			if err := maistra.ApplyServiceMeshMemberRoll(ctx, istioNamespace, namespaceGateway, namespaceA); err != nil {
				ctx.Fatalf("failed to create ServiceMeshMemberRoll: %s", err)
			}
			verifyThatIngressHasVirtualHostForMember(ctx, istioNamespace.Name(), "a")

			if err := maistra.ApplyServiceMeshMemberRoll(ctx, istioNamespace, namespaceGateway, namespaceA, namespaceB); err != nil {
				ctx.Fatalf("failed to add member to ServiceMeshMemberRoll: %s", err)
			}
			verifyThatIngressHasVirtualHostForMember(ctx, istioNamespace.Name(), "a", "b")

			if err := maistra.ApplyServiceMeshMemberRoll(ctx, istioNamespace, namespaceGateway, namespaceB); err != nil {
				ctx.Fatalf("failed to create ServiceMeshMemberRoll: %s", err)
			}
			verifyThatIngressHasVirtualHostForMember(ctx, istioNamespace.Name(), "b")
		})
}

func verifyThatIngressHasVirtualHostForMember(ctx framework.TestContext, istioNamespace string, expectedMembers ...string) {
	expectedGatewayRouteName := "http.8080"
	expectedVirtualHostsNum := len(expectedMembers)

	retry.UntilSuccessOrFail(ctx, func() error {
		podName, err := getPodName(ctx, istioNamespace, "istio-ingressgateway")
		if err != nil {
			return err
		}
		routes, err := getRoutesFromProxy(ctx, podName, istioNamespace, expectedGatewayRouteName)
		if err != nil {
			return fmt.Errorf("failed to get routes from proxy %s: %s", podName, err)
		}
		if len(routes) != 1 {
			return fmt.Errorf("expected to find exactly 1 route '%s', got %d", expectedGatewayRouteName, len(routes))
		}

		virtualHostsNum := len(routes[0].VirtualHosts)
		if virtualHostsNum != expectedVirtualHostsNum {
			return fmt.Errorf("expected to find exactly %d virtual hosts, got %d", expectedVirtualHostsNum, virtualHostsNum)
		}

	CheckExpectedMembersLoop:
		for _, member := range expectedMembers {
			expectedVirtualHostName := fmt.Sprintf("%s.maistra.io:80", member)
			for _, virtualHost := range routes[0].VirtualHosts {
				if virtualHost.Name == expectedVirtualHostName {
					continue CheckExpectedMembersLoop
				}
			}
			return fmt.Errorf("expected virtual host '%s' was not found", expectedVirtualHostName)
		}
		return nil
	}, retry.Timeout(10*time.Second))
}

type RouteConfig struct {
	Name         string         `json:"name"`
	VirtualHosts []*VirtualHost `json:"virtualHosts"`
}

type VirtualHost struct {
	Name string `json:"name"`
}

func getRoutesFromProxy(ctx framework.TestContext, pod, namespace, routeName string) ([]*RouteConfig, error) {
	istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	stdout, stderr, err := istioCtl.Invoke([]string{
		"proxy-config", "routes", fmt.Sprintf("%s.%s", pod, namespace), "--name", routeName, "-o", "json",
	})
	if err != nil || stderr != "" {
		return nil, fmt.Errorf("failed to execute command 'istioctl proxy-config': %s: %s", stderr, err)
	}

	routes := make([]*RouteConfig, 0)
	if err := json.Unmarshal([]byte(stdout), &routes); err != nil {
		return nil, fmt.Errorf("failed to unmarshall routes: %s", err)
	}

	return routes, nil
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
	// retry because of flaky validation webhook
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().EvalFile(ns, map[string][]string{"hosts": hosts}, gatewayTmpl).Apply()
	}, retry.Timeout(3*time.Second))
}

func applyVirtualServiceOrFail(ctx framework.TestContext, ns, gatewayNs, virtualServiceName string) {
	values := map[string]string{
		"name":      virtualServiceName,
		"gatewayNs": gatewayNs,
	}
	// retry because of flaky validation webhook
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().EvalFile(ns, values, virtualSvcTmpl).Apply()
	}, retry.Timeout(3*time.Second))
}
