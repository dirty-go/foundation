package app_test

import (
	"context"
	"net/http"
	"time"

	"github.com/dirty-go/foundation/app"
)

// legacyServer is a downstream-style server written against the old
// app.ServiceServer method set.
type legacyServer struct{}

func (legacyServer) ServerName() string             { return "legacy" }
func (legacyServer) Serve() error                   { return nil }
func (legacyServer) Shutdown(context.Context) error { return nil }
func (legacyServer) IsAcceptingClients() bool       { return true }
func (legacyServer) IsHealthy() bool                { return true }

// Compile-time check that pre-boot downstream usage still builds.
var (
	_ app.ServiceServer = legacyServer{}
	_ app.ServiceServer = (*app.HTTPServer)(nil)
	_ app.ReadyNotifier = (*app.HTTPServer)(nil)

	_ = func(a app.App) {
		a.AddServer(legacyServer{})
		a.AddServer(app.NewHTTPServer("http", &http.Server{ReadHeaderTimeout: time.Second}))
		<-a.Ready()
		a.Run()
	}
	_ = func() { app.RunServers([]app.ServiceServer{legacyServer{}}) }
)
