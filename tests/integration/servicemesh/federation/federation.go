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
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.DeployEastWestGW = false
	cfg.ConfigureMultiCluster = false
	cfg.DifferentTrustDomains = true
	cfg.ControlPlaneValues = `
components:
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

// CreateServiceMeshPeers wires all primary clusters together in a federation.
func CreateServiceMeshPeers(ctx framework.TestContext) error {
	ctx.Log("Creating ServiceMeshPeer resources")
	remoteIPs := map[string]string{}
	remoteCerts := map[string]*v1.ConfigMap{}
	for _, cluster := range ctx.Clusters().Primaries() {
		svc, err := cluster.CoreV1().Services("istio-system").Get(context.TODO(), "federation-ingress", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(svc.Status.LoadBalancer.Ingress) < 1 {
			return fmt.Errorf("federation-ingress svc has no public IP")
		}
		remoteIPs[cluster.Name()] = svc.Status.LoadBalancer.Ingress[0].IP
		ctx.Logf("Cluster '%s': detected %s as public IP\n", cluster.Name(), remoteIPs[cluster.Name()])
		configMap, err := cluster.CoreV1().ConfigMaps("istio-system").Get(context.TODO(), "istio-ca-root-cert", metav1.GetOptions{})
		if err != nil {
			return err
		}
		remoteCerts[cluster.Name()] = configMap.DeepCopy()
	}
	for _, cluster := range ctx.Clusters().Primaries() {
		for remoteCluster, remoteIP := range remoteIPs {
			// skip local cluster
			if remoteCluster == cluster.Name() {
				continue
			}
			configMap := remoteCerts[remoteCluster]
			configMap.ObjectMeta = metav1.ObjectMeta{
				Name: remoteCluster + "-ca-cert",
			}
			_, err := cluster.CoreV1().ConfigMaps("istio-system").Create(context.TODO(), configMap, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			err = ctx.Config(cluster).ApplyYAML("istio-system", fmt.Sprintf(`
apiVersion: federation.maistra.io/v1
kind: ServiceMeshPeer
metadata:
    name: %s
spec:
    remote:
        addresses:
        - %s
    gateways:
        ingress:
            name: federation-ingress
        egress:
            name: federation-egress
    security:
        trustDomain: %s
        clientID: %s
        certificateChain:
            kind: ConfigMap
            name: %s
`, remoteCluster, remoteIP, remoteCluster+".local", remoteCluster+".local/ns/istio-system/sa/federation-egress-service-account", remoteCluster+"-ca-cert"))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func SetupExportsAndImports(ctx framework.TestContext, exportFrom string) {
	primary := ctx.Clusters().GetByName("primary")
	err := ctx.Config(primary).ApplyYAML("istio-system", fmt.Sprintf(`
apiVersion: federation.maistra.io/v1
kind: ExportedServiceSet
metadata:
  name: cross-network-primary
  namespace: istio-system
spec:
  exportRules:
  - type: NameSelector
    nameSelector:
      namespace: %s
      name: ratings
      alias:
        namespace: bookinfo
        name: ratings
	`, exportFrom))
	if err != nil {
		ctx.Fatal(err)
	}
	secondary := ctx.Clusters().GetByName("cross-network-primary")
	err = ctx.Config(secondary).ApplyYAML("istio-system", `
apiVersion: federation.maistra.io/v1
kind: ImportedServiceSet
metadata:
  name: primary
  namespace: istio-system
spec:
  importRules:
    - type: NameSelector
      importAsLocal: false
      nameSelector:
        namespace: bookinfo
`)
	if err != nil {
		ctx.Fatal(err)
	}
}

func checkConnectivity(ctx framework.TestContext, source cluster.Cluster, namespace string) {
	var podName string
	err := retry.UntilSuccess(func() error {
		podList, err := source.PodsForSelector(context.TODO(), namespace, "app=sleep")
		if err != nil {
			return err
		}
		if len(podList.Items) < 1 {
			return fmt.Errorf("no sleep pod found in namespace %s", namespace)
		}
		podName = podList.Items[0].Name
		return nil
	}, retry.Timeout(300*time.Second), retry.Delay(time.Second))
	if err != nil {
		ctx.Fatal(err)
	}
	cmd := "curl http://ratings.bookinfo.svc.primary-imports.local:9080/ratings/123"
	err = retry.UntilSuccess(func() error {
		stdout, _, err := source.PodExec(podName, namespace, "sleep", cmd)
		if err != nil {
			return err
		} else if stdout != `{"id":123,"ratings":{"Reviewer1":5,"Reviewer2":4}}` {
			return fmt.Errorf("podexec output does not look right: %s", stdout)
		}
		return nil
	}, retry.Timeout(300*time.Second), retry.Delay(time.Second))
	if err != nil {
		ctx.Fatal(err)
	}
}
