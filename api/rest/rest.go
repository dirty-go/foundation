package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

type ErrorResponse struct {
	// Code this will be used as error code
	Code string `json:"code,omitempty"`

	// We use the term description because it describes the error
	// to the developer rather than a message for the end user.
	Description string `json:"description,omitempty"`

	Fields []ErrorResponseField `json:"fields,omitempty"`
	DocURL string               `json:"doc_url,omitempty"`
}

type ErrorResponseField struct {
	Field       string `json:"field"`
	Code        string `json:"code,omitempty"`
	Description string `json:"desciption,omitempty"`
	DocURL      string `json:"doc_url,omitempty"`
}

type EmptyRequest struct{}

type EmptyResponse struct{}

type Responder struct {
	w http.ResponseWriter
}

func RespondTo(w http.ResponseWriter) Responder { return Responder{w} }

func (r Responder) encodeToJSON(jsonData any) error {
	return json.NewEncoder(r.w).Encode(jsonData)
}

func (r Responder) Error(errorData any, httpStatusCode int) {
	r.w.Header().Set("Content-Type", "application/json")
	if httpStatusCode == 0 {
		httpStatusCode = 422
	}
	r.w.WriteHeader(httpStatusCode)
	err := r.encodeToJSON(errorData)
	if err != nil {
		panic(err)
	}
}

func (r Responder) EmptyError(httpStatusCode int) {
	r.w.Header().Set("Content-Type", "application/json")
	if httpStatusCode == 0 {
		httpStatusCode = 422
	}
	r.w.WriteHeader(httpStatusCode)
	_, _ = r.w.Write([]byte("{}"))
}

func (r Responder) Success(successData any) {
	if successData == nil {
		r.w.WriteHeader(http.StatusNoContent)
		return
	}
	r.SuccessWithHTTPStatusCode(successData, http.StatusOK)
}

func (r Responder) SuccessWithHTTPStatusCode(successData any, httpStatusCode int) {
	r.w.Header().Set("Content-Type", "application/json")
	if httpStatusCode == 0 {
		httpStatusCode = http.StatusOK
	}
	r.w.WriteHeader(httpStatusCode)
	err := r.encodeToJSON(successData)
	if err != nil {
		panic(err)
	}
}

type LogServiceServer interface {
	LogRequest(statusCode int, message string, endPoint string, method string, ipAddr string, referer string, userAgent string, latency int64, data json.RawMessage) error
}

func unmarshalSingle(data map[string]any, name string, out any) error {
	t := reflect.TypeOf(out)
	if t.Kind() != reflect.Ptr {
		return errors.New("output must be a pointer")
	}
	val, found := data[name]
	if !found {
		return fmt.Errorf("%s not found in map, contents: %v", name, data)
	}

	o := reflect.ValueOf(out).Elem()

	switch out.(type) {
	case *int:
		v, ok := val.(float64)
		if !ok {
			return fmt.Errorf("cannot assert %s to an integer via float, value: %v", name, val)
		}
		o.Set(reflect.ValueOf(int(v)))
	case *string:
		v, ok := val.(string)
		if !ok {
			return fmt.Errorf("cannot assert %s to a string, value: %v", name, val)
		}
		o.Set(reflect.ValueOf(v))
	case *bool:
		v, ok := val.(bool)
		if !ok {
			return fmt.Errorf("cannot assert %s to a bool, value: %v", name, val)
		}
		o.Set(reflect.ValueOf(v))
	default:
		return fmt.Errorf("%s is not an int, string, or bool, value: %v", name, val)
	}

	return nil
}
