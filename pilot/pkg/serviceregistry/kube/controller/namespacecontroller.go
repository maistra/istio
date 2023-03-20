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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/security/pkg/k8s"
)

const (
	// CACertNamespaceConfigMap is the name of the ConfigMap in each namespace storing the root cert of non-Kube CA.
	CACertNamespaceConfigMap = "istio-ca-root-cert"
)

var configMapLabel = map[string]string{"istio.io/config": "true"}

// NamespaceController manages reconciles a configmap in each namespace with a desired set of data.
type NamespaceController struct {
	caBundleWatcher *keycertbundle.Watcher

	queue controllers.Queue

	namespaces kclient.Client[*v1.Namespace]
	configmaps kclient.Client[*v1.ConfigMap]

	// if meshConfig.DiscoverySelectors specified, DiscoveryNamespacesFilter tracks the namespaces to be watched by this controller.
	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(kubeClient kube.Client, caBundleWatcher *keycertbundle.Watcher,
	discoveryNamespacesFilter filter.DiscoveryNamespacesFilter,
) *NamespaceController {
	c := &NamespaceController{
		caBundleWatcher:           caBundleWatcher,
		DiscoveryNamespacesFilter: discoveryNamespacesFilter,
	}
	c.queue = controllers.NewQueue("namespace controller", controllers.WithReconciler(c.insertDataForNamespace))

	c.configmaps = kclient.New[*v1.ConfigMap](kubeClient)
	c.namespaces = kclient.New[*v1.Namespace](kubeClient)

	c.configmaps.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
		if o.GetName() != CACertNamespaceConfigMap {
			// This is a change to a configmap we don't watch, ignore it
			return false
		}
		if inject.IgnoredNamespaces.Contains(o.GetNamespace()) {
			// skip special kubernetes system namespaces
			return false
		}
		if c.DiscoveryNamespacesFilter != nil && !c.DiscoveryNamespacesFilter.Filter(o) {
			// This is a change to a configmap we don't watch, ignore it
			return false
		}
		return true
	}))
	c.namespaces.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
		if inject.IgnoredNamespaces.Contains(o.GetName()) {
			// skip special kubernetes system namespaces
			return false
		}
		if c.DiscoveryNamespacesFilter != nil && !c.DiscoveryNamespacesFilter.FilterNamespace(o.(*v1.Namespace).ObjectMeta) {
			// This is a change to a namespace we don't watch, ignore it
			return false
		}
		return true
	}))

	return c
}

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync(stopCh, nc.namespaces.HasSynced, nc.configmaps.HasSynced) {
		log.Error("Failed to sync namespace controller cache")
		return
	}
	go nc.startCaBundleWatcher(stopCh)
	nc.queue.Run(stopCh)
	nc.namespaces.ShutdownHandlers()
	nc.configmaps.ShutdownHandlers()
}

// startCaBundleWatcher listens for updates to the CA bundle and update cm in each namespace
func (nc *NamespaceController) startCaBundleWatcher(stop <-chan struct{}) {
	id, watchCh := nc.caBundleWatcher.AddWatcher()
	defer nc.caBundleWatcher.RemoveWatcher(id)
	for {
		select {
		case <-watchCh:
			for _, ns := range nc.namespaces.List("", labels.Everything()) {
				nc.namespaceChange(ns)
			}
		case <-stop:
			return
		}
	}
}

// insertDataForNamespace will add data into the configmap for the specified namespace
// If the configmap is not found, it will be created.
// If you know the current contents of the configmap, using UpdateDataInConfigMap is more efficient.
func (nc *NamespaceController) insertDataForNamespace(o types.NamespacedName) error {
	ns := o.Namespace
	if ns == "" {
		// For Namespace object, it will not have o.Namespace field set
		ns = o.Name
	}
	meta := metav1.ObjectMeta{
		Name:      CACertNamespaceConfigMap,
		Namespace: ns,
		Labels:    configMapLabel,
	}
	return k8s.InsertDataToConfigMap(nc.configmaps, meta, nc.caBundleWatcher.GetCABundle())
}

// On namespace change, update the config map.
// If terminating, this will be skipped
func (nc *NamespaceController) namespaceChange(ns *v1.Namespace) {
	if ns.Status.Phase != v1.NamespaceTerminating {
		nc.syncNamespace(ns)
	}
}

func (nc *NamespaceController) syncNamespace(ns *v1.Namespace) {
	// skip special kubernetes system namespaces
	if inject.IgnoredNamespaces.Contains(ns.Name) {
		return
	}
	// skip namespaces we don't watch
	if nc.DiscoveryNamespacesFilter != nil && !nc.DiscoveryNamespacesFilter.FilterNamespace(ns.ObjectMeta) {
		return
	}
	nc.queue.Add(types.NamespacedName{Name: ns.Name})
}
