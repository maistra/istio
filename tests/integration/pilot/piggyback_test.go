//go:build integ
// +build integ

// Copyright Istio Authors
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
	"fmt"
	"strings"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestPiggyback(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).Features("usability.observability.proxy-status"). // TODO create new "agent-piggyback" feature
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		RequireIstioVersion("1.10.0").
		Run(func(t framework.TestContext) {
			// Add retry loop to handle case when the pod has disconnected from Istio temporarily
			retry.UntilSuccessOrFail(t, func() error {
				out, _, err := t.Clusters()[0].PodExec(
					apps.A[0].WorkloadsOrFail(t)[0].PodName(),
					apps.A.Config().Namespace.Name(),
					"istio-proxy",
					"pilot-agent request --debug-port 15004 GET /debug/syncz")
				if err != nil {
					return fmt.Errorf("couldn't curl sidecar: %v", err)
				}
				dr := discovery.DiscoveryResponse{}
				if err := protomarshal.Unmarshal([]byte(out), &dr); err != nil {
					return fmt.Errorf("unmarshal: %v", err)
				}
				if dr.TypeUrl != xds.TypeDebugSyncronization {
					return fmt.Errorf("the output doesn't contain expected typeURL: %s", out)
				}
				if len(dr.Resources) < 1 {
					return fmt.Errorf("the output didn't unmarshal as expected (no resources): %s", out)
				}
				if dr.Resources[0].TypeUrl != "type.googleapis.com/envoy.service.status.v3.ClientConfig" {
					return fmt.Errorf("resources[0] doesn't contain expected typeURL: %s", out)
				}
				return nil
			})

			expectSubstrings := func(have string, wants ...string) error {
				for _, want := range wants {
					if !strings.Contains(have, want) {
						return fmt.Errorf("substring %q not found; have %q", want, have)
					}
				}
				return nil
			}

			// Test gRPC-based Tap Service using istioctl.
			retry.UntilSuccessOrFail(t, func() error {
				podName := apps.A[0].WorkloadsOrFail(t)[0].PodName()
				nsName := apps.A.Config().Namespace.Name()
				pf, err := t.Clusters()[0].NewPortForwarder(podName, nsName, "localhost", 0, 15004)
				if err != nil {
					return fmt.Errorf("failed to create the port forwarder: %v", err)
				}
				pf.Start()
				defer pf.Close()

				istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{Cluster: t.Clusters().Default()})
				args := []string{"x", "proxy-status", "--plaintext", "--xds-address", pf.Address()}
				output, _, err := istioCtl.Invoke(args)
				if err != nil {
					return err
				}

				// Just verify pod A is known to Pilot; implicitly this verifies that
				// the printing code printed it.
				return expectSubstrings(output, fmt.Sprintf("%s.%s", podName, nsName))
			})
		})
}
