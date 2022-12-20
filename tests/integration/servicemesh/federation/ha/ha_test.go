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

package ha

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/servicemesh/federation"
	"istio.io/istio/tests/integration/servicemesh/maistra"
)

var (
	ns        namespace.Instance
	apps      echo.Instances
	appsMutex sync.Mutex

	// We test only GRPC port, because routing between federated clusters does not work when an imported service
	// has more than 1 port, and Istio test framework always adds GRPC port to the deployed echo apps,
	// so we cannot test HTTP or HTTPS for now.
	grpcPort = echo.Port{
		Protocol:    protocol.GRPC,
		ServicePort: 7070,
	}

	// Timeout is 6 minutes long, because we have hardcoded resync period 5 minutes long in the federation discovery controller.
	defaultRetry = echo.Retry{
		Options: []retry.Option{
			retry.Timeout(6 * time.Minute),
			retry.Delay(1 * time.Second),
		},
	}
)

func TestMain(m *testing.M) {
	// Ignoring staticcheck linter. RequireMinClusters is now deprecated, but we need at least 2 clusters for this
	// test to make it work.
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMinClusters(2).
		Setup(maistra.ApplyServiceMeshCRDs).
		Setup(istio.Setup(nil, federation.SetupConfig)).
		Setup(namespace.Setup(&ns, namespace.Config{Prefix: "app", Inject: true})).
		SetupParallel(
			maistra.DeployEchos(&apps, &appsMutex, "a", namespace.Future(&ns), maistra.AppOpts{Ports: []echo.Port{ports.GRPC}}),
			maistra.DeployEchos(&apps, &appsMutex, "b", namespace.Future(&ns), maistra.AppOpts{Ports: []echo.Port{ports.GRPC}}),
		).
		Run()
}

func TestDiscovery(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			primary := ctx.Clusters().GetByName("primary")
			secondary := ctx.Clusters().GetByName("cross-network-primary")

			appsSecondary := match.Cluster(secondary).GetMatches(apps)
			aSecondary := match.ServiceName(echo.NamespacedName{Name: "a", Namespace: ns}).GetMatches(appsSecondary).Instances()[0]

			federation.CreateServiceMeshPeersOrFail(ctx)

			ctx.NewSubTest("export app from primary cluster and route traffic to both clusters").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns.Name(),
						Name:      "b",
					})
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: ns.Name(),
					})

					t.ConfigKube(secondary).Eval(ns.Name(), map[string]string{"Namespace": ns.Name()}, `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: split-traffic-to-b-between-local-and-remote-clusters
spec:
  hosts:
  - b.{{ .Namespace }}.svc.cluster.local
  http:
  - route:
    - destination:
        host: b.{{ .Namespace }}.svc.cluster.local
      weight: 50
    - destination:
        host: b.{{ .Namespace }}.svc.primary-imports.local
      weight: 50
`).ApplyOrFail(t)

					aSecondary.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.cluster.local", ns.Name()),
						Count:   2,
						Check: check.And(
							check.GRPCStatus(codes.OK),
							check.ReachedClusters(t.AllClusters(), []cluster.Cluster{primary, secondary}),
						),
					}))
				})
		})
}

func withDefaults(opts echo.CallOptions) echo.CallOptions {
	opts.Message = "test"
	opts.Retry = defaultRetry
	opts.Port = grpcPort
	return opts
}
