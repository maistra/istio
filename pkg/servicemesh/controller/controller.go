package controller

import (
	"sort"
	"sync"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/clientset/versioned"
	versioned_v1 "istio.io/istio/pkg/servicemesh/client/clientset/versioned/typed/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/informers/externalversions"
	"istio.io/pkg/log"
)

type serviceMeshMemberRollController struct {
	informer       cache.SharedIndexInformer
	namespace      string
	memberRollName string
	seedNamespaces []string
	started        bool
	lock           sync.RWMutex
}

type MemberRollListener interface {
	UpdateNamespaces(namespaces []string)
}

type MemberRollController interface {
	Register(listener MemberRollListener)
	Start(stop chan struct{})
}

func getServiceMeshMemberRoll(obj interface{}) *v1.ServiceMeshMemberRoll {
	serviceMeshMemberRoll, ok := obj.(*v1.ServiceMeshMemberRoll)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", obj)
			return nil
		}
		serviceMeshMemberRoll, ok = tombstone.Obj.(*v1.ServiceMeshMemberRoll)
		if !ok {
			log.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
			return nil
		}
	}
	return serviceMeshMemberRoll
}

func NewMemberRollControllerFromConfigFile(kubeConfig string, namespace string, memberRollName string, resync time.Duration) (MemberRollController, error) {
	config, err := kube.BuildClientConfig(kubeConfig, "")
	if err != nil {
		return nil, err
	}
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

// check to see if the informer should be started, returning true if this was necessary
func (smmrc *serviceMeshMemberRollController) startInformer(stop chan struct{}) bool {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()

	if smmrc.started {
		return false
	}

		go smmrc.informer.Run(stop)
		smmrc.started = true
	return true
}

func (smmrc *serviceMeshMemberRollController) Start(stop chan struct{}) {
	if !smmrc.startInformer(stop) {
		return
	}

	log.Debug("Controller started, waiting for cache to warm up")
	cache.WaitForCacheSync(stop, smmrc.informer.HasSynced)

	for _, obj := range smmrc.informer.GetStore().List() {
		serviceMeshMemberRoll := getServiceMeshMemberRoll(obj)
		if smmrc.memberRollName == serviceMeshMemberRoll.Name {
			seedNamespaces := smmrc.getNamespaces(serviceMeshMemberRoll.Status.ConfiguredMembers)
			smmrc.setSeedNamespaces(seedNamespaces)
			break
	}
}
	smmrc.informer.AddEventHandler(smmrc)
}

func (smmrc *serviceMeshMemberRollController) Register(listener MemberRollListener) {
	smmrc.informer.AddEventHandler(smmrc.newServiceMeshMemberRollListener(listener))
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

func (smmrc *serviceMeshMemberRollController) getSeedNamespaces() []string {
	smmrc.lock.RLock()
	defer smmrc.lock.RUnlock()
	return smmrc.seedNamespaces
}

func (smmrc *serviceMeshMemberRollController) setSeedNamespaces(seedNamespaces []string) {
	sort.Strings(seedNamespaces)
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()
	smmrc.seedNamespaces = seedNamespaces
}

func (smmrc *serviceMeshMemberRollController) newServiceMeshMemberRollListener(listener MemberRollListener) cache.ResourceEventHandler {
	handler := &serviceMeshMemberRollListener{
		smmrc:             smmrc,
		listener:          listener,
		currentNamespaces: nil,
	}
	handler.updateNamespaces("add", smmrc.memberRollName, smmrc.getNamespaces(smmrc.getSeedNamespaces()))
	return handler
}

func (smmrc *serviceMeshMemberRollController) OnAdd(obj interface{}) {
	if serviceMeshMemberRoll := getServiceMeshMemberRoll(obj); serviceMeshMemberRoll != nil {
		smmrc.setSeedNamespaces(serviceMeshMemberRoll.Status.ConfiguredMembers)
	}
}

func (smmrc *serviceMeshMemberRollController) OnUpdate(oldObj, newObj interface{}) {
	if serviceMeshMemberRoll := getServiceMeshMemberRoll(newObj); serviceMeshMemberRoll != nil {
		smmrc.setSeedNamespaces(serviceMeshMemberRoll.Status.ConfiguredMembers)
	}
}

func (smmrc *serviceMeshMemberRollController) OnDelete(obj interface{}) {
	if serviceMeshMemberRoll := getServiceMeshMemberRoll(obj); serviceMeshMemberRoll != nil {
		smmrc.setSeedNamespaces(nil)
	}
}

type serviceMeshMemberRollListener struct {
	smmrc             *serviceMeshMemberRollController
	listener          MemberRollListener
	currentNamespaces []string
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
		log.Infof("ServiceMeshMemberRoll using incorrect name %v, ignoring", memberRollName)
	} else {
		namespaces := smmrl.smmrc.getNamespaces(members)
		sort.Strings(namespaces)
		if !smmrl.checkEquality(smmrl.currentNamespaces, namespaces) {
			smmrl.currentNamespaces = namespaces
			log.Debugf("Sending [%s] update to listener with %d member(s): %v", operation, len(namespaces), namespaces)
			smmrl.listener.UpdateNamespaces(smmrl.currentNamespaces)
		}
	}
}

func (smmrl *serviceMeshMemberRollListener) OnAdd(obj interface{}) {
	if serviceMeshMemberRoll := getServiceMeshMemberRoll(obj); serviceMeshMemberRoll != nil {
		smmrl.updateNamespaces("added", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
	}
}

func (smmrl *serviceMeshMemberRollListener) OnUpdate(oldObj, newObj interface{}) {
	if serviceMeshMemberRoll := getServiceMeshMemberRoll(newObj); serviceMeshMemberRoll != nil {
		smmrl.updateNamespaces("updated", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
	}
}

func (smmrl *serviceMeshMemberRollListener) OnDelete(obj interface{}) {
	if serviceMeshMemberRoll := getServiceMeshMemberRoll(obj); serviceMeshMemberRoll != nil {
		smmrl.updateNamespaces("deleted", serviceMeshMemberRoll.Name, nil)
	}
}
