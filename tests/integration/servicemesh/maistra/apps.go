//go:build integ
// +build integ

//
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

package maistra

import (
	"fmt"
	"sync"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type AppOpts struct {
	Name        string
	Namespace   namespace.Getter
	ClusterName string
}

func DeployEchos(apps *echo.Instances, appsMutex *sync.Mutex, opts AppOpts) func(t resource.Context) error {
	return func(t resource.Context) error {
		appConf := echo.Config{
			Service:   opts.Name,
			Namespace: opts.Namespace.Get(),
			Ports: echo.Ports{
				{
					Name:         ports.GRPC,
					Protocol:     protocol.GRPC,
					ServicePort:  7070,
					WorkloadPort: 17070,
				},
			},
		}

		var echoBuilder deployment.Builder
		var targetCluster cluster.Cluster
		if opts.ClusterName != "" {
			targetCluster = t.Clusters().GetByName(opts.ClusterName)
			if targetCluster == nil {
				return fmt.Errorf("did not find cluster by name %s", opts.ClusterName)
			}
			appConf.Cluster = targetCluster
			echoBuilder = deployment.New(t).WithClusters(targetCluster)
		} else {
			echoBuilder = deployment.New(t).WithClusters(t.Clusters()...)
		}
		echoBuilder.WithConfig(appConf)

		newApp, err := echoBuilder.Build()
		if err != nil {
			return err
		}

		appsMutex.Lock()
		defer appsMutex.Unlock()
		*apps = apps.Append(newApp)
		return nil
	}
}
