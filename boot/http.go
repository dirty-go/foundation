package boot

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dirty-go/foundation/errors"
)

// DefaultReadHeaderTimeout is the ReadHeaderTimeout NewHTTP sets on a server
// that has neither ReadHeaderTimeout nor ReadTimeout.
const DefaultReadHeaderTimeout = 10 * time.Second

// DefaultIdleTimeout is the IdleTimeout NewHTTP sets on a server that has
// neither IdleTimeout nor ReadTimeout (net/http uses ReadTimeout for idle
// keep-alive connections when IdleTimeout is 0).
const DefaultIdleTimeout = 120 * time.Second

// HTTP adapts an *http.Server to Server so any http.Handler (gin.Engine, chi,
// go-restful, http.ServeMux, ...) can be run by Run or a Group.
type HTTP struct {
	name      string
	srv       *http.Server
	ln        net.Listener // pre-bound listener used instead of srv.Addr; set by tests only
	accepting atomic.Bool
	shutdown  atomic.Bool

	ready      chan struct{}
	readyClose sync.Once
}

var (
	_ Server            = (*HTTP)(nil)
	_ ReadyNotifier     = (*HTTP)(nil)
	_ AcceptingReporter = (*HTTP)(nil)
	_ HealthReporter    = (*HTTP)(nil)
)

// NewHTTP wraps srv as a Server. The caller owns srv's configuration (Addr,
// Handler, timeouts, TLSConfig) with two exceptions, both mutating the
// caller's server:
//
//   - if both srv.ReadHeaderTimeout and srv.ReadTimeout are 0, NewHTTP sets
//     srv.ReadHeaderTimeout to DefaultReadHeaderTimeout (slow header reads);
//   - if both srv.IdleTimeout and srv.ReadTimeout are 0, NewHTTP sets
//     srv.IdleTimeout to DefaultIdleTimeout (idle keep-alive connections
//     between requests). Behind a load balancer, set IdleTimeout above the
//     balancer's idle timeout, or reused connections may fail sporadically.
//
// These defaults bound only header reads and idle connections. ReadTimeout
// and WriteTimeout are never touched, so with the defaults a slow request
// body (ReadTimeout) and a slow response read (WriteTimeout) are still
// unbounded: a client can hold a connection, its file descriptor and the
// handler goroutine indefinitely. A default would break streaming uploads and
// long responses, so set them for your workload, or bound handlers
// individually with http.MaxBytesReader (body size) and
// http.ResponseController's SetReadDeadline and SetWriteDeadline.
//
// Call NewHTTP before srv starts serving: net/http reads these timeouts per
// connection without locking, so the defaults above would race with a server
// that is already running.
//
// If srv.TLSConfig is set, it must provide certificates (Certificates,
// GetCertificate or GetConfigForClient); otherwise Serve fails. Without
// TLSConfig, the server speaks plaintext HTTP.
//
// NewHTTP panics if srv is nil: that is a programming error.
func NewHTTP(name string, srv *http.Server) *HTTP {
	if srv == nil {
		panic("boot: NewHTTP called with a nil *http.Server")
	}
	if srv.ReadHeaderTimeout == 0 && srv.ReadTimeout == 0 {
		srv.ReadHeaderTimeout = DefaultReadHeaderTimeout
	}
	if srv.IdleTimeout == 0 && srv.ReadTimeout == 0 {
		srv.IdleTimeout = DefaultIdleTimeout
	}
	return &HTTP{name: name, srv: srv, ready: make(chan struct{})}
}

// Ready implements ReadyNotifier. The channel is closed once Serve has bound
// the listener and is about to serve. It is never closed if Serve fails
// before serving or runs after Shutdown; a Shutdown racing with Serve's start
// may still see it close just before Serve returns.
func (s *HTTP) Ready() <-chan struct{} { return s.ready }

// ServerName implements Server.
func (s *HTTP) ServerName() string { return s.name }

// Serve listens on srv.Addr and blocks until the server stops. An empty Addr
// means ":http" (port 80 on all interfaces). It returns nil when stopped via
// Shutdown, including when Shutdown was called before Serve. Errors include
// the address.
func (s *HTTP) Serve() error {
	addr := s.srv.Addr
	if addr == "" {
		addr = ":http"
	}
	tlsOn := s.srv.TLSConfig != nil
	if c := s.srv.TLSConfig; tlsOn && len(c.Certificates) == 0 && c.GetCertificate == nil && c.GetConfigForClient == nil {
		return errors.New("boot: serve " + addr + ": TLSConfig has no Certificates, GetCertificate or GetConfigForClient")
	}

	ln := s.ln
	if ln == nil {
		var lc net.ListenConfig
		var err error
		if ln, err = lc.Listen(context.Background(), "tcp", addr); err != nil {
			return errors.Wrap("listen "+addr, err)
		}
	}
	// http.Server closes ln once serving, but not on every early return.
	defer func() { _ = ln.Close() }()

	if s.shutdown.Load() {
		return nil // Shutdown came first; never signal ready
	}
	s.accepting.Store(true)
	defer s.accepting.Store(false)
	s.readyClose.Do(func() { close(s.ready) })

	var err error
	if tlsOn {
		err = s.srv.ServeTLS(ln, "", "")
	} else {
		err = s.srv.Serve(ln)
	}
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.Wrap("serve "+addr, err)
}

// Shutdown gracefully stops the server; see http.Server.Shutdown. If that
// fails (e.g. ctx expired with connections still active), it force-closes
// the server with http.Server.Close and returns both errors joined. Hijacked
// connections (e.g. WebSockets) are not tracked by http.Server; close them
// via srv.RegisterOnShutdown. It may be called before Serve.
func (s *HTTP) Shutdown(ctx context.Context) error {
	s.shutdown.Store(true)
	s.accepting.Store(false)
	if err := s.srv.Shutdown(ctx); err != nil {
		return errors.Join(err, s.srv.Close())
	}
	return nil
}

// IsAcceptingClients implements AcceptingReporter: it reports whether the
// server is listening.
func (s *HTTP) IsAcceptingClients() bool { return s.accepting.Load() }

// IsHealthy implements HealthReporter: it reports whether the server is
// listening.
func (s *HTTP) IsHealthy() bool { return s.accepting.Load() }
