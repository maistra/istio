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

package extension

import (
	"context"
	"fmt"
	"time"

	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	versioned_v1 "maistra.io/api/client/versioned/typed/core/v1"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/pkg/log"
)

var controllerlog = log.RegisterScope("controller", "Extension controller", 0)

type serviceMeshExtensionController struct {
	informer cache.SharedIndexInformer
	store    map[string]*v1.ServiceMeshExtension
}

type Controller interface {
	GetExtensions() []*v1.ServiceMeshExtension
	RegisterEventHandler(handler cache.ResourceEventHandler)
	Start(<-chan struct{})
}

func NewControllerFromConfigFile(kubeConfig string, namespaces []string, mrc memberroll.MemberRollController, resync time.Duration) (Controller, error) {
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

	namespaceSet := xnsinformers.NewNamespaceSet()
	if mrc != nil {
		mrc.Register(namespaceSet, "extensions-controller")
	} else {
		// No MemberRoll configured, set namespaces based on args.
		namespaceSet.SetNamespaces(namespaces)
	}

	newInformer := func(namespace string) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					return cs.ServiceMeshExtensions(namespace).List(context.TODO(), options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					return cs.ServiceMeshExtensions(namespace).Watch(context.TODO(), options)
				},
			},
			&v1.ServiceMeshExtension{},
			resync,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
	}

	store := make(map[string]*v1.ServiceMeshExtension)
	informer := xnsinformers.NewMultiNamespaceInformer(namespaceSet, resync, newInformer)

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				extension, ok := obj.(*v1.ServiceMeshExtension)
				if ok && extension != nil {
					store[extension.Namespace+"/"+extension.Name] = extension.DeepCopy()
					controllerlog.Infof("Added extension %s/%s", extension.Namespace, extension.Name)

				}
			},
			UpdateFunc: func(old, cur interface{}) {
				extension, ok := cur.(*v1.ServiceMeshExtension)
				if ok && extension != nil {
					store[extension.Namespace+"/"+extension.Name] = extension.DeepCopy()
					controllerlog.Infof("Updated extension %s/%s", extension.Namespace, extension.Name)
				}
			},
			DeleteFunc: func(obj interface{}) {
				extension, ok := obj.(*v1.ServiceMeshExtension)
				if !ok {
					tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						controllerlog.Errorf("Couldn't get object from tombstone %#v", obj)
						return
					}
					extension, ok = tombstone.Obj.(*v1.ServiceMeshExtension)
					if !ok {
						controllerlog.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
						return
					}
				}
				delete(store, extension.Namespace+"/"+extension.Name)
				controllerlog.Infof("Deleted extension %s/%s", extension.Namespace, extension.Name)
			},
		})

	return &serviceMeshExtensionController{
		informer: informer,
		store:    store,
	}, nil
}

func (ec *serviceMeshExtensionController) GetExtensions() []*v1.ServiceMeshExtension {
	ret := []*v1.ServiceMeshExtension{}
	for _, v := range ec.store {
		ret = append(ret, v.DeepCopy())
	}
	return ret
}

func (ec *serviceMeshExtensionController) Start(stopChan <-chan struct{}) {
	go ec.informer.Run(stopChan)
}

func (ec *serviceMeshExtensionController) RegisterEventHandler(handler cache.ResourceEventHandler) {
	ec.informer.AddEventHandler(handler)
}
