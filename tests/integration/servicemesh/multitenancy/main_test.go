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

package multitenancy

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var (
	istioNs1 namespace.Instance
	istioNs2 namespace.Instance

	appNs1 namespace.Instance
	appNs2 namespace.Instance
	appNs3 namespace.Instance

	apps    echo.Instances
	appsMux sync.Mutex

	svcEntryTmpl = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/multitenancy/testdata/service-entry.tmpl.yaml")
)

func TestMain(m *testing.M) {
	// do not change order of setup functions
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMaxClusters(1).
		Setup(maistra.ApplyServiceMeshCRDs).
		SetupParallel(
			namespace.Setup(&istioNs1, namespace.Config{Prefix: "istio-system-1"}),
			namespace.Setup(&istioNs2, namespace.Config{Prefix: "istio-system-2"})).
		SetupParallel(
			maistra.Install(namespace.Future(&istioNs1), nil),
			maistra.Install(namespace.Future(&istioNs2), nil)).
		Setup(maistra.RemoveDefaultRBAC).
		SetupParallel(
			maistra.ApplyRestrictedRBAC(namespace.Future(&istioNs1)),
			maistra.ApplyRestrictedRBAC(namespace.Future(&istioNs2))).
		SetupParallel(
			maistra.DisableWebhooksAndRestart(namespace.Future(&istioNs1)),
			maistra.DisableWebhooksAndRestart(namespace.Future(&istioNs2))).
		SetupParallel(
			namespace.Setup(&appNs1, namespace.Config{Prefix: "app-1-tenant-1"}),
			namespace.Setup(&appNs2, namespace.Config{Prefix: "app-2-tenant-1"}),
			namespace.Setup(&appNs3, namespace.Config{Prefix: "app-3-tenant-2"})).
		SetupParallel(
			maistra.DeployEchos(&apps, &appsMux, "a", namespace.Future(&appNs1), maistra.AppOpts{NoSidecar: true}),
			maistra.DeployEchos(&apps, &appsMux, "b", namespace.Future(&appNs2), maistra.AppOpts{NoSidecar: true}),
			maistra.DeployEchos(&apps, &appsMux, "c", namespace.Future(&appNs3), maistra.AppOpts{NoSidecar: true}),
		).
		Run()
}

func TestMultiTenancy(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		a := match.ServiceName(echo.NamespacedName{Name: "a", Namespace: appNs1}).GetMatches(apps).Instances()[0]
		b := match.ServiceName(echo.NamespacedName{Name: "b", Namespace: appNs2}).GetMatches(apps).Instances()[0]
		c := match.ServiceName(echo.NamespacedName{Name: "c", Namespace: appNs3}).GetMatches(apps).Instances()[0]

		ctx.NewSubTest("apps can communicate across other namespaces").Run(func(t framework.TestContext) {
			for _, to := range []echo.Instance{b, c} {
				a.CallOrFail(t, echo.CallOptions{
					To: to,
					Port: echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Check: check.OK(),
				})
			}
		})

		ctx.NewSubTest("add apps to meshes").Run(func(t framework.TestContext) {
			if err := maistra.ApplyServiceMeshMemberRoll(t, istioNs1, a.NamespaceName(), b.NamespaceName()); err != nil {
				ctx.Errorf("failed to create SMMR for namespaces: %s, %s", a.NamespaceName(), b.NamespaceName())
			}
			if err := maistra.ApplyServiceMeshMemberRoll(t, istioNs2, c.NamespaceName()); err != nil {
				ctx.Errorf("failed to create SMMR for namespace: %s", c.NamespaceName())
			}

			var wg sync.WaitGroup
			var errs *multierror.Error
			for app, rev := range map[echo.Instance]string{
				a: "istio-system-1",
				b: "istio-system-1",
				c: "istio-system-2",
			} {
				wg.Add(1)
				go func(app echo.Instance, rev string) {
					defer wg.Done()
					if err := enableInjectionInDeployment(ctx, app, rev); err != nil {
						errs = multierror.Append(errs, err)
						return
					}
					if err := verifyThatIstioProxyIsInjected(ctx, app); err != nil {
						errs = multierror.Append(errs, err)
					}
				}(app, rev)
			}
			wg.Wait()

			if errs.ErrorOrNil() != nil {
				ctx.Errorf("failed to enable injection in apps: %v", errs.Error())
			}
		})

		ctx.NewSubTest("apps can communicate within its mesh").Run(func(t framework.TestContext) {
			a.CallOrFail(t, echo.CallOptions{
				To: b,
				Port: echo.Port{
					Protocol:    protocol.HTTP,
					ServicePort: 80,
				},
				Check: check.OK(),
			})
		})

		ctx.NewSubTest("apps cannot communicate with apps from outside its mesh").Run(func(t framework.TestContext) {
			a.CallOrFail(t, echo.CallOptions{
				To: c,
				Port: echo.Port{
					Protocol:    protocol.HTTP,
					ServicePort: 80,
				},
				Check: check.Status(http.StatusBadGateway),
			})
		})

		ctx.NewSubTest("service entry allows to access apps from another mesh").Run(func(t framework.TestContext) {
			values := map[string]string{
				"svcName":   c.ServiceName(),
				"namespace": c.NamespaceName(),
			}
			t.ConfigIstio().EvalFile(istioNs1.Name(), values, svcEntryTmpl).ApplyOrFail(t)

			a.CallOrFail(t, echo.CallOptions{
				To: c,
				Port: echo.Port{
					Protocol:    protocol.HTTP,
					ServicePort: 80,
				},
				Check: check.OK(),
			})
		})
	})
}

func enableInjectionInDeployment(ctx resource.Context, app echo.Instance, revision string) error {
	kubeClient := ctx.Clusters().Default().Kube()
	return retry.UntilSuccess(func() error {
		appDeployment, err := kubeClient.AppsV1().Deployments(app.NamespaceName()).Get(context.TODO(), getDeploymentName(app), v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment %s: %s", app.NamespacedName().Name, err)
		}
		appDeployment.Spec.Template.Annotations[annotation.SidecarInject.Name] = "true"
		appDeployment.Spec.Template.Labels["istio.io/rev"] = revision
		if _, err := kubeClient.AppsV1().Deployments(app.NamespaceName()).
			Update(context.TODO(), appDeployment, v1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update deployment %s: %s", app.NamespacedName(), err)
		}
		return waitUntilDeploymentReady(ctx, app, appDeployment.Status.ObservedGeneration)
	}, retry.Timeout(30*time.Second), retry.Delay(1*time.Second))
}

func waitUntilDeploymentReady(ctx resource.Context, app echo.Instance, lastSeenGeneration int64) error {
	kubeClient := ctx.Clusters().Default().Kube()
	return retry.UntilSuccess(func() error {
		appDeployment, err := kubeClient.AppsV1().Deployments(app.NamespaceName()).Get(context.TODO(), getDeploymentName(app), v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment %s: %s", app.NamespacedName(), err)
		}
		if appDeployment.Status.ObservedGeneration == lastSeenGeneration || appDeployment.Status.Replicas != appDeployment.Status.ReadyReplicas {
			return fmt.Errorf("deployment %s not ready yet", app.NamespacedName())
		}
		return nil
	}, retry.Timeout(30*time.Second), retry.Delay(1*time.Second))
}

func verifyThatIstioProxyIsInjected(ctx resource.Context, app echo.Instance) error {
	kubeClient := ctx.Clusters().Default().Kube()
	return retry.UntilSuccess(func() error {
		pods, err := kubeClient.CoreV1().Pods(app.NamespaceName()).List(context.TODO(), v1.ListOptions{LabelSelector: "app=" + app.ServiceName()})
		if err != nil {
			return fmt.Errorf("failed to list pods app=%s/%s: %s", app.ServiceName(), app.NamespaceName(), err)
		}
		for _, p := range pods.Items {
			var istioProxyFound bool
			for _, c := range p.Spec.Containers {
				if c.Name == "istio-proxy" {
					istioProxyFound = true
				}
			}
			if !istioProxyFound {
				return fmt.Errorf("container istio-proxy not found in pod %s", p.Name)
			}
		}
		return nil
	}, retry.Timeout(30*time.Second), retry.Delay(1*time.Second))
}

func getDeploymentName(app echo.Instance) string {
	return fmt.Sprintf("%s-%s", app.ServiceName(), app.Config().Version)
}
