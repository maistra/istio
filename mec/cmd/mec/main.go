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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/cache"
	v1 "maistra.io/api/core/v1"

	"istio.io/istio/mec/pkg/pullstrategy/ossm"
	"istio.io/istio/mec/pkg/server"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube"
	memberroll "istio.io/istio/pkg/servicemesh/controller"
	"istio.io/istio/pkg/servicemesh/controller/extension"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

const (
	defaultResyncPeriod = time.Minute * 5
)

var (
	baseURL        string
	tokenPath      string
	serveDirectory string
	registryURL    string
	resyncPeriod   string
	namespace      string

	mainlog        = log.RegisterScope("main", "Main function", 0)
	loggingOptions = log.DefaultOptions()
)

func main() {
	rootCmd := createCommand(os.Args[1:])
	_ = rootCmd.Execute()
}

func configureLogging(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	return nil
}

func createCommand(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               "mec [flags]",
		PersistentPreRunE: configureLogging,
		RunE: func(command *cobra.Command, args []string) error {
			resyncPeriod, err := time.ParseDuration(resyncPeriod)
			if err != nil {
				mainlog.Warnf("Failed to parse --resyncPeriod parameter, using default 5m: %v", err)
				resyncPeriod = defaultResyncPeriod
			}

			config, err := kube.BuildClientConfig("", "")
			if err != nil {
				return fmt.Errorf("failed to BuildClientConfig(): %v", err)
			}

			mrc, err := memberroll.NewMemberRollController(
				config,
				namespace,
				"default",
				resyncPeriod,
			)
			if err != nil {
				return fmt.Errorf("failed to create MemberRoll Controller: %v", err)
			}

			ec, err := extension.NewControllerFromConfigFile(
				"",
				[]string{namespace},
				mrc,
				resyncPeriod,
			)
			if err != nil {
				return fmt.Errorf("failed to create Extension Controller: %v", err)
			}

			p, err := ossm.NewOSSMPullStrategy(config)
			if err != nil {
				return fmt.Errorf("failed to create OSSMPullStrategy: %v", err)
			}

			w, err := server.NewWorker(config, p, baseURL, serveDirectory, nil)
			if err != nil {
				return fmt.Errorf("failed to create worker: %v", err)
			}

			ec.RegisterEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					w.Queue <- server.ExtensionEvent{
						Extension: obj.(*v1.ServiceMeshExtension).DeepCopy(),
						Operation: server.ExtensionEventOperationAdd,
					}
				},
				DeleteFunc: func(obj interface{}) {
					w.Queue <- server.ExtensionEvent{
						Extension: obj.(*v1.ServiceMeshExtension).DeepCopy(),
						Operation: server.ExtensionEventOperationDelete,
					}
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					w.Queue <- server.ExtensionEvent{
						Extension: newObj.(*v1.ServiceMeshExtension).DeepCopy(),
						Operation: server.ExtensionEventOperationUpdate,
					}
				},
			})

			ws := server.NewHTTPServer(8080, serveDirectory)

			fw := filewatcher.NewWatcher()
			defer fw.Close()
			err = fw.Add(tokenPath)
			if err != nil {
				return fmt.Errorf("error while creating watch on token file %s: %v", tokenPath, err)
			}

			stopChan := make(chan struct{}, 1)

			go func() {
				ch := fw.Events(tokenPath)
				if ch != nil {
					for {
						select {
						case <-ch:
							mainlog.Infof("Token file updated. Logging in to registry")
							tokenBytes, err := ioutil.ReadFile(tokenPath)
							if err != nil {
								mainlog.Errorf("Error reading token file %s: %v", tokenPath, err)
								continue
							}
							token := strings.TrimSpace(string(tokenBytes))
							output, err := p.Login(registryURL, token)
							if err != nil {
								mainlog.Errorf("Error logging in to registry: %v", err)
							} else {
								mainlog.Infof("%s", output)
							}
						case <-stopChan:
							return
						}
					}
				}
			}()
			fw.Events(tokenPath) <- fsnotify.Event{}

			mrc.Start(stopChan)
			ec.Start(stopChan)
			w.Start(stopChan)
			ws.Start(stopChan)

			cmd.WaitSignal(stopChan)

			return nil
		},
	}

	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().StringVar(&resyncPeriod, "resyncPeriod", "5m", "Resync Period for the K8s controllers")
	rootCmd.PersistentFlags().StringVar(&baseURL, "baseURL", "http://mec.istio-system.svc.cluster.local", "Base URL")
	rootCmd.PersistentFlags().StringVar(&tokenPath, "tokenPath", "/var/run/secrets/kubernetes.io/serviceaccount/token",
		"File containing to the ServiceAccount token to be used for communication with the K8s API server")
	rootCmd.PersistentFlags().StringVar(&serveDirectory, "serveDirectory", "/srv",
		"Directory form where WASM modules are served")
	rootCmd.PersistentFlags().StringVar(&registryURL, "registryURL", "image-registry.openshift-image-registry.svc:5000",
		"Registry from which to pull images by default")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "istio-system", "The namespace that MEC is running in")

	loggingOptions.AttachCobraFlags(rootCmd)
	return rootCmd
}
