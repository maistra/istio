package controller

import (
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/clientset/versioned"
	versioned_v1 "istio.io/istio/pkg/servicemesh/client/clientset/versioned/typed/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/informers/externalversions"
	"istio.io/pkg/log"
	
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type serviceMeshMemberRollController struct {
	informer          cache.SharedIndexInformer
	namespace         string
	memberRollName    string
	currentNamespaces sets.String
	started           bool
	lock              sync.Mutex
	listeners         []MemberRollListener
}

type MemberRollListener interface {
	UpdateNamespaces(namespaces []string)
}

type MemberRollController interface {
	Register(listener MemberRollListener)
	Start(stop chan struct{})
}

func NewMemberRollControllerFromConfigFile(kubeConfig string, namespace string, memberRollName string, resync time.Duration) (MemberRollController, error) {
	config, err := kube.BuildClientConfig(kubeConfig, "")
	if err != nil {
		fmt.Printf("Could not create k8s config: %v", err)
		return nil, err
	}
	cs, err := versioned_v1.NewForConfig(config)
	if err != nil {
		fmt.Printf("Could not create k8s clientset: %v", err)
		return nil, err
	}

	rc := cs.RESTClient()

	smmrc := &serviceMeshMemberRollController{
		informer:       newMemberRollSharedInformer(rc, namespace, resync),
		namespace:      namespace,
		memberRollName: memberRollName,
	}
	smmrc.informer.AddEventHandler(smmrc)
	return smmrc, nil
}

func newMemberRollSharedInformer(restClient rest.Interface, namespace string, resync time.Duration) cache.SharedIndexInformer {
	client := versioned.New(restClient)
	return externalversions.NewSharedInformerFactoryWithOptions(client, resync,
		externalversions.WithNamespace(namespace)).Maistra().V1().ServiceMeshMemberRolls().Informer()
}

func (smmrc *serviceMeshMemberRollController) Start(stop chan struct{}) {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()

	if !smmrc.started {
		go smmrc.informer.Run(stop)
		smmrc.started = true
	}
}

func (smmrc *serviceMeshMemberRollController) Register(listener MemberRollListener) {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()
	smmrc.listeners = append(smmrc.listeners, listener)
	// push initial state
	go listener.UpdateNamespaces(smmrc.currentNamespaces.UnsortedList())
}

func (smmrc *serviceMeshMemberRollController) updateNamespaces(operation string, memberRollName string, members []string) {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()
	if smmrc.memberRollName != memberRollName {
		log.Infof("ServiceMeshMemberRoll using incorrect name %v, ignoring", memberRollName)
	} else {
		namespaces := sets.NewString(members...)
		namespaces.Insert(smmrc.namespace)
		if !smmrc.currentNamespaces.Equal(namespaces) {
			smmrc.currentNamespaces = namespaces
			log.Infof("ServiceMeshMemberRoll %v %s, namespaces now %q", memberRollName, operation, smmrc.currentNamespaces.List())
			for _, listener := range smmrc.listeners {
				// give everyone their own copy
				go listener.UpdateNamespaces(smmrc.currentNamespaces.UnsortedList())
			}
		}
	}
}

func (smmrc *serviceMeshMemberRollController) OnAdd(obj interface{}) {
	serviceMeshMemberRoll := obj.(*v1.ServiceMeshMemberRoll)
	smmrc.updateNamespaces("added", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
}

func (smmrc *serviceMeshMemberRollController) OnUpdate(oldObj, newObj interface{}) {
	serviceMeshMemberRoll := newObj.(*v1.ServiceMeshMemberRoll)
	smmrc.updateNamespaces("updated", serviceMeshMemberRoll.Name, serviceMeshMemberRoll.Status.ConfiguredMembers)
}

func (smmrc *serviceMeshMemberRollController) OnDelete(obj interface{}) {
	serviceMeshMemberRoll, ok := obj.(*v1.ServiceMeshMemberRoll)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		serviceMeshMemberRoll, ok = tombstone.Obj.(*v1.ServiceMeshMemberRoll)
		if !ok {
			log.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
			return
		}
	}
	smmrc.updateNamespaces("deleted", serviceMeshMemberRoll.Name, nil)
}
