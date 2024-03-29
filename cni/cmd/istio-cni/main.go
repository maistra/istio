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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package main

import (
	"fmt"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"

	"istio.io/istio/cni/pkg/plugin"
	"istio.io/pkg/log"
	istioversion "istio.io/pkg/version"
)

func main() {
	if err := log.Configure(plugin.GetLoggingOptions("")); err != nil {
		os.Exit(1)
	}
	defer func() {
		// Log sync will send logs to install-cni container via UDS.
		// We don't need a timeout here because underlying the log pkg already handles it.
		// this may fail, but it should be safe to ignore according
		// to https://github.com/uber-go/zap/issues/328
		_ = log.Sync()
	}()

	// TODO: implement plugin version
	skel.PluginMain(plugin.CmdAdd, plugin.CmdCheck, plugin.CmdDelete, version.All,
		fmt.Sprintf("CNI plugin istio-cni %v", istioversion.Info.Version))
}
