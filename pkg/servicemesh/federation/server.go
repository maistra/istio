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

package federation

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	hashstructure "github.com/mitchellh/hashstructure/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
)

const (
	ExportLabel          = "maistra.io/exportAs"
	DiscoveryScheme      = "http"
	DefaultDiscoveryPort = 8188
)

var (
	logger = log.RegisterScope("federation-server", "federation-server", 0)
)

type Server struct {
	sync.RWMutex

	Env        *model.Environment
	listener   net.Listener
	httpServer *http.Server
	Network    string
	clusterID  string

	currentServices         map[string]*ServiceMessage
	currentGatewayEndpoints []*ServiceEndpoint

	watchMut       sync.RWMutex
	currentWatches []chan *WatchEvent
}

func NewServer(addr string, env *model.Environment, clusterID, network string) (*Server, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	fed := &Server{
		Env: env,
		httpServer: &http.Server{
			ReadTimeout:    10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
		clusterID:       clusterID,
		Network:         network,
		currentServices: make(map[string]*ServiceMessage),
		listener:        listener,
	}
	mux := mux.NewRouter()
	mux.HandleFunc("/services/", fed.handleServiceList)
	mux.HandleFunc("/watch", fed.handleWatch)
	fed.httpServer.Handler = mux
	return fed, nil
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) getServiceMessage(svc *model.Service) *ServiceMessage {
	if svc == nil || svc.Attributes.Labels[ExportLabel] == "" {
		return nil
	}
	ret := &ServiceMessage{
		Name:         svc.Attributes.Labels[ExportLabel],
		ServicePorts: make([]*ServicePort, 0),
	}
	for _, port := range svc.Ports {
		ret.ServicePorts = append(ret.ServicePorts, &ServicePort{
			Name:     port.Name,
			Port:     port.Port,
			Protocol: string(port.Protocol),
		})
	}
	return ret
}

// s has to be Lock()ed
func (s *Server) getServiceListMessage() *ServiceListMessage {
	ret := &ServiceListMessage{
		NetworkGatewayEndpoints: s.currentGatewayEndpoints,
	}
	ret.Services = []*ServiceMessage{}
	for _, svcMessage := range s.currentServices {
		ret.Services = append(ret.Services, svcMessage)
	}
	ret.Checksum = ret.GenerateChecksum()
	return ret
}

func (s *Server) handleServiceList(response http.ResponseWriter, request *http.Request) {
	s.RLock()
	defer s.RUnlock()
	ret := s.getServiceListMessage()

	respBytes, err := json.Marshal(ret)
	if err != nil {
		logger.Errorf("failed to marshal to json: %s", err)
		response.WriteHeader(500)
		return
	}
	_, err = response.Write(respBytes)
	if err != nil {
		logger.Errorf("failed to send response: %s", err)
		response.WriteHeader(500)
		return
	}
}

func (s *Server) handleWatch(response http.ResponseWriter, request *http.Request) {
	watch := make(chan *WatchEvent)
	s.watchMut.Lock()
	s.currentWatches = append(s.currentWatches, watch)
	s.watchMut.Unlock()
	defer func() {
		s.watchMut.Lock()
		for i, w := range s.currentWatches {
			if w == watch {
				s.currentWatches[i] = s.currentWatches[len(s.currentWatches)-1]
				s.currentWatches = s.currentWatches[:len(s.currentWatches)-1]
				break
			}
		}
		s.watchMut.Unlock()
	}()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Transfer-Encoding", "chunked")
	response.WriteHeader(200)
	flusher, ok := response.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	flusher.Flush()
	for {
		event := <-watch
		respBytes, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, err = response.Write(respBytes)
		if err != nil {
			logger.Errorf("failed to write http response: %s", err)
			return
		}
		_, err = response.Write([]byte("\r\n"))
		if err != nil {
			logger.Errorf("failed to write http response: %s", err)
			return
		}
		flusher.Flush()
	}
}

func (s *Server) resync() {
	_, _ = s.resyncNetworkGateways()

	s.Lock()
	defer s.Unlock()

	services, err := s.Env.Services()
	if err != nil {
		logger.Errorf("failed to call env.Services(): %s", err)
		return
	}
	serviceMap := make(map[string]*ServiceMessage)
	for _, svc := range services {
		svcMessage := s.getServiceMessage(svc)
		if svcMessage != nil {
			serviceMap[string(svc.Hostname)] = svcMessage
		}
	}

	s.currentServices = serviceMap
}

func (s *Server) resyncNetworkGateways() (bool, error) {
	s.Lock()
	defer s.Unlock()

	gatewayEndpoints := []*ServiceEndpoint{}
	for _, gateway := range s.Env.NetworkGateways()[s.Network] {
		gatewayEndpoints = append(gatewayEndpoints, &ServiceEndpoint{
			Port:     int(gateway.Port),
			Hostname: gateway.Addr,
		})
	}

	oldGatewayChecksum, err := hashstructure.Hash(s.currentGatewayEndpoints, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return false, err
	}
	newGatewayChecksum, err := hashstructure.Hash(gatewayEndpoints, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return false, err
	}
	if oldGatewayChecksum != newGatewayChecksum {
		s.currentGatewayEndpoints = gatewayEndpoints
		return true, nil
	}
	return false, nil
}

func (s *Server) Run(stopCh <-chan struct{}) {
	log.Infof("starting federation service discovery at %s", s.Addr())
	go func() {
		_ = s.httpServer.Serve(s.listener)
	}()
	<-stopCh
	_ = s.httpServer.Shutdown(context.TODO())
}

func (s *Server) UpdateService(svc *model.Service, event model.Event) {
	// this might be a NetworkGateway
	if svc != nil && svc.Attributes.ClusterExternalAddresses != nil {
		networkGatewaysChanged, _ := s.resyncNetworkGateways()
		if networkGatewaysChanged {
			s.pushWatchEvent(&WatchEvent{
				Action:  ActionUpdate,
				Service: nil,
			})
		}
	}

	var svcMessage *ServiceMessage
	switch event {
	case model.EventAdd:
		svcMessage = s.getServiceMessage(svc)
		if svcMessage != nil {
			s.Lock()
			defer s.Unlock()
			s.addService(string(svc.Hostname), svcMessage)
		}
	case model.EventUpdate:
		svcMessage = s.getServiceMessage(svc)
		s.Lock()
		defer s.Unlock()
		if svcMessage != nil {
			if existingSvc, found := s.currentServices[string(svc.Hostname)]; found {
				if existingSvc.Name != svcMessage.Name {
					s.deleteService(string(svc.Hostname), existingSvc)
					s.addService(string(svc.Hostname), svcMessage)
				} else {
					s.updateService(string(svc.Hostname), svcMessage)
				}
			} else {
				s.addService(string(svc.Hostname), svcMessage)
			}
		} else if existingSvc, found := s.currentServices[string(svc.Hostname)]; found {
			s.deleteService(string(svc.Hostname), existingSvc)
		}
	case model.EventDelete:
		s.Lock()
		defer s.Unlock()
		if existingSvc, found := s.currentServices[string(svc.Hostname)]; found {
			s.deleteService(string(svc.Hostname), existingSvc)
		}
	}
}

// s has to be Lock()ed
func (s *Server) addService(hostname string, svc *ServiceMessage) {
	s.currentServices[hostname] = svc
	e := &WatchEvent{
		Action:  ActionAdd,
		Service: svc,
	}
	s.pushWatchEvent(e)
}

// s has to be Lock()ed
func (s *Server) updateService(hostname string, svc *ServiceMessage) {
	s.currentServices[hostname] = svc
	e := &WatchEvent{
		Action:  ActionUpdate,
		Service: svc,
	}
	s.pushWatchEvent(e)
}

// s has to be Lock()ed
func (s *Server) deleteService(hostname string, svc *ServiceMessage) {
	delete(s.currentServices, hostname)
	e := &WatchEvent{
		Action:  ActionDelete,
		Service: svc,
	}
	s.pushWatchEvent(e)
}

// s has to be Lock()ed
func (s *Server) pushWatchEvent(e *WatchEvent) {
	list := s.getServiceListMessage()
	e.Checksum = list.Checksum
	s.watchMut.RLock()
	defer s.watchMut.RUnlock()
	for _, w := range s.currentWatches {
		w <- e
	}
}
