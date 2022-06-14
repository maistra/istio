/*
Copyright Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by xns-informer-gen. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "github.com/maistra/xns-informer/pkg/generated/istio/internalinterfaces"
	informers "github.com/maistra/xns-informer/pkg/informers"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// WasmPlugins returns a WasmPluginInformer.
	WasmPlugins() WasmPluginInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespaces       informers.NamespaceSet
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespaces informers.NamespaceSet, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespaces: namespaces, tweakListOptions: tweakListOptions}
}

// WasmPlugins returns a WasmPluginInformer.
func (v *version) WasmPlugins() WasmPluginInformer {
	return &wasmPluginInformer{factory: v.factory, namespaces: v.namespaces, tweakListOptions: v.tweakListOptions}
}
