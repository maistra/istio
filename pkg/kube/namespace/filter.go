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

package namespace

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

// DiscoveryNamespacesFilter tracks the set of namespaces selected for discovery, which are updated by the discovery namespace controller.
// It exposes a filter function used for filtering out objects that don't reside in namespaces selected for discovery.
type DiscoveryNamespacesFilter interface {
	// Filter returns true if the input object resides in a namespace selected for discovery
	Filter(obj any) bool
	// FilterNamespace returns true if the input namespace is a namespace selected for discovery
	FilterNamespace(nsMeta metav1.ObjectMeta) bool
	// SelectorsChanged is invoked when meshConfig's discoverySelectors change, returns any newly selected namespaces and deselected namespaces
	SelectorsChanged(discoverySelectors []*metav1.LabelSelector) (selectedNamespaces []string, deselectedNamespaces []string)
	// SyncNamespaces is invoked when namespace informer hasSynced before other controller SyncAll
	SyncNamespaces() error
	// NamespaceCreated returns true if the created namespace is selected for discovery
	NamespaceCreated(ns metav1.ObjectMeta) (membershipChanged bool)
	// NamespaceUpdated : membershipChanged will be true if the updated namespace is newly selected or deselected for discovery
	NamespaceUpdated(oldNs, newNs metav1.ObjectMeta) (membershipChanged bool, namespaceAdded bool)
	// NamespaceDeleted returns true if the deleted namespace was selected for discovery
	NamespaceDeleted(ns metav1.ObjectMeta) (membershipChanged bool)
	// GetMembers returns the namespaces selected for discovery
	GetMembers() sets.String
}

type discoveryNamespacesFilter struct {
	lock                sync.RWMutex
	namespaces          kclient.Client[*corev1.Namespace]
	discoveryNamespaces sets.String
	discoverySelectors  []labels.Selector // nil if discovery selectors are not specified, permits all namespaces for discovery
}

func NewDiscoveryNamespacesFilter(
	namespaces kclient.Client[*corev1.Namespace],
	discoverySelectors []*metav1.LabelSelector,
) DiscoveryNamespacesFilter {
	discoveryNamespacesFilter := &discoveryNamespacesFilter{
		namespaces: namespaces,
	}

	// initialize discovery namespaces filter
	discoveryNamespacesFilter.SelectorsChanged(discoverySelectors)

	return discoveryNamespacesFilter
}

func (d *discoveryNamespacesFilter) Filter(obj any) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
	// permit all objects if discovery selectors are not specified
	if len(d.discoverySelectors) == 0 {
		return true
	}

	// When an object is deleted, obj could be a DeletionFinalStateUnknown marker item.
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}

	// permit if object resides in a namespace labeled for discovery
	return d.discoveryNamespaces.Contains(object.GetNamespace())
}

func (d *discoveryNamespacesFilter) FilterNamespace(nsMeta metav1.ObjectMeta) bool {
	return d.isSelected(nsMeta.Labels)
}

// SelectorsChanged initializes the discovery filter state with the discovery selectors and selected namespaces
func (d *discoveryNamespacesFilter) SelectorsChanged(
	discoverySelectors []*metav1.LabelSelector,
) (selectedNamespaces []string, deselectedNamespaces []string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	var selectors []labels.Selector
	newDiscoveryNamespaces := sets.New[string]()

	namespaceList := d.namespaces.List("", labels.Everything())

	// convert LabelSelectors to Selectors
	for _, selector := range discoverySelectors {
		ls, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			log.Errorf("error initializing discovery namespaces filter, invalid discovery selector: %v", err)
			return
		}
		selectors = append(selectors, ls)
	}

	// range over all namespaces to get discovery namespaces
	for _, ns := range namespaceList {
		for _, selector := range selectors {
			if selector.Matches(labels.Set(ns.Labels)) {
				newDiscoveryNamespaces.Insert(ns.Name)
			}
		}
		// omitting discoverySelectors indicates discovering all namespaces
		if len(selectors) == 0 {
			for _, ns := range namespaceList {
				newDiscoveryNamespaces.Insert(ns.Name)
			}
		}
	}

	oldDiscoveryNamespaces := d.discoveryNamespaces
	selectedNamespaces = sets.SortedList(newDiscoveryNamespaces.Difference(oldDiscoveryNamespaces))
	deselectedNamespaces = sets.SortedList(oldDiscoveryNamespaces.Difference(newDiscoveryNamespaces))

	// update filter state
	d.discoveryNamespaces = newDiscoveryNamespaces
	d.discoverySelectors = selectors

	return
}

func (d *discoveryNamespacesFilter) SyncNamespaces() error {
	namespaceList := d.namespaces.List("", labels.Everything())

	d.lock.Lock()
	defer d.lock.Unlock()
	newDiscoveryNamespaces := sets.New[string]()
	// omitting discoverySelectors indicates discovering all namespaces
	if len(d.discoverySelectors) == 0 {
		for _, ns := range namespaceList {
			newDiscoveryNamespaces.Insert(ns.Name)
		}
	}

	// range over all namespaces to get discovery namespaces
	for _, ns := range namespaceList {
		for _, selector := range d.discoverySelectors {
			if selector.Matches(labels.Set(ns.Labels)) {
				newDiscoveryNamespaces.Insert(ns.Name)
			}
		}
	}

	// update filter state
	d.discoveryNamespaces = newDiscoveryNamespaces

	return nil
}

// NamespaceCreated : if newly created namespace is selected, update namespace membership
func (d *discoveryNamespacesFilter) NamespaceCreated(ns metav1.ObjectMeta) (membershipChanged bool) {
	if d.isSelected(ns.Labels) {
		d.addNamespace(ns.Name)
		return true
	}
	return false
}

// NamespaceUpdated : if updated namespace was a member and no longer selected, or was not a member and now selected, update namespace membership
func (d *discoveryNamespacesFilter) NamespaceUpdated(oldNs, newNs metav1.ObjectMeta) (membershipChanged bool, namespaceAdded bool) {
	if d.hasNamespace(oldNs.Name) && !d.isSelected(newNs.Labels) {
		d.removeNamespace(oldNs.Name)
		return true, false
	}
	if !d.hasNamespace(oldNs.Name) && d.isSelected(newNs.Labels) {
		d.addNamespace(oldNs.Name)
		return true, true
	}
	return false, false
}

// NamespaceDeleted : if deleted namespace was a member, remove it
func (d *discoveryNamespacesFilter) NamespaceDeleted(ns metav1.ObjectMeta) (membershipChanged bool) {
	if d.isSelected(ns.Labels) {
		d.removeNamespace(ns.Name)
		return true
	}
	return false
}

// GetMembers returns member namespaces
func (d *discoveryNamespacesFilter) GetMembers() sets.String {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.discoveryNamespaces.Copy()
}

func (d *discoveryNamespacesFilter) addNamespace(ns string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.discoveryNamespaces.Insert(ns)
}

func (d *discoveryNamespacesFilter) hasNamespace(ns string) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.discoveryNamespaces.Contains(ns)
}

func (d *discoveryNamespacesFilter) removeNamespace(ns string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.discoveryNamespaces.Delete(ns)
}

func (d *discoveryNamespacesFilter) isSelected(labels labels.Set) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
	// permit all objects if discovery selectors are not specified
	if len(d.discoverySelectors) == 0 {
		return true
	}

	for _, selector := range d.discoverySelectors {
		if selector.Matches(labels) {
			return true
		}
	}

	return false
}
