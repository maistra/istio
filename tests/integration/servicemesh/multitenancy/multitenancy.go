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
	"fmt"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func createServiceMeshMemberRoll(ctx framework.TestContext, c cluster.Cluster, namespace string) {
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
		scopes.Framework.Errorf("Failed to apply ServiceMeshMemberRoll: %v", err)
		ctx.Fatal(err)
	}
}

func simulateIstioOperator(ctx framework.TestContext, c cluster.Cluster) {
	scopes.Framework.Info("Patching istio deployment...")
	waitForIstiod(ctx, c)
	patchIstiodArgs(ctx, c)
	restartIstiod(ctx)
	waitForIstiod(ctx, c)
}

func waitForIstiod(ctx framework.TestContext, c cluster.Cluster) {
	if err := retry.UntilSuccess(func() error {
		istiod, err := c.AppsV1().
			Deployments("istio-system").
			Get(context.TODO(), "istiod", v1.GetOptions{})
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
	_, err := c.AppsV1().
		Deployments("istio-system").
		Patch(context.TODO(), "istiod", types.JSONPatchType, []byte(patch), v1.PatchOptions{})
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

func verifyIstioProxyConfig(ctx framework.TestContext, c cluster.Cluster, namespace string) {
	// TODO:
	// 1. Check if istio-proxies in bookinfo namespace have configured routes
	// istioctl proxy-config routes ratings.bookinfo --name 9080 -o json
	// 2. Check if istio-proxy in sleep namespace has no configured routes
	// istioctl proxy-config routes sleep.sleep --name 9080 -o json
}
