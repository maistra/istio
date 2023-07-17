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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndex_ServiceEntry(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, testC, testNW)

	// test code path where service entry creates a workload entry via `ServiceEntry.endpoints`
	// and the inlined WE has a port override
	s.addServiceEntry(t, "se.istio.io", []string{"240.240.23.45"}, "name1", testNS, nil)
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1")
	s.assertEvent(t, s.seIPXdsName("name1", "127.0.0.1"), "ns1/se.istio.io")
	assert.Equal(t, len(s.controller.ambientIndex.(*AmbientIndexImpl).byWorkloadEntry), 1)
	assert.Equal(t, s.lookup(s.addrXdsName("127.0.0.1")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.seIPXdsName("name1", "127.0.0.1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("127.0.0.1")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "name1",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services: map[string]*workloadapi.PortList{
						"ns1/se.istio.io": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8081, // port is overidden by inlined WE port
								},
							},
						},
					},
				},
			},
		},
	}})

	s.deleteServiceEntry(t, "name1", testNS)
	assert.Equal(t, len(s.controller.ambientIndex.(*AmbientIndexImpl).byWorkloadEntry), 0)
	assert.Equal(t, s.lookup(s.addrXdsName("127.0.0.1")), nil)
	s.clearEvents()

	// test code path where service entry selects workloads via `ServiceEntry.workloadSelector`
	s.addPods(t, "140.140.0.10", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "140.140.0.11", "pod2", "sa1", map[string]string{"app": "other"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2")
	s.addWorkloadEntries(t, "240.240.34.56", "name1", "sa1", map[string]string{"app": "a"})
	s.assertEvent(t, s.wleXdsName("name1"))
	s.addWorkloadEntries(t, "240.240.34.57", "name2", "sa1", map[string]string{"app": "other"})
	s.assertEvent(t, s.wleXdsName("name2"))
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")

	// a service entry should not be able to select across namespaces
	s.addServiceEntry(t, "mismatched.istio.io", []string{"240.240.23.45"}, "name1", "mismatched-ns", map[string]string{"app": "a"})
	s.assertEvent(t, "mismatched-ns/mismatched.istio.io")
	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           testNW,
					ClusterId:         testC,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					Services:          nil, // should not be selected by the mismatched service entry
				},
			},
		},
	}})
	assert.Equal(t, s.lookup(s.addrXdsName("240.240.34.56")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services:          nil, // should not be selected by the mismatched service entry
				},
			},
		},
	}})

	s.addServiceEntry(t, "se.istio.io", []string{"240.240.23.45"}, "name1", testNS, map[string]string{"app": "a"})
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")
	// we should see an update for the workloads selected by the service entry
	// do not expect event for pod2 since it is not selected by the service entry
	s.assertEvent(t, s.podXdsName("pod1"), s.wleXdsName("name1"), "ns1/se.istio.io")

	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           testNW,
					ClusterId:         testC,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					Services: map[string]*workloadapi.PortList{
						"ns1/se.istio.io": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8080,
								},
							},
						},
					},
				},
			},
		},
	}})

	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.11")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod2"),
					Name:              "pod2",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.11")},
					Node:              "node1",
					Network:           testNW,
					ClusterId:         testC,
					CanonicalName:     "other",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod2",
					Services:          nil, // labels don't match workloadSelector, this should be nil
				},
			},
		},
	}})

	assert.Equal(t, s.lookup(s.addrXdsName("240.240.34.56")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services: map[string]*workloadapi.PortList{
						"ns1/se.istio.io": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8080,
								},
							},
						},
					},
				},
			},
		},
	}})

	s.deleteServiceEntry(t, "name1", testNS)
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")
	// we should see an update for the workloads selected by the service entry
	s.assertEvent(t, s.podXdsName("pod1"), s.wleXdsName("name1"), "ns1/se.istio.io")
	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           testNW,
					ClusterId:         testC,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					Services:          nil, // vips for pod1 should be gone now
				},
			},
		},
	}})

	assert.Equal(t, s.lookup(s.addrXdsName("240.240.34.56")), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services:          nil, // vips for workload entry 1 should be gone now
				},
			},
		},
	}})
}
