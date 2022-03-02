// Copyright 2020 Istio Authors
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

package pilot

import (
	"io/ioutil"
	"path"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"

	"istio.io/istio/pkg/test/env"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
)

func mustReadFile(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

// TestDestinationRuleTLS tests that MUTUAL tls mode is respected in DestinationRule.
// This sets up a client and server with appropriate cert config and ensures we can successfully send a message.
func TestDestinationRuleTLS(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "tls",
				Inject: true,
			})

			// Setup our destination rule, enforcing TLS to "server". These certs will be created/mounted below.
			g.ApplyConfigOrFail(t, ns, `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: db-mtls
spec:
  exportTo: ["."]
  host: server
  trafficPolicy:
    tls:
      mode: MUTUAL
      clientCertificate: /etc/certs/custom/cert-chain.pem
      privateKey: /etc/certs/custom/key.pem
      caCertificates: /etc/certs/custom/root-cert.pem
`)

			ports := []echo.Port{
				{
					Name:         "grpc",
					Protocol:     protocol.GRPC,
					InstancePort: 8090,
					TLS:          true,
				},
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8091,
					TLS:          true,
				},
				{
					Name:         "tcp",
					Protocol:     protocol.TCP,
					InstancePort: 8092,
					TLS:          true,
				},
			}

			var client, server echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echo.Config{
					Service:   "client",
					Namespace: ns,
					Ports:     []echo.Port{},
					Galley:    g,
					Pilot:     p,
					Subsets: []echo.SubsetConfig{{
						Version: "v1",
						// Set up custom annotations to mount the certs. We will re-use the configmap created by "server"
						// so that we don't need to manage it ourselves.
						// The paths here match the destination rule above
						Annotations: echo.NewAnnotations().
							Set(echo.SidecarVolume, `{"custom-certs":{"configMap":{"name":"server-certs"}}}`).
							Set(echo.SidecarVolumeMount, `{"custom-certs":{"mountPath":"/etc/certs/custom"}}`),
					}},
				}).
				With(&server, echo.Config{
					Service:   "server",
					Namespace: ns,
					Ports:     ports,
					Galley:    g,
					Pilot:     p,
					// Set up TLS certs on the server. This will make the server listen with these credentials.
					TLSSettings: &common.TLSSettings{
						RootCert:   mustReadFile(t, "root-cert.pem"),
						ClientCert: mustReadFile(t, "cert-chain.pem"),
						Key:        mustReadFile(t, "key.pem"),
						// Override hostname to match the SAN in the cert we are using
						Hostname: "server.default.svc",
					},
					// Do not inject, as we are testing non-Istio TLS here
					Subsets: []echo.SubsetConfig{{
						Version:     "v1",
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
					}},
				}).
				BuildOrFail(t)

			for _, tt := range ports {
				ctx.NewSubTest(tt.Name).Run(func(ctx framework.TestContext) {
					retry.UntilSuccessOrFail(ctx, func() error {
						opts := echo.CallOptions{
							Target: server,
							Port:   &tt,
						}
						if tt.Name == "tcp" {
							opts.Scheme = scheme.TCP
						}
						resp, err := client.Call(opts)
						if err != nil {
							return err
						}
						return resp.CheckOK()
					}, retry.Delay(time.Millisecond*100))
				})
			}
		})
}
