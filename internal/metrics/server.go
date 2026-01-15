package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server serves Prometheus metrics over HTTP.
type Server struct {
	addr   string
	server *http.Server
}

// NewServer creates a metrics server listening on the given address.
// Default address is ":9090" if addr is empty.
func NewServer(addr string) *Server {
	if addr == "" {
		addr = ":9090"
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})

	return &Server{
		addr: addr,
		server: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// Start begins serving metrics. It blocks until the server stops.
// Returns nil if stopped via Shutdown, otherwise returns the error.
func (s *Server) Start() error {
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the metrics server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Addr returns the address the server is configured to listen on.
func (s *Server) Addr() string {
	return s.addr
}
