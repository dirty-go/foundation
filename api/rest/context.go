package rest

import (
	"net/http"

	"github.com/dirty-go/foundation/api"
)

type RequestContext interface {
	api.CallContext
	HTTPRequest() *http.Request
}
