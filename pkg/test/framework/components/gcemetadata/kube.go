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

package gcemetadata

import (
	"fmt"
	"io"
	"net"

	corev1 "k8s.io/api/core/v1"

	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	ns = "gce-metadata"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id        resource.ID
	ns        namespace.Instance
	cluster   cluster.Cluster
	address   string
	addressVM string
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: ctx.Clusters().GetOrDefault(cfg.Cluster),
	}

	c.id = ctx.TrackResource(c)
	var err error
	scopes.Framework.Info("=== BEGIN: Deploy GCE Metadata Server ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("gcemetadata deployment failed: %v", err)
			scopes.Framework.Infof("=== FAILED: Deploy GCE Metadata Server ===")
			_ = c.Close()
		} else {
			scopes.Framework.Info("=== SUCCEEDED: Deploy GCE Metadata Server ===")
		}
	}()

	c.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: ns,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create %q namespace for GCE Metadata Server install; err: %v", ns, err)
	}

	// apply YAML
	if err := c.cluster.ApplyYAMLFiles(c.ns.Name(), environ.GCEMetadataServerInstallFilePath); err != nil {
		return nil, fmt.Errorf("failed to apply rendered %s, err: %v", environ.GCEMetadataServerInstallFilePath, err)
	}

	var svc *corev1.Service
	if svc, _, err = testKube.WaitUntilServiceEndpointsAreReady(c.cluster.Kube(), c.ns.Name(), "gce-metadata-server"); err != nil {
		scopes.Framework.Infof("Error waiting for GCE Metadata service to be available: %v", err)
		return nil, err
	}

	// Multicluster needs to use LoadBalancer IP
	if ctx.Environment().IsMultiCluster() {
		lb, err := testKube.WaitUntilServiceLoadBalancerReady(c.cluster.Kube(), c.ns.Name(), "gce-metadata-server")
		if err != nil {
			scopes.Framework.Infof("Error waiting for GCE Metadata service LB to be available: %v", err)
			return nil, err
		}
		c.address = net.JoinHostPort(lb, fmt.Sprint(svc.Spec.Ports[0].Port))
		c.addressVM = net.JoinHostPort(lb, fmt.Sprint(svc.Spec.Ports[1].Port))
	} else {
		c.address = net.JoinHostPort(svc.Spec.ClusterIP, fmt.Sprint(svc.Spec.Ports[0].Port))
		c.addressVM = net.JoinHostPort(svc.Spec.ClusterIP, fmt.Sprint(svc.Spec.Ports[1].Port))
	}
	scopes.Framework.Infof("GCE Metadata Server in-cluster address: %s/%s", c.address, c.addressVM)

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	return nil
}

func (c *kubeComponent) Address() string {
	return c.address
}

func (c *kubeComponent) AddressVM() string {
	return c.addressVM
}
