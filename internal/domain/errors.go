package domain

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// APIError represents standard OpenAI-compatible error response
type APIError struct {
	StatusCode int           `json:"-"`
	Inner      ErrorEnvelope `json:"error"`
}

// ErrorEnvelope wraps the inner error payload
type ErrorEnvelope struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param,omitempty"`
	Code    *string `json:"code,omitempty"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API Error [%d] %s: %s", e.StatusCode, e.Inner.Type, e.Inner.Message)
}

// NewAPIError creates a formatted API error
func NewAPIError(statusCode int, errType, message string, code ...string) *APIError {
	var c *string
	if len(code) > 0 && code[0] != "" {
		c = &code[0]
	}
	return &APIError{
		StatusCode: statusCode,
		Inner: ErrorEnvelope{
			Message: message,
			Type:    errType,
			Code:    c,
		},
	}
}

// Common error constructors
func ErrUnauthorized(message string) *APIError {
	return NewAPIError(http.StatusUnauthorized, "invalid_request_error", message, "invalid_api_key")
}

func ErrInvalidRequest(message string) *APIError {
	return NewAPIError(http.StatusBadRequest, "invalid_request_error", message)
}

func ErrNotFound(message string) *APIError {
	return NewAPIError(http.StatusNotFound, "invalid_request_error", message, "model_not_found")
}

func ErrRateLimit(message string) *APIError {
	return NewAPIError(http.StatusTooManyRequests, "rate_limit_error", message, "rate_limit_exceeded")
}

func ErrInternal(message string) *APIError {
	return NewAPIError(http.StatusInternalServerError, "api_error", message, "internal_server_error")
}

func ErrBadGateway(message string) *APIError {
	return NewAPIError(http.StatusBadGateway, "api_error", message, "upstream_provider_error")
}

// WriteJSON writes the formatted error payload to HTTP response writer
func (e *APIError) WriteJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.StatusCode)
	_ = json.NewEncoder(w).Encode(e)
}
