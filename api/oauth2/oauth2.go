package oauth2

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/schema"
)

var (
	schemaEncoder = schema.NewEncoder()
	schemaDecoder = schema.NewDecoder()
)

func RespondTo(w http.ResponseWriter) Responder { return Responder{w} }

type Responder struct {
	w http.ResponseWriter
}

type GrantType string

const (
	GrantTypeAuthorizationCode GrantType = "authorization_code"
	GrantTypeClientCredentials GrantType = "client_credentials"
	GrantTypePassword          GrantType = "password"
	GrantTypeRefreshToken      GrantType = "refresh_token"

	GrantTypeUnknown GrantType = ""
)

func GrantTypeFromString(s string) GrantType {
	switch s {
	case string(GrantTypeAuthorizationCode):
		return GrantTypeAuthorizationCode
	case string(GrantTypeClientCredentials):
		return GrantTypeClientCredentials
	case string(GrantTypePassword):
		return GrantTypePassword
	case string(GrantTypeRefreshToken):
		return GrantTypeRefreshToken
	}
	return GrantTypeUnknown
}

type ErrorCode string

const (
	// ErrorInvalidRequest 4.1.2.1, 4.2.2.1, 5.2
	ErrorInvalidRequest ErrorCode = "invalid_request"
	// ErrorUnauthorizedClient 4.1.2.1, 4.2.2.1, 5.2
	ErrorUnauthorizedClient ErrorCode = "unauthorized_client"
	// ErrorAccessDenied 4.1.2.1, 4.2.2.1
	ErrorAccessDenied ErrorCode = "access_denied"
	// ErrorUnsupportedResponseType 4.1.2.1, 4.2.2.1
	ErrorUnsupportedResponseType ErrorCode = "unsupported_response_type"
	// ErrorInvalidScope 4.1.2.1, 4.2.2.1, 5.2
	ErrorInvalidScope ErrorCode = "invalid_scope"
	// ErrorServerError 4.1.2.1, 4.2.2.1
	ErrorServerError ErrorCode = "server_error"
	// ErrorTemporarilyUnavailable 4.1.2.1, 4.2.2.1
	ErrorTemporarilyUnavailable ErrorCode = "temporarily_unavailable"
	// ErrorInvalidClient 5.2
	ErrorInvalidClient ErrorCode = "invalid_client"
	// ErrorInvalidGrant 5.2
	ErrorInvalidGrant ErrorCode = "invalid_grant"
	// ErrorUnsupportedGrantType 5.2
	ErrorUnsupportedGrantType ErrorCode = "unsupported_grant_type"
)

func (errorCode ErrorCode) HTTPStatusCode() int {
	switch errorCode {
	case ErrorInvalidRequest,
		ErrorInvalidClient,
		ErrorInvalidGrant,
		ErrorUnauthorizedClient,
		ErrorUnsupportedGrantType:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

type ResponseType string

const (
	ResponseTypeCode  ResponseType = "code"
	ResponseTypeToken ResponseType = "token"

	ResponseTypeUnknown ResponseType = ""
)

func ResponseTypeFromString(s string) ResponseType {
	switch s {
	case string(ResponseTypeCode):
		return ResponseTypeCode
	case string(ResponseTypeToken):
		return ResponseTypeToken
	}
	return ResponseTypeUnknown
}

func (responseType ResponseType) String() string { return string(responseType) }

type TokenType string

// TokenTypeBearer NOTE: token types are case-insensitive
const (
	TokenTypeBearer TokenType = "bearer"
)

// TokenResponse is used on successful authorization. The authorization
// server issues an access token and optional refresh
// token, and constructs the response by adding the following parameters
// to the entity-body of the HTTP response with a 200 (OK) status code
type TokenResponse struct {
	// The access token issued by the authorization server.
	AccessToken string `json:"access_token" schema:"access_token"`
	// The type of the token issued as described in
	// Section 7.1.  Value is case insensitive.
	TokenType TokenType `json:"token_type" schema:"token_type"`
	// The lifetime in seconds of the access token.  For
	// example, the value "3600" denotes that the access token will
	// expire in one hour from the time the response was generated.
	// If omitted, the authorization server SHOULD provide the
	// expiration time via other means or document the default value.
	ExpiresIn int64 `json:"expires_in,omitempty" schema:"expires_in,omitempty"`
	// The refresh token, which can be used to obtain new
	// access tokens using the same authorization grant as described
	// in Section 6.
	RefreshToken string `json:"refresh_token,omitempty" schema:"refresh_token,omitempty"`
	// The scope of the access token as described by Section 3.3.
	Scope string `json:"scope,omitempty" schema:"scope,omitempty"`

	State string `json:"-" schema:"state,omitempty"`
}

func (TokenResponse) SwaggerDoc() map[string]string {
	return map[string]string{
		"": "See https://tools.ietf.org/html/rfc6749#section-5.1 for details.",
	}
}

type ErrorResponse struct {
	Error            ErrorCode `json:"error" schema:"error"`
	ErrorDescription string    `json:"error_description,omitempty" schema:"error_description,omitempty"`
	ErrorURI         string    `json:"error_uri,omitempty" schema:"error_uri,omitempty"`
	State            string    `json:"-" schema:"state,omitempty"`
}

func (ErrorResponse) SwaggerDoc() map[string]string {
	return map[string]string{
		"": "See https://tools.ietf.org/html/rfc6749#section-5.2 for details.",
	}
}

func (r Responder) ErrInvalidClientBasicAuthorization(realmName string, errorDesc string) {
	if realmName == "" {
		realmName = "Restricted"
	}
	r.w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", realmName))
	r.ErrorWithHTTPStatusCode(ErrorResponse{
		Error:            ErrorInvalidClient,
		ErrorDescription: errorDesc,
	}, http.StatusUnauthorized)
}

func (r Responder) Error(errorData ErrorResponse) {
	r.ErrorWithHTTPStatusCode(errorData, errorData.Error.HTTPStatusCode())
}

func (r Responder) ErrorCode(errorCode ErrorCode) {
	r.ErrorWithHTTPStatusCode(ErrorResponse{Error: errorCode}, errorCode.HTTPStatusCode())
}

func (r Responder) ErrorWithHTTPStatusCode(errorData ErrorResponse, httpStatusCode int) {
	r.w.Header().Set("Content-Type", "application/json")
	r.w.WriteHeader(httpStatusCode)
	err := json.NewEncoder(r.w).Encode(errorData)
	if err != nil {
		panic(err)
	}
}

func (r Responder) TokenCustom(tokenData any) {
	r.w.Header().Set("Content-Type", "application/json")
	r.w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(r.w).Encode(tokenData)
	if err != nil {
		panic(err)
	}
}

type AuthorizationRequest struct {
	ResponseType string `schema:"response_type"`
	ClientID     string `schema:"client_id"`
	RedirectURI  string `schema:"redirect_uri,omitempty"`
	Scope        string `schema:"scope,omitepmty"`
	State        string `schema:"state,omitempty"`
}

func AuthorizationRequestFromURLValues(values url.Values) (*AuthorizationRequest, error) {
	var req AuthorizationRequest
	err := schemaDecoder.Decode(&req, values)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

type AuthorizationResponse struct {
	Code  string `schema:"code"`
	State string `schema:"state"`
}

type AccessTokenRequest struct {
	GrantType GrantType `schema:"grant_type"`
	// Code is required in 'code' flow
	Code string `schema:"code"`
	// Username is required in 'password' flow
	Username string `schema:"username"`
	// Password is used in 'password' flow
	Password string `schema:"password"`
}

func QueryString(d any) (queryString string, err error) {
	values := url.Values{}
	err = schemaEncoder.Encode(d, values)
	if err != nil {
		return "", err
	}
	return values.Encode(), nil
}

func MustQueryString(d any) string {
	s, err := QueryString(d)
	if err != nil {
		panic(err)
	}
	return s
}
