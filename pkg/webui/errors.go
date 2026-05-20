package webui

// WebUIError provides a structured error response for the WebUI API.
// Handlers should return this type (or wrap it) instead of ad-hoc string
// errors or bare http.Error calls so that the frontend can match on Code
// rather than scraping Message text.
type WebUIError struct {
	// Code is a machine-readable error identifier (e.g., "config_conflict",
	// "provider_unavailable", "rate_limited").
	Code string `json:"code"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`

	// Details contains optional structured data for the frontend to use
	// (e.g., current config summary for a conflict error).
	Details interface{} `json:"details,omitempty"`

	// Retryable indicates whether the client should automatically retry.
	Retryable bool `json:"retryable"`
}

// Error implements the error interface.
func (e *WebUIError) Error() string {
	return e.Message
}

// NewWebUIError creates a new WebUIError with the given fields.
func NewWebUIError(code, message string, retryable bool) *WebUIError {
	return &WebUIError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}
}

// NewWebUIErrorWithDetails creates a new WebUIError with structured details.
func NewWebUIErrorWithDetails(code, message string, retryable bool, details interface{}) *WebUIError {
	return &WebUIError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		Details:   details,
	}
}
