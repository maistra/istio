package controller

import (
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/clientset/versioned"
	versioned_v1 "istio.io/istio/pkg/servicemesh/client/clientset/versioned/typed/servicemesh/v1"
	"istio.io/istio/pkg/servicemesh/client/informers/externalversions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type serviceMeshMemberRollController struct {
	informer       cache.SharedIndexInformer
	namespace      string
	memberRollName string
	started        bool
	lock           sync.Mutex
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

func (smmrc *serviceMeshMemberRollController) Start(stop chan struct{}) {
	smmrc.lock.Lock()
	defer smmrc.lock.Unlock()

	if !smmrc.started {
		go smmrc.informer.Run(stop)
		smmrc.started = true
	}
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

func (smmrc *serviceMeshMemberRollController) newServiceMeshMemberRollListener(listener MemberRollListener) cache.ResourceEventHandler {
	return &serviceMeshMemberRollListener{
		smmrc:             smmrc,
		listener:          listener,
		currentNamespaces: smmrc.getNamespaces(nil),
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
		if !smmrl.checkEquality(smmrl.currentNamespaces, namespaces) {
			smmrl.currentNamespaces = namespaces
			log.Infof("ServiceMeshMemberRoll %v %s, namespaces now %q", memberRollName, operation, smmrl.currentNamespaces)
			smmrl.listener.UpdateNamespaces(smmrl.currentNamespaces)
		}
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
			log.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		serviceMeshMemberRoll, ok = tombstone.Obj.(*v1.ServiceMeshMemberRoll)
		if !ok {
			log.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
			return
		}
	}
	smmrl.updateNamespaces("deleted", serviceMeshMemberRoll.Name, nil)
}
