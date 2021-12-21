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

package servicemesh

import (
	"context"
	"encoding/json"
	"fmt"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	maistrav1 "maistra.io/api/client/versioned/typed/core/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
)

func applyServiceMeshMemberRoll(ctx framework.TestContext, c cluster.Cluster, namespace string) {
	scopes.Framework.Info("Applying ServiceMeshMemberRoll...")
	err := ctx.Config(c).ApplyYAML("istio-system", fmt.Sprintf(`
apiVersion: maistra.io/v1
kind: ServiceMeshMemberRoll
metadata:
  name: default
  namespace: istio-system
spec:
  members:
    - %s
`, namespace))
	if err != nil {
		ctx.Fatal("Failed to apply ServiceMeshMemberRoll: %v", err)
	}
}

func configureMemberRollNameInIstiod(ctx framework.TestContext, c cluster.Cluster) {
	scopes.Framework.Info("Patching istio deployment...")
	waitForIstiod(ctx, c)
	patchIstiodArgs(ctx, c)
	restartIstiod(ctx)
	waitForIstiod(ctx, c)
}

func waitForIstiod(ctx framework.TestContext, c cluster.Cluster) {
	if err := retry.UntilSuccess(func() error {
		istiod, err := c.AppsV1().Deployments("istio-system").Get(context.TODO(), "istiod", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment istiod: %v", err)
		}
		if istiod.Status.ReadyReplicas != istiod.Status.Replicas {
			return fmt.Errorf("istiod deployment is not ready - %d of %d pods are ready", istiod.Status.ReadyReplicas, istiod.Status.Replicas)
		}
		return nil
	}, retry.Timeout(300*time.Second), retry.Delay(time.Second)); err != nil {
		ctx.Fatal(err)
	}
}

func patchIstiodArgs(ctx framework.TestContext, c cluster.Cluster) {
	patch := `[{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/0",
		"value": "--memberRollName=default"
	}]`
	_, err := c.AppsV1().Deployments("istio-system").
		Patch(context.TODO(), "istiod", types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		ctx.Fatalf("Failed to patch istiod deployment: %v", err)
	}
}

func restartIstiod(ctx framework.TestContext) {
	out, err := shell.Execute(true, "kubectl rollout restart deployment istiod -n istio-system")
	if err != nil {
		ctx.Fatalf("Failed to restart istiod: output: %s; error: %v", out, err)
	}
}

func updateServiceMeshMemberRollStatus(ctx framework.TestContext, c cluster.Cluster, memberNamespace string) {
	client, err := maistrav1.NewForConfig(c.RESTConfig())
	if err != nil {
		ctx.Fatalf("Failed to create client for maistra resources: %v", err)
	}

	smmr, err := client.ServiceMeshMemberRolls("istio-system").Get(context.TODO(), "default", metav1.GetOptions{})
	if err != nil {
		ctx.Fatalf("Failed to get SMMR default: %v", err)
	}
	scopes.Framework.Infof("SMMR %s, configured members: %v", smmr.Name, smmr.Status.ConfiguredMembers)

	smmr.Status.ConfiguredMembers = append(smmr.Status.ConfiguredMembers, memberNamespace)
	scopes.Framework.Infof("Updating SMMR %s with configured members: %v", smmr.Name, smmr.Status.ConfiguredMembers)
	_, err = client.ServiceMeshMemberRolls("istio-system").UpdateStatus(context.TODO(), smmr, metav1.UpdateOptions{})
	if err != nil {
		ctx.Fatalf("Failed to update SMMR default: %v", err)
	}
}

func httpbinShouldContainItsRouteConfiguration(ctx framework.TestContext, c cluster.Cluster, namespace string) {
	podName := getPodName(ctx, c, namespace, "httpbin")

	istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	stdout, stderr, err := istioCtl.Invoke([]string{
		"proxy-config", "routes", fmt.Sprintf("%s.%s", podName, namespace), "--name", "8000", "-o", "json",
	})
	if err != nil || stderr != "" {
		ctx.Fatalf("Failed to execute istioctl proxy-config: %s: %v", stderr, err)
	}
	fmt.Printf(stdout)

	routes := make([]*route.RouteConfiguration, 0)
	if err := json.Unmarshal([]byte(stdout), &routes); err != nil {
		ctx.Fatalf("Failed to unmarshall routes: %v", err)
	}
	if len(routes) != 1 {
		ctx.Fatalf("Expected exactly 1 route, got %d", len(routes))
	}
}

func httpbinShouldNotContainRouteConfiguration(ctx framework.TestContext, c cluster.Cluster, namespace, routePort string) {
	podName := getPodName(ctx, c, namespace, "httpbin")

	istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	stdout, stderr, err := istioCtl.Invoke([]string{
		"proxy-config", "routes", fmt.Sprintf("%s.%s", podName, namespace), "--name", routePort, "-o", "json",
	})
	if err != nil || stderr != "" {
		ctx.Fatalf("Failed to execute istioctl proxy-config: %s: %v", stderr, err)
	}
	fmt.Printf(stdout)

	routes := make([]*route.RouteConfiguration, 0)
	if err := json.Unmarshal([]byte(stdout), &routes); err != nil {
		ctx.Fatalf("Failed to unmarshall routes: %v", err)
	}
	if len(routes) != 0 {
		ctx.Fatalf("Expected no routes, got %d", len(routes))
	}
}

func getPodName(ctx framework.TestContext, c cluster.Cluster, namespace, appName string) string {
	pods, err := c.PodsForSelector(context.TODO(), namespace, fmt.Sprintf("app=%s", appName))
	if err != nil {
		ctx.Fatalf("Failed to get %s pod from namespace %s: %v", appName, namespace, err)
	}
	if len(pods.Items) == 0 {
		ctx.Fatalf("List of received %s pods from namespace %s is empty", appName, namespace)
	}
	return pods.Items[0].Name
}

func applyFakeVirtualService(ctx framework.TestContext, c cluster.Cluster, namespace string, routePort string) {
	scopes.Framework.Info("Applying VirtualService...")
	err := ctx.Config(c).ApplyYAML(namespace, fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: fake-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: fake-virtual-service
spec:
  hosts:
  - "*"
  gateways:
  - fake-gateway
  http:
  - route:
    - destination:
        host: localhost
        port:
          number: %s
`, routePort))
	if err != nil {
		ctx.Fatal("Failed to apply VirtualService: %v", err)
	}
}
