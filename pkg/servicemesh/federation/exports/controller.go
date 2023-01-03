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

package exports

import (
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pkg/servicemesh/federation/common"
	kubecontroller "istio.io/istio/pkg/servicemesh/federation/kube"
)

const controllerName = "federation-exports-controller"

type ServiceExportManager interface {
	UpdateExportsForMesh(exports *v1.ExportedServiceSet) error
	DeleteExportsForMesh(name string)
}

type Options struct {
	ResourceManager      common.ResourceManager
	ResyncPeriod         time.Duration
	ServiceExportManager ServiceExportManager
}

type Controller struct {
	*kubecontroller.Controller
	rm            common.ResourceManager
	exportManager ServiceExportManager
}

// NewController creates a new ServiceExports controller
func NewController(opt Options) (*Controller, error) {
	if err := opt.validate(); err != nil {
		return nil, fmt.Errorf("invalid Options specified for federation export controller: %s", err)
	}
	controller := &Controller{
		rm:            opt.ResourceManager,
		exportManager: opt.ServiceExportManager,
	}
	internalController := kubecontroller.NewController(kubecontroller.Options{
		Informer:     opt.ResourceManager.ExportsInformer().Informer(),
		Logger:       common.Logger.WithLabels("component", controllerName),
		ResyncPeriod: opt.ResyncPeriod,
		Reconciler:   controller.reconcile,
		HasSynced:    opt.ResourceManager.HasSynced,
	})
	controller.Controller = internalController

	return controller, nil
}

func (c *Controller) RunInformer(stopChan <-chan struct{}) {
	// no-op, informer is started by the shared factory in Federation.Start()
}

func (c *Controller) reconcile(resourceName string) error {
	c.Logger.Debugf("Reconciling ServiceExports %s", resourceName)
	defer func() {
		c.Logger.Debugf("Completed reconciliation of ServiceExports %s", resourceName)
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(resourceName)
	if err != nil {
		c.Logger.Errorf("error splitting resource name: %s", resourceName)
	}
	instance, err := c.rm.ExportsInformer().Lister().ExportedServiceSets(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsGone(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			c.exportManager.DeleteExportsForMesh(name)
			c.Logger.Info("ServiceExports deleted")
			err = nil
		}
		return err
	}

	return c.exportManager.UpdateExportsForMesh(instance)
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.ResourceManager == nil {
		allErrors = append(allErrors, fmt.Errorf("the ResourceManager field must not be nil"))
	}
	if opt.ServiceExportManager == nil {
		allErrors = append(allErrors, fmt.Errorf("the ServiceExportManager field must not be nil"))
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = common.DefaultResyncPeriod
		common.Logger.WithLabels("component", controllerName).Infof("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
	return errors.NewAggregate(allErrors)
}
