package events

import (
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

func TestRateLimitedEventFromError_ClassifiesRateLimit(t *testing.T) {
	err := agenterrors.NewRateLimitError("rate limit exceeded", nil, "openai")
	ev := RateLimitedEventFromError("openai", 1, 5, 30000, "session-123", err)
	if ev == nil {
		t.Fatal("expected non-nil event for RateLimitError")
	}
	if ev.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %q", ev.Provider)
	}
	if ev.Attempt != 1 {
		t.Errorf("expected attempt 1, got %d", ev.Attempt)
	}
}

func TestRateLimitedEventFromError_NonRateLimit(t *testing.T) {
	err := agenterrors.NewTransientError("network timeout", nil)
	ev := RateLimitedEventFromError("openai", 1, 5, 30000, "session-123", err)
	if ev != nil {
		t.Errorf("expected nil event for non-rate-limit error, got %+v", ev)
	}
}

func TestRateLimitedEventFromError_BuildsCorrectFields(t *testing.T) {
	err := agenterrors.NewRateLimitError("API rate limit", nil, "anthropic")
	ev := RateLimitedEventFromError("anthropic", 3, 10, 60000, "session-abc", err)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}

	if ev.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", ev.Provider)
	}
	if ev.Attempt != 3 {
		t.Errorf("expected attempt 3, got %d", ev.Attempt)
	}
	if ev.MaxAttempts != 10 {
		t.Errorf("expected max_attempts 10, got %d", ev.MaxAttempts)
	}
	if ev.RetryAfterMS != 60000 {
		t.Errorf("expected retry_after_ms 60000, got %d", ev.RetryAfterMS)
	}
	if ev.SessionID != "session-abc" {
		t.Errorf("expected session_id 'session-abc', got %q", ev.SessionID)
	}
	if ev.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestRateLimitedEventFromError_NilError(t *testing.T) {
	ev := RateLimitedEventFromError("openai", 1, 5, 30000, "session-123", nil)
	if ev != nil {
		t.Errorf("expected nil event for nil error, got %+v", ev)
	}
}

func TestRateLimitedEventFromError_GenericError(t *testing.T) {
	err := agenterrors.NewValidation("bad input", nil)
	ev := RateLimitedEventFromError("openai", 1, 5, 30000, "session-123", err)
	if ev != nil {
		t.Errorf("expected nil event for validation error, got %+v", ev)
	}
}
