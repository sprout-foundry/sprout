package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// formatBackgroundPromotionMessage
// =============================================================================

func TestFormatBackgroundPromotionMessage_Basic(t *testing.T) {
	result := formatBackgroundPromotionMessage("session-123", "make build", 2*time.Minute, "Building...")

	assert.Contains(t, result, "ran past its tool deadline (2 minutes)")
	assert.Contains(t, result, "still running in background session session-123")
	assert.Contains(t, result, "Command: make build")
	assert.Contains(t, result, "Output so far (partial):")
	assert.Contains(t, result, "Building...")
	assert.Contains(t, result, "IMPORTANT: the output above is partial")
	assert.Contains(t, result, "Do not run this command again")
	assert.Contains(t, result, "check_background=\"session-123\"")
	assert.Contains(t, result, "stop_background=\"session-123\"")
}

// deadlineClause states the deadline as it applied, with singular units, and
// nothing when there was none.
func TestDeadlineClause(t *testing.T) {
	tests := []struct {
		name     string
		deadline time.Duration
		want     string
	}{
		{"no deadline", 0, ""},
		{"negative", -time.Second, ""},
		{"one second", time.Second, " (1 second)"},
		{"two minutes", 2 * time.Minute, " (2 minutes)"},
		{"three hundred seconds", 300 * time.Second, " (5 minutes)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, deadlineClause(tt.deadline))
		})
	}
}

// shellToolDeadline reports the real deadline on the context — the tool
// deadline is configurable, so the promotion message must not assume 2 minutes.
func TestShellToolDeadline(t *testing.T) {
	t.Run("with deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		got := shellToolDeadline(ctx)
		assert.Greater(t, got, 4*time.Minute)
		assert.LessOrEqual(t, got, 5*time.Minute)
		assert.Equal(t, " (5 minutes)", deadlineClause(got.Round(time.Minute)))
	})

	t.Run("without deadline", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), shellToolDeadline(context.Background()))
		assert.Contains(t, formatBackgroundPromotionMessage("s1", "make", shellToolDeadline(context.Background()), "out"),
			"Command ran past its tool deadline and is still running in background session s1.")
	})
}

func TestFormatBackgroundPromotionMessage_OutputTruncation(t *testing.T) {
	// Output exceeding the 2000-char preview cap gets the doubly-partial caveat.
	longOutput := strings.Repeat("A", 2500)

	result := formatBackgroundPromotionMessage("bg-456", "npm test", 2*time.Minute, longOutput)

	assert.Contains(t, result, "... (preview truncated)")
	assert.Contains(t, result, "doubly partial")
	assert.Contains(t, result, "still running in background session bg-456")
	// Verify the preview is bounded — well under the full 2500 As.
	assert.Less(t, strings.Count(result, "A"), 2500)
}

func TestFormatBackgroundPromotionMessage_EmptyOutput(t *testing.T) {
	result := formatBackgroundPromotionMessage("empty-session", "echo hello", 2*time.Minute, "")

	assert.Contains(t, result, "still running in background session empty-session")
	assert.Contains(t, result, "Command: echo hello")
	assert.Contains(t, result, "Output so far (partial):")
	assert.Contains(t, result, "check_background=\"empty-session\"")
}

func TestFormatBackgroundPromotionMessage_ShortOutputNotTruncated(t *testing.T) {
	// Output under 2000 chars should not get the truncation caveat.
	shortOutput := "line1\nline2\nline3"

	result := formatBackgroundPromotionMessage("short-session", "ls -la", 2*time.Minute, shortOutput)

	assert.Contains(t, result, "line1")
	assert.Contains(t, result, "line2")
	assert.Contains(t, result, "line3")
	assert.NotContains(t, result, "preview truncated")
	assert.NotContains(t, result, "doubly partial")
}

func TestFormatBackgroundPromotionMessage_SpecialCharactersInCommand(t *testing.T) {
	command := "curl -X POST 'http://localhost:8080/api/test?foo=bar&baz=qux'"
	result := formatBackgroundPromotionMessage("curl-session", command, 2*time.Minute, "Response received")

	assert.Contains(t, result, command)
	assert.Contains(t, result, "still running in background session curl-session")
}

func TestFormatBackgroundPromotionMessage_SessionIDRepeated(t *testing.T) {
	// Verify the session ID appears exactly 4 times: in the background session
	// line, in the wait instruction and in the two check_background
	// instructions (wait and read output so far).
	sessionID := "my-unique-session-abc"
	result := formatBackgroundPromotionMessage(sessionID, "some cmd", 2*time.Minute, "output")

	// Count occurrences of the session ID
	count := strings.Count(result, sessionID)
	assert.Equal(t, 4, count, "session ID should appear 4 times in the message")
}
