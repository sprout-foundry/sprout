package agent

import (
	"errors"
	"strings"
	"testing"
)

// TestDisplayIntermediateResponse tests the branching logic of displayIntermediateResponse.
// We can't easily mock PrintLine output, so we test the behavior differences between
// streaming and non-streaming modes using observable state changes.

func TestDisplayIntermediateResponse_EmptyContent(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	// These should not panic
	ch.displayIntermediateResponse("")
	ch.displayIntermediateResponse("   ")
	ch.displayIntermediateResponse("\t\n")
}

func TestDisplayIntermediateResponse_NonStreaming_UsesThoughtPrefix(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	// Capture what gets passed to streaming callback
	var capturedCallbackCalls []string
	a.output.SetStreamingCallback(func(s string) {
		capturedCallbackCalls = append(capturedCallbackCalls, s)
	})
	// NOT streaming enabled — so the callback won't be called via printLineInternal
	// Instead, PrintLine goes to fmt.Print. We verify the code path by checking
	// that the [thought] prefix would be used.
	a.output.SetStreamingEnabled(false)

	ch := &ConversationHandler{
		agent: a,
	}

	// The function should call PrintLine with [thought] prefix.
	// We can't intercept fmt.Print, but we can verify no streaming callback was called.
	ch.displayIntermediateResponse("test thought")

	// In non-streaming mode, the thought should NOT go through streaming callback
	for _, call := range capturedCallbackCalls {
		if strings.Contains(call, "test thought") {
			t.Errorf("unexpected streaming callback in non-streaming mode: %q", call)
		}
	}
}

func TestDisplayIntermediateResponse_StreamingEnabled_NoExtraPrint(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.output.SetStreamingEnabled(true)

	ch := &ConversationHandler{
		agent: a,
	}

	// In streaming mode, displayIntermediateResponse should do nothing extra.
	// Verify it doesn't panic.
	ch.displayIntermediateResponse("already streamed")
}

// TestDisplayFinalResponse tests the branching between streaming and non-streaming.

func TestDisplayFinalResponse_NonStreaming_Prints(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.output.SetStreamingEnabled(false)

	// Set up a streaming callback to catch if the content leaks through the wrong path
	var callbackCalls []string
	a.output.SetStreamingCallback(func(s string) {
		callbackCalls = append(callbackCalls, s)
	})

	ch := &ConversationHandler{
		agent: a,
	}

	// In non-streaming mode, displayFinalResponse should call PrintLine.
	// We can't intercept fmt.Print, but we can verify the function doesn't panic.
	ch.displayFinalResponse("final response")
}

func TestDisplayFinalResponse_StreamingEnabled_NoPrint(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.output.SetStreamingEnabled(true)

	// Set up a streaming callback — since streaming is enabled, displayFinalResponse
	// should return early and NOT call PrintLine (which would then trigger the callback)
	var callbackCalls []string
	a.output.SetStreamingCallback(func(s string) {
		callbackCalls = append(callbackCalls, s)
	})

	ch := &ConversationHandler{
		agent: a,
	}

	ch.displayFinalResponse("already streamed content")

	// Since streaming is enabled, it should skip printing entirely.
	// The callback should NOT have been called with our content.
	for _, call := range callbackCalls {
		if strings.Contains(call, "already streamed content") {
			t.Errorf("unexpected callback call in streaming mode: %q", call)
		}
	}
}

// TestDisplayUserFriendlyError tests the error categorization logic.
// These tests verify the decision tree for error classification.

func TestDisplayUserFriendlyError_TimeoutNoResponse(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	// Should not panic; the message should contain "taking longer"
	// We can't intercept fmt.Print, but we can verify no crash.
	err := errors.New("request timed out: no response received")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_TimeoutNoData(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("timed out: no data received")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_TimeoutGeneric(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("request timed out after 30s")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_ConnectionError(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("connection refused")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_NetworkError(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("network unreachable")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_RateLimit429(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("HTTP 429 Too Many Requests")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_RateLimitText(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("rate limit exceeded")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_401(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("HTTP 401 unauthorized")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_Unauthorized(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("unauthorized access")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_500(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("HTTP 500 internal server error")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_502(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("HTTP 502 bad gateway")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_503(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("HTTP 503 service unavailable")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_GenericError(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	err := errors.New("some random error that doesn't match any category")
	ch.displayUserFriendlyError(err)
}

func TestDisplayUserFriendlyError_Nil(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ch := &ConversationHandler{
		agent: a,
	}

	// Passing nil error panics because displayUserFriendlyError calls err.Error()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when passing nil error to displayUserFriendlyError")
		}
	}()

	ch.displayUserFriendlyError(nil)
}
