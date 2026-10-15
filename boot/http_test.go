package boot

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// listen returns a loopback listener that is closed when the test ends.
func listen(t *testing.T) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// newTestHTTP returns an HTTP serving on a pre-bound loopback listener.
func newTestHTTP(t *testing.T) *HTTP {
	t.Helper()
	s := NewHTTP("test", &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second})
	s.ln = listen(t)
	s.srv.Addr = s.ln.Addr().String()
	return s
}

func waitClosed(t *testing.T, ch <-chan struct{}, want bool) {
	t.Helper()
	select {
	case <-ch:
		if !want {
			t.Fatal("channel closed, want open")
		}
	case <-time.After(200 * time.Millisecond):
		if want {
			t.Fatal("channel not closed")
		}
	}
}

func serveAsync(s *HTTP) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- s.Serve() }()
	return ch
}

func TestHTTPServeShutdown(t *testing.T) {
	s := newTestHTTP(t)
	done := serveAsync(s)
	eventually(t, s.IsAcceptingClients, "server did not start accepting clients")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitErr(t, done); err != nil {
		t.Fatalf("Serve returned %v, want nil", err)
	}
	if s.IsAcceptingClients() || s.IsHealthy() {
		t.Error("server still reports accepting/healthy after Shutdown")
	}
}

func TestHTTPListenError(t *testing.T) {
	s := NewHTTP("test", &http.Server{Addr: "invalid-addr", ReadHeaderTimeout: time.Second})
	err := s.Serve()
	if err == nil || !strings.Contains(err.Error(), "listen invalid-addr") {
		t.Fatalf("Serve = %v, want listen error with address", err)
	}
	waitClosed(t, s.Ready(), false)
}

func TestHTTPReady(t *testing.T) {
	s := newTestHTTP(t)
	waitClosed(t, s.Ready(), false)

	done := serveAsync(s)
	waitClosed(t, s.Ready(), true)
	if !s.IsAcceptingClients() {
		t.Error("Ready closed but IsAcceptingClients = false")
	}
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitErr(t, done); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

type trackingListener struct {
	net.Listener
	closed atomic.Bool
}

func (l *trackingListener) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
}

func TestHTTPShutdownBeforeServe(t *testing.T) {
	s := newTestHTTP(t)
	ln := &trackingListener{Listener: s.ln}
	s.ln = ln

	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown before Serve: %v", err)
	}
	if err := waitErr(t, serveAsync(s)); err != nil {
		t.Fatalf("Serve after Shutdown = %v, want nil", err)
	}
	waitClosed(t, s.Ready(), false)
	if s.IsAcceptingClients() {
		t.Error("accepting after Shutdown")
	}
	if !ln.closed.Load() {
		t.Error("listener not closed")
	}
}

func TestHTTPShutdownForceCloses(t *testing.T) {
	s := newTestHTTP(t)
	entered := make(chan struct{})
	s.srv.Handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done() // released when Close drops the connection
	})
	done := serveAsync(s)
	waitClosed(t, s.Ready(), true)

	reqErr := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+s.srv.Addr, nil)
		if err != nil {
			reqErr <- err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		reqErr <- err
	}()
	waitClosed(t, entered, true)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown = %v, want context.Canceled", err)
	}
	if err := waitErr(t, reqErr); err == nil {
		t.Error("in-flight request succeeded, want connection closed")
	}
	if err := waitErr(t, done); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestHTTPTLS(t *testing.T) {
	// Borrow httptest's self-signed certificate and a client trusting it.
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()

	s := newTestHTTP(t)
	s.srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	s.srv.TLSConfig = ts.TLS.Clone()
	done := serveAsync(s)
	waitClosed(t, s.Ready(), true)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+s.srv.Addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("TLS request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.TLS == nil || string(body) != "ok" {
		t.Errorf("got TLS=%v body=%q, want TLS and %q", resp.TLS != nil, body, "ok")
	}

	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitErr(t, done); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestHTTPTLSMisconfig(t *testing.T) {
	// Hold the port: if Serve listened before validating, it would fail with
	// a listen error instead of the TLS one.
	held := listen(t)
	s := NewHTTP("tls", &http.Server{Addr: held.Addr().String(), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ReadHeaderTimeout: time.Second})

	err := s.Serve()
	if err == nil || !strings.Contains(err.Error(), "TLSConfig has no Certificates") {
		t.Fatalf("Serve = %v, want TLS config error", err)
	}
	if strings.Contains(err.Error(), "listen") {
		t.Errorf("Serve = %v, want validation before listen", err)
	}
	waitClosed(t, s.Ready(), false)
	if s.IsAcceptingClients() {
		t.Error("accepting after failed Serve")
	}
}

func TestNewHTTPReadHeaderTimeout(t *testing.T) {
	tests := []struct {
		name                       string
		readHeader, read, wr, idle time.Duration
		want                       time.Duration
	}{
		{name: "none set", want: DefaultReadHeaderTimeout},
		{name: "ReadHeaderTimeout set", readHeader: time.Second, want: time.Second},
		{name: "ReadTimeout set", read: time.Second, want: 0},
		{name: "other timeouts untouched", wr: time.Second, idle: time.Minute, want: DefaultReadHeaderTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &http.Server{ReadHeaderTimeout: tt.readHeader, ReadTimeout: tt.read, WriteTimeout: tt.wr, IdleTimeout: tt.idle}
			NewHTTP("x", srv)
			if srv.ReadHeaderTimeout != tt.want {
				t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, tt.want)
			}
			if srv.ReadTimeout != tt.read || srv.WriteTimeout != tt.wr || srv.IdleTimeout != tt.idle {
				t.Error("NewHTTP changed Read/Write/IdleTimeout")
			}
		})
	}
}

func TestNewHTTPNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewHTTP(nil) did not panic")
		}
	}()
	NewHTTP("x", nil)
}
