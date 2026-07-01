// Package events provides the rate-limited event payload and helper for
// publishing rate-limit signals to the WebUI.
package events

import (
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// RateLimitedEvent is the payload for EventTypeRateLimited. The
// approval broker publishes this when a tool call returns a
// RateLimitError so the WebUI can show "rate-limited, retrying…"
// and disable the input until the backoff elapses.
type RateLimitedEvent struct {
	Provider     string `json:"provider"`
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"max_attempts"`
	RetryAfterMS int    `json:"retry_after_ms"`
	Message      string `json:"message"`
	SessionID    string `json:"session_id,omitempty"`
}

// RateLimitedEventFromError builds a RateLimitedEvent from a typed
// error and a backoff context. If the cause is not a rate-limit
// error, returns nil — caller should check before publishing.
func RateLimitedEventFromError(provider string, attempt, maxAttempts, retryAfterMS int, sessionID string, err error) *RateLimitedEvent {
	if !agenterrors.IsRateLimited(err) {
		return nil
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &RateLimitedEvent{
		Provider:     provider,
		Attempt:      attempt,
		MaxAttempts:  maxAttempts,
		RetryAfterMS: retryAfterMS,
		Message:      msg,
		SessionID:    sessionID,
	}
}
