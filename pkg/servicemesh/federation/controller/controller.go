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
package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/federation"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	kubecontroller "istio.io/istio/pkg/kube/controller"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	clientsetservicemeshv1alpha1 "istio.io/istio/pkg/servicemesh/client/v1alpha1/clientset/versioned"
	informersservicemeshv1alpha1 "istio.io/istio/pkg/servicemesh/client/v1alpha1/informers/externalversions/servicemesh/v1alpha1"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/pkg/log"
)

func init() {
	schemasBuilder := collection.NewSchemasBuilder()
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Destinationrules)
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Virtualservices)
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Gateways)
	schemasBuilder.MustAdd(collections.IstioSecurityV1Beta1Authorizationpolicies)
	// XXX: we should consider adding this directly to the service registry
	schemasBuilder.MustAdd(collections.IstioNetworkingV1Alpha3Serviceentries)
	schemas = schemasBuilder.Build()
}

var (
	logger  = log.RegisterScope("federation-controller", "federation-controller", 0)
	schemas collection.Schemas
)

const (
	defaultResyncPeriod = 60 * time.Second
)

type Options struct {
	MemberRollController memberroll.MemberRollController
	ClientSet            clientsetservicemeshv1alpha1.Interface
	ResyncPeriod         time.Duration
	Namespace            string
	ServiceController    *aggregate.Controller
	XDSUpdater           model.XDSUpdater
	Env                  *model.Environment
}

type Controller struct {
	*kubecontroller.Controller
	model.ConfigStoreCache
	cs           clientsetservicemeshv1alpha1.Interface
	env          *model.Environment
	sc           *aggregate.Controller
	xds          model.XDSUpdater
	mu           sync.Mutex
	stopChannels map[string]chan struct{}
}

var _ model.ConfigStore = (*Controller)(nil)
var _ model.ConfigStoreCache = (*Controller)(nil)

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	opt.validate()

	var informer cache.SharedIndexInformer
	// Currently, we only watch istio system namespace for MeshFederation resources, which is why this block is disabled.
	if opt.MemberRollController != nil && false {
		newInformer := func(namespace string) cache.SharedIndexInformer {
			return cache.NewSharedIndexInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return opt.ClientSet.MaistraV1alpha1().MeshFederations(namespace).List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						return opt.ClientSet.MaistraV1alpha1().MeshFederations(namespace).Watch(context.TODO(), options)
					},
				},
				&v1alpha1.MeshFederation{},
				opt.ResyncPeriod,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			)
		}

		namespaceSet := xnsinformers.NewNamespaceSet()
		informer = xnsinformers.NewMultiNamespaceInformer(namespaceSet, opt.ResyncPeriod, newInformer)
		opt.MemberRollController.Register(namespaceSet, "federation-controller")
	} else {
		informer = informersservicemeshv1alpha1.NewMeshFederationInformer(
			opt.ClientSet, opt.Namespace, opt.ResyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	}

	controller := &Controller{
		ConfigStoreCache: memory.NewController(memory.Make(schemas)),
		cs:               opt.ClientSet,
		env:              opt.Env,
		sc:               opt.ServiceController,
		stopChannels:     make(map[string]chan struct{}),
		xds:              opt.XDSUpdater,
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
	go c.ConfigStoreCache.Run(stopChan)
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
	logger.Debugf("Reconciling MeshFederation %s", resourceName)
	defer func() {
		logger.Infof("Completed reconciliation of MeshFederation %s", resourceName)
	}()

	ctx := context.TODO()

	namespace, name, err := cache.SplitMetaNamespaceKey(resourceName)
	if err != nil {
		logger.Errorf("error splitting resource name: %s", resourceName)
	}
	instance, err := c.cs.MaistraV1alpha1().MeshFederations(namespace).Get(
		ctx, name, metav1.GetOptions{
			TypeMeta: metav1.TypeMeta{
				Kind:       "MeshFederation",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
		})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsGone(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			err = c.delete(ctx, &v1alpha1.MeshFederation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			})
			if err == nil {
				logger.Info("MeshFederation deleted")
			}
		}
		// Error reading the object
		return err
	}

	return c.update(ctx, instance)
}

func (c *Controller) update(ctx context.Context, instance *v1alpha1.MeshFederation) error {
	registry := c.getRegistry(instance.Name)

	egressGatewayService := fmt.Sprintf("%s.%s.svc.cluster.local",
		instance.Spec.Gateways.Egress.Name, instance.Namespace)

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
		if federationRegistry, ok := registry.(*federation.Controller); ok {
			if federationRegistry.NetworkAddress() != instance.Spec.NetworkAddress {
				// TODO: support updates
				logger.Warnf("updating NetworkAddress for MeshFederation (%s) is not supported", instance.Name)
			}
		} else {
			return fmt.Errorf("registry %s is not a Federation registry (type=%T)", instance.Name, registry)
		}
	} else {
		// if there's no existing registry
		logger.Infof("Creating Istio resources for Federation discovery")
		if err := c.createDiscoveryResources(ctx, instance, c.env.Mesh()); err != nil {
			return err
		}
		logger.Infof("Initializing Federation service registry %q at %s", instance.Name, instance.Spec.NetworkAddress)
		// create a registry instance
		options := federation.Options{
			NetworkAddress: instance.Spec.NetworkAddress,
			EgressService:  egressGatewayService,
			ClusterID:      instance.Name,
			XDSUpdater:     c.xds,
			ResyncPeriod:   time.Minute * 5,
			NetworkName:    fmt.Sprintf("network-%s", instance.Name),
		}
		registry = federation.NewController(options)
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

func (c *Controller) delete(ctx context.Context, instance *v1alpha1.MeshFederation) error {
	var allErrors []error
	registry := c.getRegistry(instance.Name)
	if registry != nil {
		// make sure it's one of ours
		if registry.Provider() == serviceregistry.Federation {
			// unregister federation registry
			logger.Infof("Removing registry for Federation cluster %s", instance.Name)
			c.sc.DeleteRegistry(registry.Cluster())
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

func (opt Options) validate() {
	// we want a hard fail.  most of these would be caused by programming errors
	if opt.ClientSet == nil {
		panic("Client field must not be nil")
	}
	if opt.ServiceController == nil {
		panic("ServiceController field must not be nil")
	}
	if opt.XDSUpdater == nil {
		panic("XDSUpdater field must not be nil")
	}
	if opt.Env == nil {
		panic("Env field must not be nil")
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = defaultResyncPeriod
		logger.Warnf("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
}
