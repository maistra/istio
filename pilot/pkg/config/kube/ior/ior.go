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

package ior

import (
	"fmt"
	"sync"

	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/servicemesh/controller"
	"istio.io/pkg/log"
)

// IORLog is IOR-scoped log
var IORLog = log.RegisterScope("ior", "IOR logging", 0)

// Register configures IOR component to respond to Gateway creations and removals
func Register(
	k8sClient KubeClient,
	routerClient routev1.RouteV1Interface,
	store model.ConfigStoreController,
	pilotNamespace string,
	mrc controller.MemberRollController,
	stop <-chan struct{},
	errorChannel chan error,
) error {
	IORLog.Info("Registering IOR component")

	r := newRoute(k8sClient, routerClient, store, pilotNamespace, mrc, stop)
	r.errorChannel = errorChannel

	alive := true
	var aliveLock sync.Mutex
	go func(stop <-chan struct{}) {
		// Stop responding to events when we are no longer a leader.
		// Two notes here:
		// (1) There's no such method "UnregisterEventHandler()"
		// (2) It might take a few seconds to this channel to be closed. So, both pods might be leader for a few seconds.
		<-stop
		IORLog.Info("This pod is no longer a leader. IOR stopped responding")
		aliveLock.Lock()
		alive = false
		aliveLock.Unlock()
	}(stop)

	IORLog.Debugf("Registering IOR into Istio's Gateway broadcast")
	kind := collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind()
	store.RegisterEventHandler(kind, func(old, curr config.Config, event model.Event) {
		aliveLock.Lock()
		defer aliveLock.Unlock()
		if !alive {
			return
		}

		// encapsulate in goroutine to not slow down processing because of waiting for mutex
		go func() {
			_, ok := curr.Spec.(*networking.Gateway)
			if !ok {
				IORLog.Errorf("could not decode object as Gateway. Object = %v", curr)
				return
			}

			debugMessage := fmt.Sprintf("Event %v arrived:", event)
			if event == model.EventUpdate {
				debugMessage += fmt.Sprintf("\tOld object: %v", old)
			}
			debugMessage += fmt.Sprintf("\tNew object: %v", curr)
			IORLog.Debug(debugMessage)

			if err := r.handleEvent(event, curr); err != nil {
				IORLog.Errora(err)
				if r.errorChannel != nil {
					r.errorChannel <- err
				}
			}
		}()
	})

	return nil
}
