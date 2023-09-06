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
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	clusterRoles = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/clusterrole.yaml")
	roles        = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/role.yaml")
	roleBindings = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/rolebinding.yaml")
	smmrTmpl     = filepath.Join(env.IstioSrc, "tests/integration/servicemesh/maistra/testdata/smmr.tmpl.yaml")
)

type InstallationOptions struct {
	EnableGatewayAPI bool
	OutboundAllowAny bool
}

func ApplyServiceMeshCRDs(ctx resource.Context) error {
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
	}
	return err
}

func ApplyGatewayAPICRDs(ctx resource.Context) error {
	for _, c := range ctx.Clusters() {
		if err := c.ApplyYAMLFiles(
			"", filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/gateway-api-crd.yaml"),
		); err != nil {
			return err
		}
	}
	return nil
}

func Install(istioNs namespace.Getter, opts *InstallationOptions) resource.SetupFn {
	enableGatewayAPI := false
	outboundTrafficPolicyMode := "REGISTRY_ONLY"
	if opts != nil {
		enableGatewayAPI = opts.EnableGatewayAPI
		if opts.OutboundAllowAny {
			outboundTrafficPolicyMode = "ALLOW_ANY"
		}
	}
	return istio.Setup(nil, func(ctx resource.Context, cfg *istio.Config) {
		ctx.Settings().SkipWorkloadClasses = append(ctx.Settings().SkipWorkloadClasses, echo.Delta, echo.Headless, echo.TProxy, echo.VM, echo.External)
		ctx.Settings().SkipDelta = true
		ctx.Settings().SkipTProxy = true
		ctx.Settings().SkipVM = true

		cfg.SystemNamespace = istioNs.Get().Name()
		cfg.Values["global.istioNamespace"] = istioNs.Get().Name()
		cfg.ControlPlaneValues = fmt.Sprintf(`
namespace: %[1]s
revision: %[2]s
meshConfig:
  outboundTrafficPolicy:
    mode: %[3]s
components:
  pilot:
    k8s:
      overlays:
      - apiVersion: apps/v1
        kind: Deployment
        name: istiod-%[2]s
        patches:
        - path: spec.template.spec.containers.[name:discovery].args[-1]
          value: "--memberRollName=default"
        - path: spec.template.spec.containers.[name:discovery].args[-1]
          value: "--enableCRDScan=false"
        - path: spec.template.spec.containers.[name:discovery].args[-1]
          value: "--enableNodeAccess=false"
        - path: spec.template.spec.containers.[name:discovery].args[-1]
          value: "--enableIngressClassName=false"
values:
  global:
    istioNamespace: %[1]s
  pilot:
    env:
      PILOT_ENABLE_GATEWAY_API: %[4]t
      PILOT_ENABLE_GATEWAY_API_STATUS: %[4]t
      PILOT_ENABLE_GATEWAY_API_DEPLOYMENT_CONTROLLER: %[4]t
      PRIORITIZED_LEADER_ELECTION: false
`, istioNs.Get().Name(), istioNs.Get().Prefix(), outboundTrafficPolicyMode, enableGatewayAPI)
	})
}

func RemoveDefaultRBAC(ctx resource.Context) error {
	kubeClient := ctx.Clusters().Default().Kube()
	if err := kubeClient.RbacV1().ClusterRoleBindings().DeleteCollection(
		context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "app=istio-reader"}); err != nil {
		return err
	}
	if err := kubeClient.RbacV1().ClusterRoles().DeleteCollection(
		context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "app=istio-reader"}); err != nil {
		return err
	}
	if err := kubeClient.RbacV1().ClusterRoleBindings().DeleteCollection(
		context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "app=istiod"}); err != nil {
		return err
	}
	if err := kubeClient.RbacV1().ClusterRoles().DeleteCollection(
		context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "app=istiod"}); err != nil {
		return err
	}
	return nil
}

func ApplyRestrictedRBAC(istioNs namespace.Getter) resource.SetupFn {
	return func(ctx resource.Context) error {
		values := map[string]string{
			"istioNamespace": istioNs.Get().Name(),
			"revision":       istioNs.Get().Prefix(),
		}
		if err := ctx.ConfigIstio().EvalFile(istioNs.Get().Name(), values, clusterRoles).Apply(); err != nil {
			return err
		}
		if err := applyRolesToMemberNamespaces(ctx.ConfigIstio(), values, istioNs.Get().Name()); err != nil {
			return err
		}
		return nil
	}
}

func DisableWebhooksAndRestart(istioNs namespace.Getter) resource.SetupFn {
	return func(ctx resource.Context) error {
		kubeClient := ctx.Clusters().Default().Kube()
		var lastSeenGeneration int64
		if err := waitForIstiod(kubeClient, istioNs.Get(), &lastSeenGeneration); err != nil {
			return err
		}
		if err := patchIstiodArgs(kubeClient, istioNs.Get(), disableWebhookPatch); err != nil {
			return err
		}
		if err := waitForIstiod(kubeClient, istioNs.Get(), &lastSeenGeneration); err != nil {
			return err
		}
		return nil
	}
}

func waitForIstiod(kubeClient kubernetes.Interface, istioNs namespace.Instance, lastSeenGeneration *int64) error {
	err := retry.UntilSuccess(func() error {
		istiod, err := kubeClient.AppsV1().Deployments(istioNs.Name()).Get(context.TODO(), "istiod-"+istioNs.Prefix(), metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get istiod deployment: %v", err)
		}
		if istiod.Status.ReadyReplicas != istiod.Status.Replicas {
			return fmt.Errorf("istiod deployment is not ready - %d of %d pods are ready", istiod.Status.ReadyReplicas, istiod.Status.Replicas)
		}
		if *lastSeenGeneration != 0 && istiod.Status.ObservedGeneration == *lastSeenGeneration {
			return fmt.Errorf("istiod deployment is not ready - Generation has not been updated")
		}
		*lastSeenGeneration = istiod.Status.ObservedGeneration
		return nil
	}, retry.Timeout(30*time.Second), retry.Delay(time.Second))
	return err
}

func patchIstiodArgs(kubeClient kubernetes.Interface, istioNs namespace.Instance, patch string) error {
	return retry.UntilSuccess(func() error {
		_, err := kubeClient.AppsV1().Deployments(istioNs.Name()).
			Patch(context.TODO(), "istiod-"+istioNs.Prefix(), types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("failed to patch istiod deployment: %v", err)
		}
		return nil
	}, retry.Timeout(10*time.Second), retry.Delay(time.Second))
}

func ApplyServiceMeshMemberRoll(ctx framework.TestContext, istioNs namespace.Instance, memberNamespaces ...string) error {
	smmrValues := map[string][]string{"members": memberNamespaces}
	if err := retry.UntilSuccess(func() error {
		if err := ctx.ConfigIstio().EvalFile(istioNs.Name(), smmrValues, smmrTmpl).Apply(apply.NoCleanup); err != nil {
			return fmt.Errorf("failed to apply SMMR resource: %s", err)
		}
		return nil
	}, retry.Timeout(10*time.Second), retry.Delay(time.Second)); err != nil {
		return err
	}

	roleValues := map[string]string{
		"istioNamespace": istioNs.Name(),
		"revision":       istioNs.Prefix(),
	}
	if err := applyRolesToMemberNamespaces(ctx.ConfigIstio(), roleValues, memberNamespaces...); err != nil {
		return err
	}
	return updateServiceMeshMemberRollStatus(ctx.Clusters().Default(), istioNs.Name(), memberNamespaces...)
}

func EnableIOR(ctx resource.Context, ns namespace.Instance) error {
	kubeClient := ctx.Clusters().Default().Kube()
	var lastSeenGeneration int64
	if err := waitForIstiod(kubeClient, ns, &lastSeenGeneration); err != nil {
		return err
	}
	if err := patchIstiodArgs(kubeClient, ns, enableIORPatch); err != nil {
		return err
	}
	if err := waitForIstiod(kubeClient, ns, &lastSeenGeneration); err != nil {
		return err
	}
	return nil
}

func DisableIOR(ctx resource.Context, ns namespace.Instance) error {
	kubeClient := ctx.Clusters().Default().Kube()
	var lastSeenGeneration int64
	if err := waitForIstiod(kubeClient, ns, &lastSeenGeneration); err != nil {
		return err
	}
	if err := patchIstiodArgs(kubeClient, ns, disableIORPatch); err != nil {
		return err
	}
	if err := waitForIstiod(kubeClient, ns, &lastSeenGeneration); err != nil {
		return err
	}
	return nil
}

func updateServiceMeshMemberRollStatus(c cluster.Cluster, istioNamespace string, memberNamespaces ...string) error {
	client, err := maistrav1.NewForConfig(c.RESTConfig())
	if err != nil {
		return fmt.Errorf("failed to create client for maistra resources: %s", err)
	}

	return retry.UntilSuccess(func() error {
		smmr, err := client.ServiceMeshMemberRolls(istioNamespace).Get(context.TODO(), "default", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get SMMR default: %s", err)
		}
		smmr.Status.ConfiguredMembers = memberNamespaces
		_, err = client.ServiceMeshMemberRolls(istioNamespace).UpdateStatus(context.TODO(), smmr, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update SMMR default: %s", err)
		}
		return nil
	}, retry.Timeout(10*time.Second))
}

func applyRolesToMemberNamespaces(c config.Factory, values map[string]string, namespaces ...string) error {
	for _, ns := range namespaces {
		if err := c.EvalFile(ns, values, roles, roleBindings).Apply(apply.NoCleanup); err != nil {
			return fmt.Errorf("failed to apply Roles: %s", err)
		}
	}
	return nil
}

const disableWebhookPatch = `[
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "INJECTION_WEBHOOK_CONFIG_NAME",
			"value": ""
		}
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/2",
		"value": {
			"name": "VALIDATION_WEBHOOK_CONFIG_NAME",
			"value": ""
		}
	}
]`

const enableIORPatch = `[
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "ENABLE_IOR",
			"value": "true"
		}
	}
]`

const disableIORPatch = `[
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "ENABLE_IOR",
			"value": "false"
		}
	}
]`
