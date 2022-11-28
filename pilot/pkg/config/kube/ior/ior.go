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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// IORLog is IOR-scoped log
var IORLog = log.RegisterScope("ior", "IOR logging", 0)

type IOR struct {
	route
}

func Run(
	kubeClient kube.Client,
	store model.ConfigStoreController,
	stop <-chan struct{},
) {
	IORLog.Info("setting up IOR")
	rc, err := NewRouterClient()
	if err != nil {
		return
	}

	r := newRoute(NewKubeClient(kubeClient), rc, store)

	r.Run(stop)
}
