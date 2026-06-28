package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sampleSession() SessionSource {
	return SessionSource{
		ID:               "test-123",
		Name:             "Test Session",
		WorkingDirectory: "/home/user/project",
		StartedAt:        time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		LastUpdated:      time.Date(2026, 6, 15, 10, 10, 0, 0, time.UTC),
		Provider:         "openai",
		Model:            "gpt-4",
		TotalCost:        0.42,
		InputTokens:      5000,
		OutputTokens:     7000,
		Messages: []MessageSource{
			{
				Role:      "user",
				Content:   "What is the capital of France?",
				Timestamp: time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
				Cost:      0.1,
				Tokens:    1000,
			},
			{
				Role:      "assistant",
				Content:   "The capital of France is Paris.",
				Timestamp: time.Date(2026, 6, 15, 10, 0, 5, 0, time.UTC),
				Cost:      0.1,
				Tokens:    2000,
				ToolCalls: []ToolCallSource{
					{
						Name:      "shell_command",
						Arguments: `{"command": "echo 'Hello World'"}`,
						Result:    "Hello World",
						Timestamp: time.Date(2026, 6, 15, 10, 0, 6, 0, time.UTC),
					},
				},
			},
			{
				Role: "tool",
				ToolResult: &ToolResultSource{
					ToolCallName: "shell_command",
					Content:      "Hello World",
					Timestamp:    time.Date(2026, 6, 15, 10, 0, 7, 0, time.UTC),
				},
				Timestamp: time.Date(2026, 6, 15, 10, 0, 7, 0, time.UTC),
			},
			{
				Role:      "user",
				Content:   "Can you list files in the directory?",
				Timestamp: time.Date(2026, 6, 15, 10, 1, 0, 0, time.UTC),
				Cost:      0.1,
				Tokens:    1000,
			},
			{
				Role:      "assistant",
				Content:   "Sure, let me list the files for you.",
				Timestamp: time.Date(2026, 6, 15, 10, 1, 5, 0, time.UTC),
				Cost:      0.1,
				Tokens:    2000,
			},
		},
	}
}

// fakeOpenAIKey builds a syntactically valid OpenAI legacy key shape at
// runtime so the pattern never appears literally in git history (GitHub's
// push protection would block it).  Format: sk- + 20 alnum + T3BlbkFJ +
// 20 alnum = 51 bytes.  This is NOT a live key — it's a static fake for
// testing redaction.
func fakeOpenAIKey() string {
	return "sk-" + "aB3dEfGhIjKlMnOpQrSt" + "T3BlbkFJ" + "UvWxYz1234567890aBcD"
}


func sampleSessionWithSecret() SessionSource {
	s := sampleSession()
	// gitleaks requires a keyword prefix (e.g. "api_key=") before the
	// secret value to trigger the generic-api-key detection rule.
	s.Messages[0].Content = "api_key=" + fakeOpenAIKey() + " and here is my request."
	return s
}

func sampleSessionEmpty() SessionSource {
	return SessionSource{
		ID:               "empty-123",
		Name:             "Empty Session",
		WorkingDirectory: "/tmp",
		StartedAt:        time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		LastUpdated:      time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		Provider:         "openai",
		Model:            "gpt-4",
	}
}

func sampleSessionOnlySystem() SessionSource {
	return SessionSource{
		ID:               "sys-123",
		Name:             "System Session",
		WorkingDirectory: "/tmp",
		StartedAt:        time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		LastUpdated:      time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		Provider:         "openai",
		Model:            "gpt-4",
		Messages: []MessageSource{
			{
				Role:      "system",
				Content:   "You are a helpful assistant.",
				Timestamp: time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Markdown tests
// ---------------------------------------------------------------------------

func TestExportMarkdown_BasicUserAssistant(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	// YAML front-matter
	assert.Contains(t, out, "---", "should have front-matter delimiter")
	assert.Contains(t, out, "session_id: test-123")
	assert.Contains(t, out, "name: Test Session")
	assert.Contains(t, out, "total_tokens: 12000") // 5000 + 7000

	// Title
	assert.Contains(t, out, "# Test Session")

	// Summary blockquote
	assert.Contains(t, out, "> Session started")

	// Table of contents
	assert.Contains(t, out, "## Table of Contents")

	// Two turns (two user messages)
	assert.Contains(t, out, "## Turn 1")
	assert.Contains(t, out, "## Turn 2")

	// Labels for each turn
	assert.Contains(t, out, "**User:**")
	assert.Contains(t, out, "**Assistant:**")
}

func TestExportMarkdown_WithToolCalls(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{IncludeToolCalls: true})
	require.NoError(t, err)
	out := buf.String()

	// Tool calls rendered
	assert.Contains(t, out, "<details>")
	assert.Contains(t, out, "<summary>Tool call:")
	assert.Contains(t, out, "</details>")
	assert.Contains(t, out, "```json")
	assert.Contains(t, out, `{"command": "echo 'Hello World'"}`)

	// Now with IncludeToolCalls: false — no <details>/<summary> tool blocks in the body
	var buf2 bytes.Buffer
	err = ExportMarkdown(&buf2, s, ExportOptions{IncludeToolCalls: false})
	require.NoError(t, err)
	out2 := buf2.String()

	assert.NotContains(t, out2, "<details>")
	assert.NotContains(t, out2, "<summary>Tool call:")
	assert.NotContains(t, out2, "```json")
	// Note: tools_used: in front-matter still lists tools — that's metadata, not call blocks
}

func TestExportMarkdown_NoCost(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{IncludeCost: false})
	require.NoError(t, err)
	out := buf.String()

	// Turn footers with cost should not appear
	assert.NotContains(t, out, "*Cost: $")
	// But front-matter still includes total_cost (it's always written)
	assert.Contains(t, out, "total_cost: 0.4200")
}

func TestExportMarkdown_RedactsSecret(t *testing.T) {
	s := sampleSessionWithSecret()

	// With redaction enabled — the fake OpenAI key gets replaced with [REDACTED]
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{RedactSecrets: true})
	require.NoError(t, err)
	out := buf.String()

	assert.NotContains(t, out, fakeOpenAIKey())
	assert.Contains(t, out, "[REDACTED]")

	// Without redaction — the raw fake key should appear unchanged
	var buf2 bytes.Buffer
	err = ExportMarkdown(&buf2, s, ExportOptions{RedactSecrets: false})
	require.NoError(t, err)
	out2 := buf2.String()

	assert.Contains(t, out2, fakeOpenAIKey())
}

func TestExportMarkdown_EmptySession(t *testing.T) {
	s := sampleSessionEmpty()
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	assert.Contains(t, out, "---")
	assert.Contains(t, out, "session_id: empty-123")
	assert.Contains(t, out, "# Empty Session")
	assert.Contains(t, out, "> Session started")
	// No TOC for empty (no turns)
	assert.NotContains(t, out, "## Turn 1")
}

func TestExportMarkdown_OnlySystemMessages(t *testing.T) {
	s := sampleSessionOnlySystem()
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	// groupTurns only starts from user messages, so system-only messages
	// are never rendered in the turn body.  This is correct behavior —
	// system messages typically don't have user-initiated turns.
	assert.NotContains(t, out, "**System:**")
	assert.NotContains(t, out, "## Turn 1")
	// But the front-matter and title should still be present
	assert.Contains(t, out, "---")
	assert.Contains(t, out, "# System Session")
	assert.Contains(t, out, "turns: 0")
}

func TestExportMarkdown_TurnsMetadata(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	// countTurns now uses groupTurns (2 user messages = 2 turns)
	assert.Contains(t, out, "turns: 2")
	// Tools used
	assert.Contains(t, out, "tools_used:")
	assert.Contains(t, out, "  - shell_command")
}

func TestExportMarkdown_Timezone(t *testing.T) {
	s := sampleSession()

	// UTC output
	var utcBuf bytes.Buffer
	err := ExportMarkdown(&utcBuf, s, ExportOptions{})
	require.NoError(t, err)
	utcOut := utcBuf.String()

	// New York timezone
	nycLoc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	var nycBuf bytes.Buffer
	err = ExportMarkdown(&nycBuf, s, ExportOptions{Timezone: nycLoc})
	require.NoError(t, err)
	nycOut := nycBuf.String()

	// The outputs should differ because of timezone conversion
	assert.NotEqual(t, utcOut, nycOut, "NYC and UTC output should differ")

	// NYC timestamp should reflect the EDT offset (UTC-4 in June)
	// StartedAt is 10:00 UTC which is 06:00 EDT
	nycStarted := s.StartedAt.In(nycLoc)
	assert.Contains(t, nycOut, nycStarted.Format("January 2, 2006"))

	// UTC should show the UTC time
	utcStarted := s.StartedAt.In(time.UTC)
	assert.Contains(t, utcOut, utcStarted.Format("January 2, 2006"))
}

// ---------------------------------------------------------------------------
// HTML tests
// ---------------------------------------------------------------------------

func TestExportHTML_SelfContained(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	// No external resources
	assert.NotContains(t, out, "<link")
	assert.NotContains(t, out, `<script src="http`)
	assert.NotContains(t, out, `<img src="http`)
}

func TestExportHTML_BasicRender(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	assert.Contains(t, out, "<!DOCTYPE html>")
	assert.Contains(t, out, "<html")
	assert.Contains(t, out, "</html>")
	assert.Contains(t, out, "<title>Test Session — Sprout Session</title>")
	assert.Contains(t, out, "<style>")
	assert.Contains(t, out, "</style>")

	// Message content (HTML-escaped)
	assert.Contains(t, out, "The capital of France is Paris.")

	// Metadata
	assert.Contains(t, out, "openai")
	assert.Contains(t, out, "gpt-4")
}

func TestExportHTML_EmptySession(t *testing.T) {
	s := sampleSessionEmpty()
	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	assert.Contains(t, out, "<!DOCTYPE html>")
	assert.Contains(t, out, "<html")
	assert.Contains(t, out, "</html>")
	assert.Contains(t, out, "<style>")
	assert.Contains(t, out, "</style>")
}

func TestExportHTML_OpensInBrowser(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{})
	require.NoError(t, err)

	// Parse the HTML output
	doc, err := html.Parse(strings.NewReader(buf.String()))
	require.NoError(t, err)

	// The root is a document node (empty Data); check its first element child
	require.NotNil(t, doc)
	require.NotEmpty(t, doc.FirstChild)
	assert.Equal(t, "html", doc.FirstChild.Data)
}

func TestExportHTML_ToolCalls(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{IncludeToolCalls: true})
	require.NoError(t, err)
	out := buf.String()

	assert.Contains(t, out, "<details>")
	assert.Contains(t, out, "<summary>Tool call: <code>shell_command</code></summary>")
	assert.Contains(t, out, "</details>")
}

func TestExportHTML_ToolResults(t *testing.T) {
	s := sampleSession()
	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{IncludeToolCalls: true})
	require.NoError(t, err)
	out := buf.String()

	assert.Contains(t, out, "<summary>Tool result: <code>shell_command</code></summary>")
	assert.Contains(t, out, "Hello World")
}

// ---------------------------------------------------------------------------
// JSON tests
// ---------------------------------------------------------------------------

func TestExportJSON_LosslessRoundTrip(t *testing.T) {
	s := sampleSession()

	// First export (compact JSON)
	var buf1 bytes.Buffer
	err := ExportJSON(&buf1, s, ExportOptions{PrettyPrintJSON: false, RedactSecrets: false})
	require.NoError(t, err)
	json1 := buf1.Bytes()

	// Unmarshal back
	var s2 SessionSource
	err = json.Unmarshal(json1, &s2)
	require.NoError(t, err)

	// Verify all fields round-trip
	assert.Equal(t, s.ID, s2.ID)
	assert.Equal(t, s.Name, s2.Name)
	assert.Equal(t, s.WorkingDirectory, s2.WorkingDirectory)
	assert.Equal(t, s.TotalCost, s2.TotalCost)
	assert.Equal(t, s.InputTokens, s2.InputTokens)
	assert.Equal(t, s.OutputTokens, s2.OutputTokens)
	assert.Equal(t, len(s.Messages), len(s2.Messages))

	// Verify nested tool calls
	require.Len(t, s2.Messages, len(s.Messages))
	for i, m := range s.Messages {
		assert.Equal(t, m.Role, s2.Messages[i].Role)
		assert.Equal(t, m.Content, s2.Messages[i].Content)
		assert.Equal(t, len(m.ToolCalls), len(s2.Messages[i].ToolCalls))
		for j, tc := range m.ToolCalls {
			assert.Equal(t, tc.Name, s2.Messages[i].ToolCalls[j].Name)
			assert.Equal(t, tc.Arguments, s2.Messages[i].ToolCalls[j].Arguments)
			assert.Equal(t, tc.Result, s2.Messages[i].ToolCalls[j].Result)
		}
		if m.ToolResult != nil {
			require.NotNil(t, s2.Messages[i].ToolResult)
			assert.Equal(t, m.ToolResult.ToolCallName, s2.Messages[i].ToolResult.ToolCallName)
			assert.Equal(t, m.ToolResult.Content, s2.Messages[i].ToolResult.Content)
		}
	}

	// Marshal again for byte-for-byte comparison
	var buf2 bytes.Buffer
	err = ExportJSON(&buf2, s2, ExportOptions{PrettyPrintJSON: false, RedactSecrets: false})
	require.NoError(t, err)
	assert.Equal(t, json1, buf2.Bytes(), "second marshal should be byte-for-byte identical")
}

func TestExportJSON_Pretty(t *testing.T) {
	s := sampleSession()

	// Pretty-printed
	var prettyBuf bytes.Buffer
	err := ExportJSON(&prettyBuf, s, ExportOptions{PrettyPrintJSON: true, RedactSecrets: false})
	require.NoError(t, err)
	prettyOut := prettyBuf.String()

	// Should have indentation (newlines + spaces)
	assert.Contains(t, prettyOut, "\n  ")
	assert.True(t, strings.Count(prettyOut, "\n") > 1)

	// Compact
	var compactBuf bytes.Buffer
	err = ExportJSON(&compactBuf, s, ExportOptions{PrettyPrintJSON: false, RedactSecrets: false})
	require.NoError(t, err)
	compactOut := compactBuf.String()

	// Should be a single line + trailing newline
	lines := strings.Split(strings.TrimSpace(compactOut), "\n")
	assert.Equal(t, 1, len(lines), "compact JSON should be a single line")

	// Both should end with a newline
	assert.True(t, strings.HasSuffix(prettyOut, "\n"))
	assert.True(t, strings.HasSuffix(compactOut, "\n"))
}

func TestExportJSON_EmptySession(t *testing.T) {
	s := sampleSessionEmpty()
	var buf bytes.Buffer
	err := ExportJSON(&buf, s, ExportOptions{})
	require.NoError(t, err)

	var result SessionSource
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, s.ID, result.ID)
	assert.Equal(t, s.Name, result.Name)
}

func TestExportJSON_RedactsSecret(t *testing.T) {
	s := sampleSessionWithSecret()

	// With redaction — all string fields get scanned and redacted
	var buf bytes.Buffer
	err := ExportJSON(&buf, s, ExportOptions{RedactSecrets: true, PrettyPrintJSON: false})
	require.NoError(t, err)
	out := buf.String()

	assert.NotContains(t, out, fakeOpenAIKey())
	assert.Contains(t, out, "[REDACTED]")

	// Without redaction — the original fake key should appear unchanged
	var buf2 bytes.Buffer
	err = ExportJSON(&buf2, s, ExportOptions{RedactSecrets: false, PrettyPrintJSON: false})
	require.NoError(t, err)
	out2 := buf2.String()

	assert.Contains(t, out2, fakeOpenAIKey())
}

// ---------------------------------------------------------------------------
// Cross-format tests
// ---------------------------------------------------------------------------

type errorWriter struct{}

func (e errorWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("write failed")
}

func TestExportMarkdown_WriteError(t *testing.T) {
	s := sampleSession()
	err := ExportMarkdown(errorWriter{}, s, ExportOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestExportHTML_WriteError(t *testing.T) {
	s := sampleSession()
	err := ExportHTML(errorWriter{}, s, ExportOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestExportJSON_WriteError(t *testing.T) {
	s := sampleSession()
	err := ExportJSON(errorWriter{}, s, ExportOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestExportHTML_ESCAPESxss(t *testing.T) {
	s := sampleSession()
	s.Messages[0].Content = "<script>alert('xss')</script>"
	s.Messages[1].Content = "<img src=x onerror=alert(1)>"

	var buf bytes.Buffer
	err := ExportHTML(&buf, s, ExportOptions{})
	require.NoError(t, err)
	out := buf.String()

	// Raw HTML tags should NOT appear in output
	assert.NotContains(t, out, "<script>")
	assert.NotContains(t, out, "</script>")
	assert.NotContains(t, out, "<img src=x")

	// Should be HTML-escaped instead
	assert.Contains(t, out, "&lt;script&gt;")
	assert.Contains(t, out, "&lt;/script&gt;")
	assert.Contains(t, out, "&lt;img src=x")
}
