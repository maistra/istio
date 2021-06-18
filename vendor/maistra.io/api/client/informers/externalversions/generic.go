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

// Code generated by informer-gen. DO NOT EDIT.

package externalversions

import (
	"fmt"

	schema "k8s.io/apimachinery/pkg/runtime/schema"
	cache "k8s.io/client-go/tools/cache"
	v1 "maistra.io/api/core/v1"
	v1alpha1 "maistra.io/api/core/v1alpha1"
	v2 "maistra.io/api/core/v2"
)

// GenericInformer is type of SharedIndexInformer which will locate and delegate to other
// sharedInformers based on type
type GenericInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() cache.GenericLister
}

type genericInformer struct {
	informer cache.SharedIndexInformer
	resource schema.GroupResource
}

// Informer returns the SharedIndexInformer.
func (f *genericInformer) Informer() cache.SharedIndexInformer {
	return f.informer
}

// Lister returns the GenericLister.
func (f *genericInformer) Lister() cache.GenericLister {
	return cache.NewGenericLister(f.Informer().GetIndexer(), f.resource)
}

// ForResource gives generic access to a shared informer of the matching type
// TODO extend this to unknown resources with a client pool
func (f *sharedInformerFactory) ForResource(resource schema.GroupVersionResource) (GenericInformer, error) {
	switch resource {
	// Group=core, Version=v1
	case v1.SchemeGroupVersion.WithResource("meshfederations"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().MeshFederations().Informer()}, nil
	case v1.SchemeGroupVersion.WithResource("serviceexports"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().ServiceExports().Informer()}, nil
	case v1.SchemeGroupVersion.WithResource("serviceimports"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().ServiceImports().Informer()}, nil
	case v1.SchemeGroupVersion.WithResource("servicemeshcontrolplanes"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().ServiceMeshControlPlanes().Informer()}, nil
	case v1.SchemeGroupVersion.WithResource("servicemeshextensions"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().ServiceMeshExtensions().Informer()}, nil
	case v1.SchemeGroupVersion.WithResource("servicemeshmembers"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().ServiceMeshMembers().Informer()}, nil
	case v1.SchemeGroupVersion.WithResource("servicemeshmemberrolls"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1().ServiceMeshMemberRolls().Informer()}, nil

		// Group=core, Version=v1alpha1
	case v1alpha1.SchemeGroupVersion.WithResource("servicemeshextensions"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V1alpha1().ServiceMeshExtensions().Informer()}, nil

		// Group=core, Version=v2
	case v2.SchemeGroupVersion.WithResource("servicemeshcontrolplanes"):
		return &genericInformer{resource: resource.GroupResource(), informer: f.Core().V2().ServiceMeshControlPlanes().Informer()}, nil

	}

	return nil, fmt.Errorf("no informer found for %v", resource)
}
