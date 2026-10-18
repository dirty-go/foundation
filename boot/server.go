// Package boot runs servers and manages their lifecycle: start, readiness,
// and graceful shutdown on context cancellation or a serve failure.
//
// Use Run (or RunSignals in main) for the common case, or a Group when you
// need options, readiness (Group.Ready), or an accepting check
// (Group.AllAccepting). SignalContext turns SIGINT/SIGTERM into context
// cancellation for either.
//
// Server is the minimal contract. A server can also report readiness and
// health through the optional ReadyNotifier, AcceptingReporter and
// HealthReporter interfaces, which Group detects by type assertion.
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
}

// ReadyNotifier is optionally implemented by a Server that can signal when it
// starts accepting clients. Group.Ready prefers it over AcceptingReporter;
// Group.AllAccepting doesn't use it.
//
// A wrapper that embeds Server hides this and the other optional interfaces;
// forward them explicitly (var _ ReadyNotifier = (*wrapper)(nil)).
type ReadyNotifier interface {
	// Ready returns a channel that is closed once the server is accepting
	// clients. It is never closed if the server fails to start.
	Ready() <-chan struct{}
}

// AcceptingReporter is optionally implemented by a Server that can report
// whether it is accepting clients. Group.Ready polls it for servers that
// don't implement ReadyNotifier, and Group.AllAccepting uses it when present.
type AcceptingReporter interface {
	// IsAcceptingClients reports whether the server is ready to serve clients.
	IsAcceptingClients() bool
}

// HealthReporter is optionally implemented by a Server that can report its
// health. Group doesn't use it; callers type-assert for their own probes.
type HealthReporter interface {
	// IsHealthy reports whether the server is considerably healthy.
	IsHealthy() bool
}
