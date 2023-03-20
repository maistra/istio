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

package ambient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"istio.io/istio/cni/pkg/ambient/constants"
	ebpf "istio.io/istio/cni/pkg/ebpf/server"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/lazy"
)

type Server struct {
	kubeClient kube.Client
	ctx        context.Context
	queue      controllers.Queue

	namespaces kclient.Client[*corev1.Namespace]
	pods       kclient.Client[*corev1.Pod]

	mu         sync.Mutex
	ztunnelPod *corev1.Pod

	iptablesCommand lazy.Lazy[string]
	redirectMode    RedirectMode
	ebpfServer      *ebpf.RedirectServer
}

type AmbientConfigFile struct {
	ZTunnelReady bool   `json:"ztunnelReady"`
	RedirectMode string `json:"redirectMode"`
}

func NewServer(ctx context.Context, args AmbientArgs) (*Server, error) {
	client, err := buildKubeClient(args.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}
	// Set some defaults
	s := &Server{
		ctx:        ctx,
		kubeClient: client,
	}

	s.iptablesCommand = lazy.New(func() (string, error) {
		return s.detectIptablesCommand(), nil
	})

	switch args.RedirectMode {
	case IptablesMode:
		s.redirectMode = IptablesMode
		// We need to find our Host IP -- is there a better way to do this?
		h, err := GetHostIP(s.kubeClient.Kube())
		if err != nil || h == "" {
			return nil, fmt.Errorf("error getting host IP: %v", err)
		}
		HostIP = h
		log.Infof("HostIP=%v", HostIP)
	case EbpfMode:
		s.redirectMode = EbpfMode
		s.ebpfServer = ebpf.NewRedirectServer()
		s.ebpfServer.Start(ctx.Done())
	}

	s.setupHandlers()

	s.UpdateConfig()

	return s, nil
}

func (s *Server) isZTunnelRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ztunnelPod != nil
}

// buildKubeClient creates the kube client
func buildKubeClient(kubeConfig string) (kube.Client, error) {
	// Used by validation
	kubeRestConfig, err := kube.DefaultRestConfig(kubeConfig, "", func(config *rest.Config) {
		config.QPS = 80
		config.Burst = 160
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating kube config: %v", err)
	}

	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig), "")
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client: %v", err)
	}

	return client, nil
}

func (s *Server) Start() {
	log.Debug("CNI ambient server starting")
	s.kubeClient.RunAndWait(s.ctx.Done())
	go func() {
		s.queue.Run(s.ctx.Done())
	}()
}

func (s *Server) Stop() {
	log.Info("CNI ambient server terminating, cleaning up node net rules")
	s.cleanupNode()
}

func (s *Server) UpdateConfig() {
	log.Debug("Generating new ambient config file")

	cfg := &AmbientConfigFile{
		ZTunnelReady: s.isZTunnelRunning(),
		RedirectMode: s.redirectMode.String(),
	}

	if err := cfg.write(); err != nil {
		log.Errorf("Failed to write config file: %v", err)
	}
	log.Debug("Done")
}

var ztunnelLabels = labels.SelectorFromValidatedSet(labels.Set{"app": "ztunnel"})

func (s *Server) ReconcileZtunnel() error {
	pods := s.pods.List(metav1.NamespaceAll, ztunnelLabels)
	var activePod *corev1.Pod
	for _, p := range pods {
		ready := kube.CheckPodReady(p) == nil
		if !ready {

			log.Debugf("ztunnel pod not ready")
			continue
		}
		if activePod == nil {
			// Only pod ready, mark this as active
			activePod = p
			log.Debugf("ztunnel pod set as active")
		} else if p.CreationTimestamp.After(activePod.CreationTimestamp.Time) {
			// If we have multiple pods that are ready, use the newest one.
			// This ensures on a rolling update we start sending traffic to the new pod and drain the old one.
			activePod = p
			log.Debugf("newest ztunnel pod set as active")
		}
	}

	needsUpdate := false
	s.mu.Lock()
	if getUID(s.ztunnelPod) != getUID(activePod) {
		// Active pod change
		s.ztunnelPod = activePod
		needsUpdate = true
	}
	s.mu.Unlock()

	if !needsUpdate {
		log.Debugf("active ztunnel unchanged")
		return nil
	}
	s.UpdateConfig()
	if activePod == nil {
		log.Infof("active ztunnel updated, no ztunnel running on the node")
		s.cleanupNode()
		return nil
	}
	log.Infof("active ztunnel updated to %v", activePod.Name)

	captureDNS := getEnvFromPod(activePod, "ISTIO_META_DNS_CAPTURE") == "true"

	switch s.redirectMode {
	case IptablesMode:
		// TODO: we should not cleanup and recreate; this has downtime. We should mutate the existing rules in place
		s.cleanupNode()
		// TODO: this will fail for any networking setup that doesn't create veths for host<->pod networking.
		// Do we care about that?
		veth, err := getVethWithDestinationOf(activePod.Status.PodIP)
		if err != nil {
			return fmt.Errorf("failed to get veth device: %v", err)
		}
		// Create node-level networking rules for redirection
		err = s.CreateRulesOnNode(veth.Attrs().Name, activePod.Status.PodIP, captureDNS)
		if err != nil {
			return fmt.Errorf("failed to configure node for ztunnel: %v", err)
		}
		// Collect info needed to jump into node proxy netns and configure it.
		peerNs, err := getNsNameFromNsID(veth.Attrs().NetNsID)
		if err != nil {
			return fmt.Errorf("failed to get ns name: %v", err)
		}
		hostIP, err := GetHostIPByRoute(s.pods)
		if err != nil || hostIP == "" {
			log.Warnf("failed to getting host IP: %v", err)
		}
		peerIndex, err := getPeerIndex(veth)
		if err != nil {
			return fmt.Errorf("failed to get veth peerIndex: %v", err)
		}
		// Create pod-level networking rules for redirection (from within pod netns)
		err = s.CreateRulesWithinNodeProxyNS(peerIndex, activePod.Status.PodIP, peerNs, hostIP)
		if err != nil {
			return fmt.Errorf("failed to configure node for ztunnel: %v", err)
		}
	case EbpfMode:
		h, err := GetHostIPByRoute(s.pods)
		if err != nil || h == "" {
			log.Warnf("failed to getting host IP: %v", err)
		} else if HostIP != h {
			log.Infof("HostIP changed: (%v) -> (%v)", HostIP, h)
			HostIP = h
			if err := s.ebpfServer.UpdateHostIP([]string{HostIP}); err != nil {
				log.Errorf("failed to update host IP: %v", err)
			}
		}

		// TODO: this will fail for any networking setup that doesn't create veths for host<->pod networking.
		// Do we care about that?
		if err := s.updateNodeProxyEBPF(activePod, captureDNS); err != nil {
			return fmt.Errorf("failed to configure ztunnel: %v", err)
		}
	}

	// Reconcile namespaces, as it is possible for the original reconciliation to have failed, and a
	// small pod to have started up before ztunnel is running... so we need to go back and make sure we
	// catch the existing pods
	s.ReconcileNamespaces()
	return nil
}

// getUID is a nil safe UID accessor
func getUID(o *corev1.Pod) types.UID {
	if o == nil {
		return ""
	}
	return o.GetUID()
}

func (c *AmbientConfigFile) write() error {
	configFile := constants.AmbientConfigFilepath

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	log.Infof("Writing ambient config: %s", data)

	return atomicWrite(configFile, data)
}

func atomicWrite(filename string, data []byte) error {
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpFile, filename)
}

func ReadAmbientConfig() (*AmbientConfigFile, error) {
	configFile := constants.AmbientConfigFilepath

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return &AmbientConfigFile{
			ZTunnelReady: false,
		}, nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	cfg := &AmbientConfigFile{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
