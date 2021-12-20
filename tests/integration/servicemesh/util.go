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

package servicemesh

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"path"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	// import maistra CRD manifests
	_ "maistra.io/api/manifests"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	manifestsDir      = env.IstioSrc + "/vendor/maistra.io/api/manifests"
	bookinfoManifests = []string{
		env.IstioSrc + "/samples/bookinfo/platform/kube/bookinfo.yaml",
		env.IstioSrc + "/samples/bookinfo/networking/bookinfo-gateway.yaml",
	}
	sleepManifest = env.IstioSrc + "/samples/sleep/sleep.yaml"
	rnd           = rand.New(rand.NewSource(time.Now().UnixNano()))
)

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
				return err
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

func InstallBookinfo(ctx framework.TestContext, c cluster.Cluster, namespace string) {
	if err := c.ApplyYAMLFiles(namespace, bookinfoManifests...); err != nil {
		ctx.Fatal(err)
	}
	if err := retry.UntilSuccess(func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(c, namespace, "app=ratings")); err != nil {
			return fmt.Errorf("ratings pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(300*time.Second), retry.Delay(time.Second)); err != nil {
		ctx.Fatal(err)
	}
}

func InstallSleep(ctx framework.TestContext, c cluster.Cluster, namespace string) {
	if err := c.ApplyYAMLFiles(namespace, sleepManifest); err != nil {
		ctx.Fatal(err)
	}
	if err := retry.UntilSuccess(func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(c, namespace, "app=sleep")); err != nil {
			return fmt.Errorf("sleep pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(300*time.Second), retry.Delay(time.Second)); err != nil {
		ctx.Fatal(err)
	}
}

// TODO for some reason namespace.NewOrFail() doesn't work so I'm doing this manually
func CreateNamespace(ctx framework.TestContext, cluster cluster.Cluster, prefix string) string {
	ns, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%d", prefix, rnd.Intn(99999)),
			Labels: map[string]string{
				"istio-injection": "enabled",
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
	name := ns.Name
	ctx.Cleanup(func() {
		cluster.CoreV1().Namespaces().Delete(context.TODO(), name, metav1.DeleteOptions{})
	})
	return name
}
