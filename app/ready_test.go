package app

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dirty-go/foundation/boot"
	"github.com/dirty-go/foundation/errors"
)

// pollServer implements ServiceServer but not ReadyNotifier.
type pollServer struct{ accepting atomic.Bool }

func (*pollServer) ServerName() string             { return "poll" }
func (*pollServer) Serve() error                   { return nil }
func (*pollServer) Shutdown(context.Context) error { return nil }
func (s *pollServer) IsAcceptingClients() bool     { return s.accepting.Load() }
func (s *pollServer) IsHealthy() bool              { return s.accepting.Load() }

// waitClosed checks that ch closes (within 2s) or stays open (for 200ms).
func waitClosed(t *testing.T, ch <-chan struct{}, want bool) {
	t.Helper()
	timeout := 200 * time.Millisecond
	if want {
		timeout = 2 * time.Second
	}
	select {
	case <-ch:
		if !want {
			t.Fatal("channel closed, want open")
		}
	case <-time.After(timeout):
		if want {
			t.Fatal("channel not closed")
		}
	}
}

func TestHTTPServerReady(t *testing.T) {
	s := NewHTTPServer("test", &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second})
	waitClosed(t, s.Ready(), false)

	done := make(chan error, 1)
	go func() { done <- s.Serve() }()
	waitClosed(t, s.Ready(), true)
	if !s.IsAcceptingClients() {
		t.Error("Ready closed but IsAcceptingClients = false")
	}

	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestHTTPServerReadyNotClosedOnListenError(t *testing.T) {
	s := NewHTTPServer("test", &http.Server{Addr: "invalid-addr", ReadHeaderTimeout: time.Second})
	if err := s.Serve(); err == nil {
		t.Fatal("Serve with invalid addr returned nil error")
	}
	waitClosed(t, s.Ready(), false)
}

func TestBaseReadyWithoutServers(t *testing.T) {
	b := &Base{}
	ch := b.Ready()
	waitClosed(t, ch, false)
	b.Run()
	waitClosed(t, ch, true)
	if b.Ready() != ch {
		t.Error("Ready returned a different channel")
	}
}

func TestBaseAddServerNilPanics(t *testing.T) {
	for name, srv := range map[string]ServiceServer{"nil": nil, "typed nil": (*pollServer)(nil)} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				err, ok := recover().(error)
				if !ok || !errors.Is(err, boot.ErrNilServer) {
					t.Fatalf("recovered %v, want error wrapping boot.ErrNilServer", err)
				}
			}()
			(&Base{}).AddServer(srv)
		})
	}
}

func TestBaseAllAcceptingAndRun(t *testing.T) {
	b := &Base{}
	srv := &pollServer{}
	b.AddServer(srv)
	if b.IsAllServersAcceptingClients() {
		t.Error("IsAllServersAcceptingClients = true with server not accepting")
	}
	srv.accepting.Store(true)
	if !b.IsAllServersAcceptingClients() {
		t.Error("IsAllServersAcceptingClients = false with server accepting")
	}

	// pollServer.Serve returns nil at once; run still waits for ctx, as on main.
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.run(ctx) }()
	waitClosed(t, b.Ready(), true)
	waitClosed(t, done, false)
	cancel()
	waitClosed(t, done, true)
}

func TestBaseAddServerAfterRunPanics(t *testing.T) {
	b := &Base{}
	b.run(t.Context()) // no servers: returns at once
	defer func() {
		err, ok := recover().(error)
		if !ok || !errors.Is(err, boot.ErrStarted) {
			t.Fatalf("recovered %v, want error wrapping boot.ErrStarted", err)
		}
	}()
	b.AddServer(&pollServer{})
}
