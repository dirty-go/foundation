package app

import (
	"net/http"

	"github.com/dirty-go/foundation/boot"
)

// HTTPServer adapts an *http.Server to a server runnable by App.Run.
//
// Deprecated: use boot.HTTP.
type HTTPServer = boot.HTTP

// NewHTTPServer wraps srv; see boot.NewHTTP, including the default
// ReadHeaderTimeout it sets and its panic on a nil srv.
//
// Deprecated: use boot.NewHTTP.
func NewHTTPServer(name string, srv *http.Server) *HTTPServer {
	return boot.NewHTTP(name, srv)
}
