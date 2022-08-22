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

package common

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/errors"
	maistraclient "maistra.io/api/client/versioned"

	"istio.io/istio/pkg/kube"
)

type ControllerOptions struct {
	KubeClient   kube.Client
	MaistraCS    maistraclient.Interface
	ResyncPeriod time.Duration
	Namespace    string
}

func (opt ControllerOptions) validate() error {
	var allErrors []error
	if opt.KubeClient == nil {
		allErrors = append(allErrors, fmt.Errorf("the KubeClient field must not be nil"))
	}
	if opt.MaistraCS == nil {
		allErrors = append(allErrors, fmt.Errorf("the MaistraCS field must not be nil"))
	}
	if opt.ResyncPeriod == 0 {
		opt.ResyncPeriod = DefaultResyncPeriod
		Logger.WithLabels("component", "ControllerOptions").Infof("ResyncPeriod not specified, defaulting to %s", opt.ResyncPeriod)
	}
	return errors.NewAggregate(allErrors)
}
