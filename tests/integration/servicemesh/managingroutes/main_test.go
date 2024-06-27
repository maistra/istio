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
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	routeapiv1 "github.com/openshift/api/route/v1"
	routeversioned "github.com/openshift/client-go/route/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/servicemesh/maistra"
	"istio.io/istio/tests/integration/servicemesh/router"
)

var (
	istioNamespace namespace.Instance

	gatewayTmpl    = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/managingroutes/testdata/gateway.tmpl.yaml")
	virtualSvcTmpl = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/managingroutes/testdata/virtual-service.tmpl.yaml")
)

const DefaultGatewayCount = 1

func TestMain(m *testing.M) {
	// do not change order of setup functions
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMaxClusters(1).
		Setup(router.InstallOpenShiftRouter).
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
			gatewayName := "default-gateway"

			namespaceGateway := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "gateway", Inject: true}).Name()
			namespaceA := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "a", Inject: true}).Name()
			namespaceB := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "b", Inject: true}).Name()
			namespaceC := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "c", Inject: true}).Name()
			applyVirtualServiceOrFail(ctx, namespaceA, namespaceGateway, gatewayName, "a")
			applyVirtualServiceOrFail(ctx, namespaceB, namespaceGateway, gatewayName, "b")
			applyVirtualServiceOrFail(ctx, namespaceC, namespaceGateway, gatewayName, "c")

			if err := maistra.ApplyServiceMeshMemberRoll(ctx, istioNamespace, namespaceGateway, namespaceA, namespaceB, namespaceC); err != nil {
				ctx.Fatalf("failed to add member to ServiceMeshMemberRoll: %s", err)
			}

			if err := maistra.EnableIOR(ctx, istioNamespace); err != nil {
				ctx.Error("failed to enable IOR: %s", err)
			}

			ctx.Cleanup(func() {
				if err := maistra.DisableIOR(ctx, istioNamespace); err != nil {
					ctx.Fatalf("failed to disable IOR: %s", err)
				}
			})

			ctx.NewSubTest("RouteCreation").Run(func(t framework.TestContext) {
				labelSetA := map[string]string{
					"a": "a",
				}
				t.Cleanup(func() {
					deleteGatewayOrFail(t, namespaceGateway, gatewayName, labelSetA, "a", "b")
					ensureRoutesCleared(t)
				})
				applyGatewayOrFail(t, namespaceGateway, gatewayName, labelSetA, "a", "b")

				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "a.maistra.io")
				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "b.maistra.io")
			})

			ctx.NewSubTest("RouteChange").Run(func(t framework.TestContext) {
				labelSetA := map[string]string{
					"a": "a",
				}
				t.Cleanup(func() {
					deleteGatewayOrFail(t, namespaceGateway, gatewayName, labelSetA, "a", "b")
					ensureRoutesCleared(t)
				})
				applyGatewayOrFail(t, namespaceGateway, gatewayName, labelSetA, "a", "b", "c")

				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "a.maistra.io")
				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "b.maistra.io")
				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "c.maistra.io")

				applyGatewayOrFail(t, namespaceGateway, gatewayName, labelSetA, "a", "b")

				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "a.maistra.io")
				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "b.maistra.io")
				verifyThatRouteIsMissingOrFail(t, namespaceGateway, gatewayName, "c.maistra.io")
			})

			ctx.NewSubTest("RouteLabelUpdate").Run(func(t framework.TestContext) {
				labelSetA := map[string]string{
					"a": "a",
				}
				labelSetB := map[string]string{
					"b": "b",
				}
				t.Cleanup(func() {
					deleteGatewayOrFail(t, namespaceGateway, gatewayName, labelSetB, "a")
					ensureRoutesCleared(t)
				})

				applyGatewayOrFail(t, namespaceGateway, gatewayName, labelSetA, "a")
				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "a.maistra.io")

				applyGatewayOrFail(t, namespaceGateway, gatewayName, labelSetB, "a")
				verifyThatRouteExistsOrFail(t, namespaceGateway, gatewayName, "a.maistra.io")

				routeClient := t.AllClusters().Default().Route()
				retry.UntilSuccessOrFail(t, func() error {
					route, err := findRoute(routeClient, namespaceGateway, gatewayName, "a.maistra.io")
					if err != nil || route == nil {
						return fmt.Errorf("failed to find route: %s", err)
					}

					labels := route.GetObjectMeta().GetLabels()
					val, ok := labels["b"]

					if ok && val == "b" {
						return nil
					}

					return fmt.Errorf("expected to find 'b' in the labels, but got %s", labels)
				}, retry.BackoffDelay(500*time.Millisecond))
			})
		})
}

func verifyThatRouteExistsOrFail(ctx framework.TestContext, gatewayNamespace, gatewayName, host string) {
	routeClient := ctx.AllClusters().Default().Route()

	retry.UntilSuccessOrFail(ctx, func() error {
		route, err := findRoute(routeClient, gatewayNamespace, gatewayName, host)
		if err != nil {
			return fmt.Errorf("failed to get Routes: %v", err)
		}

		if route == nil {
			return fmt.Errorf("no Route found")
		}

		return nil
	}, retry.BackoffDelay(500*time.Millisecond))
}

func verifyThatRouteIsMissingOrFail(ctx framework.TestContext, gatewayNamespace, gatewayName, host string) {
	routeClient := ctx.AllClusters().Default().Route()

	retry.UntilSuccessOrFail(ctx, func() error {
		route, err := findRoute(routeClient, gatewayNamespace, gatewayName, host)
		if err != nil {
			return fmt.Errorf("failed to get Routes: %v", err)
		}

		if route != nil {
			return fmt.Errorf("found unexpected Route")
		}

		return nil
	}, retry.BackoffDelay(500*time.Millisecond))
}

func findRoute(routeClient routeversioned.Interface, gatewayNamespace, gatewayName, host string) (*routeapiv1.Route, error) {
	routes, err := routeClient.RouteV1().Routes(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Routes: %s", err)
	}

	if len(routes.Items) != 0 {
		for _, route := range routes.Items {
			if route.Spec.Host == host && strings.HasPrefix(route.Name, fmt.Sprintf("%s-%s-", gatewayNamespace, gatewayName)) {
				return &route, nil
			}
		}
	}

	return nil, nil
}

func ensureRoutesCleared(ctx framework.TestContext) {
	routeClient := ctx.AllClusters().Default().Route()

	retry.UntilSuccessOrFail(ctx, func() error {
		routes, err := routeClient.RouteV1().Routes(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{LabelSelector: "maistra.io/generated-by=ior"})
		if err != nil {
			return fmt.Errorf("failed to get Routes: %s", err)
		}

		if count := len(routes.Items); count != DefaultGatewayCount {
			return fmt.Errorf("found unexpected routes %d", count)
		}

		return nil
	}, retry.BackoffDelay(500*time.Millisecond))
}

type RouteConfig struct {
	Name         string         `json:"name"`
	VirtualHosts []*VirtualHost `json:"virtualHosts"`
}

type VirtualHost struct {
	Name string `json:"name"`
}

func applyGatewayOrFail(ctx framework.TestContext, ns, name string, labels map[string]string, hosts ...string) {
	// retry because of flaky validation webhook
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().EvalFile(ns, map[string]interface{}{"hosts": hosts, "name": name, "labels": labels}, gatewayTmpl).Apply()
	}, retry.Timeout(15*time.Second))
}

func deleteGatewayOrFail(ctx framework.TestContext, ns, name string, labels map[string]string, hosts ...string) {
	// retry because of flaky validation webhook
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().EvalFile(ns, map[string]interface{}{"hosts": hosts, "name": name, "labels": labels}, gatewayTmpl).Delete()
	}, retry.Timeout(15*time.Second))
}

func applyVirtualServiceOrFail(ctx framework.TestContext, ns, gatewayNs, gatewayName, virtualServiceName string) {
	values := map[string]string{
		"name":        virtualServiceName,
		"gatewayNs":   gatewayNs,
		"gatewayName": gatewayName,
	}
	retry.UntilSuccessOrFail(ctx, func() error {
		return ctx.ConfigIstio().EvalFile(ns, values, virtualSvcTmpl).Apply()
	}, retry.Timeout(15*time.Second))
}
