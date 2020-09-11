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
	"sync"
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	controller "istio.io/istio/pkg/servicemesh/controller/memberroll"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/listwatch"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	versioned_v1alpha1 "istio.io/istio/pkg/servicemesh/client/v1alpha1/clientset/versioned/typed/servicemesh/v1alpha1"
	"istio.io/pkg/log"
)

type serviceMeshExtensionController struct {
	informer cache.SharedIndexInformer
	store    map[string]*v1alpha1.ServiceMeshExtension
	started  bool
	lock     sync.Mutex
}

type ExtensionController interface {
	GetExtensions() []*v1alpha1.ServiceMeshExtension
	RegisterEventHandler(handler cache.ResourceEventHandler)
	Start(<-chan struct{})
}

func NewExtensionControllerFromConfigFile(kubeConfig string, namespaces []string, mrc controller.MemberRollController, resync time.Duration) (ExtensionController, error) {
	config, err := kube.BuildClientConfig(kubeConfig, "")
	if err != nil {
		fmt.Printf("Could not create k8s config: %v", err)
		return nil, err
	}
	cs, err := versioned_v1alpha1.NewForConfig(config)
	if err != nil {
		fmt.Printf("Could not create k8s clientset: %v", err)
		return nil, err
	}

	mlw := listwatch.MultiNamespaceListerWatcher(
		namespaces,
		func(namespace string) cache.ListerWatcher {
			return &cache.ListWatch{
				ListFunc: func(opts meta_v1.ListOptions) (result runtime.Object, err error) {
					return cs.ServiceMeshExtensions(namespace).List(context.TODO(), opts)
				},
				WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
					return cs.ServiceMeshExtensions(namespace).Watch(context.TODO(), opts)
				},
			}
		},
	)
	if mrc != nil {
		mrc.Register(mlw, "extensions-controller")
	}
	store := make(map[string]*v1alpha1.ServiceMeshExtension, 0)
	informer := cache.NewSharedIndexInformer(
		mlw, &v1alpha1.ServiceMeshExtension{},
		resync, cache.Indexers{})

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				extension, ok := obj.(*v1alpha1.ServiceMeshExtension)
				if ok && extension != nil {
					store[extension.Namespace+"/"+extension.Name] = extension.DeepCopy()
					log.Infof("Added extension %s/%s", extension.Namespace, extension.Name)

				}
			},
			UpdateFunc: func(old, cur interface{}) {
				extension, ok := cur.(*v1alpha1.ServiceMeshExtension)
				if ok && extension != nil {
					store[extension.Namespace+"/"+extension.Name] = extension.DeepCopy()
					log.Infof("Updated extension %s/%s", extension.Namespace, extension.Name)
				}
			},
			DeleteFunc: func(obj interface{}) {
				extension, ok := obj.(*v1alpha1.ServiceMeshExtension)
				if !ok {
					tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						log.Errorf("Couldn't get object from tombstone %#v", obj)
						return
					}
					extension, ok = tombstone.Obj.(*v1alpha1.ServiceMeshExtension)
					if !ok {
						log.Errorf("Tombstone contained object that is not a service mesh member roll %#v", obj)
						return
					}
				}
				store[extension.Namespace+"/"+extension.Name] = nil
				log.Infof("Deleted extension %s/%s", extension.Namespace, extension.Name)
			},
		})

	return &serviceMeshExtensionController{
		informer: informer,
		store:    store,
	}, nil
}

func (ec *serviceMeshExtensionController) GetExtensions() []*v1alpha1.ServiceMeshExtension {
	ret := []*v1alpha1.ServiceMeshExtension{}
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
