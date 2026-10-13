package app

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/dirty-go/foundation/errors"
)

// HTTPServer adapts an *http.Server to ServiceServer so any http.Handler
// (gin.Engine, chi, go-restful, http.ServeMux, ...) can be run by RunServers.
type HTTPServer struct {
	name      string
	srv       *http.Server
	accepting atomic.Bool
}

var _ ServiceServer = &HTTPServer{}

// NewHTTPServer wraps srv as a ServiceServer. The caller owns srv's
// configuration (Addr, Handler, timeouts, TLSConfig). If srv.TLSConfig is
// set, it must provide the certificates (Certificates or GetCertificate).
func NewHTTPServer(name string, srv *http.Server) *HTTPServer {
	return &HTTPServer{name: name, srv: srv}
}

// ServerName implements ServiceServer.
func (s *HTTPServer) ServerName() string { return s.name }

// Serve listens on srv.Addr and blocks until the server stops. It returns nil
// when stopped via Shutdown.
func (s *HTTPServer) Serve() error {
	addr := s.srv.Addr
	if addr == "" {
		addr = ":http"
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return errors.Wrap("listen "+addr, err)
	}

	s.accepting.Store(true)
	defer s.accepting.Store(false)

	if s.srv.TLSConfig != nil {
		err = s.srv.ServeTLS(ln, "", "")
	} else {
		err = s.srv.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops the server; see http.Server.Shutdown.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.accepting.Store(false)
	return s.srv.Shutdown(ctx)
}

// IsAcceptingClients reports whether the server is listening.
func (s *HTTPServer) IsAcceptingClients() bool { return s.accepting.Load() }

// IsHealthy reports whether the server is listening.
func (s *HTTPServer) IsHealthy() bool { return s.accepting.Load() }
