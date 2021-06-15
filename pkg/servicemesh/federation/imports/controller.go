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

package imports

import (
	"context"
	"fmt"

	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	maistrainformers "maistra.io/api/client/informers/externalversions/core/v1"
	maistraclient "maistra.io/api/client/versioned"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/federation"
	kubecontroller "istio.io/istio/pkg/kube/controller"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/istio/pkg/servicemesh/federation/common"
)

const controllerName = "federation-imports-controller"

type Options struct {
	common.ControllerOptions
	ServiceController *aggregate.Controller
}

type Controller struct {
	*kubecontroller.Controller
	cs                maistraclient.Interface
	serviceController *aggregate.Controller
}

// NewController creates a new ServiceImports controller
func NewController(opt Options) (*Controller, error) {
	if err := opt.validate(); err != nil {
		return nil, fmt.Errorf("invalid Options specified for federation import controller: %s", err)
	}

	cs, err := maistraclient.NewForConfig(opt.KubeClient.RESTConfig())
	if err != nil {
		return nil, fmt.Errorf("error creating ClientSet for ServiceMesh: %v", err)
	}

	mrc := opt.KubeClient.GetMemberRoll()

	return internalNewController(cs, mrc, opt), nil
}

// allows using a fake client set for testing purposes
func internalNewController(cs maistraclient.Interface, mrc memberroll.MemberRollController, opt Options) *Controller {
	logger := common.Logger.WithLabels("component", controllerName)
	var informer cache.SharedIndexInformer
	// Currently, we only watch istio system namespace for MeshFederation resources, which is why this block is disabled.
	if mrc != nil && false {
		newInformer := func(namespace string) cache.SharedIndexInformer {
			return cache.NewSharedIndexInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return cs.CoreV1().ServiceImports(namespace).List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						return cs.CoreV1().ServiceImports(namespace).Watch(context.TODO(), options)
					},
				},
				&v1.MeshFederation{},
				opt.ResyncPeriod,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			)
		}

		namespaceSet := xnsinformers.NewNamespaceSet()
		informer = xnsinformers.NewMultiNamespaceInformer(namespaceSet, opt.ResyncPeriod, newInformer)
		mrc.Register(namespaceSet, controllerName)
	} else {
		informer = maistrainformers.NewServiceImportsInformer(
			cs, opt.Namespace, opt.ResyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	}

	controller := &Controller{
		cs:                cs,
		serviceController: opt.ServiceController,
	}
	internalController := kubecontroller.NewController(kubecontroller.Options{
		Informer:     informer,
		Logger:       logger,
		ResyncPeriod: opt.ResyncPeriod,
		Reconciler:   controller.reconcile,
	})
	controller.Controller = internalController

	return controller
}

func (c *Controller) HasSynced() bool {
	return c.Controller.HasSynced()
}

func (c *Controller) reconcile(resourceName string) error {
	c.Logger.Debugf("Reconciling ServiceImports %s", resourceName)
	defer func() {
		c.Logger.Debugf("Completed reconciliation of ServiceImports %s", resourceName)
	}()

	ctx := context.TODO()

	namespace, name, err := cache.SplitMetaNamespaceKey(resourceName)
	if err != nil {
		c.Logger.Errorf("error splitting resource name: %s", resourceName)
	}
	instance, err := c.cs.CoreV1().ServiceImports(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsGone(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			c.deleteImportsForMesh(namespace, name)
			c.Logger.Info("ServiceImports deleted")
			err = nil
		}
		return err
	}

	c.updateImportsForMesh(instance)
	return nil
}

func (c *Controller) deleteImportsForMesh(namespace, name string) {
	c.updateImportsForMesh(&v1.ServiceImports{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	})
}

func (c *Controller) updateImportsForMesh(instance *v1.ServiceImports) {
	if instance.Name == "default" {
		for _, registry := range c.serviceController.GetRegistries() {
			if registry.Provider() == serviceregistry.Federation {
				if federationRegistry, ok := registry.(*federation.Controller); ok {
					federationRegistry.UpdateDefaultImportConfig(instance)
				}
			}
		}
	} else {
		for _, registry := range c.serviceController.GetRegistries() {
			if registry.Cluster() == instance.Name {
				if federationRegistry, ok := registry.(*federation.Controller); ok {
					federationRegistry.UpdateImportConfig(instance)
					break
				}
			}
		}
	}
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.KubeClient == nil {
		allErrors = append(allErrors, fmt.Errorf("the KubeClient field must not be nil"))
	}
	if opt.ServiceController == nil {
		allErrors = append(allErrors, fmt.Errorf("the ServiceController field must not be nil"))
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = common.DefaultResyncPeriod
		common.Logger.WithLabels("component", controllerName).Infof("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
	return errors.NewAggregate(allErrors)
}
