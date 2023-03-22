//go:build integ
// +build integ

//
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
	"context"
	"fmt"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	maistrav1 "maistra.io/api/client/versioned/typed/core/v1"
	"maistra.io/api/manifests"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

type InstallationOptions struct {
	EnableGatewayAPI bool
}

func ApplyServiceMeshCRDs(ctx resource.Context) (err error) {
	crds, err := manifests.GetManifestsByName()
	if err != nil {
		return fmt.Errorf("cannot read maistra CRD YAMLs: %s", err)
	}

	for _, c := range ctx.Clusters().Kube().Primaries() {
		for _, crd := range crds {
			// we need to manually Create() the CRD because Apply() wants to write its content into an annotation which fails because of size limitations
			rawJSON, err := yaml.YAMLToJSON(crd)
			if err != nil {
				return err
			}
			crd := apiextv1.CustomResourceDefinition{}
			_, _, err = unstructured.UnstructuredJSONScheme.Decode(rawJSON, nil, &crd)
			if err != nil {
				return err
			}
			if _, err := c.Ext().ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), &crd, metav1.CreateOptions{}); err != nil {
				if !errors.IsAlreadyExists(err) {
					return err
				}
			}
		}

		c.InvalidateDiscovery()
	}
	return err
}

func ApplyGatewayAPICRDs() resource.SetupFn {
	return func(ctx resource.Context) error {
		for _, c := range ctx.Clusters() {
			if err := c.ApplyYAMLFiles(
				"", filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/gateway-api-crd.yaml"),
			); err != nil {
				return err
			}
		}
		return nil
	}
}

func Install(opts InstallationOptions) resource.SetupFn {
	return func(ctx resource.Context) error {
		kubeClient := ctx.Clusters().Default().Kube()
		istiod, err := waitForIstiod(kubeClient, 0)
		if err != nil {
			return err
		}
		if err := ctx.Clusters().Default().ApplyYAMLFiles(
			"", filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/clusterrole.yaml"),
		); err != nil {
			return err
		}
		if err := applyRolesToMemberNamespaces(ctx.Clusters().Default(), "istio-system"); err != nil {
			return err
		}
		if err := patchIstiodArgs(kubeClient, generateMaistraArguments(opts)); err != nil {
			return err
		}
		if _, err := waitForIstiod(kubeClient, istiod.Generation); err != nil {
			return err
		}
		return nil
	}
}

func EnableIOR(ctx resource.Context) error {
	kubeClient := ctx.Clusters().Default().Kube()
	istiod, err := waitForIstiod(kubeClient, 0)
	if err != nil {
		return err
	}
	if err := patchIstiodArgs(kubeClient, enableIOR); err != nil {
		return err
	}
	if _, err := waitForIstiod(kubeClient, istiod.Generation); err != nil {
		return err
	}
	return nil
}

func DisableIOR(ctx resource.Context) error {
	kubeClient := ctx.Clusters().Default().Kube()
	istiod, err := waitForIstiod(kubeClient, 0)
	if err != nil {
		return err
	}
	if err := patchIstiodArgs(kubeClient, disableIOR); err != nil {
		return err
	}
	if _, err := waitForIstiod(kubeClient, istiod.Generation); err != nil {
		return err
	}
	return nil
}

func ApplyServiceMeshMemberRoll(ctx framework.TestContext, memberNamespaces ...string) error {
	memberRollYAML := `
apiVersion: maistra.io/v1
kind: ServiceMeshMemberRoll
metadata:
  name: default
spec:
  members:
`
	for _, ns := range memberNamespaces {
		memberRollYAML += fmt.Sprintf("  - %s\n", ns)
	}
	if err := retry.UntilSuccess(func() error {
		if err := ctx.ConfigIstio().YAML("istio-system", memberRollYAML).Apply(); err != nil {
			return fmt.Errorf("failed to apply SMMR resource: %s", err)
		}
		return nil
	}, retry.Timeout(10*time.Second), retry.Delay(time.Second)); err != nil {
		return err
	}

	if err := applyRolesToMemberNamespaces(ctx.Clusters().Default(), memberNamespaces...); err != nil {
		return err
	}
	return updateServiceMeshMemberRollStatus(ctx.Clusters().Default(), memberNamespaces...)
}

func updateServiceMeshMemberRollStatus(c cluster.Cluster, memberNamespaces ...string) error {
	client, err := maistrav1.NewForConfig(c.RESTConfig())
	if err != nil {
		return fmt.Errorf("failed to create client for maistra resources: %s", err)
	}

	return retry.UntilSuccess(func() error {
		smmr, err := client.ServiceMeshMemberRolls("istio-system").Get(context.TODO(), "default", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get SMMR default: %s", err)
		}
		smmr.Status.ConfiguredMembers = memberNamespaces
		_, err = client.ServiceMeshMemberRolls("istio-system").UpdateStatus(context.TODO(), smmr, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update SMMR default: %s", err)
		}
		return nil
	}, retry.Timeout(10*time.Second))
}

func applyRolesToMemberNamespaces(c cluster.Cluster, namespaces ...string) error {
	for _, ns := range namespaces {
		if err := c.ApplyYAMLFiles(
			ns,
			filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/role.yaml"),
			filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/rolebinding.yaml")); err != nil {
			return fmt.Errorf("failed to apply Roles and RoleBindings: %s", err)
		}
	}
	return nil
}

func waitForIstiod(kubeClient kubernetes.Interface, lastSeenGeneration int64) (*appsv1.Deployment, error) {
	var istiod *appsv1.Deployment
	err := retry.UntilSuccess(func() error {
		var err error
		istiod, err = kubeClient.AppsV1().Deployments("istio-system").Get(context.TODO(), "istiod", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get istiod deployment: %v", err)
		}
		if istiod.Status.ReadyReplicas != istiod.Status.Replicas {
			return fmt.Errorf("istiod deployment is not ready - %d of %d pods are ready", istiod.Status.ReadyReplicas, istiod.Status.Replicas)
		}
		if lastSeenGeneration != 0 && istiod.Status.ObservedGeneration == lastSeenGeneration {
			return fmt.Errorf("istiod deployment is not ready - Generation has not been updated")
		}
		return nil
	}, retry.Timeout(30*time.Second), retry.Delay(time.Second))
	if err != nil {
		return nil, err
	}
	return istiod, nil
}

func patchIstiodArgs(kubeClient kubernetes.Interface, patch string) error {
	return retry.UntilSuccess(func() error {
		_, err := kubeClient.AppsV1().Deployments("istio-system").
			Patch(context.TODO(), "istiod", types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("failed to patch istiod deployment: %v", err)
		}
		return nil
	}, retry.Timeout(10*time.Second), retry.Delay(time.Second))
}

func generateMaistraArguments(opts InstallationOptions) string {
	return fmt.Sprintf(`[
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/1",
		"value": "--memberRollName=default"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/2",
		"value": "--enableCRDScan=false"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/3",
		"value": "--enableNodeAccess=false"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/4",
		"value": "--enableIngressClassName=false"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "ENABLE_IOR",
			"value": "false"
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/2",
		"value": {
			"name": "PILOT_ENABLE_GATEWAY_API",
			"value": "%[1]t"
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/3",
		"value": {
			"name": "PILOT_ENABLE_GATEWAY_API_STATUS",
			"value": "%[1]t"
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/4",
		"value": {
			"name": "PILOT_ENABLE_GATEWAY_API_DEPLOYMENT_CONTROLLER",
			"value": "%[1]t"
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/5",
		"value": {
			"name": "PRIORITIZED_LEADER_ELECTION",
			"value": "false"
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/6",
		"value": {
			"name": "INJECTION_WEBHOOK_CONFIG_NAME",
			"value": ""
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/7",
		"value": {
			"name": "VALIDATION_WEBHOOK_CONFIG_NAME",
			"value": ""
		}
	}
]`, opts.EnableGatewayAPI)
}

const enableIOR = `[
	{
		"op": "replace",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "ENABLE_IOR",
			"value": "true"
		}
	}
]`

const disableIOR = `[
	{
		"op": "replace",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "ENABLE_IOR",
			"value": "false"
		}
	}
]`
