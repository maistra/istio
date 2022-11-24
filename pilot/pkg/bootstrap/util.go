// Copyright Istio Authors
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

package bootstrap

import (
	"net/http"

	"k8s.io/client-go/rest"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/pkg/ledger"
	"istio.io/pkg/log"
)

func hasKubeRegistry(registries []string) bool {
	for _, r := range registries {
		if provider.ID(r) == provider.Kubernetes {
			return true
		}
	}
	return false
}

func buildLedger(ca RegistryOptions) ledger.Ledger {
	var result ledger.Ledger
	if ca.DistributionTrackingEnabled {
		result = ledger.Make(ca.DistributionCacheRetention)
	} else {
		result = &model.DisabledLedger{}
	}
	return result
}

func installRequestLogger(config *rest.Config) {
	config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return requestLogger{rt: rt}
	})
}

type requestLogger struct {
	rt http.RoundTripper
}

func (rl requestLogger) RoundTrip(req *http.Request) (*http.Response, error) {
	log.Infof("Performing Kubernetes API request: %s %s", req.Method, req.URL)
	return rl.rt.RoundTrip(req)
}

var _ http.RoundTripper = requestLogger{}
