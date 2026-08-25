//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func makeTestSession1() agent.ConversationState {
	now := time.Now()
	return agent.ConversationState{
		SessionID:        "test-export-1",
		Name:             "Test Export Session One",
		WorkingDirectory: "/home/testuser/project",
		TotalCost:        0.05,
		PromptTokens:     100,
		CompletionTokens: 200,
		LastUpdated:      now,
		Messages: []api.Message{
			{Role: "user", Content: "Help me with the embedding index"},
			{Role: "assistant", Content: "I can help with the embedding index issue"},
		},
	}
}

func makeTestSession2() agent.ConversationState {
	now := time.Now().Add(-time.Hour) // older than session 1
	return agent.ConversationState{
		SessionID:        "test-export-2",
		Name:             "Test Export Session Two",
		WorkingDirectory: "/home/testuser/other-project",
		TotalCost:        0.03,
		PromptTokens:     50,
		CompletionTokens: 80,
		LastUpdated:      now,
		Messages: []api.Message{
			{Role: "user", Content: "Debug this auth error"},
			{Role: "assistant", Content: "checking the auth error in the config"},
		},
	}
}

func makeTestSessionWithToolCalls() agent.ConversationState {
	now := time.Now()
	return agent.ConversationState{
		SessionID:        "test-tool-calls",
		Name:             "Session With Tool Calls",
		WorkingDirectory: "/home/testuser/project",
		TotalCost:        0.10,
		PromptTokens:     150,
		CompletionTokens: 300,
		LastUpdated:      now,
		Messages: []api.Message{
			{Role: "user", Content: "Read the file config.yaml"},
			{
				Role:    "assistant",
				Content: "Let me read that file for you.",
				ToolCalls: []api.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: api.ToolCallFunction{
							Name:      "read_file",
							Arguments: `{"path": "config.yaml"}`,
						},
					},
				},
			},
			{Role: "tool", Content: "key: value\nfoo: bar", ToolCallID: "call_1"},
		},
	}
}

// ---------------------------------------------------------------------------
// Test setup
// ---------------------------------------------------------------------------

func setupExportStateDir(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Write test sessions into the scoped directory structure
	for _, session := range []agent.ConversationState{makeTestSession1(), makeTestSession2()} {
		_, err := WriteTestSession(tmpDir, session.SessionID, session.WorkingDirectory, session)
		require.NoError(t, err)
	}

	// Override GetStateDir so agent.LoadStateWithoutAgent and
	// agent.ListAllSessionsWithTimestamps find our temp directory
	restore := agent.SetGetStateDirForTest(tmpDir)
	t.Cleanup(func() { restore() })

	return tmpDir
}

func setupExportStateDirWithToolCalls(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	_, err := WriteTestSession(tmpDir, "test-tool-calls", "/home/testuser/project", makeTestSessionWithToolCalls())
	require.NoError(t, err)

	restore := agent.SetGetStateDirForTest(tmpDir)
	t.Cleanup(func() { restore() })

	return tmpDir
}

// ---------------------------------------------------------------------------
// makeExportCmd creates a standalone cobra command with the same flags as
// exportCmd but WITHOUT being a child of rootCmd (which would trigger the
// web-server startup in PersistentPreRunE).  This lets us call Execute()
// safely in tests.
// ---------------------------------------------------------------------------

func makeExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "export [session-id]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd, args)
		},
	}
	cmd.Flags().String("format", "markdown", "Output format: markdown, html, or json")
	cmd.Flags().String("output", "", "Write to file instead of stdout (default: stdout)")
	cmd.Flags().Bool("latest", false, "Export the most-recently-updated session")
	cmd.Flags().Bool("all", false, "Export all saved sessions (concatenated)")
	cmd.Flags().Bool("include-tool-calls", false, "Include tool call details in the output")
	cmd.Flags().Bool("no-cost", false, "Omit cost/tokens in the output")
	cmd.Flags().Bool("no-secret-redaction", false, "Disable secret redaction in exported content")
	cmd.Flags().Bool("no-pretty-json", false, "Do not pretty-print JSON output (json format only)")
	return cmd
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// Test 1: Export a known session to stdout, verify it contains session-id and heading
func TestExportCmd_MarkdownDefault(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "test-export-1", "output should contain the session ID")
	assert.Contains(t, output, "Test Export Session One", "output should contain the session name")
	assert.Contains(t, output, "embedding index", "output should contain message content")
}

// Test 2: --format html produces <html> and <style> in the output
func TestExportCmd_HTML(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--format", "html"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "<html", "output should contain HTML tag")
	assert.Contains(t, output, "<style>", "output should contain embedded CSS")
	assert.Contains(t, output, "Test Export Session One", "output should contain the session name")
}

// Test 3: --format json produces parseable JSON with expected top-level keys
func TestExportCmd_JSON(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--format", "json"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Verify it's valid JSON with expected keys
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Contains(t, result, "session_id", "JSON should have session_id")
	assert.Contains(t, result, "name", "JSON should have name")
	assert.Contains(t, result, "messages", "JSON should have messages")
	assert.Contains(t, result, "total_cost", "JSON should have total_cost")
	assert.Equal(t, "test-export-1", result["session_id"])
}

// Test 4: --output PATH writes the file
func TestExportCmd_OutputFile(t *testing.T) {
	setupExportStateDir(t)

	outFile := filepath.Join(t.TempDir(), "exported.md")
	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--output", outFile})
	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "test-export-1")
	assert.Contains(t, content, "Test Export Session One")
}

// Test 5: --all runs without error against a temp dir containing 2 fake sessions
func TestExportCmd_All(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"--all"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Should contain both sessions (they are concatenated)
	assert.Contains(t, output, "Test Export Session One", "should contain session 1")
	assert.Contains(t, output, "Test Export Session Two", "should contain session 2")
}

// Test 6: --latest picks the newest
func TestExportCmd_Latest(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"--latest"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Session 1 is newer (time.Now() vs time.Now() - 1h)
	assert.Contains(t, output, "Test Export Session One", "latest should be session 1")
	assert.NotContains(t, output, "Test Export Session Two", "latest should NOT be session 2")
}

// Test 7: --no-cost and --include-tool-calls both apply
func TestExportCmd_NoCost_And_IncludeToolCalls(t *testing.T) {
	setupExportStateDirWithToolCalls(t)

	// First, WITH tool calls and cost (to verify presence)
	cmd1 := makeExportCmd()
	cmd1.SetArgs([]string{"test-tool-calls", "--format", "markdown", "--include-tool-calls"})
	outputWith := testutil.CaptureStdout(t, func() {
		_ = cmd1.Execute()
	})
	assert.Contains(t, outputWith, "read_file", "with --include-tool-calls should show tool name")
	// Cost is included by default in markdown (turn footers)
	assert.Contains(t, outputWith, "Cost:", "default should include cost footer")

	// Now with --no-cost
	cmd2 := makeExportCmd()
	cmd2.SetArgs([]string{"test-tool-calls", "--format", "markdown", "--include-tool-calls", "--no-cost"})
	outputNoCost := testutil.CaptureStdout(t, func() {
		_ = cmd2.Execute()
	})
	assert.Contains(t, outputNoCost, "read_file", "tool calls still present with --no-cost")
	assert.NotContains(t, outputNoCost, "Cost:", "--no-cost should suppress cost footer")
}

// Test 8: --format docx returns non-zero exit and clear error
func TestExportCmd_InvalidFormat(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--format", "docx"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid format")
	assert.Contains(t, err.Error(), "docx")
}

// Test 9: Non-existent session returns non-zero exit and clear error
func TestExportCmd_MissingSession(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"non-existent-session-id"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Test 10: --help lists all flags
func TestExportCmd_Help(t *testing.T) {
	cmd := makeExportCmd()
	cmd.SetArgs([]string{"--help"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Help should list our key flags
	assert.Contains(t, output, "--format", "help should list --format flag")
	assert.Contains(t, output, "--output", "help should list --output flag")
	assert.Contains(t, output, "--latest", "help should list --latest flag")
	assert.Contains(t, output, "--all", "help should list --all flag")
	assert.Contains(t, output, "--include-tool-calls", "help should list --include-tool-calls flag")
	assert.Contains(t, output, "--no-cost", "help should list --no-cost flag")
	assert.Contains(t, output, "--no-secret-redaction", "help should list --no-secret-redaction flag")
	assert.Contains(t, output, "--no-pretty-json", "help should list --no-pretty-json flag")
}

// ---------------------------------------------------------------------------
// Additional edge case tests
// ---------------------------------------------------------------------------

func TestExportCmd_NoArgs(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session-id")
}

func TestExportCmd_SessionID_With_Latest(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "either")
}

func TestExportCmd_Latest_And_All(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"--latest", "--all"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestExportCmd_All_JSON(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"--all", "--format", "json"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Should be a valid JSON array
	var results []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	assert.Len(t, results, 2, "should have 2 sessions in the array")
}

func TestExportCmd_All_Markdown_Separators(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"--all", "--format", "markdown"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Multiple sessions should be separated
	assert.Contains(t, output, "---", "markdown mode should have separators between sessions")
}

func TestExportCmd_NoPrettyJSON(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--format", "json", "--no-pretty-json"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Compact JSON should be on a single line (no indentation)
	lines := bytes.Split([]byte(output), []byte("\n"))
	// The JSON content itself is one line, followed by a trailing newline
	assert.LessOrEqual(t, len(lines), 3, "compact JSON should be a single line")
	// Verify it's still valid JSON
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, "test-export-1", result["session_id"])
}

func TestExportCmd_NoSecretRedaction(t *testing.T) {
	setupExportStateDir(t)

	cmd := makeExportCmd()
	cmd.SetArgs([]string{"test-export-1", "--format", "markdown", "--no-secret-redaction"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestExportCmd_Registered(t *testing.T) {
	// Verify exportCmd is a child of rootCmd
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "export [session-id]" {
			found = true
			break
		}
	}
	assert.True(t, found, "exportCmd should be registered under rootCmd")
}
