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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/google/uuid"

	"istio.io/pkg/log"
)

type HTTPServer struct {
	serveDirectory string
	mux            *http.ServeMux
	srv            *http.Server
}

func (s *HTTPServer) handleRequest(res http.ResponseWriter, req *http.Request) {
	uuid, err := uuid.Parse(filepath.Base(req.URL.Path))
	if err != nil {
		log.Errorf("Could not parse request path '%s' as UUID: %s", req.URL.Path, err)
		res.WriteHeader(404)
		return
	}
	filename := path.Join(s.serveDirectory, uuid.String())
	if _, err := os.Stat(filename); err != nil {

		log.Errorf("Failed to open file %s: %s", filename, err)
		res.WriteHeader(404)
		return
	}
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Errorf("Failed to read file %s: %s", filename, err)
		res.WriteHeader(404)
		return
	}
	res.WriteHeader(200)
	if _, err := res.Write(data); err != nil {
		log.Errorf("error writing response: %s", err)
	}
}

func (s *HTTPServer) Start(stopChan <-chan struct{}) {
	go func() {
		if err := s.srv.ListenAndServe(); err != nil {
			log.Errorf("error listening and serving: %s", err)
		}
	}()
}

func NewHTTPServer(port uint, serveDirectory string) *HTTPServer {
	s := &HTTPServer{
		serveDirectory: serveDirectory,
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/", s.handleRequest)

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: s.mux,
	}
	return s
}
