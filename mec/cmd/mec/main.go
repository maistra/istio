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
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mec/pkg/pullstrategy/ossm"
	"istio.io/istio/mec/pkg/server"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
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
)

func main() {
	cmd := createCommand(os.Args[1:])
	if err := cmd.Execute(); err != nil {
		log.Errora(err)
	}
}

func createCommand(args []string) *cobra.Command {
	cmd := &cobra.Command{
		Use: "mec [flags]",
		Run: func(cmd *cobra.Command, args []string) {
			resyncPeriod, err := time.ParseDuration(resyncPeriod)
			if err != nil {
				log.Warnf("Failed to parse --resyncPeriod parameter, using default 5m: %v", err)
				resyncPeriod = defaultResyncPeriod
			}
			config, err := kube.BuildClientConfig("", "")
			if err != nil {
				log.Errorf("Failed to BuildClientConfig(): %v", err)
			}
			mrc, err := memberroll.NewMemberRollController(
				config,
				namespace,
				"default",
				resyncPeriod,
			)
			if err != nil {
				log.Errorf("Failed to create MemberRoll Controller: %v", err)
			}
			ec, err := extension.NewControllerFromConfigFile(
				"",
				[]string{"test"},
				mrc,
				resyncPeriod,
			)
			if err != nil {
				log.Errorf("Failed to create Extension Controller: %v", err)
			}

			p, err := ossm.NewOSSMPullStrategy(config, namespace)
			if err != nil {
				log.Errorf("Failed to create OSSMPullStrategy: %v", err)
			}
			w, err := server.NewWorker(config, p, baseURL, serveDirectory)
			if err != nil {
				log.Errorf("Failed to create worker: %v", err)
				return
			}
			ec.RegisterEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					w.Queue <- server.ExtensionEvent{
						Extension: obj.(*v1alpha1.ServiceMeshExtension).DeepCopy(),
						Operation: server.ExtensionEventOperationAdd,
					}
				},
				DeleteFunc: func(obj interface{}) {
					w.Queue <- server.ExtensionEvent{
						Extension: obj.(*v1alpha1.ServiceMeshExtension).DeepCopy(),
						Operation: server.ExtensionEventOperationDelete,
					}
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					w.Queue <- server.ExtensionEvent{
						Extension: newObj.(*v1alpha1.ServiceMeshExtension).DeepCopy(),
						Operation: server.ExtensionEventOperationUpdate,
					}
				},
			})

			ws := server.NewHTTPServer(8080, serveDirectory)

			fw := filewatcher.NewWatcher()
			err = fw.Add(tokenPath)
			if err != nil {
				_ = fw.Close()
				log.Errorf("Error while creating watch on token file %s: %v", tokenPath, err)
			}

			stopChan := make(chan struct{}, 1)

			go func() {
				ch := fw.Events(tokenPath)
				if ch != nil {
					for {
						select {
						case <-ch:
							log.Infof("Token file updated. Logging in to registry")
							tokenBytes, err := ioutil.ReadFile(tokenPath)
							if err != nil {
								log.Errorf("Error reading token file %s: %v", tokenPath, err)
							}
							token := strings.TrimSpace(string(tokenBytes))
							output, err := p.Login(registryURL, token)
							if err != nil {
								log.Errorf("Error logging in to registry: %v", err)
							} else {
								log.Infof("%s", output)
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

			sigc := make(chan os.Signal, 1)
			signal.Notify(sigc,
				syscall.SIGHUP,
				syscall.SIGINT,
				syscall.SIGTERM,
				syscall.SIGQUIT)
			<-sigc
			fw.Close()
			close(stopChan)
		},
	}

	cmd.SetArgs(args)
	cmd.PersistentFlags().StringVar(&resyncPeriod, "resyncPeriod", "5m", "Resync Period for the K8s controllers")
	cmd.PersistentFlags().StringVar(&baseURL, "baseURL", "http://mec.istio-system.svc.cluster.local", "Base URL")
	cmd.PersistentFlags().StringVar(&tokenPath, "tokenPath", "/var/run/secrets/kubernetes.io/serviceaccount/token",
		"File containing to the ServiceAccount token to be used for communication with the K8s API server")
	cmd.PersistentFlags().StringVar(&serveDirectory, "serveDirectory", "/srv",
		"Directory form where WASM modules are served")
	cmd.PersistentFlags().StringVar(&registryURL, "registryURL", "image-registry.openshift-image-registry.svc:5000",
		"Registry from which to pull images by default")
	cmd.PersistentFlags().StringVar(&namespace, "namespace", "istio-system", "The namespace that MEC is running in")

	return cmd
}
