// Package boot runs servers and manages their lifecycle: start, readiness,
// and graceful shutdown on context cancellation or a serve failure.
//
// Use Run (or RunSignals in main) for the common case, or a Group when you
// need options, readiness (Group.Ready), or health (Group.AllAccepting).
// SignalContext turns SIGINT/SIGTERM into context cancellation for either.
package boot

import "context"

// Server is a long-running server managed by Run and Group.
type Server interface {
	// ServerName returns the name of the server, used in logs and errors. It
	// doesn't have to be unique.
	ServerName() string

	// Serve starts the server. It blocks until the server stops and returns
	// nil when stopped gracefully (e.g. by Shutdown).
	Serve() error

	// Shutdown gracefully stops the server, giving up when ctx is done. It
	// must cause a running Serve to return. It may be called on a server
	// whose Serve hasn't started yet or has already returned, and must not
	// block in either case; a Serve started after Shutdown should return
	// promptly.
	Shutdown(ctx context.Context) error

	// IsAcceptingClients reports whether the server is ready to serve clients.
	IsAcceptingClients() bool

	// IsHealthy reports whether the server is considerably healthy.
	IsHealthy() bool
}

// ReadyNotifier is optionally implemented by a Server that can signal when it
// starts accepting clients. Group.Ready polls IsAcceptingClients for servers
// that don't implement it.
type ReadyNotifier interface {
	// Ready returns a channel that is closed once the server is accepting
	// clients. It is never closed if the server fails to start.
	Ready() <-chan struct{}
}
