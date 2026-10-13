package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerServeShutdown(t *testing.T) {
	s := NewHTTPServer("test", &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second})

	done := make(chan error, 1)
	go func() { done <- s.Serve() }()

	deadline := time.Now().Add(2 * time.Second)
	for !s.IsAcceptingClients() {
		if time.Now().After(deadline) {
			t.Fatal("server did not start accepting clients")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}

	if s.IsAcceptingClients() || s.IsHealthy() {
		t.Error("server still reports accepting/healthy after Shutdown")
	}
}

func TestHTTPServerListenError(t *testing.T) {
	s := NewHTTPServer("test", &http.Server{Addr: "invalid-addr", ReadHeaderTimeout: time.Second})
	if err := s.Serve(); err == nil {
		t.Fatal("Serve with invalid addr returned nil error")
	}
}
