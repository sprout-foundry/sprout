package agent

import (
	"errors"
	"testing"
)

func TestDisplayUserFriendlyError(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.initSubManagers()
	ch := &ConversationHandler{agent: a}

	tests := []struct {
		name string
		err  error
	}{
		{name: "timeout with no response received", err: errors.New("request timed out: no response received")},
		{name: "timeout with no data received", err: errors.New("request timed out: no data received")},
		{name: "generic timeout", err: errors.New("request timed out")},
		{name: "connection error", err: errors.New("connection refused")},
		{name: "network error", err: errors.New("network unreachable")},
		{name: "rate limit 429", err: errors.New("HTTP 429 Too Many Requests")},
		{name: "rate limit text", err: errors.New("rate limit exceeded")},
		{name: "unauthorized 401", err: errors.New("HTTP 401 Unauthorized")},
		{name: "unauthorized text", err: errors.New("unauthorized access")},
		{name: "server error 500", err: errors.New("HTTP 500 Internal Server Error")},
		{name: "server error 502", err: errors.New("HTTP 502 Bad Gateway")},
		{name: "server error 503", err: errors.New("HTTP 503 Service Unavailable")},
		{name: "generic error", err: errors.New("something went wrong")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Verify the function doesn't panic for any error category
			ch.displayUserFriendlyError(tt.err)
		})
	}
}

func TestDisplayUserFriendlyError_NilAgent(t *testing.T) {
	t.Parallel()
	// Should not panic with a minimal agent
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.initSubManagers()
	ch := &ConversationHandler{agent: a}
	ch.displayUserFriendlyError(errors.New("test error"))
}
