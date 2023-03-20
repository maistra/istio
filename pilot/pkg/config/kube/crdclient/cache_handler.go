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

package crdclient

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/pkg/log"
)

// cacheHandler abstracts the logic of an informer with a set of handlers. Handlers can be added at runtime
// and will be invoked on each informer event.
type cacheHandler struct {
	client   *Client
	informer kclient.Untyped
	schema   resource.Schema
}

func (h *cacheHandler) onEvent(old any, curr any, event model.Event) error {
	currItem := controllers.ExtractObject(curr)
	if currItem == nil {
		return nil
	}

	currConfig := TranslateObject(currItem, h.schema.GroupVersionKind(), h.client.domainSuffix)

	var oldConfig config.Config
	if old != nil {
		oldItem, ok := old.(runtime.Object)
		if !ok {
			log.Warnf("Old Object can not be converted to runtime Object %v, is type %T", old, old)
			return nil
		}
		oldConfig = TranslateObject(oldItem, h.schema.GroupVersionKind(), h.client.domainSuffix)
	}

	if h.client.objectInRevision(&currConfig) {
		h.callHandlers(oldConfig, currConfig, event)
		return nil
	}

	// Check if the object was in our revision, but has been moved to a different revision. If so,
	// it has been effectively deleted from our revision, so process it as a delete event.
	if event == model.EventUpdate && old != nil && h.client.objectInRevision(&oldConfig) {
		log.Debugf("Object %s/%s has been moved to a different revision, deleting",
			currConfig.Namespace, currConfig.Name)
		h.callHandlers(oldConfig, currConfig, model.EventDelete)
		return nil
	}

	log.Debugf("Skipping event %s for object %s/%s from different revision",
		event, currConfig.Namespace, currConfig.Name)
	return nil
}

func (h *cacheHandler) callHandlers(old config.Config, curr config.Config, event model.Event) {
	// TODO we may consider passing a pointer to handlers instead of the value. While spec is a pointer, the meta will be copied
	for _, f := range h.client.handlers[h.schema.GroupVersionKind()] {
		f(old, curr, event)
	}
}

func createCacheHandler(cl *Client, schema resource.Schema, i informers.GenericInformer) *cacheHandler {
	scope.Debugf("registered CRD %v", schema.GroupVersionKind())
	h := &cacheHandler{
		client:   cl,
		schema:   schema,
		informer: kclient.NewUntyped(cl.client, i.Informer(), kclient.Filter{ObjectFilter: cl.namespacesFilter}),
	}

	kind := schema.Kind()
	h.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			incrementEvent(kind, "add")
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventAdd)
			})
		},
		UpdateFunc: func(old, cur any) {
			incrementEvent(kind, "update")
			cl.queue.Push(func() error {
				return h.onEvent(old, cur, model.EventUpdate)
			})
		},
		DeleteFunc: func(obj any) {
			incrementEvent(kind, "delete")
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventDelete)
			})
		},
	})
	return h
}
