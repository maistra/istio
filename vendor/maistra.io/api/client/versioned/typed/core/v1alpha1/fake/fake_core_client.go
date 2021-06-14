// Copyright Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
	v1alpha1 "maistra.io/api/client/versioned/typed/core/v1alpha1"
)

type FakeCoreV1alpha1 struct {
	*testing.Fake
}

func (c *FakeCoreV1alpha1) FederationStatuses(namespace string) v1alpha1.FederationStatusInterface {
	return &FakeFederationStatuses{c, namespace}
}

func (c *FakeCoreV1alpha1) MeshFederations(namespace string) v1alpha1.MeshFederationInterface {
	return &FakeMeshFederations{c, namespace}
}

func (c *FakeCoreV1alpha1) ServiceExports(namespace string) v1alpha1.ServiceExportsInterface {
	return &FakeServiceExports{c, namespace}
}

func (c *FakeCoreV1alpha1) ServiceImports(namespace string) v1alpha1.ServiceImportsInterface {
	return &FakeServiceImports{c, namespace}
}

func (c *FakeCoreV1alpha1) ServiceMeshExtensions(namespace string) v1alpha1.ServiceMeshExtensionInterface {
	return &FakeServiceMeshExtensions{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeCoreV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
