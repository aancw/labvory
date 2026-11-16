// Package httpx provides transport-layer helpers: JSON envelope, middleware, CSRF, auth, rate limiting.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// MaxBodyBytes limits request bodies (forms and JSON payloads are small).
const MaxBodyBytes = 1 << 20 // 1 MiB

// APIError is the uniform error envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type errorBody struct {
	Error APIError `json:"error"`
}

type jsonError struct{ APIError }

func (e jsonError) Error() string { return e.Message }

// ErrUnauthorized is returned when no valid session is present.
var ErrUnauthorized = jsonError{APIError{Code: "unauthorized", Message: "Authentication required."}}

// ErrForbidden is returned when CSRF or authorization checks fail.
var ErrForbidden = jsonError{APIError{Code: "forbidden", Message: "Request rejected."}}

// Error writes a structured API error with the given HTTP status.
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{APIError{Code: code, Message: message, Details: details}})
}

// WriteJSON encodes v as the JSON response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// DecodeJSON parses a JSON request body into dst, enforcing a size limit.
func DecodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	// Reject trailing content.
	if dec.More() {
		return errors.New("unexpected trailing data in body")
	}
	return nil
}
