package app

import "github.com/dirty-go/foundation/boot"

// ServiceServer abstracts all service servers.
//
// Deprecated: use boot.Server.
type ServiceServer = boot.Server

// ReadyNotifier is optionally implemented by a server that can signal when it
// starts accepting clients.
//
// Deprecated: use boot.ReadyNotifier.
type ReadyNotifier = boot.ReadyNotifier
