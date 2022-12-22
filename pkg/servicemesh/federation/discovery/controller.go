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

package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	v1 "maistra.io/api/federation/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	federationregistry "istio.io/istio/pilot/pkg/serviceregistry/federation"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	kubecontroller "istio.io/istio/pkg/kube/controller"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/server"
	"istio.io/istio/pkg/servicemesh/federation/status"
)

const controllerName = "federation-discovery-controller"

type Options struct {
	ResourceManager   common.ResourceManager
	ResyncPeriod      time.Duration
	ServiceController *aggregate.Controller
	XDSUpdater        model.XDSUpdater
	Env               *model.Environment
	ConfigStore       model.ConfigStoreController
	FederationManager server.FederationManager
	StatusManager     status.Manager
	LocalNetwork      string
	LocalClusterID    string
}

type Controller struct {
	*kubecontroller.Controller
	model.ConfigStoreController
	localNetwork      string
	localClusterID    string
	rm                common.ResourceManager
	env               *model.Environment
	federationManager server.FederationManager
	statusManager     status.Manager
	sc                *aggregate.Controller
	xds               model.XDSUpdater
	mu                sync.Mutex
	stopChannels      map[cluster.ID]chan struct{}
	trustBundles      map[string]string
}

var (
	_ model.ConfigStore           = (*Controller)(nil)
	_ model.ConfigStoreController = (*Controller)(nil)
)

// NewController creates a new Aggregate controller
func NewController(opt Options) (*Controller, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}

	logger := common.Logger.WithLabels("component", controllerName)

	controller := &Controller{
		ConfigStoreController: opt.ConfigStore,
		localClusterID:        opt.LocalClusterID,
		localNetwork:          opt.LocalNetwork,
		rm:                    opt.ResourceManager,
		env:                   opt.Env,
		sc:                    opt.ServiceController,
		stopChannels:          make(map[cluster.ID]chan struct{}),
		xds:                   opt.XDSUpdater,
		federationManager:     opt.FederationManager,
		statusManager:         opt.StatusManager,
		trustBundles:          map[string]string{},
	}
	internalController := kubecontroller.NewController(kubecontroller.Options{
		Informer:     opt.ResourceManager.PeerInformer().Informer(),
		Logger:       logger,
		ResyncPeriod: opt.ResyncPeriod,
		Reconciler:   controller.reconcile,
	})
	controller.Controller = internalController

	return controller, nil
}

func (c *Controller) Run(stopChan <-chan struct{}) {
	c.Controller.Start(stopChan)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, registryStopCh := range c.stopChannels {
		close(registryStopCh)
	}
}

func (c *Controller) Start(stopChan <-chan struct{}) {
	c.Controller.StartController(stopChan, c.HasSynced)
}

func (c *Controller) HasSynced() bool {
	return c.Controller.HasSynced() && c.rm.KubeClient().KubeInformer().Core().V1().ConfigMaps().Informer().HasSynced()
}

func (c *Controller) RunInformer(_ <-chan struct{}) {
	// no-op, informer is started by the shared factory in Federation.Start()
}

func (c *Controller) reconcile(resourceName string) error {
	c.Logger.Debugf("Reconciling ServiceMeshPeer %s", resourceName)
	defer func() {
		if err := c.statusManager.PushStatus(); err != nil {
			c.Logger.Errorf("error pushing FederationStatus for mesh %s: %s", resourceName, err)
		}
		c.Logger.Debugf("Completed reconciliation of ServiceMeshPeer %s", resourceName)
	}()

	ctx := context.TODO()

	namespace, name, err := cache.SplitMetaNamespaceKey(resourceName)
	if err != nil {
		c.Logger.Errorf("error splitting resource name: %s", resourceName)
	}
	instance, err := c.rm.PeerInformer().Lister().ServiceMeshPeers(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsGone(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			err = c.delete(ctx, &v1.ServiceMeshPeer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			})
			if err == nil {
				c.Logger.Info("ServiceMeshPeer deleted")
			}
		}
		// Error reading the object
		return err
	}

	return c.update(ctx, instance)
}

func (c *Controller) update(ctx context.Context, instance *v1.ServiceMeshPeer) error {
	registry := c.getRegistry(cluster.ID(instance.Name))
	if instance.Spec.Security.TrustDomain != "" && instance.Spec.Security.TrustDomain != c.env.Mesh().GetTrustDomain() {
		rootCert, err := c.getRootCertForMesh(instance)
		if err != nil {
			return fmt.Errorf("could not get root cert for mesh %s: %v", instance.Name, err.Error())
		}
		c.updateRootCert(instance.Spec.Security.TrustDomain, rootCert)
	}

	if err := c.createDiscoveryResources(ctx, instance, c.env.Mesh()); err != nil {
		return err
	}

	importConfig, err := c.rm.ImportsInformer().Lister().ImportedServiceSets(instance.Namespace).Get(instance.Name)
	if err != nil && !(apierrors.IsNotFound(err) || apierrors.IsGone(err)) {
		c.Logger.Errorf("error retrieving ServiceImports associated with ServiceMeshPeer %s: %s", instance.Name, err)
		return err
	}

	// check for existing registry
	if registry != nil {
		// if there's an existing registry, make sure it's one of ours
		if registry.Provider() != provider.Federation {
			return fmt.Errorf(
				"cannot create Federation registry for %s, registry exists and belongs to another provider (%s)",
				instance.Name, registry.Provider())
		}
		if federationRegistry, ok := registry.(*federationregistry.Controller); ok {
			c.Logger.Debugf("updating settings for ServiceMeshPeer %s", instance.Name)
			federationRegistry.UpdatePeerConfig(instance)
			federationRegistry.UpdateImportConfig(importConfig)
		} else {
			return fmt.Errorf("registry %s is not a Federation registry (type=%T)", instance.Name, registry)
		}
	} else {
		// if there's no existing registry
		exportConfig, err := c.rm.ExportsInformer().Lister().ExportedServiceSets(instance.Namespace).Get(instance.Name)
		if err != nil && !(apierrors.IsNotFound(err) || apierrors.IsGone(err)) {
			c.Logger.Errorf("error retrieving ServiceExports associated with ServiceMeshPeer %s: %s", instance.Name, err)
			return err
		}
		statusHandler := c.statusManager.PeerAdded(types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace})
		if err := c.federationManager.AddPeer(instance, exportConfig, statusHandler); err != nil {
			return err
		}
		c.Logger.Infof("initializing Federation service registry for %q at %s", instance.Name, instance.Spec.Remote.Addresses)
		// create a registry instance
		options := federationregistry.Options{
			KubeClient:     c.rm.KubeClient(),
			ConfigStore:    c.ConfigStoreController,
			StatusHandler:  statusHandler,
			XDSUpdater:     c.xds,
			ResyncPeriod:   time.Minute * 5,
			DomainSuffix:   c.env.DomainSuffix,
			LocalClusterID: c.localClusterID,
			LocalNetwork:   c.localNetwork,
			ClusterID:      instance.Name,
			Network:        fmt.Sprintf("network-%s", instance.Name),
		}
		registry = federationregistry.NewController(options, instance, importConfig)
		// register the new instance
		c.sc.AddRegistry(registry)

		stopCh := make(chan struct{})
		c.mu.Lock()
		defer c.mu.Unlock()
		c.stopChannels[cluster.ID(instance.Name)] = stopCh
		go registry.Run(stopCh)
	}

	return nil
}

func (c *Controller) delete(ctx context.Context, instance *v1.ServiceMeshPeer) error {
	var allErrors []error
	// delete the server
	c.federationManager.DeletePeer(instance.Name)

	// delete trust bundle
	if instance.Spec.Security.TrustDomain != "" {
		c.updateRootCert(instance.Spec.Security.TrustDomain, "")
	}

	// delete the registry
	registry := c.getRegistry(cluster.ID(instance.Name))
	if registry != nil {
		// make sure it's one of ours
		if registry.Provider() == provider.Federation {
			// unregister federation registry
			c.Logger.Infof("Removing registry for Federation cluster %s", instance.Name)
			c.sc.DeleteRegistry(registry.Cluster(), provider.Federation)
			c.mu.Lock()
			defer c.mu.Unlock()
			if registryStopCh := c.stopChannels[registry.Cluster()]; registryStopCh != nil {
				close(registryStopCh)
				delete(c.stopChannels, registry.Cluster())
			}
		} else {
			allErrors = append(allErrors,
				fmt.Errorf("cannot delete Federation registry for %s, registry belongs to another provider (%s)",
					instance.Name, registry.Provider()))
		}
	}

	if err := c.deleteDiscoveryResources(ctx, instance); err != nil {
		allErrors = append(allErrors, err)
	}

	c.statusManager.PeerDeleted(types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace})

	return utilerrors.NewAggregate(allErrors)
}

func (c *Controller) getRegistry(clusterID cluster.ID) serviceregistry.Instance {
	for _, registry := range c.sc.GetRegistries() {
		if registry.Cluster() == clusterID {
			return c.sc.Unwrap(registry)
		}
	}
	return nil
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.ResourceManager == nil {
		allErrors = append(allErrors, fmt.Errorf("the ResourceManager field must not be nil"))
	}
	if opt.ServiceController == nil {
		allErrors = append(allErrors, fmt.Errorf("the ServiceController field must not be nil"))
	}
	if opt.XDSUpdater == nil {
		allErrors = append(allErrors, fmt.Errorf("the XDSUpdater field must not be nil"))
	}
	if opt.Env == nil {
		allErrors = append(allErrors, fmt.Errorf("the Env field must not be nil"))
	}
	if opt.FederationManager == nil {
		allErrors = append(allErrors, fmt.Errorf("the FederationManager field must not be nil"))
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = common.DefaultResyncPeriod
		common.Logger.WithLabels("component", controllerName).Warnf("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
	return utilerrors.NewAggregate(allErrors)
}

func (c *Controller) getRootCertForMesh(instance *v1.ServiceMeshPeer) (string, error) {
	if instance == nil {
		return "", nil
	}
	name := instance.Spec.Security.CertificateChain.Name
	if name == "" {
		name = common.DefaultFederationCARootResourceName(instance)
	}
	entryKey := common.DefaultFederationRootCertName
	switch instance.Spec.Security.CertificateChain.Kind {
	case "", "ConfigMap":
		cm, err := c.rm.KubeClient().KubeInformer().Core().V1().ConfigMaps().Lister().ConfigMaps(instance.Namespace).Get(name)
		if err != nil {
			return "", fmt.Errorf("error getting configmap %s in namespace %s: %v", name, instance.Namespace, err)
		}
		if cert, exists := cm.Data[entryKey]; exists {
			return cert, nil
		}
		return "", fmt.Errorf("missing entry %s in ConfigMap %s/%s", entryKey, instance.Namespace, name)
	default:
		return "", fmt.Errorf("unknown Kind for CertificateChain object reference: %s", instance.Spec.Security.CertificateChain.Kind)
	}
}

func (c *Controller) updateRootCert(trustDomain string, rootCert string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if rootCert == "" {
		delete(c.trustBundles, trustDomain)
	} else if existingCert, ok := c.trustBundles[trustDomain]; !ok || existingCert != rootCert {
		c.trustBundles[trustDomain] = rootCert
	} else {
		// we didn't update the trust bundles, so we return early without pushing
		return
	}
	c.xds.ConfigUpdate(&model.PushRequest{
		Full:   true,
		Reason: []model.TriggerReason{model.GlobalUpdate},
	})
}

func (c *Controller) GetTrustBundles() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	// make a copy
	ret := map[string]string{}
	for td, cert := range c.trustBundles {
		ret[td] = cert
	}
	return ret
}
