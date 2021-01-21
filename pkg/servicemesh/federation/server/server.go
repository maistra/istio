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

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	hashstructure "github.com/mitchellh/hashstructure/v2"
	"k8s.io/apimachinery/pkg/util/errors"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
	"istio.io/istio/pkg/servicemesh/federation/export"
	federationmodel "istio.io/istio/pkg/servicemesh/federation/model"
	"istio.io/pkg/log"
)

const (
	meshURLParameter = "mesh"
)

var logger = log.RegisterScope("federation-server", "federation-server", 0)

type Options struct {
	BindAddress string
	Env         *model.Environment
	Network     string
	ConfigStore model.ConfigStoreCache
}

type FederationManager interface {
	AddMeshFederation(mesh *v1alpha1.MeshFederation, exports *v1alpha1.ServiceExports) error
	DeleteMeshFederation(name string)
	UpdateExportsForMesh(exports *v1alpha1.ServiceExports) error
	DeleteExportsForMesh(name string)
}

type Server struct {
	sync.RWMutex

	Env        *model.Environment
	listener   net.Listener
	httpServer *http.Server

	configStore model.ConfigStoreCache

	defaultExportConfig *export.ServiceExporter
	meshes              *sync.Map

	// XXX: we need to decide if we really want to allow this or not.
	// Gateway configuration is managed explicitly through the MeshFederation
	// resource and using other gateway addresses over discovery would force
	// us to know what the workload identifiers were so we could manage the
	// routing config for each mesh.  This may or may not be possible.
	Network string

	currentGatewayEndpoints []*federationmodel.ServiceEndpoint
}

var _ FederationManager = (*Server)(nil)

func NewServer(opt Options) (*Server, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", opt.BindAddress)
	if err != nil {
		return nil, err
	}
	fed := &Server{
		Env: opt.Env,
		httpServer: &http.Server{
			ReadTimeout:    10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
		configStore: opt.ConfigStore,
		meshes:      &sync.Map{},
		Network:     opt.Network,
		listener:    listener,
	}
	mux := mux.NewRouter()
	mux.HandleFunc("/services/{mesh}", fed.handleServiceList)
	mux.HandleFunc("/watch/{mesh}", fed.handleWatch)
	fed.httpServer.Handler = mux
	return fed, nil
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) AddMeshFederation(mesh *v1alpha1.MeshFederation, exports *v1alpha1.ServiceExports) error {
	exportConfig := export.NewServiceExporter(exports, s.defaultExportConfig)

	untypedMeshServer, ok := s.meshes.Load(mesh.Name)
	if untypedMeshServer != nil && ok {
		return fmt.Errorf("exporter already exists for federation %s", mesh.Name)
	}
	meshServer := &meshServer{
		GatewayEndpointsProvider: s,
		Env:                      s.Env,
		mesh:                     mesh,
		exportConfig:             exportConfig,
		configStore:              s.configStore,
		currentServices:          make(map[federationmodel.ServiceKey]*federationmodel.ServiceMessage),
	}
	if _, loaded := s.meshes.LoadOrStore(mesh.Name, meshServer); !loaded {
		meshServer.resync()
	}
	return nil
}

func (s *Server) DeleteMeshFederation(name string) {
	ms, ok := s.meshes.Load(name)
	s.meshes.Delete(name)
	if ms == nil || !ok {
		return
	}
	ms.(*meshServer).stop()
}

func (s *Server) UpdateExportsForMesh(exports *v1alpha1.ServiceExports) error {
	untypedMeshServer, ok := s.meshes.Load(exports.Name)
	if untypedMeshServer == nil || !ok {
		return fmt.Errorf("cannot update exporter for non-existent federation: %s", exports.Name)
	}
	untypedMeshServer.(*meshServer).updateExportConfig(export.NewServiceExporter(exports, s.defaultExportConfig))
	return nil
}

func (s *Server) DeleteExportsForMesh(name string) {
	untypedMeshServer, ok := s.meshes.Load(name)
	if untypedMeshServer == nil || !ok {
		return
	}
	// set an empty set of export rules
	untypedMeshServer.(*meshServer).updateExportConfig(&export.ServiceExporter{})
}

func (s *Server) getMeshServerForRequest(request *http.Request) (*meshServer, error) {
	vars := mux.Vars(request)
	if vars == nil {
		return nil, fmt.Errorf("no mesh specified")
	}
	meshName := vars[meshURLParameter]
	untypedMesh, ok := s.meshes.Load(meshName)
	if !ok || untypedMesh == nil {
		return nil, fmt.Errorf("unknown mesh specified: %s", meshName)
	}

	return untypedMesh.(*meshServer), nil
}

func (s *Server) handleServiceList(response http.ResponseWriter, request *http.Request) {
	mesh, err := s.getMeshServerForRequest(request)
	if err != nil {
		logger.Errorf("error handling /services/ request: %s", err)
		response.WriteHeader(400)
		return
	}
	ret := mesh.getServiceListMessage()

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
	mesh, err := s.getMeshServerForRequest(request)
	if err != nil {
		logger.Errorf("error handling /watch request: %s", err)
		response.WriteHeader(400)
		return
	}
	mesh.handleWatch(response)
}

func (s *Server) Run(stopCh <-chan struct{}) {
	logger.Infof("starting federation service discovery at %s", s.Addr())
	go func() {
		_ = s.httpServer.Serve(s.listener)
	}()
	<-stopCh
	_ = s.httpServer.Shutdown(context.TODO())
}

func (s *Server) GetGatewayEndpoints() []*federationmodel.ServiceEndpoint {
	s.Lock()
	defer s.Unlock()
	return append([]*federationmodel.ServiceEndpoint(nil), s.currentGatewayEndpoints...)
}

func (s *Server) resyncNetworkGateways() (bool, error) {
	s.Lock()
	defer s.Unlock()

	gatewayEndpoints := []*federationmodel.ServiceEndpoint{}
	for _, gateway := range s.Env.NetworkGateways() {
		gatewayEndpoints = append(gatewayEndpoints, &federationmodel.ServiceEndpoint{
			Port:     int(gateway.Port),
			Hostname: gateway.Addr,
		})
	}

	newGatewayChecksum, err := hashstructure.Hash(gatewayEndpoints, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return false, err
	}

	oldGatewayChecksum, err := hashstructure.Hash(s.currentGatewayEndpoints, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return false, err
	}
	if oldGatewayChecksum != newGatewayChecksum {
		s.currentGatewayEndpoints = gatewayEndpoints
		return true, nil
	}
	return false, nil
}

func (s *Server) UpdateService(svc *model.Service, event model.Event) {
	// this might be a NetworkGateway
	if svc != nil {
		networkGatewaysChanged, _ := s.resyncNetworkGateways()
		if networkGatewaysChanged {
			s.meshes.Range(func(_, value interface{}) bool {
				value.(*meshServer).resync()
				return true
			})
			s.meshes.Range(func(_, value interface{}) bool {
				value.(*meshServer).pushWatchEvent(&federationmodel.WatchEvent{
					Action:  federationmodel.ActionUpdate,
					Service: nil,
				})
				return true
			})
		}
	}
	s.meshes.Range(func(_, value interface{}) bool {
		value.(*meshServer).serviceUpdated(svc, event)
		return true
	})
}

// resync ensures the export lists are current.  used for testing
func (s *Server) resync() {
	_, _ = s.resyncNetworkGateways()
	s.meshes.Range(func(_, value interface{}) bool {
		value.(*meshServer).resync()
		return true
	})
}

type GatewayEndpointsProvider interface {
	GetGatewayEndpoints() []*federationmodel.ServiceEndpoint
}

func serviceKeyForService(svc *model.Service) federationmodel.ServiceKey {
	return federationmodel.ServiceKey{
		Name:      svc.Attributes.Name,
		Namespace: svc.Attributes.Namespace,
		Hostname:  string(svc.Hostname),
	}
}

type meshServer struct {
	GatewayEndpointsProvider
	sync.RWMutex

	Env *model.Environment

	mesh         *v1alpha1.MeshFederation
	exportConfig *export.ServiceExporter

	configStore model.ConfigStoreCache

	currentServices map[federationmodel.ServiceKey]*federationmodel.ServiceMessage

	watchMut       sync.RWMutex
	currentWatches []chan *federationmodel.WatchEvent
}

func (s *meshServer) updateExportConfig(exportConfig *export.ServiceExporter) {
	s.Lock()
	s.exportConfig = exportConfig
	s.Unlock()
	s.resync()
}

func (s *meshServer) getServiceMessage(svc *model.Service, exportedName *v1alpha1.ServiceName) *federationmodel.ServiceMessage {
	if svc == nil || exportedName == nil {
		return nil
	}
	ret := &federationmodel.ServiceMessage{
		ServiceKey: federationmodel.ServiceKey{
			Name:      exportedName.Name,
			Namespace: exportedName.Namespace,
			Hostname:  fmt.Sprintf("%s.%s.svc.%s.local", exportedName.Name, exportedName.Namespace, s.mesh.Name),
		},
		ServicePorts: make([]*federationmodel.ServicePort, 0),
	}
	for _, port := range svc.Ports {
		ret.ServicePorts = append(ret.ServicePorts, &federationmodel.ServicePort{
			Name:     port.Name,
			Port:     port.Port,
			Protocol: string(port.Protocol),
		})
	}
	return ret
}

// s has to be Lock()ed
func (s *meshServer) getServiceListMessage() *federationmodel.ServiceListMessage {
	ret := &federationmodel.ServiceListMessage{
		NetworkGatewayEndpoints: s.GetGatewayEndpoints(),
	}
	ret.Services = []*federationmodel.ServiceMessage{}
	for _, svcMessage := range s.currentServices {
		ret.Services = append(ret.Services, svcMessage)
	}
	sort.Slice(ret.Services, func(i, j int) bool { return strings.Compare(ret.Services[i].Hostname, ret.Services[j].Hostname) < 0 })
	ret.Checksum = ret.GenerateChecksum()
	return ret
}

func (s *meshServer) handleServiceList(response http.ResponseWriter) {
	ret := func() *federationmodel.ServiceListMessage {
		s.RLock()
		defer s.RUnlock()
		return s.getServiceListMessage()
	}()

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

func (s *meshServer) handleWatch(response http.ResponseWriter) {
	watch := make(chan *federationmodel.WatchEvent)
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

func (s *meshServer) resync() {
	s.Lock()
	defer s.Unlock()
	services, err := s.Env.Services()
	if err != nil {
		logger.Errorf("failed to call env.Services(): %s", err)
		return
	}
	for _, svc := range services {
		if svc.Attributes.Name == "" || svc.Attributes.Namespace == "" {
			logger.Debugf("skipping service with no Namespace/Name: %s", svc.Hostname)
			continue
		} else if svc.External() {
			logger.Debugf("skipping external service: %s", svc.Hostname)
			continue
		}
		svcMessage := s.getServiceMessage(svc, s.exportConfig.ExportedNameForService(svc))
		svcKey := serviceKeyForService(svc)
		if existingSvc, found := s.currentServices[svcKey]; found {
			if existingSvc.GenerateChecksum() == svcMessage.GenerateChecksum() {
				continue
			}
			if existingSvc.Name != svcMessage.Name || existingSvc.Namespace != svcMessage.Namespace {
				s.deleteService(svcKey, existingSvc)
				s.addService(svcKey, svcMessage)
			} else {
				s.updateService(svcKey, svcMessage)
			}
		} else if svcMessage != nil {
			s.addService(svcKey, svcMessage)
		}
	}
}

func (s *meshServer) serviceUpdated(svc *model.Service, event model.Event) {
	if svc == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	var svcMessage *federationmodel.ServiceMessage
	switch event {
	case model.EventAdd:
		svcMessage = s.getServiceMessage(svc, s.exportConfig.ExportedNameForService(svc))
		if svcMessage != nil {
			s.addService(serviceKeyForService(svc), svcMessage)
		}
	case model.EventUpdate:
		svcMessage = s.getServiceMessage(svc, s.exportConfig.ExportedNameForService(svc))
		svcKey := serviceKeyForService(svc)
		if svcMessage != nil {
			if existingSvc, found := s.currentServices[svcKey]; found {
				if existingSvc.Name != svcMessage.Name || existingSvc.Namespace != svcMessage.Namespace {
					s.deleteService(svcKey, existingSvc)
					s.addService(svcKey, svcMessage)
				} else {
					s.updateService(svcKey, svcMessage)
				}
			} else {
				s.addService(svcKey, svcMessage)
			}
		} else if existingSvc, found := s.currentServices[svcKey]; found {
			s.deleteService(svcKey, existingSvc)
		}
	case model.EventDelete:
		svcKey := serviceKeyForService(svc)
		if existingSvc, found := s.currentServices[svcKey]; found {
			s.deleteService(svcKey, existingSvc)
		}
	}
}

// s has to be Lock()ed
func (s *meshServer) addService(svc federationmodel.ServiceKey, msg *federationmodel.ServiceMessage) {
	if err := s.createExportResources(svc, msg); err != nil {
		logger.Errorf("error creating resources for exported service %s => %s: %s", svc.Hostname, msg.Hostname, err)
		return
	}
	s.currentServices[svc] = msg
	e := &federationmodel.WatchEvent{
		Action:  federationmodel.ActionAdd,
		Service: msg,
	}
	s.pushWatchEvent(e)
}

// s has to be Lock()ed
func (s *meshServer) updateService(svc federationmodel.ServiceKey, msg *federationmodel.ServiceMessage) {
	// resources used to configure export are all based on names, so we don't need to update them
	s.currentServices[svc] = msg
	e := &federationmodel.WatchEvent{
		Action:  federationmodel.ActionUpdate,
		Service: msg,
	}
	s.pushWatchEvent(e)
}

// s has to be Lock()ed
func (s *meshServer) deleteService(svc federationmodel.ServiceKey, msg *federationmodel.ServiceMessage) {
	if err := s.deleteExportResources(svc, msg); err != nil {
		logger.Errorf("couldn't remove resources associated with exported service %s => %s: %s", svc.Hostname, msg.Hostname, err)
		// let the deletion go through, so the other mesh won't try to call us
	}
	delete(s.currentServices, svc)
	e := &federationmodel.WatchEvent{
		Action:  federationmodel.ActionDelete,
		Service: msg,
	}
	s.pushWatchEvent(e)
}

// s has to be Lock()ed
func (s *meshServer) pushWatchEvent(e *federationmodel.WatchEvent) {
	list := s.getServiceListMessage()
	e.Checksum = list.Checksum
	s.watchMut.RLock()
	defer s.watchMut.RUnlock()
	for _, w := range s.currentWatches {
		w <- e
	}
}

func (s *meshServer) stop() {
	s.Lock()
	defer s.Unlock()
	s.watchMut.Lock()
	defer s.watchMut.Unlock()

	// copy map as deleteService() removes entries
	currentServices := make(map[federationmodel.ServiceKey]*federationmodel.ServiceMessage)
	for source, svc := range s.currentServices {
		currentServices[source] = svc
	}
	// send a delete event for all the services
	for source, svc := range currentServices {
		s.deleteService(source, svc)
	}

	for _, watch := range s.currentWatches {
		close(watch)
	}
}

func (opt Options) validate() error {
	var allErrors []error
	if opt.Env == nil {
		allErrors = append(allErrors, fmt.Errorf("the Env field must not be nil"))
	}
	if opt.ConfigStore == nil {
		allErrors = append(allErrors, fmt.Errorf("the ConfigStore field must not be nil"))
	}
	return errors.NewAggregate(allErrors)
}
