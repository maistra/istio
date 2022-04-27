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

package smmr

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	maistrav1 "maistra.io/api/client/versioned/typed/core/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/servicemesh"
)

const gatewayRouteName = "http.8080"

func EnableMultiTenancy(ctx resource.Context) error {
	if err := servicemesh.ApplyServiceMeshCRDs(ctx); err != nil {
		return err
	}
	if err := servicemesh.ApplyGatewayAPICRDs(ctx); err != nil {
		return err
	}
	scopes.Framework.Info("Patching istiod deployment...")
	for _, cluster := range ctx.Clusters() {
		istiod, err := waitForIstiod(cluster, 0)
		if err != nil {
			return err
		}
		if err := configureNamespaces(cluster, "istio-system"); err != nil {
			return err
		}
		scopes.Framework.Info("Patching ClusterRole and ClusterRoleBindings...")
		if err := cluster.ApplyYAMLFiles("", filepath.Join(env.IstioSrc, "tests/integration/servicemesh/smmr/testdata/clusterrole.yaml")); err != nil {
			scopes.Framework.Errorf("failed to patch: %v", err)
		}
		if err := patchIstiodArgs(cluster); err != nil {
			return err
		}
		if _, err := waitForIstiod(cluster, istiod.Generation); err != nil {
			return err
		}
	}
	return nil
}

func waitForIstiod(c cluster.Cluster, lastSeenGeneration int64) (*v1.Deployment, error) {
	var istiod *v1.Deployment
	if err := retry.UntilSuccess(func() error {
		var err error
		istiod, err = c.AppsV1().Deployments("istio-system").Get(context.TODO(), "istiod", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment istiod: %v", err)
		}
		if istiod.Status.ReadyReplicas != istiod.Status.Replicas {
			return fmt.Errorf("istiod deployment is not ready - %d of %d pods are ready", istiod.Status.ReadyReplicas, istiod.Status.Replicas)
		}
		if lastSeenGeneration != 0 && istiod.Status.ObservedGeneration == lastSeenGeneration {
			return fmt.Errorf("istiod deployment is not ready - Generation has not been updated")
		}
		return nil
	}, retry.Timeout(300*time.Second), retry.Delay(time.Second)); err != nil {
		return nil, err
	}
	return istiod, nil
}

func patchIstiodArgs(c cluster.Cluster) error {
	patch := `[
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/1",
		"value": "--memberRollName=default"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/2",
		"value": "--enableCRDScan=false"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/3",
		"value": "--disableNodeAccess=true"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/4",
		"value": "--enableIngressClassName=false"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "INJECTION_WEBHOOK_CONFIG_NAME",
			"value": ""
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/2",
		"value": {
			"name": "VALIDATION_WEBHOOK_CONFIG_NAME",
			"value": ""
		}
	}
]`

	_, err := c.AppsV1().Deployments("istio-system").
		Patch(context.TODO(), "istiod", types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch istiod deployment: %v", err)
	}
	return nil
}

func CreateServiceMeshMemberRoll(ctx framework.TestContext, c cluster.Cluster, namespaces ...string) {
	scopes.Framework.Info("Applying ServiceMeshMemberRoll...")
	memberRollYAML := `
apiVersion: maistra.io/v1
kind: ServiceMeshMemberRoll
metadata:
  name: default
spec:
  members:
`
	for _, ns := range namespaces {
		memberRollYAML += fmt.Sprintf(`  - %s
`, ns)
	}
	err := ctx.Config(c).ApplyYAML("istio-system", memberRollYAML)
	if err != nil {
		ctx.Fatalf("Failed to apply SMMR default: %v", err)
	}

	updateServiceMeshMemberRollStatus(ctx, c, append(namespaces, "istio-system")...)
}

func addNamespaceToServiceMeshMemberRoll(ctx framework.TestContext, c cluster.Cluster, namespace string) {
	client, err := maistrav1.NewForConfig(c.RESTConfig())
	if err != nil {
		ctx.Fatalf("failed to create client for maistra resources: %v", err)
	}

	smmr, err := client.ServiceMeshMemberRolls("istio-system").Get(context.TODO(), "default", metav1.GetOptions{})
	if err != nil {
		ctx.Fatalf("failed to get SMMR default: %v", err)
	}

	smmr.Spec.Members = append(smmr.Spec.Members, namespace)
	scopes.Framework.Infof("Updating SMMR spec %s with members: %v", smmr.Name, smmr.Spec.Members)
	_, err = client.ServiceMeshMemberRolls("istio-system").Update(context.TODO(), smmr, metav1.UpdateOptions{})
	if err != nil {
		ctx.Fatalf("failed to update SMMR default: %v", err)
	}

	updateServiceMeshMemberRollStatus(ctx, c, namespace)
}

func updateServiceMeshMemberRollStatus(ctx framework.TestContext, c cluster.Cluster, memberNamespaces ...string) {
	if err := configureNamespaces(c, memberNamespaces...); err != nil {
		ctx.Fatal(err)
	}

	client, err := maistrav1.NewForConfig(c.RESTConfig())
	if err != nil {
		ctx.Fatalf("failed to create client for maistra resources: %v", err)
	}

	smmr, err := client.ServiceMeshMemberRolls("istio-system").Get(context.TODO(), "default", metav1.GetOptions{})
	if err != nil {
		ctx.Fatalf("failed to get SMMR default: %v", err)
	}

	smmr.Status.ConfiguredMembers = append(smmr.Status.ConfiguredMembers, memberNamespaces...)
	scopes.Framework.Infof("Updating SMMR status %s with configured members: %v", smmr.Name, smmr.Status.ConfiguredMembers)
	_, err = client.ServiceMeshMemberRolls("istio-system").UpdateStatus(context.TODO(), smmr, metav1.UpdateOptions{})
	if err != nil {
		ctx.Fatalf("failed to update SMMR default: %v", err)
	}
}

func configureNamespaces(c cluster.Cluster, namespaces ...string) error {
	for _, ns := range namespaces {
		scopes.Framework.Infof("Applying Roles and RoleBindings to namespace %s", ns)
		if err := c.ApplyYAMLFiles(
			ns,
			filepath.Join(env.IstioSrc, "tests/integration/servicemesh/smmr/testdata/role.yaml"),
			filepath.Join(env.IstioSrc, "tests/integration/servicemesh/smmr/testdata/rolebinding.yaml")); err != nil {
			return fmt.Errorf("failed to apply Roles and RoleBindings: %v", err)
		}
	}
	return nil
}

func checkIfIngressHasConfiguredRouteWithVirtualHost(
	ctx framework.TestContext, c cluster.Cluster, expectedVirtualHostsNum int, expectedVirtualHostName string,
) {
	podName := getPodName(ctx, c, "istio-system", "istio-ingressgateway")
	routes := getRoutesFromProxy(ctx, podName, "istio-system", gatewayRouteName)
	if len(routes) != 1 {
		ctx.Fatalf("expected to find exactly 1 route '%s', got %d", gatewayRouteName, len(routes))
	}

	virtualHostsNum := len(routes[0].VirtualHosts)
	if virtualHostsNum != expectedVirtualHostsNum {
		ctx.Fatalf("expected to contain exactly %d virtual host, got %d", expectedVirtualHostsNum, virtualHostsNum)
	}

	var foundVirtualHost *VirtualHost
	for _, virtualHost := range routes[0].VirtualHosts {
		if virtualHost.Name == fmt.Sprintf("%s.maistra.io:80", expectedVirtualHostName) {
			foundVirtualHost = virtualHost
			break
		}
	}

	if foundVirtualHost == nil {
		ctx.Fatalf("expected virtual host '%s' was not found", fmt.Sprintf("%s.maistra.io:80", expectedVirtualHostName))
	}
}

func checkIfIngressHasConfiguredRouteWithoutVirtualHost(ctx framework.TestContext, c cluster.Cluster, unWantedVirtualHostName string) {
	podName := getPodName(ctx, c, "istio-system", "istio-ingressgateway")
	routes := getRoutesFromProxy(ctx, podName, "istio-system", gatewayRouteName)
	if len(routes) != 1 {
		ctx.Fatalf("expected to find exactly 1 route '%s', got %d", gatewayRouteName, len(routes))
	}

	for _, virtualHost := range routes[0].VirtualHosts {
		if virtualHost.Name == fmt.Sprintf("%s.maistra.io:80", unWantedVirtualHostName) {
			ctx.Fatalf("expected to not find virtual host '%s', but was found", virtualHost.Name)
		}
	}
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

func getPodName(ctx framework.TestContext, c cluster.Cluster, namespace, appName string) string {
	pods, err := c.PodsForSelector(context.TODO(), namespace, fmt.Sprintf("app=%s", appName))
	if err != nil {
		ctx.Fatalf("failed to get %s pod from namespace %s: %v", appName, namespace, err)
	}
	if len(pods.Items) == 0 {
		ctx.Fatalf("list of received %s pods from namespace %s is empty", appName, namespace)
	}
	return pods.Items[0].Name
}

func applyGateway(ctx framework.TestContext, c cluster.Cluster, ns string) {
	scopes.Framework.Info("Applying Gateway...")
	err := ctx.Config(c).ApplyYAML(ns, `
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
    - "httpbin.maistra.io"
    - "sleep.maistra.io"
`)
	if err != nil {
		ctx.Fatal("failed to apply Gateway: %v", err)
	}
}

func applyVirtualService(ctx framework.TestContext, c cluster.Cluster, ns, gatewayNs, virtualServiceName string) {
	scopes.Framework.Info("Applying VirtualService...")
	err := ctx.Config(c).ApplyYAML(ns, fmt.Sprintf(`
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
`, virtualServiceName, virtualServiceName, gatewayNs))
	if err != nil {
		ctx.Fatal("failed to apply VirtualService %s: %v", virtualServiceName, err)
	}
}
