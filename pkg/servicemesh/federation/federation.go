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

package federation

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/discovery"
	"istio.io/istio/pkg/servicemesh/federation/exports"
	"istio.io/istio/pkg/servicemesh/federation/imports"
	"istio.io/istio/pkg/servicemesh/federation/server"
	"istio.io/istio/pkg/servicemesh/federation/status"
	"istio.io/pkg/log"
)

func init() {
	schemasBuilder := collection.NewSchemasBuilder()
	discovery.Schemas.ForEach(func(s collection.Schema) (done bool) {
		// only error is already exists, which we don't care about
		_ = schemasBuilder.Add(s)
		return false
	})
	server.Schemas.ForEach(func(s collection.Schema) (done bool) {
		// only error is already exists, which we don't care about
		_ = schemasBuilder.Add(s)
		return false
	})
	schemas = schemasBuilder.Build()
}

var (
	schemas collection.Schemas
)

type Options struct {
	common.ControllerOptions
	BindAddress       string
	Env               *model.Environment
	XDSUpdater        model.XDSUpdater
	ServiceController *aggregate.Controller
	LocalNetwork      string
	LocalClusterID    string
	IstiodNamespace   string
	IstiodPodName     string
	GetCARootCertFn   func() string
}

type Federation struct {
	configStore         model.ConfigStoreCache
	server              *server.Server
	exportController    *exports.Controller
	importController    *imports.Controller
	discoveryController *discovery.Controller
}

func New(opt Options) (*Federation, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	name := types.NamespacedName{Name: opt.IstiodPodName, Namespace: opt.IstiodNamespace}
	statusManager, err := status.NewManager(name, opt.KubeClient)
	if err != nil {
		return nil, err
	}
	configStore := newConfigStore()
	server, err := server.NewServer(server.Options{
		BindAddress:     opt.BindAddress,
		Env:             opt.Env,
		Network:         opt.LocalNetwork,
		ConfigStore:     configStore,
		GetCARootCertFn: opt.GetCARootCertFn,
	})
	if err != nil {
		return nil, err
	}
	exportController, err := exports.NewController(exports.Options{
		ControllerOptions:    opt.ControllerOptions,
		ServiceExportManager: server,
	})
	if err != nil {
		return nil, err
	}
	importController, err := imports.NewController(imports.Options{
		ControllerOptions: opt.ControllerOptions,
		ServiceController: opt.ServiceController,
	})
	if err != nil {
		return nil, err
	}
	discoveryController, err := discovery.NewController(discovery.Options{
		LocalClusterID:    opt.LocalClusterID,
		LocalNetwork:      opt.LocalNetwork,
		ControllerOptions: opt.ControllerOptions,
		ServiceController: opt.ServiceController,
		XDSUpdater:        opt.XDSUpdater,
		Env:               opt.Env,
		ConfigStore:       configStore,
		FederationManager: server,
		StatusManager:     statusManager,
	})
	if err != nil {
		return nil, err
	}

	federation := &Federation{
		configStore:         configStore,
		server:              server,
		exportController:    exportController,
		importController:    importController,
		discoveryController: discoveryController,
	}
	return federation, nil
}

func newConfigStore() model.ConfigStoreCache {
	return memory.NewController(memory.Make(schemas))
}

func (f *Federation) ConfigStore() model.ConfigStoreCache {
	return f.configStore
}

func (f *Federation) RegisterServiceHandlers(serviceController *aggregate.Controller) {
	serviceController.AppendServiceHandler(f.server.UpdateService)
}

func (f *Federation) StartControllers(stopCh <-chan struct{}) {
	go f.exportController.Start(stopCh)
	go f.importController.Start(stopCh)
	f.discoveryController.Start(stopCh)
}

func (f *Federation) ControllersSynced() bool {
	return f.importController.HasSynced() && f.exportController.HasSynced() && f.discoveryController.HasSynced()
}

func (f *Federation) StartServer(stopCh <-chan struct{}) {
	f.server.Run(stopCh)
}

func (f *Federation) GetTrustBundles() map[string]string {
	return f.discoveryController.GetTrustBundles()
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.KubeClient == nil {
		allErrors = append(allErrors, fmt.Errorf("the KubeClient field must not be nil"))
	}
	if opt.XDSUpdater == nil {
		allErrors = append(allErrors, fmt.Errorf("the XDSUpdater field must not be nil"))
	}
	if opt.Env == nil {
		allErrors = append(allErrors, fmt.Errorf("the Env field must not be nil"))
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = common.DefaultResyncPeriod
		log.Warnf("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
	if opt.ServiceController == nil {
		allErrors = append(allErrors, fmt.Errorf("the ServiceController field must not be nil"))
	}
	return errors.NewAggregate(allErrors)
}
