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
	res.Write(data)
	return
}

func (s *HTTPServer) Start(stopChan <-chan struct{}) {
	go s.srv.ListenAndServe()
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
