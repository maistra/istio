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

package maistra

import (
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

func TestMultiNamespace(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Enable mTLS in strict mode for the entire mesh.
			if err := enableMTLS(ctx); err != nil {
				ctx.Fatal(err)
			}

			member1 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "member",
				Inject: true,
			})

			member2 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "member",
				Inject: true,
			})

			_ = namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "non-member",
				Inject: true,
			})

			// Add member namespaces to the SMMR.
			if err := updateSMMR(ctx, member1, member2); err != nil {
				ctx.Fatal(err)
			}

			var memberClient, server echo.Instance

			echoboot.NewBuilder(ctx).
				With(&memberClient, echo.Config{
					Service:   "client",
					Namespace: member1,
					Ports:     []echo.Port{},
				}).
				With(&server, echo.Config{
					Service:   "server",
					Namespace: member2,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							InstancePort: 8090,
						}},
				}).
				BuildOrFail(t)

			// Traffic from member namespace to member namespace should succeed.
			retry.UntilSuccessOrFail(t, func() error {
				resp, err := memberClient.Call(echo.CallOptions{
					Target:   server,
					PortName: "http",
				})
				if err != nil {
					return err
				}
				return resp.CheckOK()
			}, retry.Delay(time.Millisecond*100))

			t.Cleanup(func() {
				if err := disableMTLS(ctx); err != nil {
					t.Errorf("failed to disable mTLS: %v", err)
				}

				if err := updateSMMR(ctx); err != nil {
					t.Errorf("failed to remove namespaces from SMMR: %v", err)
				}
			})
		})
}
