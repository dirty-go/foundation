package app

import (
	"context"
)

// ServiceServer abstracts all service servers
type ServiceServer interface {
	// ServerName returns the name of the server. This is not to be unique
	ServerName() string

	// Serve starts the server.
	// This method is blocking and won't return until the server is stopped
	// (e.g., through Shutdown)
	Serve() error

	// Shutdown gracefully stops the server.
	Shutdown(ctx context.Context) error

	// IsAcceptingClients returns true if the service is ready to serve clients.
	IsAcceptingClients() bool

	// IsHealthy returns true if the service is considerably healthy.
	IsHealthy() bool
}

// ReadyNotifier is optionally implemented by a ServiceServer that can signal
// when it starts accepting clients. App.Ready polls IsAcceptingClients for
// servers that don't implement it.
type ReadyNotifier interface {
	// Ready returns a channel that is closed once the server is accepting
	// clients. It is never closed if the server fails to start.
	Ready() <-chan struct{}
}
