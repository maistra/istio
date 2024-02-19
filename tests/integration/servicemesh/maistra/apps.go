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
	"sort"
	"strconv"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type AppOpts struct {
	Revision string
}

func DeployEchos(apps *echo.Instances, appNamespaces map[string]namespace.Getter, opts *AppOpts) func(t resource.Context) error {
	return func(t resource.Context) error {
		// apps are sorted by name to deterministically fetch expected app from the apps variable
		var appNames []string
		for name := range appNamespaces {
			appNames = append(appNames, name)
		}
		sort.Strings(appNames)

		echoBuilder := deployment.New(t).WithClusters(t.Clusters()...)
		for _, appName := range appNames {
			appConf := echo.Config{
				Service:   appName,
				Namespace: appNamespaces[appName].Get(),
				Ports:     ports.All(),
			}
			if opts != nil && opts.Revision != "" {
				appConf.Subsets = []echo.SubsetConfig{
					{
						Labels: map[string]string{
							"istio.io/rev": opts.Revision,
						},
					},
				}
			} else {
				appConf.Subsets = []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarInject: {
								Value: strconv.FormatBool(false),
							},
						},
					},
				}
			}
			echoBuilder.WithConfig(appConf)
		}
		var err error
		*apps, err = echoBuilder.Build()
		return err
	}
}
