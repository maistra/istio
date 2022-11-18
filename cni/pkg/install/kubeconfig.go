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

package install

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pkg/file"
)

const kubeconfigTemplate = `# Kubeconfig file for Istio CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: {{.KubernetesServiceProtocol}}://[{{.KubernetesServiceHost}}]:{{.KubernetesServicePort}}
    {{.TLSConfig}}
users:
- name: istio-cni
  user:
    token: "{{.ServiceAccountToken}}"
contexts:
- name: istio-cni-context
  context:
    cluster: local
    user: istio-cni
current-context: istio-cni-context
`

type kubeconfigFields struct {
	KubernetesServiceProtocol string
	KubernetesServiceHost     string
	KubernetesServicePort     string
	ServiceAccountToken       string
	TLSConfig                 string
}

func createKubeconfigFile(cfg *config.InstallConfig, saToken string) (kubeconfigFilepath string, err error) {
	if len(cfg.K8sServiceHost) == 0 {
		err = fmt.Errorf("KUBERNETES_SERVICE_HOST not set. Is this not running within a pod?")
		return
	}

	if len(cfg.K8sServicePort) == 0 {
		err = fmt.Errorf("KUBERNETES_SERVICE_PORT not set. Is this not running within a pod?")
		return
	}

	var tpl *template.Template
	tpl, err = template.New("kubeconfig").Parse(kubeconfigTemplate)
	if err != nil {
		return
	}

	protocol := cfg.K8sServiceProtocol
	if protocol == "" {
		protocol = "https"
	}

	caFile := cfg.KubeCAFile
	if caFile == "" {
		caFile = constants.ServiceAccountPath + "/ca.crt"
	}

	var tlsConfig string
	if cfg.SkipTLSVerify {
		tlsConfig = "insecure-skip-tls-verify: true"
	} else {
		if !file.Exists(caFile) {
			return "", fmt.Errorf("file does not exist: %s", caFile)
		}
		var caContents []byte
		caContents, err = os.ReadFile(caFile)
		if err != nil {
			return
		}
		caBase64 := base64.StdEncoding.EncodeToString(caContents)
		tlsConfig = "certificate-authority-data: " + caBase64
	}

	fields := kubeconfigFields{
		KubernetesServiceProtocol: protocol,
		KubernetesServiceHost:     cfg.K8sServiceHost,
		KubernetesServicePort:     cfg.K8sServicePort,
		ServiceAccountToken:       saToken,
		TLSConfig:                 tlsConfig,
	}

	var kcbb bytes.Buffer
	if err := tpl.Execute(&kcbb, fields); err != nil {
		return "", err
	}

	var kcbbToPrint bytes.Buffer
	fields.ServiceAccountToken = "<redacted>"
	if !cfg.SkipTLSVerify {
		fields.TLSConfig = fmt.Sprintf("certificate-authority-data: <CA cert from %s>", caFile)
	}
	if err := tpl.Execute(&kcbbToPrint, fields); err != nil {
		return "", err
	}

	// When using Multus, the net.d dir might not exist yet, so we must create it
	if err := os.MkdirAll(cfg.MountedCNINetDir, os.FileMode(0o755)); err != nil {
		return "", err
	}

	kubeconfigFilepath = filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename)
	installLog.Infof("write kubeconfig file %s with: \n%+v", kubeconfigFilepath, kcbbToPrint.String())
	if err = file.AtomicWrite(kubeconfigFilepath, kcbb.Bytes(), os.FileMode(cfg.KubeconfigMode)); err != nil {
		return "", err
	}

	return
}
