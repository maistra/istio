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

package maistra

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	servicemeshv1 "istio.io/istio/pkg/servicemesh/apis/servicemesh/v1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var i istio.Instance

const smmrName = "maistra-test"

const maistraValues = `values:
  pilot:
    env:
      MEMBER_ROLL_NAME: "maistra-test"
      ENABLE_CRD_SCAN: "false"
      DISABLE_NODE_ACCESS: "true"
      ENABLE_INGRESS_CLASS_NAME: "false"
      PILOT_ENABLED_SERVICE_APIS: "false"`

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// TODO: It might be better to disable the injection webhooks here to more
	// closely mimic a Maistra deployment, but that complicates deployments.
	framework.
		NewSuite(m).
		Setup(applyResources).
		Setup(waitForCRDs).
		Setup(createSMMR).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = maistraValues
		})).
		Run()
}

// applyResources applies the YAML in the testdata directory.
func applyResources(ctx resource.Context) error {
	resources := []string{"namespace.yaml", "crd.yaml", "rbac.yaml"}

	for _, file := range resources {
		yaml, err := ioutil.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			return err
		}

		if err := ctx.Config().ApplyYAML("", string(yaml)); err != nil {
			return err
		}
	}

	return nil
}

// waitForCRDs waits for the Maistra CRDs we need to become available.
func waitForCRDs(ctx resource.Context) error {
	return wait.PollImmediate(100*time.Millisecond, 10*time.Second, func() (bool, error) {
		crds := ctx.Clusters().Default().Ext().ApiextensionsV1beta1().CustomResourceDefinitions()
		smmr, err := crds.Get(context.TODO(), "servicemeshmemberrolls.maistra.io", metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		for _, cond := range smmr.Status.Conditions {
			if cond.Type == apiextv1beta1.Established && cond.Status == apiextv1beta1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})
}

// createSMMR creates the SMMR resource and updates the status.
func createSMMR(ctx resource.Context) error {
	namespace := istio.DefaultSystemNamespace
	maistra := ctx.Clusters().Default().ServiceMeshV1().MaistraV1()
	memberRolls := maistra.ServiceMeshMemberRolls(namespace)

	smmr := &servicemeshv1.ServiceMeshMemberRoll{
		ObjectMeta: metav1.ObjectMeta{
			Name:      smmrName,
			Namespace: namespace,
		},
	}

	_, err := memberRolls.Create(context.TODO(), smmr, metav1.CreateOptions{})
	return err
}

// updateSMMR sets the given namespaces as the configuired members on the SMMR.
func updateSMMR(ctx resource.Context, namespaces ...namespace.Instance) error {
	maistra := ctx.Clusters().Default().ServiceMeshV1().MaistraV1()
	memberRolls := maistra.ServiceMeshMemberRolls(istio.DefaultSystemNamespace)

	smmr, err := memberRolls.Get(context.TODO(), smmrName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	var members []string

	for _, ns := range namespaces {
		members = append(members, ns.Name())
	}

	smmr.Status.ConfiguredMembers = members
	_, err = memberRolls.UpdateStatus(context.TODO(), smmr, metav1.UpdateOptions{})
	return err
}

// enableMTLS enables mTLS in strict mode mesh-wide.
func enableMTLS(ctx resource.Context) error {
	mtls, err := ioutil.ReadFile(filepath.Join("testdata", "global-mtls.yaml"))
	if err != nil {
		return err
	}

	return ctx.Config().ApplyYAML("", string(mtls))
}

// disableMTLS disables mTLS in strict mode mesh-wide.
func disableMTLS(ctx resource.Context) error {
	mtls, err := ioutil.ReadFile(filepath.Join("testdata", "global-mtls.yaml"))
	if err != nil {
		return err
	}

	return ctx.Config().DeleteYAML("", string(mtls))
}
