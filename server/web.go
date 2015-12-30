package server

import (
	"fmt"
	"net/http"

	"github.com/Sirupsen/logrus"

	"github.com/rancher/rancher-net/backend"
)

type Server struct {
	Backend backend.Backend
}

func (s *Server) ListenAndServe(listen string) error {
	http.HandleFunc("/ping", s.ping)
	http.HandleFunc("/v1/reload", s.reload)
	logrus.Infof("Listening on %s", listen)
	return http.ListenAndServe(listen, nil)
}

func (s *Server) ping(rw http.ResponseWriter, req *http.Request) {
	rw.Write([]byte("OK"))
}

func (s *Server) reload(rw http.ResponseWriter, req *http.Request) {
	msg := "Reloaded Configuration\n"
	if err := s.Backend.Reload(); err != nil {
		rw.WriteHeader(500)
		msg = fmt.Sprintf("Failed to reload configuration: %v\n", err)
	}

	rw.Write([]byte(msg))
}
