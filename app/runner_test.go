package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dirty-go/foundation/boot"
	"github.com/dirty-go/foundation/errors"
)

func TestShutdownOnly(t *testing.T) {
	shutdownA := &boot.ShutdownError{Server: "a", Err: context.DeadlineExceeded}
	shutdownB := &boot.ShutdownError{Server: "b", Err: errors.New("panic: shutdown boom")}
	abandoned := fmt.Errorf("%w (shutdown: a)", boot.ErrAbandoned)
	serve := errors.Wrap("b serve", errors.New("listen: address in use"))

	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"single shutdown", shutdownA, true},
		{"joined shutdowns", errors.Join(shutdownA, shutdownB), true},
		{"shutdown and abandoned", errors.Join(shutdownA, abandoned), true},
		{"abandoned", errors.Join(abandoned), true},
		{"serve", errors.Join(serve), false},
		{"serve and shutdown", errors.Join(serve, shutdownA), false},
		{"serve and abandoned", errors.Join(serve, abandoned), false},
		{"started", boot.ErrStarted, false},
		{"serve wrapping shutdown", errors.Join(errors.Wrap("b serve", shutdownA)), false},
		{"wrapped join", errors.Wrap("x", errors.Join(serve, shutdownA)), false},
		{"serve wrapping abandoned", errors.Join(errors.Wrap("b serve", abandoned)), false},
		{"entity serve", errors.Join(errors.Wrap("b serve", errors.Ent("x", shutdownA))), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan bool, 1)
			go func() { done <- shutdownOnly(tt.err) }()
			select {
			case got := <-done:
				if got != tt.want {
					t.Errorf("shutdownOnly(%v) = %v, want %v", tt.err, got, tt.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("shutdownOnly(%v) did not terminate", tt.err)
			}
		})
	}
}

func TestRunServersWaitsForContext(t *testing.T) {
	srv := &pollServer{} // Serve returns nil at once
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); runServers(ctx, []ServiceServer{srv}) }()
	waitClosed(t, done, false)
	cancel()
	waitClosed(t, done, true)
}

func TestRunServersEmptyReturns(t *testing.T) {
	done := make(chan struct{})
	go func() { defer close(done); RunServers(nil) }()
	waitClosed(t, done, true)
}

// failingShutdownServer serves until Shutdown, which then fails.
type failingShutdownServer struct{ stop chan struct{} }

func (*failingShutdownServer) ServerName() string { return "failing-shutdown" }
func (s *failingShutdownServer) Serve() error     { <-s.stop; return nil }
func (s *failingShutdownServer) Shutdown(context.Context) error {
	close(s.stop)
	return errors.New("drain timed out")
}
func (*failingShutdownServer) IsAcceptingClients() bool { return true }
func (*failingShutdownServer) IsHealthy() bool          { return true }

// A Shutdown error must be logged, not fatal: log.Fatal would kill the test.
func TestRunServersShutdownErrorNotFatal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runServers(ctx, []ServiceServer{&failingShutdownServer{stop: make(chan struct{})}})
	}()
	waitClosed(t, done, false)
	cancel()
	waitClosed(t, done, true)
}
