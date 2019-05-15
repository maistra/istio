package controller

import (
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha3"
	"istio.io/istio/pkg/servicemesh/client/clientset/versioned"
	versioned_v1alpha3 "istio.io/istio/pkg/servicemesh/client/clientset/versioned/typed/servicemesh/v1alpha3"
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
	cs, err := versioned_v1alpha3.NewForConfig(config)
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
		externalversions.WithNamespace(namespace)).Istio().V1alpha3().ServiceMeshMemberRolls().Informer()
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
	smmrc.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			serviceMeshMemberRoll := obj.(*v1alpha3.ServiceMeshMemberRoll)
			if smmrc.memberRollName != serviceMeshMemberRoll.Name {
				log.Infof("Add called with ServiceMeshMemberRoll using incorrect name %v, ignoring", serviceMeshMemberRoll.Name)
			} else {
				namespaces := smmrc.getNamespaces(serviceMeshMemberRoll.Spec.Members)
				log.Infof("ServiceMeshMemberRoll %v added, namespaces now %q", serviceMeshMemberRoll.Name, namespaces)
				listener.UpdateNamespaces(namespaces)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			serviceMeshMemberRoll := newObj.(*v1alpha3.ServiceMeshMemberRoll)
			if smmrc.memberRollName != serviceMeshMemberRoll.Name {
				log.Infof("Update called with ServiceMeshMemberRoll using incorrect name %v, ignoring", serviceMeshMemberRoll.Name)
			} else {
				namespaces := smmrc.getNamespaces(serviceMeshMemberRoll.Spec.Members)
				log.Infof("ServiceMeshMemberRoll %v updated, namespaces now %q", serviceMeshMemberRoll.Name, namespaces)
				listener.UpdateNamespaces(namespaces)
			}
		},
		DeleteFunc: func(obj interface{}) {
			serviceMeshMemberRoll, ok := obj.(*v1alpha3.ServiceMeshMemberRoll)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					log.Errorf("Couldn't get object from tombstone %#v", obj)
					return
				}
				serviceMeshMemberRoll, ok = tombstone.Obj.(*v1alpha3.ServiceMeshMemberRoll)
				if !ok {
					log.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
					return
				}
			}
			if smmrc.memberRollName != serviceMeshMemberRoll.Name {
				log.Infof("Delete called with ServiceMeshMemberRoll using incorrect name %v, ignoring", serviceMeshMemberRoll.Name)
			} else {
				namespaces := smmrc.getNamespaces(nil)
				log.Infof("ServiceMeshMemberRoll %v deleted, namespaces now %q", serviceMeshMemberRoll.Name, namespaces)
				listener.UpdateNamespaces(namespaces)
			}
		},
	})
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
