// Package agent provides the seed integration layer.
//
// seed_conversions.go — identity conversion helpers between seed/core types
// and sprout api types (now aliases), plus error wrapping utilities.

package agent

import (
	"fmt"
	"regexp"
	"strings"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// Conversion helpers — identity functions (types are now aliases)
// ---------------------------------------------------------------------------

// seedRequestToSprout returns the request unchanged since
// api.ChatRequest and core.ChatRequest are the same type.
func seedRequestToSprout(req *core.ChatRequest) *core.ChatRequest {
	return req
}



// sproutResponseToSeed returns the response unchanged since
// api.ChatResponse and core.ChatResponse are the same type.
func sproutResponseToSeed(resp *api.ChatResponse) *core.ChatResponse {
	return resp
}

// ---------------------------------------------------------------------------
// Error wrapping (mimics old ErrorHandler.HandleAPIFailure)
// ---------------------------------------------------------------------------

// wrapError converts a non-retryable error into a user-friendly string.
// This is called when seed.Run() returns an error that should be shown
// to the user rather than propagated as a Go error.
func wrapError(err error) string {
	msg := err.Error()

	// If the error is already wrapped with "chat failed:" prefix, strip it
	// to avoid double-wrapping
	chatFailedRe := regexp.MustCompile(`^chat failed: (.+)$`)
	if matches := chatFailedRe.FindStringSubmatch(msg); len(matches) > 1 {
		msg = matches[1]
	}

	// Use typed error classification first, then fall back to string matching
	// for errors that aren't AgentError types (backward compat bridge).
	if agenterrors.IsProviderError(err) {
		return fmt.Sprintf("Authentication failed: %s. Please check your API key and configuration.", msg)
	}
	if agenterrors.IsRateLimited(err) {
		return fmt.Sprintf("Rate limit exceeded: %s. Please wait before making more requests.", msg)
	}
	if agenterrors.IsTransient(err) {
		return fmt.Sprintf("The AI service encountered a temporary error and could not recover after several attempts: %s", msg)
	}

	// Fallback: string matching for errors that aren't typed AgentErrors.
	// This maintains backward compatibility with legacy error sources.
	if strings.Contains(msg, "authentication") || strings.Contains(msg, "invalid API key") || strings.Contains(msg, "unauthorized") {
		return fmt.Sprintf("Authentication failed: %s. Please check your API key and configuration.", msg)
	}
	if strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "429") {
		return fmt.Sprintf("Rate limit exceeded: %s. Please wait before making more requests.", msg)
	}
	if strings.Contains(msg, "transient error") || strings.Contains(msg, "max retries exhausted") {
		return fmt.Sprintf("The AI service encountered a temporary error and could not recover after several attempts: %s", msg)
	}
	if strings.Contains(msg, "context deadline") || strings.Contains(msg, "timeout") {
		return fmt.Sprintf("The request timed out: %s. Please try again.", msg)
	}

	// Default: return the error message as-is
	return fmt.Sprintf("An error occurred: %s", msg)
}
