package api

import (
	"context"

	"github.com/google/uuid"
)

// A RequestID in our implementation is used as idempotency token.
//
// A good explanation of idempotency token can be viewed here:
// https://www.youtube.com/watch?v=IP-rGJKSZ3s
type RequestID = uuid.UUID

// CallContext holds information obtained from the request. This information
// is generally obtained from the request's metadata (e.g., HTTP request
// header).
type CallContext interface {
	context.Context

	// MethodName returns the name of the method this call is directed to.
	//
	// For HTTP, this method returns the value as "<HTTP_METHOD> <URL>", e.g.,
	// GET /users/me
	//
	MethodName() string

	// RequestID returns the idempotency token, if provided in the call request.
	RequestID() *RequestID

	// RemoteAddress returns the IP address where this call was initiated
	// from. This method might return empty string if it's unable to resolve
	// the address (e.g., behind a proxy and the proxy didn't forward the
	// the origin IP).
	RemoteAddress() string
}

type CallInfo struct {
	MethodName string
	RequestID  *RequestID
}

type CallRemoteInfo struct {
	Address string
}
