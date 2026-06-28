package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
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

// ---------------------------------------------------------------------------
// Markdown round-trip tests
// ---------------------------------------------------------------------------

func TestExportMarkdown_RoundTrip(t *testing.T) {
	s := sampleSession()

	// Export to Markdown with all options enabled so we exercise the full
	// parser — tool calls, cost footers, etc.
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{
		IncludeToolCalls: true,
		IncludeCost:      true,
		RedactSecrets:    false,
	})
	require.NoError(t, err)
	md := buf.String()

	// Parse the Markdown back.
	parsed, err := parseMarkdownSession(md)
	require.NoError(t, err)

	// ---- Front-matter fields ----
	assert.Equal(t, s.ID, parsed.ID, "session ID")
	assert.Equal(t, s.Name, parsed.Name, "session name")
	assert.Equal(t, s.WorkingDirectory, parsed.WorkingDirectory, "working directory")
	assert.Equal(t, s.LastUpdated, parsed.LastUpdated, "last updated")
	assert.Equal(t, s.TotalCost, parsed.TotalCost, "total cost")

	// total_tokens in front-matter = InputTokens + OutputTokens (combined).
	expectedTotalTokens := s.InputTokens + s.OutputTokens
	assert.Equal(t, expectedTotalTokens, parsed.totalTokens, "total tokens")

	// turns count
	assert.Equal(t, 2, parsed.turns, "turn count")

	// tools_used
	assert.Equal(t, []string{"shell_command"}, parsed.toolsUsed, "tools used")

	// NOTE: StartedAt, Provider, Model, InputTokens, OutputTokens are NOT
	// written to front-matter by writeFrontMatter — they are lost in the
	// round-trip.  This is a known limitation of the Markdown format.
	t.Logf("Known gaps: StartedAt, Provider, Model, InputTokens, OutputTokens are not in front-matter")

	// ---- Messages ----
	// sampleSession has 5 messages across 2 turns:
	//   Turn 1: user, assistant (with tool call), tool result
	//   Turn 2: user, assistant
	assert.Equal(t, len(s.Messages), len(parsed.Messages), "message count")

	for i, orig := range s.Messages {
		p := parsed.Messages[i]

		assert.Equal(t, orig.Role, p.Role, "message[%d].Role", i)
		assert.Equal(t, orig.Content, p.Content, "message[%d].Content", i)

		// NOTE: Timestamp, Cost, and Tokens are NOT written per-message by
		// writeMessage. Timestamps only appear in turn headers (## Turn N —
		// <RFC3339>), and Cost/Tokens only appear in the turn footer as
		// aggregates (*Cost: $X.XXXX | Tokens: N*). These are known gaps.
		t.Logf("message[%d]: Timestamp, Cost, Tokens not written per-message by renderer", i)

		// Tool calls — Name and Arguments round-trip.
		// NOTE: ToolCall.Result is NOT written to Markdown by writeToolCall,
		// so the parsed Result will be empty. We only assert Name + Arguments.
		assert.Equal(t, len(orig.ToolCalls), len(p.ToolCalls), "message[%d].ToolCalls count", i)
		for j, tc := range orig.ToolCalls {
			assert.Equal(t, tc.Name, p.ToolCalls[j].Name, "message[%d].ToolCalls[%d].Name", i, j)
			assert.Equal(t, tc.Arguments, p.ToolCalls[j].Arguments, "message[%d].ToolCalls[%d].Arguments", i, j)
			// Result is not round-trippable — log it instead of asserting.
			if tc.Result != "" {
				t.Logf("message[%d].ToolCalls[%d].Result = %q (not written to MD, parsed as empty)", i, j, tc.Result)
			}
		}

		// Tool result
		if orig.ToolResult != nil {
			require.NotNil(t, p.ToolResult, "message[%d].ToolResult should not be nil", i)
			assert.Equal(t, orig.ToolResult.ToolCallName, p.ToolResult.ToolCallName, "message[%d].ToolResult.ToolCallName", i)
			assert.Equal(t, orig.ToolResult.Content, p.ToolResult.Content, "message[%d].ToolResult.Content", i)
		}
	}
}

func TestExportMarkdown_RoundTrip_NoToolCalls(t *testing.T) {
	s := sampleSession()

	// Export with IncludeToolCalls: false — no <details> blocks should appear.
	var buf bytes.Buffer
	err := ExportMarkdown(&buf, s, ExportOptions{
		IncludeToolCalls: false,
		IncludeCost:      true,
		RedactSecrets:    false,
	})
	require.NoError(t, err)
	md := buf.String()

	// Verify no <details> blocks in the body.
	assert.NotContains(t, md, "<details>", "should have no <details> blocks when IncludeToolCalls is false")
	assert.NotContains(t, md, "Tool call:", "should have no 'Tool call:' text")
	assert.NotContains(t, md, "Tool result:", "should have no 'Tool result:' text")

	// Parse the Markdown back.
	parsed, err := parseMarkdownSession(md)
	require.NoError(t, err)

	// Front-matter still lists tools_used (it's metadata, not call blocks).
	assert.Equal(t, []string{"shell_command"}, parsed.toolsUsed, "tools_used in front-matter is still present")

	// But the parsed messages should have no tool calls or tool results.
	for i, m := range parsed.Messages {
		assert.Empty(t, m.ToolCalls, "message[%d] should have no tool calls", i)
		assert.Nil(t, m.ToolResult, "message[%d] should have no tool result", i)
	}

	// We still get the user and assistant messages (4 of them: 2 user + 2 assistant).
	// The tool message is omitted from the Markdown when IncludeToolCalls is false,
	// so the parser should not see it either.
	assert.Equal(t, 4, len(parsed.Messages), "should have 4 messages (2 user + 2 assistant)")
}

// ---------------------------------------------------------------------------
// Markdown parser helpers (strict to the format produced by mdPrinter)
// ---------------------------------------------------------------------------

// mdParsed holds the round-tripped session data extracted from Markdown.
// It reuses SessionSource for the message list but adds private fields for
// front-matter values that don't map 1:1 to SessionSource fields.
type mdParsed struct {
	SessionSource
	totalTokens int
	turns       int
	toolsUsed   []string
}

// parseMarkdownSession parses a Markdown document produced by ExportMarkdown
// back into a structure that mirrors SessionSource. It is intentionally
// strict to the renderer's output format — this is a round-trip test, not a
// general Markdown parser.
func parseMarkdownSession(md string) (*mdParsed, error) {
	result := &mdParsed{}

	// 1. Extract front-matter (between first --- and second ---).
	fm, body, err := splitFrontMatter(md)
	if err != nil {
		return nil, fmt.Errorf("split front-matter: %w", err)
	}

	// Parse YAML front-matter into a node tree so we can access lists.
	var rawNode yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &rawNode); err != nil {
		return nil, fmt.Errorf("parse front-matter YAML: %w", err)
	}

	// The root node is the mapping itself (yaml.Unmarshal doesn't wrap in a
	// document node when there's no explicit document marker).
	mappingNode := &rawNode
	if rawNode.Kind == yaml.DocumentNode && len(rawNode.Content) > 0 {
		mappingNode = rawNode.Content[0]
	}
	if mappingNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("front-matter is not a YAML mapping (kind=%d)", mappingNode.Kind)
	}

	// Walk the mapping's key-value pairs.
	fmMap := make(map[string]interface{})
	for i := 0; i+1 < len(mappingNode.Content); i += 2 {
		key := mappingNode.Content[i].Value
		val := mappingNode.Content[i+1]
		if val.Kind == yaml.SequenceNode {
			// List value (e.g., tools_used).
			strs := make([]string, 0, len(val.Content))
			for _, item := range val.Content {
				strs = append(strs, item.Value)
			}
			fmMap[key] = strs
		} else {
			fmMap[key] = val.Value
		}
	}

	result.ID = fmStr(fmMap, "session_id")
	result.Name = fmStr(fmMap, "name")
	result.WorkingDirectory = fmStr(fmMap, "working_directory")
	result.LastUpdated, _ = time.Parse(time.RFC3339, fmStr(fmMap, "last_updated"))
	result.TotalCost, _ = parseFloat(fmStr(fmMap, "total_cost"))
	result.totalTokens, _ = parseInt(fmStr(fmMap, "total_tokens"))
	result.turns, _ = parseInt(fmStr(fmMap, "turns"))
	result.toolsUsed = fmStrList(fmMap, "tools_used")

	// NOTE: StartedAt, Provider, Model, InputTokens, OutputTokens are NOT
	// written by writeFrontMatter — they cannot be recovered from front-matter.

	// 2. Parse turns from body.
	turns, err := parseTurnsFromBody(body)
	if err != nil {
		return nil, fmt.Errorf("parse turns: %w", err)
	}

	// Flatten turn messages into a single message list.
	for _, turn := range turns {
		result.Messages = append(result.Messages, turn.Messages...)
	}

	return result, nil
}

func fmStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	return v.(string)
}

func fmStrList(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	return v.([]string)
}

func splitFrontMatter(md string) (fm, body string, err error) {
	first := strings.Index(md, "---")
	if first < 0 {
		return "", "", fmt.Errorf("no front-matter start delimiter")
	}
	rest := md[first+3:]

	second := strings.Index(rest, "\n---")
	if second < 0 {
		return "", "", fmt.Errorf("no front-matter end delimiter")
	}
	fm = rest[:second]
	body = rest[second+4:] // skip "\n---"
	return fm, body, nil
}

type turnData struct {
	Num       int
	Timestamp time.Time
	Messages  []MessageSource
	body      string // accumulated raw body lines (internal)
}

func parseTurnsFromBody(body string) ([]turnData, error) {
	// Match "## Turn N — <timestamp>" headers.
	re := regexp.MustCompile(`## Turn (\d+) — (.+)$`)
	lineRe := regexp.MustCompile(`\n`)

	lines := lineRe.Split(body, -1)
	var turns []turnData

	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) == 3 {
			turnNum, _ := parseInt(matches[1])
			ts, _ := time.Parse(time.RFC3339, matches[2])
			turns = append(turns, turnData{Num: turnNum, Timestamp: ts})
			continue
		}
		if len(turns) > 0 {
			// Accumulate lines into the last turn's body.
			turns[len(turns)-1].body = turns[len(turns)-1].body + "\n" + line
		}
	}

	// Parse messages from each turn body.
	for i := range turns {
		msgs, err := parseMessagesFromTurnBody(turns[i].body)
		if err != nil {
			return nil, fmt.Errorf("parse turn %d messages: %w", turns[i].Num, err)
		}
		turns[i].Messages = msgs
		turns[i].body = ""
	}

	return turns, nil
}

// parseMessagesFromTurnBody walks a turn's raw body and extracts messages
// in order: user, assistant (+ tool calls), tool results.
func parseMessagesFromTurnBody(body string) ([]MessageSource, error) {
	var messages []MessageSource
	pos := 0

	for pos < len(body) {
		rest := body[pos:]

		// Skip leading blank lines.
		trimmed := strings.TrimLeft(rest, "\n")
		pos += len(rest) - len(trimmed)
		rest = trimmed

		if len(rest) == 0 {
			break
		}

		// Skip turn separator lines (standalone "---" between turns).
		if strings.HasPrefix(rest, "---\n") || rest == "---" {
			pos += 3
			continue
		}

		// Skip cost footer: "*Cost: $X.XXXX | Tokens: N*"
		if strings.HasPrefix(rest, "*Cost: $") {
			endLine := strings.Index(rest, "\n")
			if endLine < 0 {
				break
			}
			pos += endLine + 1
			continue
		}

		// **User:** message.
		if strings.HasPrefix(rest, "**User:**") {
			msg, consumed := parseUserMessage(rest)
			messages = append(messages, msg)
			pos += consumed
			continue
		}

		// **Assistant:** message (with optional tool calls).
		if strings.HasPrefix(rest, "**Assistant:**") {
			msg, consumed := parseAssistantMessage(rest)
			messages = append(messages, msg)
			pos += consumed
			continue
		}

		// **System:** message.
		if strings.HasPrefix(rest, "**System:**") {
			msg, consumed := parseSystemMessage(rest)
			messages = append(messages, msg)
			pos += consumed
			continue
		}

		// Standalone tool result <details> block.
		if strings.HasPrefix(rest, "<details>") && strings.Contains(rest, "Tool result:") {
			msg, consumed := parseToolResultDetails(rest)
			messages = append(messages, msg)
			pos += consumed
			continue
		}

		// Safety: advance one line to avoid infinite loop on unexpected content.
		endLine := strings.Index(rest, "\n")
		if endLine < 0 {
			break
		}
		pos += endLine + 1
	}

	return messages, nil
}

// parseUserMessage parses "**User:**\n\n> <blockquote content>\n\n".
// Returns the message and the number of characters consumed from rest.
func parseUserMessage(rest string) (MessageSource, int) {
	prefix := "**User:**\n\n"
	if !strings.HasPrefix(rest, prefix) {
		return MessageSource{}, 0
	}
	rest = rest[len(prefix):]

	// Collect blockquoted lines starting with "> ".
	var contentLines []string
	pos := 0
	for pos < len(rest) {
		lineEnd := strings.Index(rest[pos:], "\n")
		if lineEnd < 0 {
			lineEnd = len(rest) - pos
		}
		line := rest[pos : pos+lineEnd]

		if strings.HasPrefix(line, "> ") {
			inner := strings.TrimPrefix(line, "> ")
			inner = unescapeMDQuotes(inner)
			contentLines = append(contentLines, inner)
		} else if strings.TrimSpace(line) == "" {
			break
		} else {
			break
		}
		pos += lineEnd + 1
	}

	content := strings.Join(contentLines, "\n")

	// Skip trailing blank lines after the blockquote.
	after := rest[pos:]
	skipped := strings.IndexFunc(after, func(r rune) bool { return r != '\n' })
	if skipped < 0 {
		skipped = len(after)
	}
	consumed := len(prefix) + pos + skipped

	return MessageSource{
		Role:    "user",
		Content: content,
	}, consumed
}

// parseAssistantMessage parses "**Assistant:**\n\n<content>\n\n[<details> tool calls]".
func parseAssistantMessage(rest string) (MessageSource, int) {
	prefix := "**Assistant:**\n\n"
	if !strings.HasPrefix(rest, prefix) {
		return MessageSource{}, 0
	}
	rest = rest[len(prefix):]

	// Content is everything until <details>, **User:**, *Cost:, or end.
	pos := 0
	for pos < len(rest) {
		remaining := rest[pos:]
		if strings.HasPrefix(remaining, "<details>") {
			break
		}
		if strings.HasPrefix(remaining, "**User:**") ||
			strings.HasPrefix(remaining, "**Assistant:**") ||
			strings.HasPrefix(remaining, "**System:**") {
			break
		}
		if strings.HasPrefix(remaining, "*Cost:") {
			break
		}

		lineEnd := strings.Index(rest[pos:], "\n")
		if lineEnd < 0 {
			lineEnd = len(rest) - pos
		}
		pos += lineEnd + 1
	}

	content := strings.TrimRight(rest[:pos], "\n")

	// Parse tool call <details> blocks that follow.
	consumed := len(prefix) + pos
	var toolCalls []ToolCallSource

	toolRest := rest[pos:]
	for {
		trimmed := strings.TrimLeft(toolRest, "\n")
		consumed += len(toolRest) - len(trimmed)
		toolRest = trimmed

		if !strings.HasPrefix(toolRest, "<details>") {
			break
		}

		if strings.Contains(toolRest, "Tool call:") {
			tc, dLen := parseToolCallDetails(toolRest)
			toolCalls = append(toolCalls, tc)
			toolRest = toolRest[dLen:]
			consumed += dLen
		} else {
			// Tool result or unexpected — stop here.
			break
		}
	}

	msg := MessageSource{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
	return msg, consumed
}

// parseToolCallDetails parses a <details> block for a tool call.
// Format: <details>\n<summary>Tool call: <code>NAME</code></summary>\n\n```json\nARGS\n```\n\n</details>\n\n
func parseToolCallDetails(rest string) (ToolCallSource, int) {
	summaryRe := regexp.MustCompile(`<summary>Tool call: <code>([^<]+)</code></summary>`)
	nameMatch := summaryRe.FindStringSubmatch(rest)
	if len(nameMatch) < 2 {
		return ToolCallSource{}, 0
	}
	name := nameMatch[1]

	// Extract arguments from ```json\n...\n```
	jsonStart := strings.Index(rest, "```json")
	if jsonStart < 0 {
		return ToolCallSource{}, 0
	}
	jsonStart += 7 // skip "```json"
	if jsonStart < len(rest) && rest[jsonStart] == '\n' {
		jsonStart++
	}
	jsonEnd := strings.Index(rest[jsonStart:], "```")
	if jsonEnd < 0 {
		return ToolCallSource{}, 0
	}
	args := strings.TrimSpace(rest[jsonStart : jsonStart+jsonEnd])

	// Find </details> to determine consumed length.
	detailsEnd := strings.Index(rest, "</details>")
	if detailsEnd < 0 {
		return ToolCallSource{}, 0
	}
	consumed := detailsEnd + len("</details>")

	// Skip trailing newlines.
	after := rest[consumed:]
	skipped := strings.IndexFunc(after, func(r rune) bool { return r != '\n' })
	if skipped < 0 {
		skipped = len(after)
	}
	consumed += skipped

	return ToolCallSource{
		Name:      name,
		Arguments: args,
	}, consumed
}

// parseToolResultDetails parses a standalone <details> block for a tool result.
// Format: <details>\n<summary>Tool result: <code>NAME</code></summary>\n\n<content>\n\n</details>\n\n
func parseToolResultDetails(rest string) (MessageSource, int) {
	summaryRe := regexp.MustCompile(`<summary>Tool result: <code>([^<]+)</code></summary>`)
	nameMatch := summaryRe.FindStringSubmatch(rest)
	if len(nameMatch) < 2 {
		return MessageSource{}, 0
	}
	name := nameMatch[1]

	// Find content between </summary> and </details>.
	summaryEnd := strings.Index(rest, "</summary>")
	if summaryEnd < 0 {
		return MessageSource{}, 0
	}
	afterSummary := rest[summaryEnd+len("</summary>"):]
	afterSummary = strings.TrimLeft(afterSummary, "\n")

	detailsEnd := strings.Index(afterSummary, "</details>")
	if detailsEnd < 0 {
		return MessageSource{}, 0
	}
	content := strings.TrimSpace(afterSummary[:detailsEnd])

	// Calculate consumed from original rest.
	afterSummaryRaw := rest[summaryEnd+len("</summary>"):]
	afterSummaryTrimmed := strings.TrimLeft(afterSummaryRaw, "\n")
	blankSkip := len(afterSummaryRaw) - len(afterSummaryTrimmed)
	consumed := summaryEnd + len("</summary>") + blankSkip + detailsEnd + len("</details>")

	// Skip trailing newlines.
	after := rest[consumed:]
	skipped := strings.IndexFunc(after, func(r rune) bool { return r != '\n' })
	if skipped < 0 {
		skipped = len(after)
	}
	consumed += skipped

	return MessageSource{
		Role: "tool",
		ToolResult: &ToolResultSource{
			ToolCallName: name,
			Content:      content,
		},
	}, consumed
}

// parseSystemMessage parses "**System:**\n\n<content>\n\n".
func parseSystemMessage(rest string) (MessageSource, int) {
	prefix := "**System:**\n\n"
	if !strings.HasPrefix(rest, prefix) {
		return MessageSource{}, 0
	}
	rest = rest[len(prefix):]

	pos := 0
	for pos < len(rest) {
		if strings.HasPrefix(rest[pos:], "\n\n") {
			break
		}
		pos++
	}
	content := strings.TrimSpace(rest[:pos])
	consumed := len(prefix) + pos
	// Skip trailing newlines.
	after := rest[consumed:]
	skipped := strings.IndexFunc(after, func(r rune) bool { return r != '\n' })
	if skipped < 0 {
		skipped = len(after)
	}
	consumed += skipped

	return MessageSource{
		Role:    "system",
		Content: content,
	}, consumed
}

// unescapeMDQuotes reverses escapeMDQuotes: strips one leading space from
// each non-empty line that starts with a space.
func unescapeMDQuotes(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, " ") {
			lines[i] = line[1:]
		}
	}
	return strings.Join(lines, "\n")
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
