package rest

import (
	"net/http"

	"github.com/timemore/foundation/api"
)

type RequestContext interface {
	api.CallContext
	HTTPRequest() *http.Request
}
