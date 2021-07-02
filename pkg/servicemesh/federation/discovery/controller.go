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
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	maistrainformers "maistra.io/api/client/informers/externalversions/core/v1"
	maistraclient "maistra.io/api/client/versioned"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	federationregistry "istio.io/istio/pilot/pkg/serviceregistry/federation"
	"istio.io/istio/pkg/kube"
	kubecontroller "istio.io/istio/pkg/kube/controller"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/istio/pkg/servicemesh/federation/common"
	"istio.io/istio/pkg/servicemesh/federation/server"
	"istio.io/istio/pkg/servicemesh/federation/status"
	"istio.io/istio/pkg/spiffe"
)

const controllerName = "federation-discovery-controller"

type Options struct {
	common.ControllerOptions
	ServiceController *aggregate.Controller
	XDSUpdater        model.XDSUpdater
	Env               *model.Environment
	ConfigStore       model.ConfigStoreCache
	FederationManager server.FederationManager
	StatusManager     status.Manager
	LocalNetwork      string
	LocalClusterID    string
}

type Controller struct {
	*kubecontroller.Controller
	model.ConfigStoreCache
	localNetwork      string
	localClusterID    string
	kubeClient        kube.Client
	cs                maistraclient.Interface
	env               *model.Environment
	federationManager server.FederationManager
	statusManager     status.Manager
	sc                *aggregate.Controller
	xds               model.XDSUpdater
	mu                sync.Mutex
	stopChannels      map[string]chan struct{}
	trustBundles      map[string]string
}

var _ model.ConfigStore = (*Controller)(nil)
var _ model.ConfigStoreCache = (*Controller)(nil)

// NewController creates a new Aggregate controller
func NewController(opt Options) (*Controller, error) {
	if err := opt.validate(); err != nil {
		return nil, err
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
						return cs.CoreV1().MeshFederations(namespace).List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						return cs.CoreV1().MeshFederations(namespace).Watch(context.TODO(), options)
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
		informer = maistrainformers.NewMeshFederationInformer(
			cs, opt.Namespace, opt.ResyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	}

	controller := &Controller{
		ConfigStoreCache:  opt.ConfigStore,
		localClusterID:    opt.LocalClusterID,
		localNetwork:      opt.LocalNetwork,
		kubeClient:        opt.KubeClient,
		cs:                cs,
		env:               opt.Env,
		sc:                opt.ServiceController,
		stopChannels:      make(map[string]chan struct{}),
		xds:               opt.XDSUpdater,
		federationManager: opt.FederationManager,
		statusManager:     opt.StatusManager,
		trustBundles:      map[string]string{},
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

func (c *Controller) Run(stopChan <-chan struct{}) {
	c.Controller.Start(stopChan)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, registryStopCh := range c.stopChannels {
		close(registryStopCh)
	}
}

func (c *Controller) HasSynced() bool {
	return c.Controller.HasSynced()
}

func (c *Controller) reconcile(resourceName string) error {
	c.Logger.Debugf("Reconciling MeshFederation %s", resourceName)
	defer func() {
		if err := c.statusManager.PushStatus(); err != nil {
			c.Logger.Errorf("error pushing FederationStatus for mesh %s: %s", resourceName, err)
		}
		c.Logger.Debugf("Completed reconciliation of MeshFederation %s", resourceName)
	}()

	ctx := context.TODO()

	namespace, name, err := cache.SplitMetaNamespaceKey(resourceName)
	if err != nil {
		c.Logger.Errorf("error splitting resource name: %s", resourceName)
	}
	instance, err := c.cs.CoreV1().MeshFederations(namespace).Get(
		ctx, name, metav1.GetOptions{
			TypeMeta: metav1.TypeMeta{
				Kind:       "MeshFederation",
				APIVersion: v1.SchemeGroupVersion.String(),
			},
		})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsGone(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			err = c.delete(ctx, &v1.MeshFederation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			})
			if err == nil {
				c.Logger.Info("MeshFederation deleted")
			}
		}
		// Error reading the object
		return err
	}

	return c.update(ctx, instance)
}

func (c *Controller) update(ctx context.Context, instance *v1.MeshFederation) error {
	registry := c.getRegistry(instance.Name)

	egressGatewayService := fmt.Sprintf("%s.%s.svc.%s",
		instance.Spec.Gateways.Egress.Name, instance.Namespace, c.env.GetDomainSuffix())
	var err error
	var remoteCert string
	if instance.Spec.Security.TrustDomain != "" {
		extraTrustedCerts := []*x509.Certificate{}
		if instance.Spec.Security.CertificateChain != "" {
			// if fetching fails, we'll use what the user supplied as remote root
			remoteCert = instance.Spec.Security.CertificateChain
			block, _ := pem.Decode([]byte(instance.Spec.Security.CertificateChain))
			if block == nil {
				c.Logger.Warnf("failed to parse certificate from MeshFederation resource '%s'", instance.Name)
			} else {
				extraTrustedCerts, err = x509.ParseCertificates(block.Bytes)
				if err != nil {
					c.Logger.Warnf("failed to parse certificate from MeshFederation resource '%s'", instance.Name)
				}
			}
		}
		endpoint := fmt.Sprintf("https://%s:%d/trust_bundle", instance.Spec.NetworkAddress, 8188)
		tbMap, err := spiffe.RetrieveSpiffeBundleRootCerts(
			map[string]string{
				instance.Spec.Security.TrustDomain: endpoint,
			},
			extraTrustedCerts,
		)
		if err != nil {
			c.Logger.Errorf("failed to retrieve certificate from NetworkAddress for MeshFederation resource '%s'", instance.Name)
		} else {
			remoteCert = ""
			for _, cert := range tbMap[instance.Spec.Security.TrustDomain] {
				b := &pem.Block{
					Type:  "CERTIFICATE",
					Bytes: cert.Raw,
				}
				var buf bytes.Buffer
				if err := pem.Encode(&buf, b); err != nil {
					c.Logger.Errorf("failed to encode certificate retrieved from %s into PEM format: %s", endpoint, err)
				} else {
					remoteCert += buf.String()
				}
			}
		}
		c.updateRootCert(instance.Spec.Security.TrustDomain, remoteCert)
	}

	// check for existing registry
	if registry != nil {
		// if there's an existing registry
		// make sure it's one of ours
		if registry.Provider() != serviceregistry.Federation {
			return fmt.Errorf(
				"cannot create Federation registry for %s, registry exists and belongs to another provider (%s)",
				instance.Name, registry.Provider())
		}
		// check to see if it needs updating
		// TODO: support updates
		if federationRegistry, ok := registry.(*federationregistry.Controller); ok {
			if federationRegistry.NetworkAddress() != instance.Spec.NetworkAddress {
				// TODO: support updates
				c.Logger.Warnf("updating NetworkAddress for MeshFederation (%s) is not supported", instance.Name)
			}
		} else {
			return fmt.Errorf("registry %s is not a Federation registry (type=%T)", instance.Name, registry)
		}
	} else {
		// if there's no existing registry
		c.Logger.Infof("Creating export handler for Federation to %s", instance.Name)
		exportConfig, err := c.cs.CoreV1().ServiceExports(instance.Namespace).Get(context.TODO(), instance.Name, metav1.GetOptions{})
		if err != nil && !(apierrors.IsNotFound(err) || apierrors.IsGone(err)) {
			c.Logger.Errorf("error retrieving ServiceExports associated with MeshFederation %s: %s", instance.Name, err)
			return err
		}
		defaultImportConfig, err := c.cs.CoreV1().ServiceImports(instance.Namespace).Get(context.TODO(), "default", metav1.GetOptions{})
		if err != nil && !(apierrors.IsNotFound(err) || apierrors.IsGone(err)) {
			c.Logger.Errorf("error retrieving default ServiceImports associated with MeshFederation %s: %s", instance.Name, err)
			return err
		}
		importConfig, err := c.cs.CoreV1().ServiceImports(instance.Namespace).Get(context.TODO(), instance.Name, metav1.GetOptions{})
		if err != nil && !(apierrors.IsNotFound(err) || apierrors.IsGone(err)) {
			c.Logger.Errorf("error retrieving ServiceImports associated with MeshFederation %s: %s", instance.Name, err)
			return err
		}

		statusHandler := c.statusManager.FederationAdded(types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace})
		if err := c.federationManager.AddMeshFederation(instance, exportConfig, statusHandler); err != nil {
			return err
		}

		c.Logger.Infof("Creating Istio resources for Federation discovery from %s", instance.Name)
		if err := c.createDiscoveryResources(ctx, instance, c.env.Mesh()); err != nil {
			return err
		}

		c.Logger.Infof("Initializing Federation service registry for %q at %s", instance.Name, instance.Spec.NetworkAddress)
		// create a registry instance
		options := federationregistry.Options{
			NetworkAddress: instance.Spec.NetworkAddress,
			EgressName:     instance.Spec.Gateways.Egress.Name,
			EgressService:  egressGatewayService,
			Namespace:      instance.Namespace,
			UseDirectCalls: instance.Spec.Security.AllowDirectOutbound,
			KubeClient:     c.kubeClient,
			ConfigStore:    c.ConfigStoreCache,
			StatusHandler:  statusHandler,
			XDSUpdater:     c.xds,
			ResyncPeriod:   time.Minute * 5,
			DomainSuffix:   c.env.GetDomainSuffix(),
			LocalClusterID: c.localClusterID,
			LocalNetwork:   c.localNetwork,
			ClusterID:      instance.Name,
			Network:        fmt.Sprintf("network-%s", instance.Name),
		}
		registry = federationregistry.NewController(options, instance, defaultImportConfig, importConfig)
		// register the new instance
		c.sc.AddRegistry(registry)

		stopCh := make(chan struct{})
		c.mu.Lock()
		defer c.mu.Unlock()
		c.stopChannels[instance.Name] = stopCh
		go registry.Run(stopCh)
	}

	return nil
}

func (c *Controller) delete(ctx context.Context, instance *v1.MeshFederation) error {
	var allErrors []error
	// delete the server
	c.federationManager.DeleteMeshFederation(instance.Name)

	// delete trust bundle
	if instance.Spec.Security.TrustDomain != "" {
		c.updateRootCert(instance.Spec.Security.TrustDomain, "")
	}

	// delete the registry
	registry := c.getRegistry(instance.Name)
	if registry != nil {
		// make sure it's one of ours
		if registry.Provider() == serviceregistry.Federation {
			// unregister federation registry
			c.Logger.Infof("Removing registry for Federation cluster %s", instance.Name)
			c.sc.DeleteRegistry(registry.Cluster(), serviceregistry.Federation)
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

	c.statusManager.FederationDeleted(types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace})

	return utilerrors.NewAggregate(allErrors)
}

func (c *Controller) getRegistry(name string) serviceregistry.Instance {
	for _, registry := range c.sc.GetRegistries() {
		if registry.Cluster() == name {
			return registry
		}
	}
	return nil
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.KubeClient == nil {
		allErrors = append(allErrors, fmt.Errorf("the KubeClient field must not be nil"))
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
