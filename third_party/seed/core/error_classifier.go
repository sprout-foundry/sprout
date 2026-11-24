package core

import (
	"regexp"
	"strings"
)

// ClassifyError wraps a raw provider error in a typed error based on
// error message patterns. If err is nil or already a typed error, it
// is returned unchanged.
func ClassifyError(err error, provider string) error {
	if err == nil {
		return nil
	}

	// Return typed errors unchanged.
	if IsTransient(err) || IsRateLimit(err) || IsContextOverflow(err) || IsAuthError(err) || IsClientError(err) || IsToolThreadingError(err) {
		return err
	}

	msg := strings.ToLower(err.Error())

	// Rate limit patterns.
	if containsAny(msg, "rate limit", "rate_limit", "too many requests", "quota exceeded", "quota_exceeded",
		"insufficient_quota", "insufficient quota", "request rate limited") ||
		containsStatusCode(msg, "429") {
		return &RateLimitError{
			Provider: provider,
			Wrapped:  err,
		}
	}

	// Auth error patterns.
	if containsAny(msg, "unauthorized", "authentication failed", "authentication error",
		"invalid api key", "invalid_api_key", "api key is not valid", "api key invalid", "api key rejected",
		"permission denied") ||
		containsStatusCode(msg, "401") {
		return &AuthError{
			Provider: provider,
			Wrapped:  err,
		}
	}

	// Context overflow patterns. Must come BEFORE the generic 4xx client-error
	// check below: providers like OpenAI return context-length errors as
	// "HTTP 400: This model's maximum context length is …", which contains a
	// standalone "400" and would otherwise be classified as a generic
	// non-retryable ClientError. The text patterns here are more specific than
	// the bare status code, so they take precedence.
	if containsAny(msg, "context window", "maximum context", "max_tokens", "max context", "exceed_context_size",
		"available context size", "maximum context length", "context_length_exceeded",
		"input is too long", "prompt is too long") {
		return &ContextOverflowError{
			Wrapped: err,
		}
	}

	// Tool-call threading rejection. Some providers (MiniMax, DeepSeek) return
	// HTTP 400 with a distinctive message when tool results don't follow their
	// tool calls. Detect this BEFORE the generic 4xx client-error check below
	// so callers can trigger diagnostic capture and targeted recovery instead
	// of an opaque ClientError. MiniMax's code is 2013.
	if containsAny(msg, "tool call result does not follow", "does not follow tool call",
		"tool result does not follow", "tool call does not match",
		"tool_use_ids must match", "tool messages must follow") {
		return &ToolThreadingError{
			Provider: provider,
			Wrapped:  err,
		}
	}

	// Client error patterns (4xx), and 501: the server does not offer what
	// was asked, which a retry cannot change.
	if containsAny(msg, "bad request", "invalid parameter", "invalid api parameter",
		"payment required", "not found", "unprocessable", "method not allowed", "conflict",
		"payload too large", "uri too long", "unsupported media type",
		"too many fields exceeded", "request entity too large", "not implemented") ||
		containsStatusCode(msg, "501") ||
		containsStatusCode(msg, "400") || containsStatusCode(msg, "402") || containsStatusCode(msg, "403") ||
		containsStatusCode(msg, "404") || containsStatusCode(msg, "405") ||
		containsStatusCode(msg, "409") || containsStatusCode(msg, "410") ||
		containsStatusCode(msg, "413") || containsStatusCode(msg, "414") ||
		containsStatusCode(msg, "415") || containsStatusCode(msg, "422") ||
		containsStatusCode(msg, "431") {
		return &ClientError{
			Provider: provider,
			Wrapped:  err,
		}
	}

	// Transient error: network / timeout patterns.
	if containsAny(msg, "timeout", "deadline exceeded", "context deadline") {
		return &TransientError{
			Op:       "chat",
			Provider: provider,
			Wrapped:  err,
		}
	}

	// Transient error: server error / connection patterns.
	if containsAny(msg, "internal server error", "service unavailable", "bad gateway", "gateway timeout",
		"connection refused", "connection reset", "etimedout", "etx") ||
		containsStatusCode(msg, "500") || containsStatusCode(msg, "502") ||
		containsStatusCode(msg, "503") || containsStatusCode(msg, "504") {
		return &TransientError{
			Op:       "chat",
			Provider: provider,
			Wrapped:  err,
		}
	}

	// Default: wrap everything else as TransientError.
	return &TransientError{
		Op:       "chat",
		Provider: provider,
		Wrapped:  err,
	}
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// statusCodeRe matches an HTTP status code as a standalone number
// (not embedded in a longer digit sequence).
var statusCodeRe = regexp.MustCompile(`\b(\d{3})\b`)

// containsStatusCode reports whether msg contains the given 3-digit
// HTTP status code as a standalone number (not part of a longer number).
func containsStatusCode(msg, code string) bool {
	allMatches := statusCodeRe.FindAllStringSubmatch(msg, -1)
	for _, m := range allMatches {
		if m[1] == code {
			return true
		}
	}
	return false
}
