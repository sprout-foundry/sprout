package core

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Sentinel errors returned by the agent lifecycle.
var (
	// ErrNoProvider is returned when NewAgent is called without a Provider.
	ErrNoProvider = errors.New("no provider configured")

	// ErrNoExecutor is returned when NewAgent is called without a ToolExecutor.
	ErrNoExecutor = errors.New("no tool executor configured")

	// ErrInterrupted is returned when the conversation is interrupted by the user.
	ErrInterrupted = errors.New("conversation interrupted")

	// ErrMaxIterations is returned when the maximum iteration count is exceeded.
	ErrMaxIterations = errors.New("maximum iterations exceeded")

	// ErrPaused is returned when Run is called while the agent is paused.
	ErrPaused = errors.New("agent is paused")

	// ErrZeroChoices is returned when the provider returns a valid response
	// with zero choices, indicating an empty completion.
	ErrZeroChoices = errors.New("provider returned zero choices")
)

// TransientError indicates a temporary failure that may succeed on retry.
type TransientError struct {
	// Op is the operation that failed (e.g. "chat", "stream").
	Op string
	// Provider is the provider that returned the error.
	Provider string
	// RetryAfter is a suggested delay before retrying (zero means use default backoff).
	RetryAfter time.Duration
	// Wrapped is the underlying error.
	Wrapped error
}

func (e *TransientError) Error() string {
	base := "transient error"
	if e.Op != "" {
		base += " during " + e.Op
	}
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	if e.Wrapped != nil {
		base += ": " + e.Wrapped.Error()
	}
	return base
}

func (e *TransientError) Unwrap() error { return e.Wrapped }

// IsTransient reports whether err is a TransientError.
func IsTransient(err error) bool {
	var t *TransientError
	return errors.As(err, &t)
}

// RateLimitError indicates the provider has rate-limited requests.
type RateLimitError struct {
	// Provider is the provider that returned the rate limit error.
	Provider string
	// RetryAfter is a suggested delay before retrying (from Retry-After header or similar).
	RetryAfter time.Duration
	// Attempt is the request attempt number when the limit was hit.
	Attempt int
	// Wrapped is the underlying error.
	Wrapped error
}

func (e *RateLimitError) Error() string {
	base := "rate limit exceeded"
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	if e.Attempt > 0 {
		base += ", attempt " + strconv.Itoa(e.Attempt)
	}
	if e.Wrapped != nil {
		base += ": " + e.Wrapped.Error()
	}
	return base
}

func (e *RateLimitError) Unwrap() error { return e.Wrapped }

// IsRateLimit reports whether err is a RateLimitError.
func IsRateLimit(err error) bool {
	var r *RateLimitError
	return errors.As(err, &r)
}

// ContextOverflowError indicates the context window is exceeded.
type ContextOverflowError struct {
	// TokensUsed is the number of tokens that were estimated or used.
	TokensUsed int
	// TokensLimit is the provider's context window limit.
	TokensLimit int
	// Wrapped is the underlying error.
	Wrapped error
}

func (e *ContextOverflowError) Error() string {
	base := "context window exceeded"
	if e.TokensUsed > 0 || e.TokensLimit > 0 {
		base += fmt.Sprintf(" (%d/%d tokens)", e.TokensUsed, e.TokensLimit)
	}
	if e.Wrapped != nil {
		base += ": " + e.Wrapped.Error()
	}
	return base
}

func (e *ContextOverflowError) Unwrap() error { return e.Wrapped }

// IsContextOverflow reports whether err is a ContextOverflowError.
func IsContextOverflow(err error) bool {
	var c *ContextOverflowError
	return errors.As(err, &c)
}

// AuthError indicates authentication failure with a provider.
type AuthError struct {
	// Provider is the provider that returned the auth error.
	Provider string
	// Wrapped is the underlying error.
	Wrapped error
}

func (e *AuthError) Error() string {
	base := "authentication failed"
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	if e.Wrapped != nil {
		base += ": " + e.Wrapped.Error()
	}
	return base
}

func (e *AuthError) Unwrap() error { return e.Wrapped }

// IsAuthError reports whether err is an AuthError.
func IsAuthError(err error) bool {
	var a *AuthError
	return errors.As(err, &a)
}

// ClientError indicates the request was invalid or cannot be fulfilled
// (e.g., HTTP 400 Bad Request, 404 Not Found, 422 Unprocessable Entity).
// These errors are permanent — retrying will not help.
type ClientError struct {
	// Provider is the provider that returned the error.
	Provider string
	// Wrapped is the underlying error.
	Wrapped error
}

func (e *ClientError) Error() string {
	base := "client error"
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	if e.Wrapped != nil {
		base += ": " + e.Wrapped.Error()
	}
	return base
}

func (e *ClientError) Unwrap() error { return e.Wrapped }

// IsClientError reports whether err is a ClientError.
func IsClientError(err error) bool {
	var c *ClientError
	return errors.As(err, &c)
}

// ToolThreadingError indicates the provider rejected the request because tool
// result messages did not satisfy its tool-call threading invariants: each
// tool result must immediately follow the assistant message containing its
// tool call, and multiple results for one assistant turn must appear in the
// same order as the tool calls. Some providers (MiniMax, DeepSeek) enforce
// this strictly and return HTTP 400 with a code like 2013: "tool call result
// does not follow tool call".
//
// This is a permanent ClientError variant — retrying an identical request
// won't help. It is separated from the generic ClientError so callers can
// trigger diagnostic capture and targeted recovery (re-running the threading
// cleanup) rather than failing opaquely.
type ToolThreadingError struct {
	// Provider is the provider that returned the rejection.
	Provider string
	// Violations summarizes the threading problems detected in the prepared
	// message list at capture time. Empty when the error was classified from
	// a provider response before local validation ran.
	Violations []ToolThreadingViolation
	// Wrapped is the underlying error returned by the provider.
	Wrapped error
}

func (e *ToolThreadingError) Error() string {
	base := "tool call threading error"
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	if len(e.Violations) > 0 {
		base += fmt.Sprintf(": %d violation(s)", len(e.Violations))
	}
	if e.Wrapped != nil {
		base += ": " + e.Wrapped.Error()
	}
	return base
}

func (e *ToolThreadingError) Unwrap() error { return e.Wrapped }

// IsToolThreadingError reports whether err is a *ToolThreadingError.
func IsToolThreadingError(err error) bool {
	var t *ToolThreadingError
	return errors.As(err, &t)
}

// ContentFilteredError indicates that the provider's content filter blocked the
// response after a retry attempt was already made.
type ContentFilteredError struct {
	// Provider is the provider that returned the content filter.
	Provider string
}

func (e *ContentFilteredError) Error() string {
	base := "response filtered by content policy (retry exhausted)"
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	return base
}

// IsContentFiltered reports whether err is a ContentFilteredError.
func IsContentFiltered(err error) bool {
	var c *ContentFilteredError
	return errors.As(err, &c)
}

// BlankResponseError indicates the model produced consecutive blank or
// repetitive responses and the conversation was force-finalized.
type BlankResponseError struct {
	// Provider is the provider that produced the blank responses.
	Provider string
	// Count is the number of consecutive blank/repetitive responses.
	Count int
}

func (e *BlankResponseError) Error() string {
	base := fmt.Sprintf("model produced %d consecutive blank or repetitive responses", e.Count)
	if e.Provider != "" {
		base += " (" + e.Provider + ")"
	}
	return base
}

// IsBlankResponse reports whether err is a BlankResponseError.
func IsBlankResponse(err error) bool {
	var b *BlankResponseError
	return errors.As(err, &b)
}
