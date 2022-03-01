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

package istio

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	kubeCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

const (
	// DefaultSystemNamespace default value for SystemNamespace
	DefaultSystemNamespace = "istio-system"

	// IntegrationTestDefaultsIOP is the path of the default IstioOperator spec to use
	// for integration tests
	IntegrationTestDefaultsIOP = "tests/integration/iop-integration-test-defaults.yaml"

	// IntegrationTestDefaultsIOPWithQUIC is the path of the default IstioOperator spec to
	// use for integration tests involving QUIC
	IntegrationTestDefaultsIOPWithQUIC = "tests/integration/iop-integration-test-defaults-with-quic.yaml"

	// IntegrationTestRemoteDefaultsIOP is the path of the default IstioOperator spec to use
	// on remote clusters for integration tests
	IntegrationTestRemoteDefaultsIOP = "tests/integration/iop-remote-integration-test-defaults.yaml"

	// IntegrationTestIstiodlessRemoteDefaultsIOP is the path of the default IstioOperator spec to use
	// on remote clusters for integration tests when --istio.test.istio.istiodlessRemotes is set.
	IntegrationTestIstiodlessRemoteDefaultsIOP = "tests/integration/iop-istiodless-remote-integration-test-defaults.yaml"

	// IntegrationTestExternalIstiodPrimaryDefaultsIOP is the path of the default IstioOperator spec to use
	// on external istiod primary clusters for integration tests
	IntegrationTestExternalIstiodPrimaryDefaultsIOP = "tests/integration/iop-externalistiod-primary-integration-test-defaults.yaml"

	// IntegrationTestExternalIstiodConfigDefaultsIOP is the path of the default IstioOperator spec to use
	// on external istiod config clusters for integration tests
	IntegrationTestExternalIstiodConfigDefaultsIOP = "tests/integration/iop-externalistiod-config-integration-test-defaults.yaml"

	// IntegrationTestExternalIstiodRemoteDefaultsIOP is the path of the default IstioOperator spec to use
	// on external istiod remote clusters for integration tests
	IntegrationTestExternalIstiodRemoteDefaultsIOP = "tests/integration/iop-externalistiod-remote-integration-test-defaults.yaml"

	// IntegrationTestExternalIstiodRemoteDefaultsIOP is the path of the default IstioOperator spec to use
	// to install gateways on external istiod remote clusters for integration tests
	IntegrationTestExternalIstiodRemoteGatewaysIOP = "tests/integration/iop-externalistiod-remote-integration-test-gateways.yaml"

	// DefaultDeployTimeout for Istio
	DefaultDeployTimeout = time.Second * 300

	// DefaultCIDeployTimeout for Istio
	DefaultCIDeployTimeout = time.Minute * 10

	// DefaultUndeployTimeout for Istio.
	DefaultUndeployTimeout = time.Second * 300

	// DefaultCIUndeployTimeout for Istio.
	DefaultCIUndeployTimeout = time.Second * 900
)

var (
	helmValues      string
	operatorOptions string

	settingsFromCommandline = &Config{
		SystemNamespace:         DefaultSystemNamespace,
		IstioNamespace:          DefaultSystemNamespace,
		ConfigNamespace:         DefaultSystemNamespace,
		TelemetryNamespace:      DefaultSystemNamespace,
		PolicyNamespace:         DefaultSystemNamespace,
		IngressNamespace:        DefaultSystemNamespace,
		EgressNamespace:         DefaultSystemNamespace,
		DeployIstio:             true,
		DeployTimeout:           0,
		UndeployTimeout:         0,
		PrimaryClusterIOPFile:   IntegrationTestDefaultsIOP,
		ConfigClusterIOPFile:    IntegrationTestDefaultsIOP,
		RemoteClusterIOPFile:    IntegrationTestRemoteDefaultsIOP,
		DeployEastWestGW:        true,
		DumpKubernetesManifests: false,
		IstiodlessRemotes:       false,
		EnableCNI:               false,
		ConfigureMultiCluster:   true,
	}
)

// Config provide kube-specific Config from flags.
type Config struct {
	// The namespace where the Istio components (<=1.1) reside in a typical deployment (default: "istio-system").
	SystemNamespace string

	// The namespace in which istio ca and cert provisioning components are deployed.
	IstioNamespace string

	// The namespace in which config, discovery and auto-injector are deployed.
	ConfigNamespace string

	// The namespace in which kiali, tracing providers, graphana, prometheus are deployed.
	TelemetryNamespace string

	// The namespace in which istio policy checker is deployed.
	PolicyNamespace string

	// The namespace in which istio ingressgateway is deployed
	IngressNamespace string

	// The namespace in which istio egressgateway is deployed
	EgressNamespace string

	// DeployTimeout the timeout for deploying Istio.
	DeployTimeout time.Duration

	// UndeployTimeout the timeout for undeploying Istio.
	UndeployTimeout time.Duration

	// The IstioOperator spec file to be used for Control plane cluster by default
	PrimaryClusterIOPFile string

	// The IstioOperator spec file to be used for Config cluster by default
	ConfigClusterIOPFile string

	// The IstioOperator spec file to be used for Remote cluster by default
	RemoteClusterIOPFile string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// These values are applied to non-remote clusters
	ControlPlaneValues string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// These values are only applied to remote clusters
	// Default value will be ControlPlaneValues if no remote values provided
	RemoteClusterValues string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// These values are only applied to remote config clusters
	// Default value will be ControlPlaneValues if no remote values provided
	ConfigClusterValues string

	// Overrides for the Helm values file.
	Values map[string]string

	// Indicates that the test should deploy Istio into the target Kubernetes cluster before running tests.
	DeployIstio bool

	// Do not wait for the validation webhook before completing the deployment. This is useful for
	// doing deployments without Galley.
	SkipWaitForValidationWebhook bool

	// Indicates that the test should deploy Istio's east west gateway into the target Kubernetes cluster
	// before running tests.
	DeployEastWestGW bool

	// Indicates that the test should deploy Istio's using helm charts
	DeployHelm bool

	// DumpKubernetesManifests will cause Kubernetes YAML generated by istioctl install/generate to be dumped to artifacts.
	DumpKubernetesManifests bool

	// IstiodlessRemotes makes remote clusters run without istiod, using webhooks/ca from the primary cluster.
	// TODO we could set this per-cluster if istiod was smarter about patching remotes.
	IstiodlessRemotes bool

	// OperatorOptions overrides default operator configuration.
	OperatorOptions map[string]string

	// EnableCNI indicates the test should have CNI enabled.
	EnableCNI bool

	ConfigureMultiCluster bool

	DifferentTrustDomains bool
}

func (c *Config) OverridesYAML() string {
	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return ""
	}

	return fmt.Sprintf(`
global:
  hub: %s
  tag: %s
`, s.Hub, s.Tag)
}

func (c *Config) IstioOperatorConfigYAML(iopYaml string) string {
	data := ""
	if iopYaml != "" {
		data = Indent(iopYaml, "  ")
	}

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return ""
	}

	return fmt.Sprintf(`
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  hub: %s
  tag: %s
%s
`, s.Hub, s.Tag, data)
}

// Indent indents a block of text with an indent string
func Indent(text, indent string) string {
	if text[len(text)-1:] == "\n" {
		result := ""
		for _, j := range strings.Split(text[:len(text)-1], "\n") {
			result += indent + j + "\n"
		}
		return result
	}
	result := ""
	for _, j := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		result += indent + j + "\n"
	}
	return result[:len(result)-1]
}

// DefaultConfig creates a new Config from defaults, environments variables, and command-line parameters.
func DefaultConfig(ctx resource.Context) (Config, error) {
	// Make a local copy.
	s := *settingsFromCommandline

	iopFile := s.PrimaryClusterIOPFile
	if iopFile != "" && !path.IsAbs(s.PrimaryClusterIOPFile) {
		iopFile = filepath.Join(env.IstioSrc, s.PrimaryClusterIOPFile)
	}

	if err := checkFileExists(iopFile); err != nil {
		scopes.Framework.Warnf("Default IOPFile missing: %v", err)
	}

	deps, err := image.SettingsFromCommandLine()
	if err != nil {
		return Config{}, err
	}

	if s.Values, err = newHelmValues(ctx, deps); err != nil {
		return Config{}, err
	}

	if s.OperatorOptions, err = parseConfigOptions(operatorOptions); err != nil {
		return Config{}, err
	}

	if ctx.Settings().CIMode {
		s.DeployTimeout = DefaultCIDeployTimeout
		s.UndeployTimeout = DefaultCIUndeployTimeout
	} else {
		s.DeployTimeout = DefaultDeployTimeout
		s.UndeployTimeout = DefaultUndeployTimeout
	}

	return s, nil
}

// DefaultConfigOrFail calls DefaultConfig and fails t if an error occurs.
func DefaultConfigOrFail(t test.Failer, ctx resource.Context) Config {
	cfg, err := DefaultConfig(ctx)
	if err != nil {
		t.Fatalf("Get istio config: %v", err)
	}
	return cfg
}

func checkFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return nil
}

func newHelmValues(ctx resource.Context, s *image.Settings) (map[string]string, error) {
	userValues, err := parseConfigOptions(helmValues)
	if err != nil {
		return nil, err
	}

	// Copy the defaults first.
	values := make(map[string]string)

	// Common values
	values[image.HubValuesKey] = s.Hub
	values[image.TagValuesKey] = s.Tag
	values[image.ImagePullPolicyValuesKey] = s.PullPolicy

	// Copy the user values.
	for k, v := range userValues {
		values[k] = v
	}

	// Always pull Docker images if using the "latest".
	if values[image.TagValuesKey] == image.LatestTag {
		values[image.ImagePullPolicyValuesKey] = string(kubeCore.PullAlways)
	}

	// We need more information on Envoy logs to detect usage of any deprecated feature
	if ctx.Settings().FailOnDeprecation {
		values["global.proxy.logLevel"] = "debug"
		values["global.proxy.componentLogLevel"] = "misc:debug"
	}

	return values, nil
}

func parseConfigOptions(options string) (map[string]string, error) {
	out := make(map[string]string)
	if options == "" {
		return out, nil
	}

	values := strings.Split(options, ",")
	for _, v := range values {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed parsing config options: %s", options)
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out, nil
}

// String implements fmt.Stringer
func (c *Config) String() string {
	result := ""

	result += fmt.Sprintf("SystemNamespace:                %s\n", c.SystemNamespace)
	result += fmt.Sprintf("IstioNamespace:                 %s\n", c.IstioNamespace)
	result += fmt.Sprintf("ConfigNamespace:                %s\n", c.ConfigNamespace)
	result += fmt.Sprintf("TelemetryNamespace:             %s\n", c.TelemetryNamespace)
	result += fmt.Sprintf("PolicyNamespace:                %s\n", c.PolicyNamespace)
	result += fmt.Sprintf("IngressNamespace:               %s\n", c.IngressNamespace)
	result += fmt.Sprintf("EgressNamespace:                %s\n", c.EgressNamespace)
	result += fmt.Sprintf("DeployIstio:                    %v\n", c.DeployIstio)
	result += fmt.Sprintf("DeployEastWestGW:               %v\n", c.DeployEastWestGW)
	result += fmt.Sprintf("DeployTimeout:                  %s\n", c.DeployTimeout.String())
	result += fmt.Sprintf("UndeployTimeout:                %s\n", c.UndeployTimeout.String())
	result += fmt.Sprintf("Values:                         %v\n", c.Values)
	result += fmt.Sprintf("PrimaryClusterIOPFile:          %s\n", c.PrimaryClusterIOPFile)
	result += fmt.Sprintf("ConfigClusterIOPFile:           %s\n", c.ConfigClusterIOPFile)
	result += fmt.Sprintf("RemoteClusterIOPFile:           %s\n", c.RemoteClusterIOPFile)
	result += fmt.Sprintf("SkipWaitForValidationWebhook:   %v\n", c.SkipWaitForValidationWebhook)
	result += fmt.Sprintf("DeployHelm:                     %v\n", c.DeployHelm)
	result += fmt.Sprintf("DumpKubernetesManifests:        %v\n", c.DumpKubernetesManifests)
	result += fmt.Sprintf("IstiodlessRemotes:              %v\n", c.IstiodlessRemotes)
	result += fmt.Sprintf("OperatorOptions:                %v\n", c.OperatorOptions)
	result += fmt.Sprintf("EnableCNI:                      %v\n", c.EnableCNI)

	return result
}

// ClaimSystemNamespace retrieves the namespace for the Istio system components from the environment.
func ClaimSystemNamespace(ctx resource.Context) (namespace.Instance, error) {
	istioCfg, err := DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	nsCfg := namespace.Config{
		Prefix: istioCfg.SystemNamespace,
		Inject: false,
	}
	return namespace.Claim(ctx, nsCfg)
}

// ClaimSystemNamespaceOrFail calls ClaimSystemNamespace, failing the test if an error occurs.
func ClaimSystemNamespaceOrFail(t test.Failer, ctx resource.Context) namespace.Instance {
	t.Helper()
	i, err := ClaimSystemNamespace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}
