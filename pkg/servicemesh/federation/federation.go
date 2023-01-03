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
	"crypto/tls"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	maistraclient "maistra.io/api/client/versioned"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube"
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

var schemas collection.Schemas

type Options struct {
	KubeClient          kube.Client
	FederationNamespace string
	ResyncPeriod        time.Duration
	BindAddress         string
	Env                 *model.Environment
	XDSUpdater          model.XDSUpdater
	ServiceController   *aggregate.Controller
	LocalNetwork        string
	LocalClusterID      string
	IstiodNamespace     string
	IstiodPodName       string
	TLSConfig           *tls.Config
}

type Federation struct {
	configStore         model.ConfigStoreController
	resourceManager     common.ResourceManager
	server              *server.Server
	exportController    *exports.Controller
	importController    *imports.Controller
	discoveryController *discovery.Controller
	leaderElection      *leaderelection.LeaderElection
}

func New(opt Options) (*Federation, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	cs, err := maistraclient.NewForConfig(opt.KubeClient.RESTConfig())
	if err != nil {
		return nil, fmt.Errorf("error creating ClientSet for ServiceMesh: %v", err)
	}
	return internalNew(opt, cs)
}

func internalNew(opt Options, cs maistraclient.Interface) (*Federation, error) {
	resourceManager, err := common.NewResourceManager(common.ControllerOptions{
		KubeClient:   opt.KubeClient,
		MaistraCS:    cs,
		ResyncPeriod: opt.ResyncPeriod,
		Namespace:    opt.FederationNamespace,
	}, opt.KubeClient.GetMemberRoll())
	if err != nil {
		return nil, err
	}
	leaderElection := leaderelection.NewLeaderElection(opt.IstiodNamespace, opt.IstiodPodName, "servicemesh-federation", "test", opt.KubeClient)
	name := types.NamespacedName{Name: opt.IstiodPodName, Namespace: opt.IstiodNamespace}
	statusManager := status.NewManager(name, resourceManager, leaderElection)
	configStore := newConfigStore()
	srv, err := server.NewServer(server.Options{
		BindAddress: opt.BindAddress,
		Env:         opt.Env,
		Network:     opt.LocalNetwork,
		ConfigStore: configStore,
		TLSConfig:   opt.TLSConfig,
	})
	if err != nil {
		return nil, err
	}
	exportController, err := exports.NewController(exports.Options{
		ResourceManager:      resourceManager,
		ResyncPeriod:         opt.ResyncPeriod,
		ServiceExportManager: srv,
	})
	if err != nil {
		return nil, err
	}
	importController, err := imports.NewController(imports.Options{
		ResourceManager:   resourceManager,
		ResyncPeriod:      opt.ResyncPeriod,
		ServiceController: opt.ServiceController,
	})
	if err != nil {
		return nil, err
	}
	discoveryController, err := discovery.NewController(discovery.Options{
		ResourceManager:   resourceManager,
		LocalClusterID:    opt.LocalClusterID,
		LocalNetwork:      opt.LocalNetwork,
		ServiceController: opt.ServiceController,
		XDSUpdater:        opt.XDSUpdater,
		Env:               opt.Env,
		ConfigStore:       configStore,
		FederationManager: srv,
		StatusManager:     statusManager,
	})
	if err != nil {
		return nil, err
	}

	federation := &Federation{
		configStore:         configStore,
		server:              srv,
		exportController:    exportController,
		importController:    importController,
		discoveryController: discoveryController,
		leaderElection:      leaderElection,
		resourceManager:     resourceManager,
	}
	return federation, nil
}

func newConfigStore() model.ConfigStoreController {
	return memory.NewController(memory.Make(schemas))
}

func (f *Federation) ConfigStore() model.ConfigStoreController {
	return f.configStore
}

func (f *Federation) RegisterServiceHandlers(serviceController *aggregate.Controller) {
	serviceController.AppendServiceHandler(f.server.UpdateService)
}

func (f *Federation) StartControllers(stopCh <-chan struct{}) {
	go f.leaderElection.Run(stopCh)
	go f.exportController.Start(stopCh)
	go f.importController.Start(stopCh)
	f.discoveryController.Start(stopCh)
}

func (f *Federation) HasSynced() bool {
	return f.resourceManager.HasSynced()
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
