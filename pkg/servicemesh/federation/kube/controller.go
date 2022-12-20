// Copyright Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kube

import (
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/pkg/log"
)

const (
	maxRetries = 5
)

type (
	ReconcilerFunc func(name string) error
	Options        struct {
		ResyncPeriod time.Duration
		Informer     cache.SharedIndexInformer
		Reconciler   ReconcilerFunc
		Logger       *log.Scope
		HasSynced    func() bool
	}
)

type Controller struct {
	Logger       *log.Scope
	informer     cache.SharedIndexInformer
	queue        workqueue.RateLimitingInterface
	resyncPeriod time.Duration
	reconcile    ReconcilerFunc
	hasSynced    func() bool
}

// NewController creates a new Aggregate controller
func NewController(opt Options) *Controller {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	if opt.Logger == nil {
		opt.Logger = log.RegisterScope("kube-controller", "kube-controller", 0)
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = 60 * time.Second
	}
	controller := &Controller{
		informer:     opt.Informer,
		queue:        queue,
		Logger:       opt.Logger,
		resyncPeriod: opt.ResyncPeriod,
		reconcile:    opt.Reconciler,
		hasSynced:    opt.HasSynced,
	}

	_, err := controller.informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				key, err := cache.MetaNamespaceKeyFunc(obj)
				controller.Logger.Debugf("Processing add: %s", key)
				if err == nil {
					queue.Add(key)
				} else {
					controller.Logger.Errorf("error retrieving key for object %T", obj)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				key, err := cache.MetaNamespaceKeyFunc(newObj)
				controller.Logger.Debugf("Processing update: %s", key)
				if err == nil {
					queue.Add(key)
				} else {
					controller.Logger.Errorf("error retrieving key for object %T", newObj)
				}
			},
			DeleteFunc: func(obj interface{}) {
				key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				controller.Logger.Debugf("Processing delete: %s", key)
				if err == nil {
					queue.Add(key)
				} else {
					controller.Logger.Errorf("error retrieving key for object %T", obj)
				}
			},
		})
	if err != nil {
		controller.Logger.Errorf("failed to add event handler: %s", err)
	}

	return controller
}

func (c *Controller) Start(stopChan <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	t0 := time.Now()
	c.Logger.Info("Starting controller")

	c.RunInformer(stopChan)

	cache.WaitForCacheSync(stopChan, c.hasSynced)
	c.Logger.Infof("Controller synced in %s", time.Since(t0))

	c.Logger.Info("Starting workers")
	wait.Until(c.worker, c.resyncPeriod, stopChan)
}

func (c *Controller) RunInformer(stopChan <-chan struct{}) {
	go c.informer.Run(stopChan)
}

func (c *Controller) worker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	resourceName, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(resourceName)

	err := c.reconcile(resourceName.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(resourceName)
	} else if c.queue.NumRequeues(resourceName) < maxRetries {
		c.Logger.Errorf("Error processing %s (will retry): %v", resourceName, err)
		c.queue.AddRateLimited(resourceName)
	} else {
		c.Logger.Errorf("Error processing %s (giving up): %v", resourceName, err)
		c.queue.Forget(resourceName)
		utilruntime.HandleError(err)
	}

	return true
}
