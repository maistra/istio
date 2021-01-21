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

package export

import (
	"context"
	"fmt"
	"time"

	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	kubecontroller "istio.io/istio/pkg/kube/controller"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	clientsetservicemeshv1alpha1 "istio.io/istio/pkg/servicemesh/client/v1alpha1/clientset/versioned"
	informersservicemeshv1alpha1 "istio.io/istio/pkg/servicemesh/client/v1alpha1/informers/externalversions/servicemesh/v1alpha1"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/pkg/log"
)

const (
	defaultResyncPeriod = 60 * time.Second
)

var logger = log.RegisterScope("federation-exports-controller", "federation-exports-controller", 0)

type ServiceExportManager interface {
	UpdateExportsForMesh(exports *v1alpha1.ServiceExports) error
	DeleteExportsForMesh(name string)
}

type Options struct {
	common.ControllerOptions
	ServiceExportManager ServiceExportManager
}

type Controller struct {
	*kubecontroller.Controller
	cs            clientsetservicemeshv1alpha1.Interface
	exportManager ServiceExportManager
}

// newExportsController creates a new ServiceExports controller
func NewController(opt Options) (*Controller, error) {
	if err := opt.validate(); err != nil {
		return nil, fmt.Errorf("invalid Options specified for federation export controller: %s", err)
	}

	cs, err := clientsetservicemeshv1alpha1.NewForConfig(opt.KubeClient.RESTConfig())
	if err != nil {
		return nil, fmt.Errorf("error creating ClientSet for ServiceMesh: %v", err)
	}

	mrc := opt.KubeClient.GetMemberRoll()

	return internalNewController(cs, mrc, opt), nil
}

// allows using a fake client set for testing purposes
func internalNewController(cs clientsetservicemeshv1alpha1.Interface, mrc memberroll.MemberRollController, opt Options) *Controller {
	var informer cache.SharedIndexInformer
	// Currently, we only watch istio system namespace for MeshFederation resources, which is why this block is disabled.
	if mrc != nil && false {
		newInformer := func(namespace string) cache.SharedIndexInformer {
			return cache.NewSharedIndexInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return cs.MaistraV1alpha1().ServiceExports(namespace).List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						return cs.MaistraV1alpha1().ServiceExports(namespace).Watch(context.TODO(), options)
					},
				},
				&v1alpha1.MeshFederation{},
				opt.ResyncPeriod,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			)
		}

		namespaceSet := xnsinformers.NewNamespaceSet()
		informer = xnsinformers.NewMultiNamespaceInformer(namespaceSet, opt.ResyncPeriod, newInformer)
		mrc.Register(namespaceSet, "federation-exports-controller")
	} else {
		informer = informersservicemeshv1alpha1.NewServiceExportsInformer(
			cs, opt.Namespace, opt.ResyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	}

	controller := &Controller{
		cs:            cs,
		exportManager: opt.ServiceExportManager,
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
	logger.Debugf("Reconciling MeshFederation %s", resourceName)
	defer func() {
		logger.Infof("Completed reconciliation of ServiceExports %s", resourceName)
	}()

	ctx := context.TODO()

	namespace, name, err := cache.SplitMetaNamespaceKey(resourceName)
	if err != nil {
		logger.Errorf("error splitting resource name: %s", resourceName)
	}
	instance, err := c.cs.MaistraV1alpha1().ServiceExports(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsGone(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			c.exportManager.DeleteExportsForMesh(name)
			logger.Info("ServiceExports deleted")
			err = nil
		}
		return err
	}

	return c.exportManager.UpdateExportsForMesh(instance)
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.KubeClient == nil {
		allErrors = append(allErrors, fmt.Errorf("the KubeClient field must not be nil"))
	}
	if opt.ServiceExportManager == nil {
		allErrors = append(allErrors, fmt.Errorf("the ServiceExportManager field must not be nil"))
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = defaultResyncPeriod
		logger.Warnf("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
	return errors.NewAggregate(allErrors)
}
