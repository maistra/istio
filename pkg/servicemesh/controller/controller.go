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

package controller

import (
	"sort"
	"sync"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	v1 "istio.io/istio/pkg/servicemesh/apis/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/v1/clientset/versioned"
	versioned_v1 "istio.io/istio/pkg/servicemesh/client/v1/clientset/versioned/typed/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/v1/informers/externalversions"
	"istio.io/pkg/log"
)

var smmrLog = log.RegisterScope("smmr", "SMMR controller", 0)

type serviceMeshMemberRollController struct {
	informer       cache.SharedIndexInformer
	namespace      string
	memberRollName string
	started        bool
	lock           sync.Mutex
	cacheWarmed    bool
}

type MemberRollListener interface {
	SetNamespaces(namespaces ...string)
}

type MemberRollController interface {
	Register(listener MemberRollListener, name string)
	Start(stopCh <-chan struct{})
}

func NewMemberRollController(config *rest.Config, namespace string, memberRollName string, resync time.Duration) (MemberRollController, error) {
	cs, err := versioned_v1.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	rc := cs.RESTClient()

	return &serviceMeshMemberRollController{
		informer:       newMemberRollSharedInformer(rc, namespace, resync),
		namespace:      namespace,
		memberRollName: memberRollName,
	}, nil
}

func newMemberRollSharedInformer(restClient rest.Interface, namespace string, resync time.Duration) cache.SharedIndexInformer {
	client := versioned.New(restClient)
	return externalversions.NewSharedInformerFactoryWithOptions(client, resync,
		externalversions.WithNamespace(namespace)).Maistra().V1().ServiceMeshMemberRolls().Informer()
}

func (smmrc *serviceMeshMemberRollController) Start(stopCh <-chan struct{}) {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()

	if !smmrc.started {
		go smmrc.informer.Run(stopCh)
		smmrc.started = true

		smmrLog.Debug("Controller started, waiting for cache to warm up")
		go func() {
			for {
				if cache.WaitForNamedCacheSync("SMMR", stopCh, smmrc.informer.HasSynced) {
					smmrLog.Debug("Cache warmed up. From now on will send the initial update to listeners")
					smmrc.cacheWarmed = true
					return
				}
				smmrLog.Debug("Cache not synced, trying again")
				time.Sleep(time.Second)
			}
		}()
	}
}

func (smmrc *serviceMeshMemberRollController) Register(listener MemberRollListener, name string) {
	smmrc.informer.AddEventHandler(smmrc.newServiceMeshMemberRollListener(listener, name))
}

func (smmrc *serviceMeshMemberRollController) getNamespaces(namespaces []string) []string {
	result := append([]string(nil), namespaces...)

	found := false
	for _, namespace := range namespaces {
		if namespace == smmrc.namespace {
			found = true
			break
		}
	}
	if !found {
		result = append(result, smmrc.namespace)
	}
	return result
}

func (smmrc *serviceMeshMemberRollController) newServiceMeshMemberRollListener(listener MemberRollListener, name string) cache.ResourceEventHandler {
	handler := &serviceMeshMemberRollListener{
		smmrc:             smmrc,
		listener:          listener,
		currentNamespaces: nil,
		name:              name,
	}

	if smmrc.cacheWarmed {
		smmrLog.Debugf("Listener for %q created. Ready to send an initial update", name)

		var members []string
		for _, item := range smmrc.informer.GetIndexer().List() {
			smmr := item.(*v1.ServiceMeshMemberRoll)
			members = smmr.Status.ConfiguredMembers
		}
		members = smmrc.getNamespaces(members)
		handler.updateNamespaces("added", smmrc.memberRollName, members)
	} else {
		smmrLog.Debugf("Listener for %q created. Not sending an initial update", name)
	}

	return handler
}

type serviceMeshMemberRollListener struct {
	smmrc             *serviceMeshMemberRollController
	listener          MemberRollListener
	currentNamespaces []string
	name              string
}

func (smmrl *serviceMeshMemberRollListener) checkEquality(lhs, rhs []string) bool {
	if (lhs == nil) != (rhs == nil) {
		return false
	}
	if len(lhs) != len(rhs) {
		return false
	}
	for n, val := range lhs {
		if val != rhs[n] {
			return false
		}
	}
	return true
}

func (smmrl *serviceMeshMemberRollListener) updateNamespaces(operation string, memberRollName string, members []string) {
	if smmrl.smmrc.memberRollName != memberRollName {
		smmrLog.Errorf("ServiceMeshMemberRoll using incorrect name %v, ignoring", memberRollName)
	} else {
		namespaces := smmrl.smmrc.getNamespaces(members)
		sort.Strings(namespaces)
		smmrl.currentNamespaces = namespaces
		smmrLog.Debugf("Sending [%s] update to listener %q with %d member(s): %v", operation, smmrl.name, len(namespaces), namespaces)
		smmrl.listener.SetNamespaces(smmrl.currentNamespaces...)
	}
}

func (smmrl *serviceMeshMemberRollListener) OnAdd(obj interface{}) {
	serviceMeshMemberRoll := obj.(*v1.ServiceMeshMemberRoll)
	smmrl.updateNamespaces("added", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
}

func (smmrl *serviceMeshMemberRollListener) OnUpdate(oldObj, newObj interface{}) {
	serviceMeshMemberRoll := newObj.(*v1.ServiceMeshMemberRoll)
	smmrl.updateNamespaces("updated", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
}

func (smmrl *serviceMeshMemberRollListener) OnDelete(obj interface{}) {
	serviceMeshMemberRoll, ok := obj.(*v1.ServiceMeshMemberRoll)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			smmrLog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		serviceMeshMemberRoll, ok = tombstone.Obj.(*v1.ServiceMeshMemberRoll)
		if !ok {
			smmrLog.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
			return
		}
	}
	smmrl.updateNamespaces("deleted", serviceMeshMemberRoll.Name, nil)
}
