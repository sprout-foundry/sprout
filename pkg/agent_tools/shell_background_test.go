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

	assert.Contains(t, result, "Command timed out after 2 minutes")
	assert.Contains(t, result, "still running in background session session-123")
	assert.Contains(t, result, "Command: make build")
	assert.Contains(t, result, "Output so far:")
	assert.Contains(t, result, "Building...")
	assert.Contains(t, result, "Check progress: use shell_command with check_background=\"session-123\"")
	assert.Contains(t, result, "Stop it: use shell_command with stop_background=\"session-123\"")
}

func TestFormatBackgroundPromotionMessage_OutputTruncation(t *testing.T) {
	// Create output that exceeds the 2000 char maxPreview limit
	longOutput := strings.Repeat("A", 2500)

	result := formatBackgroundPromotionMessage("bg-456", "npm test", longOutput)

	// Should be truncated to 2000 chars + truncation message
	assert.Contains(t, result, "... (output truncated)")
	assert.Contains(t, result, "still running in background session bg-456")

	// The truncated preview should be exactly 2000 chars before the truncation marker
	// Verify the output portion was truncated by checking it doesn't contain the full 2500 chars
	assert.Less(t, strings.Count(result, "A"), 2500)
}

func TestFormatBackgroundPromotionMessage_EmptyOutput(t *testing.T) {
	result := formatBackgroundPromotionMessage("empty-session", "echo hello", "")

	assert.Contains(t, result, "still running in background session empty-session")
	assert.Contains(t, result, "Command: echo hello")
	assert.Contains(t, result, "Output so far:")
	assert.Contains(t, result, "Check progress: use shell_command with check_background=\"empty-session\"")
}

func TestFormatBackgroundPromotionMessage_ShortOutputNotTruncated(t *testing.T) {
	// Output under 2000 chars should not be truncated
	shortOutput := "line1\nline2\nline3"

	result := formatBackgroundPromotionMessage("short-session", "ls -la", shortOutput)

	assert.Contains(t, result, "line1")
	assert.Contains(t, result, "line2")
	assert.Contains(t, result, "line3")
	assert.NotContains(t, result, "output truncated")
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
