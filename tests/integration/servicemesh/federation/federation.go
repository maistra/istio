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

package federation

import (
	"context"
	"path/filepath"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	exportedServiceSetTmpl = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/federation/testdata/exported-service-set.tmpl.yaml")
	importedServiceSetTmpl = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/federation/testdata/imported-service-set.tmpl.yaml")
	serviceMeshPeerTmpl    = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/federation/testdata/service-mesh-peer.tmpl.yaml")
)

func SetupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.IstiodlessRemotes = false // We need istiod for Federation to work
	cfg.DeployEastWestGW = false
	cfg.ConfigureMultiCluster = false
	cfg.ConfigureRemoteCluster = false
	cfg.DifferentTrustDomains = true
	cfg.ControlPlaneValues = `
components:
  pilot:
    k8s:
      overlays:
      - apiVersion: apps/v1
        kind: Deployment
        name: istiod
        patches:
        - path: spec.template.spec.containers.[name:discovery].args[-1]
          value: "--resync=3s"
  ingressGateways:
  - name: federation-ingress
    namespace: istio-system
    enabled: true
    label:
      unique: ingress
    k8s:
      service:
        ports:
        # required for handling service requests from mesh2
        - port: 15443
          name: tls
        # required for handing discovery requests from mesh2
        - port: 8188
          name: https-discovery
  egressGateways:
  - name: federation-egress
    namespace: istio-system
    enabled: true
    label:
      unique: egress
    k8s:
      service:
        ports:
        # required for handling service requests from mesh2
        - port: 15443
          name: tls
        # required for handing discovery requests from mesh2
        - port: 8188
          name: https-discovery
`
}

// CreateServiceMeshPeersOrFail wires all primary clusters together in a federation.
func CreateServiceMeshPeersOrFail(ctx framework.TestContext) {
	ctx.Log("Creating ServiceMeshPeer resources")
	remoteIPs := map[string]string{}
	remoteCerts := map[string]*v1.ConfigMap{}
	for _, cluster := range ctx.Clusters().Primaries() {
		svc, err := cluster.Kube().CoreV1().Services("istio-system").Get(context.TODO(), "federation-ingress", metav1.GetOptions{})
		if err != nil {
			ctx.Fatalf("failed to get service federation-ingress: %s", err)
		}
		if len(svc.Status.LoadBalancer.Ingress) < 1 {
			ctx.Fatalf("federation-ingress svc has no public IP")
		}
		remoteIPs[cluster.Name()] = svc.Status.LoadBalancer.Ingress[0].IP
		ctx.Logf("Cluster '%s': detected %s as public IP\n", cluster.Name(), remoteIPs[cluster.Name()])
		configMap, err := cluster.Kube().CoreV1().ConfigMaps("istio-system").Get(context.TODO(), "istio-ca-root-cert", metav1.GetOptions{})
		if err != nil {
			ctx.Fatalf("failed to get config map istio-ca-root-cert: %s", err)
		}
		remoteCerts[cluster.Name()] = configMap.DeepCopy()
	}
	for _, cluster := range ctx.Clusters().Primaries() {
		cluster := cluster // ensures that ctx.Cleanup() is performed on the correct cluster
		for remoteCluster, remoteIP := range remoteIPs {
			// skip local cluster
			if remoteCluster == cluster.Name() {
				continue
			}
			caCertConfigMapName := remoteCluster + "-ca-cert"
			configMap := remoteCerts[remoteCluster]
			configMap.ObjectMeta = metav1.ObjectMeta{
				Name: caCertConfigMapName,
			}
			_, err := cluster.Kube().CoreV1().ConfigMaps("istio-system").Create(context.TODO(), configMap, metav1.CreateOptions{})
			ctx.Cleanup(func() {
				if err := cluster.Kube().CoreV1().ConfigMaps("istio-system").Delete(context.TODO(), caCertConfigMapName, metav1.DeleteOptions{}); err != nil {
					ctx.Fatalf("failed to delete config map %s: %s", caCertConfigMapName, err)
				}
			})
			if err != nil {
				ctx.Fatalf("failed to create config map %s: %s", configMap.ObjectMeta.Name, err)
			}
			ctx.ConfigKube(cluster).EvalFile("istio-system", map[string]string{
				"RemoteClusterName": remoteCluster,
				"RemoteClusterAddr": remoteIP,
			}, serviceMeshPeerTmpl).ApplyOrFail(ctx)
		}
	}
}

type NameSelector struct {
	Namespace      string
	Name           string
	AliasNamespace string
	AliasName      string
	ImportAsLocal  bool
}

func ExportServiceOrFail(ctx framework.TestContext, from, to cluster.Cluster, selectors ...NameSelector) {
	ctx.ConfigKube(from).EvalFile("istio-system", map[string]any{
		"ExportTo":      to.Name(),
		"NameSelectors": selectors,
	}, exportedServiceSetTmpl).ApplyOrFail(ctx)
}

func ImportServiceOrFail(ctx framework.TestContext, to, from cluster.Cluster, selectors ...NameSelector) {
	ctx.ConfigKube(to).EvalFile("istio-system", map[string]any{
		"ImportFrom":    from.Name(),
		"NameSelectors": selectors,
	}, importedServiceSetTmpl).ApplyOrFail(ctx)
}
