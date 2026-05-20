package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// formatBackgroundPromotionMessage
// =============================================================================

func TestFormatBackgroundPromotionMessage_Basic(t *testing.T) {
	result := formatBackgroundPromotionMessage("session-123", "make build", "Building...")

	assert.Contains(t, result, "exceeded the 2-minute tool deadline")
	assert.Contains(t, result, "still running in background session session-123")
	assert.Contains(t, result, "Command: make build")
	assert.Contains(t, result, "Output so far (partial):")
	assert.Contains(t, result, "Building...")
	assert.Contains(t, result, "IMPORTANT: the output above is partial")
	assert.Contains(t, result, "actively poll")
	assert.Contains(t, result, "check_background=\"session-123\"")
	assert.Contains(t, result, "stop_background=\"session-123\"")
}

func TestFormatBackgroundPromotionMessage_OutputTruncation(t *testing.T) {
	// Output exceeding the 2000-char preview cap gets the doubly-partial caveat.
	longOutput := strings.Repeat("A", 2500)

	result := formatBackgroundPromotionMessage("bg-456", "npm test", longOutput)

	assert.Contains(t, result, "... (preview truncated)")
	assert.Contains(t, result, "doubly partial")
	assert.Contains(t, result, "still running in background session bg-456")
	// Verify the preview is bounded — well under the full 2500 As.
	assert.Less(t, strings.Count(result, "A"), 2500)
}

func TestFormatBackgroundPromotionMessage_EmptyOutput(t *testing.T) {
	result := formatBackgroundPromotionMessage("empty-session", "echo hello", "")

	assert.Contains(t, result, "still running in background session empty-session")
	assert.Contains(t, result, "Command: echo hello")
	assert.Contains(t, result, "Output so far (partial):")
	assert.Contains(t, result, "check_background=\"empty-session\"")
}

func TestFormatBackgroundPromotionMessage_ShortOutputNotTruncated(t *testing.T) {
	// Output under 2000 chars should not get the truncation caveat.
	shortOutput := "line1\nline2\nline3"

	result := formatBackgroundPromotionMessage("short-session", "ls -la", shortOutput)

	assert.Contains(t, result, "line1")
	assert.Contains(t, result, "line2")
	assert.Contains(t, result, "line3")
	assert.NotContains(t, result, "preview truncated")
	assert.NotContains(t, result, "doubly partial")
}

func TestFormatBackgroundPromotionMessage_SpecialCharactersInCommand(t *testing.T) {
	command := "curl -X POST 'http://localhost:8080/api/test?foo=bar&baz=qux'"
	result := formatBackgroundPromotionMessage("curl-session", command, "Response received")

	assert.Contains(t, result, command)
	assert.Contains(t, result, "still running in background session curl-session")
}

func TestFormatBackgroundPromotionMessage_SessionIDRepeated(t *testing.T) {
	// Verify the session ID appears exactly 3 times: in the background session line,
	// in the check_background instruction, and in the stop_background instruction
	sessionID := "my-unique-session-abc"
	result := formatBackgroundPromotionMessage(sessionID, "some cmd", "output")

	// Count occurrences of the session ID
	count := strings.Count(result, sessionID)
	assert.Equal(t, 3, count, "session ID should appear 3 times in the message")
}
