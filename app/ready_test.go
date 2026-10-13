package app

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// pollServer implements ServiceServer but not ReadyNotifier.
type pollServer struct{ accepting atomic.Bool }

func (*pollServer) ServerName() string             { return "poll" }
func (*pollServer) Serve() error                   { return nil }
func (*pollServer) Shutdown(context.Context) error { return nil }
func (s *pollServer) IsAcceptingClients() bool     { return s.accepting.Load() }
func (s *pollServer) IsHealthy() bool              { return s.accepting.Load() }

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

func TestWaitServersReady(t *testing.T) {
	notifier := NewHTTPServer("http", &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second})
	poller := &pollServer{}

	ready := make(chan struct{})
	go waitServersReady([]ServiceServer{notifier, poller}, ready)

	done := make(chan error, 1)
	go func() { done <- notifier.Serve() }()
	waitClosed(t, notifier.Ready(), true)
	waitClosed(t, ready, false) // poller not ready yet

	poller.accepting.Store(true)
	waitClosed(t, ready, true)

	if err := notifier.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
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
