//go:build integ
// +build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package externalca

import (
	"testing"

	"istio.io/istio/pkg/kube"
	csrctrl "istio.io/istio/pkg/test/csrctrl/controllers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	apps     deployment.SingleNamespaceView
	stopChan = make(chan struct{})
)

func TestMain(m *testing.M) {
	// Integration test for testing interoperability with external CA's that are integrated with K8s CSR API
	// Refer to https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/
	// nolint: staticcheck
	var certs []csrctrl.SignerRootCert
	framework.NewSuite(m).
		// https://issues.redhat.com/browse/OSSM-5978
		Skip("https://github.com/istio/istio/issues/1000000").
		Label(label.CustomSetup).
		RequireMinVersion(19).
		Setup(func(ctx resource.Context) error {
			var clients []kube.Client
			for _, c := range ctx.AllClusters() {
				clients = append(clients, c)
			}
			var err error
			certs, err = csrctrl.RunCSRController("clusterissuers.istio.io/signer1,clusterissuers.istio.io/signer2", stopChan, clients)
			return err
		}).
		Setup(istio.Setup(nil, func(ctx resource.Context, cfg *istio.Config) {
			var isExternalControlPlane bool
			for _, cluster := range ctx.AllClusters() {
				if cluster.IsExternalControlPlane() {
					isExternalControlPlane = true
				}
			}

			cfg.ControlPlaneValues = generateConfigYaml(certs, false, isExternalControlPlane)
			cfg.ConfigClusterValues = generateConfigYaml(certs, true, false)
		})).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Run()
	stopChan <- struct{}{}
	close(stopChan)
}

func generateConfigYaml(certs []csrctrl.SignerRootCert, isConfigCluster bool, isExternalControlPlane bool) string {
	cert1 := certs[0]
	cert2 := certs[1]

	cfgYaml := tmpl.MustEvaluate(`
values:
  pilot:
    env:
      EXTERNAL_CA: ISTIOD_RA_KUBERNETES_API
  meshConfig:
    defaultConfig:
      proxyMetadata:
        ISTIO_META_CERT_SIGNER: signer1
    trustDomainAliases: [some-other, trust-domain-foo]
    caCertificates:
    - pem: |
{{.rootcert1 | indent 8}}
      certSigners:
      - {{.signer1}}
    - pem: |
{{.rootcert2 | indent 8}}
      certSigners:
      - {{.signer2}}
{{- if not .isConfigCluster}}
components:
  pilot:
    enabled: true
    k8s:
      env:
      - name: CERT_SIGNER_DOMAIN
        value: clusterissuers.istio.io
      - name: PILOT_CERT_PROVIDER
        value: k8s.io/clusterissuers.istio.io/signer2
      overlays:
        # Amend ClusterRole to add permission for istiod to approve certificate signing by custom signer
        - kind: ClusterRole
          name: istiod-clusterrole-istio-system
          patches:
            - path: rules[-1]
              value: |
                apiGroups:
                - certificates.k8s.io
                resourceNames:
                - clusterissuers.istio.io/*
                resources:
                - signers
                verbs:
                - approve
{{- end }}
{{- if .isExternalControlPlane}}
        - kind: Deployment
          name: istiod
          patches:
            - path: spec.template.spec.volumes[100]
              value: |-
                name: config-volume
                configMap:
                  name: istio
            - path: spec.template.spec.volumes[100]
              value: |-
                name: inject-volume
                configMap:
                  name: istio-sidecar-injector
            - path: spec.template.spec.containers[0].volumeMounts[100]
              value: |-
                name: config-volume
                mountPath: /etc/istio/config
            - path: spec.template.spec.containers[0].volumeMounts[100]
              value: |-
                name: inject-volume
                mountPath: /var/lib/istio/inject
{{- end }}
`, map[string]any{
		"rootcert1":              cert1.Rootcert,
		"signer1":                cert1.Signer,
		"rootcert2":              cert2.Rootcert,
		"signer2":                cert2.Signer,
		"isConfigCluster":        isConfigCluster,
		"isExternalControlPlane": isExternalControlPlane,
	})
	return cfgYaml
}
