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

package revisions

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/util/sets"
)

// TagWatcher keeps track of the current tags and can notify watchers
// when the tags change.
type TagWatcher interface {
	Run(stopCh <-chan struct{})
	HasSynced() bool
	AddHandler(handler TagHandler)
	GetMyTags() sets.Set[string]
}

// TagHandler is a callback for when the tags revision change.
type TagHandler func(sets.Set[string])

type tagWatcher struct {
	revision string
	handlers []TagHandler

	queue    controllers.Queue
	webhooks kclient.Client[*admissionregistrationv1.MutatingWebhookConfiguration]
	index    *kclient.Index[string, *admissionregistrationv1.MutatingWebhookConfiguration]
}

func NewTagWatcher(client kube.Client, revision string) TagWatcher {
	p := &tagWatcher{
		revision: revision,
	}
	p.queue = controllers.NewQueue("tag", controllers.WithReconciler(func(key types.NamespacedName) error {
		p.notifyHandlers()
		return nil
	}))
	p.webhooks = kclient.NewFiltered[*admissionregistrationv1.MutatingWebhookConfiguration](client, kubetypes.Filter{
		ObjectFilter: isTagWebhook,
	})
	p.index = kclient.CreateIndexWithDelegate[string, *admissionregistrationv1.MutatingWebhookConfiguration](p.webhooks,
		func(o *admissionregistrationv1.MutatingWebhookConfiguration) []string {
			rev := o.GetLabels()[label.IoIstioRev.Name]
			if rev == "" {
				return nil
			}
			return []string{rev}
		}, controllers.ObjectHandler(p.queue.AddObject))
	return p
}

func (p *tagWatcher) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("tag watcher", stopCh, p.webhooks.HasSynced) {
		return
	}
	// Notify handlers of initial state
	p.notifyHandlers()
	p.queue.Run(stopCh)
}

// AddHandler registers a new handler for updates to tag changes.
func (p *tagWatcher) AddHandler(handler TagHandler) {
	p.handlers = append(p.handlers, handler)
}

func (p *tagWatcher) HasSynced() bool {
	return p.queue.HasSynced()
}

func (p *tagWatcher) GetMyTags() sets.String {
	res := sets.New(p.revision)
	for _, wh := range p.index.Lookup(p.revision) {
		res.Insert(wh.GetLabels()[IstioTagLabel])
	}
	return res
}

// notifyHandlers notifies all registered handlers on tag change.
func (p *tagWatcher) notifyHandlers() {
	myTags := p.GetMyTags()
	for _, handler := range p.handlers {
		handler(myTags)
	}
}

func isTagWebhook(uobj any) bool {
	obj, ok := uobj.(controllers.Object)
	if !ok {
		return false
	}
	_, ok = obj.GetLabels()[IstioTagLabel]
	return ok
}

const IstioTagLabel = "istio.io/tag"
