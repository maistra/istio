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

package ingress

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

var (
	pod           = "test"
	serviceIP     = "1.2.3.4"
	hostname      = "foo.bar.com"
	nodeIP        = "10.0.0.2"
	testNamespace = "test"
)

func setupFake(t *testing.T, client kubelib.Client) {
	t.Helper()
	if _, err := client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingressgateway",
			Namespace: "istio-system",
			Labels: map[string]string{
				"istio": "ingressgateway",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "foo_node",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Kube().CoreV1().Services(testNamespace).Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingress",
			Namespace: testNamespace,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					IP: serviceIP,
				}},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Kube().CoreV1().Services(testNamespace).Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingress-hostname",
			Namespace: testNamespace,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					Hostname: hostname,
				}},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Kube().CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo_node",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeExternalIP,
					Address: nodeIP,
				},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func fakeMeshHolder(ingressService string) mesh.Holder {
	config := mesh.DefaultMeshConfig()
	config.IngressService = ingressService
	return mesh.NewFixedWatcher(config)
}

func makeStatusSyncer(t *testing.T) *StatusSyncer {
	setEnvs(t, map[string]string{"POD_NAME": pod, "POD_NAMESPACE": testNamespace})

	client := kubelib.NewFakeClient()
	setupFake(t, client)
	sync := NewStatusSyncer(fakeMeshHolder("istio-ingress"), client, kubecontroller.Options{})
	client.RunAndWait(test.NewStop(t))
	return sync
}

// setEnvs set the envs with given value.
func setEnvs(t *testing.T, inputs map[string]string) {
	for k, v := range inputs {
		t.Setenv(k, v)
	}
}

func TestRunningAddresses(t *testing.T) {
	t.Run("service", testRunningAddressesWithService)
	t.Run("hostname", testRunningAddressesWithHostname)
}

func testRunningAddressesWithService(t *testing.T) {
	syncer := makeStatusSyncer(t)
	address, err := syncer.runningAddresses(testNamespace)
	if err != nil {
		t.Fatal(err)
	}

	if len(address) != 1 || address[0] != serviceIP {
		t.Errorf("Address is not correctly set to service ip")
	}
}

func testRunningAddressesWithHostname(t *testing.T) {
	syncer := makeStatusSyncer(t)
	syncer.meshHolder = fakeMeshHolder("istio-ingress-hostname")

	address, err := syncer.runningAddresses(testNamespace)
	if err != nil {
		t.Fatal(err)
	}

	if len(address) != 1 || address[0] != hostname {
		t.Errorf("Address is not correctly set to hostname")
	}
}

func TestRunningAddressesWithPod(t *testing.T) {
	syncer := makeStatusSyncer(t)
	syncer.meshHolder = fakeMeshHolder("")

	address, err := syncer.runningAddresses(ingressNamespace)
	if err != nil {
		t.Fatal(err)
	}

	if len(address) != 1 || address[0] != nodeIP {
		t.Errorf("Address is not correctly set to node ip %v %v", address, nodeIP)
	}
}
