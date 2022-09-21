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

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"maistra.io/api/client/informers/externalversions"
	"maistra.io/api/client/versioned"
	versioned_v1 "maistra.io/api/client/versioned/typed/core/v1"
	v1 "maistra.io/api/core/v1"

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
	cacheLock      sync.RWMutex
}

type MemberRollListener interface {
	SetNamespaces(namespaces []string)
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
		externalversions.WithNamespace(namespace)).Core().V1().ServiceMeshMemberRolls().Informer()
}

func (smmrc *serviceMeshMemberRollController) Start(stopCh <-chan struct{}) {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()

	if smmrc.started {
		return
	}

	go smmrc.informer.Run(stopCh)
	smmrc.started = true

	smmrLog.Debug("Controller started, waiting for cache to warm up")
	go func() {
		if cache.WaitForNamedCacheSync("smmr", stopCh, smmrc.informer.HasSynced) {
			smmrLog.Debug("Cache synced. Will update listeners.")

			smmrc.cacheLock.Lock()
			defer smmrc.cacheLock.Unlock()

			smmrc.cacheWarmed = true
		}
	}()
}

func (smmrc *serviceMeshMemberRollController) Register(listener MemberRollListener, name string) {
	// ensure listener has no namespaces until the smmrc initializes it with the actual list of namespaces in the member roll
	listener.SetNamespaces(nil)

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
		seedCh:            make(chan struct{}),
	}

	// Previously we sent an immediate initial update to all listeners when they
	// were registered that included only the Istio system namespace.  That
	// caused problems with some controllers, e.g. IOR, because they expect the
	// callback to be called with the full authoritative set of namespaces and
	// may remove resources for namespaces not in the list.
	//
	// This instead waits for the informer's cache to sync, then sends an
	// initial update only if the expected SMMR is not found in the cache.
	go func() {
		_ = wait.PollImmediateInfinite(100*time.Millisecond, func() (done bool, err error) {
			smmrc.cacheLock.RLock()
			defer smmrc.cacheLock.RUnlock()

			return smmrc.cacheWarmed, nil
		})

		smmrLog.Infof("Cache synced for listener %q", name)

		// Closing seedCh allows the handler to start processing events.
		defer close(handler.seedCh)

		cacheKey := smmrc.namespace + "/" + smmrc.memberRollName
		_, exists, _ := smmrc.informer.GetStore().GetByKey(cacheKey)
		if exists {
			// No need to send initial update.  The informer will do it.
			return
		}

		smmrLog.Infof("Seeding listener %q with system namespace.", name)
		handler.updateNamespaces("seed", smmrc.memberRollName, nil)
	}()

	return handler
}

type serviceMeshMemberRollListener struct {
	smmrc             *serviceMeshMemberRollController
	listener          MemberRollListener
	currentNamespaces []string
	name              string
	seedCh            chan struct{}
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

		if smmrl.checkEquality(smmrl.currentNamespaces, namespaces) {
			return
		}

		smmrl.currentNamespaces = namespaces
		smmrLog.Debugf("Sending [%s] update to listener %q with %d member(s): %v", operation, smmrl.name, len(namespaces), namespaces)
		smmrl.listener.SetNamespaces(smmrl.currentNamespaces)
	}
}

func (smmrl *serviceMeshMemberRollListener) OnAdd(obj interface{}) {
	<-smmrl.seedCh // Block events until we've sent the initial update.

	serviceMeshMemberRoll := obj.(*v1.ServiceMeshMemberRoll)
	smmrl.updateNamespaces("added", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
}

func (smmrl *serviceMeshMemberRollListener) OnUpdate(oldObj, newObj interface{}) {
	<-smmrl.seedCh // Block events until we've sent the initial update.

	serviceMeshMemberRoll := newObj.(*v1.ServiceMeshMemberRoll)
	smmrl.updateNamespaces("updated", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
}

func (smmrl *serviceMeshMemberRollListener) OnDelete(obj interface{}) {
	<-smmrl.seedCh // Block events until we've sent the initial update.

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
