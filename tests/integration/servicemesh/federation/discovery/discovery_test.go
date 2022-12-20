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

package discovery

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
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
	ns1 namespace.Instance
	ns2 namespace.Instance

	appsPrimary      echo.Instances
	appsPrimaryMux   sync.Mutex
	appsSecondary    echo.Instances
	appsSecondaryMux sync.Mutex

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
		SetupParallel(
			namespace.Setup(&ns1, namespace.Config{Prefix: "app", Inject: true}),
			namespace.Setup(&ns2, namespace.Config{Prefix: "app", Inject: true}),
		).
		SetupParallel(
			maistra.DeployEchos(&appsPrimary, &appsPrimaryMux, "a", namespace.Future(&ns1), maistra.AppOpts{
				ClusterName: "primary",
				Ports:       []echo.Port{ports.GRPC},
			}),
			maistra.DeployEchos(&appsPrimary, &appsPrimaryMux, "b", namespace.Future(&ns1), maistra.AppOpts{
				ClusterName: "primary",
				Ports:       []echo.Port{ports.GRPC},
			}),
			maistra.DeployEchos(&appsPrimary, &appsPrimaryMux, "d", namespace.Future(&ns2), maistra.AppOpts{
				ClusterName: "primary",
				Ports:       []echo.Port{ports.GRPC},
			}),
			maistra.DeployEchos(&appsSecondary, &appsSecondaryMux, "c", namespace.Future(&ns1), maistra.AppOpts{
				ClusterName: "cross-network-primary",
				Ports:       []echo.Port{ports.GRPC},
			}),
		).
		Run()
}

func TestDiscovery(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			primary := ctx.Clusters().GetByName("primary")
			secondary := ctx.Clusters().GetByName("cross-network-primary")
			c := match.ServiceName(echo.NamespacedName{Name: "c", Namespace: ns1}).GetMatches(appsSecondary.Instances()).Instances()[0]

			federation.CreateServiceMeshPeersOrFail(ctx)

			ctx.NewSubTest("export one app from primary cluster and import all to secondary - only the exported one is accessible").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns1.Name(),
						Name:      "a",
					})
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: ns1.Name(),
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("a.%s.svc.primary-imports.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.primary-imports.local", ns1.Name()),
						Check:   check.Error(),
					}))
				})

			ctx.NewSubTest("export all apps from primary cluster and import all to secondary - all are accessible").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns1.Name(),
					})
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: ns1.Name(),
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("a.%s.svc.primary-imports.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.primary-imports.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export one app with aliases from primary cluster and import all to secondary - only the exported one is accessible").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace:      ns1.Name(),
						Name:           "a",
						AliasNamespace: "apps",
						AliasName:      "a-imported",
					})
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: "apps",
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "a-imported.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "a.apps.svc.primary-imports.local",
						Check:   check.Error(),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "b.apps.svc.primary-imports.local",
						Check:   check.Error(),
					}))
				})

			ctx.NewSubTest("export all apps from with aliases from primary cluster and import all to secondary - all are accessible").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace:      ns1.Name(),
						AliasNamespace: "apps",
					})
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: "apps",
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "a.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "b.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export all apps with and without aliases from primary cluster and import all to secondary").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace:      ns1.Name(),
						Name:           "a",
						AliasNamespace: "apps",
						AliasName:      "a-imported",
					}, federation.NameSelector{
						Namespace: ns1.Name(),
						Name:      "b",
					})
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: "apps",
					}, federation.NameSelector{
						Namespace: ns1.Name(),
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "a-imported.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.primary-imports.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export a namespace from primary cluster and import services from that namespace as local to secondary").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns1.Name(),
					})
					// importing as local requires namespace alias
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace:      ns1.Name(),
						Name:           "a",
						AliasNamespace: "apps",
						AliasName:      "a",
						ImportAsLocal:  true,
					}, federation.NameSelector{
						// namespace alias can be same as exported namespace
						Namespace:      ns1.Name(),
						Name:           "b",
						AliasNamespace: ns1.Name(),
						AliasName:      "b",
						ImportAsLocal:  true,
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "a.apps.svc.cluster.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.cluster.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export a namespace from primary cluster and import as local to secondary").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns1.Name(),
					})
					// importing as local requires namespace alias
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace:      ns1.Name(),
						AliasNamespace: ns1.Name(),
						ImportAsLocal:  true,
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("a.%s.svc.cluster.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.cluster.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export a namespace from primary cluster and import as local with wildcard to secondary").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns1.Name(),
					})
					// importing as local requires namespace alias
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace:      ns1.Name(),
						AliasNamespace: ns1.Name(),
						AliasName:      "*",
						ImportAsLocal:  true,
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("a.%s.svc.cluster.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.cluster.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export apps from primary cluster and import all to secondary as local and remote").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace: ns1.Name(),
					})
					// importing as local requires namespace alias
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace:      ns1.Name(),
						Name:           "a",
						AliasNamespace: ns1.Name(),
						AliasName:      "a",
						ImportAsLocal:  true,
					}, federation.NameSelector{
						Namespace: ns1.Name(),
						Name:      "b",
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("a.%s.svc.cluster.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: fmt.Sprintf("b.%s.svc.primary-imports.local", ns1.Name()),
						Check:   check.GRPCStatus(codes.OK),
					}))
				})

			ctx.NewSubTest("export all namespaces from primary cluster and import all to secondary").
				Run(func(t framework.TestContext) {
					federation.ExportServiceOrFail(ctx, primary, secondary, federation.NameSelector{
						Namespace:      "*",
						Name:           "*",
						AliasNamespace: "apps",
					})
					// Namespaces exprted nas "*" cannot be imported without an alias
					federation.ImportServiceOrFail(ctx, secondary, primary, federation.NameSelector{
						Namespace: "apps",
					})

					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "a.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "b.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
					}))
					c.CallOrFail(t, withDefaults(echo.CallOptions{
						Address: "d.apps.svc.primary-imports.local",
						Check:   check.GRPCStatus(codes.OK),
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
