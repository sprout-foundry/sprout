package agent

import (
	"errors"
	"testing"
)

// TestDisplayIntermediateResponse2 tests that display doesn't panic
func TestDisplayIntermediateResponse2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	// Should not panic
	ch.displayIntermediateResponse("")
	ch.displayIntermediateResponse("test content")
}

// TestDisplayFinalResponse2 tests that display doesn't panic
func TestDisplayFinalResponse2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	// Should not panic
	ch.displayFinalResponse("")
	ch.displayFinalResponse("test final")
}

// TestDisplayUserFriendlyError2 tests that error display doesn't panic
func TestDisplayUserFriendlyError2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	errStrings := []string{
		"request timed out",
		"connection refused",
		"HTTP 401 unauthorized",
		"HTTP 500",
		"rate limit exceeded",
	}

	for _, errStr := range errStrings {
		ch.displayUserFriendlyError(errors.New(errStr))
	}
}

// TestDisplayFunctions_MinimalAgent2 verifies displays work with a minimal
// (non-nil) agent that has no output config. This exercises the non-streaming
// code path without a full agent setup.
func TestDisplayFunctions_MinimalAgent2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	// Should not panic
	ch.displayIntermediateResponse("test")
	ch.displayFinalResponse("final")
	ch.displayUserFriendlyError(errors.New("error"))
}
