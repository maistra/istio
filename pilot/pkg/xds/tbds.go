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

package xds

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v1 "maistra.io/api/security/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	fedmodel "istio.io/istio/pkg/servicemesh/federation/model"
)

// TbdsGenerator generates trust bundle configuration for proxies to consume
type TbdsGenerator struct {
	TrustBundleProvider fedmodel.TrustBundleProvider
}

var _ model.XdsResourceGenerator = &TbdsGenerator{}

func tbdsNeedsPush(req *model.PushRequest) bool {
	if req == nil {
		return true
	}

	if !req.Full {
		return false
	}

	return len(req.ConfigsUpdated) == 0
}

// Generate returns protobuf containing TrustBundle for given proxy
func (e *TbdsGenerator) Generate(_ *model.Proxy, _ *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !tbdsNeedsPush(req) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	if e.TrustBundleProvider == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}
	tb := &v1.TrustBundleResponse{}
	trustBundles := e.TrustBundleProvider.GetTrustBundles()
	if len(trustBundles) == 0 {
		return nil, model.DefaultXdsLogDetails, nil
	}
	for td, cert := range trustBundles {
		tb.TrustBundles = append(tb.TrustBundles, &v1.TrustBundle{
			TrustDomain: td,
			RootCert:    cert,
		})
	}
	resources := model.Resources{&discovery.Resource{Resource: protoconv.MessageToAny(tb)}}
	return resources, model.DefaultXdsLogDetails, nil
}
