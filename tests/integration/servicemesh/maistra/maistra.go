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
	"io/ioutil"
	"istio.io/istio/pkg/test/util/retry"
	v1 "k8s.io/api/apps/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"path"
	"strings"
	"time"

	// import maistra CRD manifests
	_ "maistra.io/api/manifests"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
)

var manifestsDir = env.IstioSrc + "/vendor/maistra.io/api/manifests"

func ApplyServiceMeshCRDs(ctx resource.Context) (err error) {
	crds, err := findCRDs()
	if err != nil {
		return fmt.Errorf("cannot read maistra CRD YAMLs: %s", err)
	}
	for _, cluster := range ctx.Clusters().Kube().Primaries() {
		for _, crd := range crds {
			// we need to manually Create() the CRD because Apply() wants to write its content into an annotation which fails because of size limitations
			rawYAML, err := ioutil.ReadFile(crd)
			if err != nil {
				return err
			}
			rawJSON, err := yaml.YAMLToJSON(rawYAML)
			if err != nil {
				return err
			}
			crd := apiextv1.CustomResourceDefinition{}
			_, _, err = unstructured.UnstructuredJSONScheme.Decode(rawJSON, nil, &crd)
			if err != nil {
				return err
			}
			if _, err := cluster.Ext().ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), &crd, metav1.CreateOptions{}); err != nil {
				if !errors.IsAlreadyExists(err) {
					return err
				}
			}
		}
	}
	return err
}

func findCRDs() (list []string, err error) {
	list = []string{}
	files, err := ioutil.ReadDir(manifestsDir)
	if err != nil {
		return
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".yaml") {
			list = append(list, path.Join(manifestsDir, file.Name()))
		}
	}
	return
}

func Install(ctx resource.Context) error {
	kubeClient := ctx.Clusters().Default().Kube()
	istiod, err := waitForIstiod(kubeClient, 0)
	if err != nil {
		return err
	}
	if err := patchIstiodArgs(kubeClient); err != nil {
		return err
	}
	if _, err := waitForIstiod(kubeClient, istiod.Generation); err != nil {
		return err
	}
	return nil
}

func waitForIstiod(kubeClient kubernetes.Interface, lastSeenGeneration int64) (*v1.Deployment, error) {
	var istiod *v1.Deployment
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

func patchIstiodArgs(kubeClient kubernetes.Interface) error {
	patch := `[
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/args/1",
		"value": "--memberRollName=default"
	},
	{
		"op": "add",
		"path": "/spec/template/spec/containers/0/env/1",
		"value": {
			"name": "PRIORITIZED_LEADER_ELECTION",
			"value": "false"
		}
	}
]`
	return retry.UntilSuccess(func() error {
		_, err := kubeClient.AppsV1().Deployments("istio-system").
			Patch(context.TODO(), "istiod", types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("failed to patch istiod deployment: %v", err)
		}
		return nil
	}, retry.Timeout(10*time.Second), retry.Delay(time.Second))
}
