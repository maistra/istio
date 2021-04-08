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
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/kube"
)

func TestInvalidOptions(t *testing.T) {
	testCases := []struct {
		name string
		opt  Options
	}{
		{
			name: "client",
			opt: Options{
				Client:            nil,
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
				Env:               &model.Environment{},
			},
		},
		{
			name: "service-controller",
			opt: Options{
				Client:            kube.NewFakeClient(),
				ServiceController: nil,
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
				Env:               &model.Environment{},
			},
		},
		{
			name: "xds-updater",
			opt: Options{
				Client:            kube.NewFakeClient(),
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        nil,
				Env:               &model.Environment{},
			},
		},
		{
			name: "env",
			opt: Options{
				Client:            kube.NewFakeClient(),
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
				Env:               nil,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("failed to panic")
				}
			}()
			NewController(tc.opt)
		})
	}
}

func TestRegister(t *testing.T) {
	testCases := []struct {
		name string
		opt  Options
	}{
		{
			name: "",
			opt: Options{
				Client:            nil,
				ServiceController: &aggregate.Controller{},
				XDSUpdater:        &v1alpha3.FakeXdsUpdater{},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
		})
	}
}
