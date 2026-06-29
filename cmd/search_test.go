//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/search"
	"github.com/sprout-foundry/sprout/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fixtures — session JSON content used by setupCLISearchIndex
// ---------------------------------------------------------------------------

const (
	cliSession1JSON = `{
		"session_id": "cli-embed",
		"name": "CLI Embedding Session",
		"working_directory": "/home/user/project",
		"total_cost": 0.05,
		"messages": [
			{"role": "user", "content": "help me fix the embedding index"},
			{"role": "assistant", "content": "I can help with the embedding index issue"}
		]
	}`

	cliSession2JSON = `{
		"session_id": "cli-auth",
		"name": "CLI Auth Session",
		"working_directory": "/tmp/repro",
		"total_cost": 0.03,
		"messages": [
			{"role": "user", "content": "test this auth error"},
			{"role": "assistant", "content": "checking the auth error in the config"}
		]
	}`

	cliSession3JSON = `{
		"session_id": "cli-misc",
		"name": "CLI Misc Session",
		"working_directory": "/home/user/project",
		"total_cost": 0.01,
		"messages": [
			{"role": "user", "content": "just a general note"},
			{"role": "assistant", "content": "got it"}
		]
	}`
)

// ---------------------------------------------------------------------------
// setupCLISearchIndex isolates the search index into a temp directory for
// CLI tests.  Same idea as setupTestSearchIndex in pkg/agent_commands but
// self-contained here.
// ---------------------------------------------------------------------------

func setupCLISearchIndex(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Override HOME so DefaultIndexPath() resolves into our temp tree.
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create directory structure: ~/.sprout/sessions/scoped/<hash>/
	sessionsDir := filepath.Join(tmpDir, ".sprout", "sessions", "scoped", "hash1")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))

	// Write fixture session files.
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_cli-embed.json"), []byte(cliSession1JSON), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_cli-auth.json"), []byte(cliSession2JSON), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_cli-misc.json"), []byte(cliSession3JSON), 0o644))

	// Pre-build the index so runSearch() can find it.
	idx, err := search.LoadIndex(search.DefaultIndexPath())
	require.NoError(t, err)
	idx, err = search.BuildIndex(sessionsDir, idx)
	require.NoError(t, err)
	require.NoError(t, search.SaveIndex(search.DefaultIndexPath(), idx))

	return tmpDir
}

// ---------------------------------------------------------------------------
// makeSearchCmd creates a standalone cobra command with the same flags as
// searchCmd but WITHOUT being a child of rootCmd (which would trigger the
// web-server startup in PersistentPreRunE).  This lets us call Execute()
// safely in tests.
// ---------------------------------------------------------------------------

func makeSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "search <query>",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, args)
		},
	}
	cmd.Flags().Bool("reindex", false, "Force full index rebuild before searching")
	cmd.Flags().String("cwd", "", "Restrict to sessions in a specific working directory")
	cmd.Flags().String("since", "", "Only sessions with LastUpdated >= date (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("until", "", "Only sessions with LastUpdated <= date")
	cmd.Flags().Int("limit", 0, "Max results (default 20)")
	cmd.Flags().Bool("json", false, "Output as JSON array instead of formatted text")
	return cmd
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSearchCmd_NoArgs(t *testing.T) {
	cmd := makeSearchCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires at least 1 arg")
}

func TestSearchCmd_SearchQuery(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "CLI Embedding Session")
	assert.Contains(t, output, "session")
	assert.Contains(t, output, "matched")
}

func TestSearchCmd_JsonOutput(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--json", "embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Verify the output is valid JSON array
	var results []search.SearchResult
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	assert.NotEmpty(t, results)
	assert.Contains(t, results[0].SessionID, "cli-embed")
	assert.Contains(t, results[0].Name, "CLI Embedding")
}

func TestSearchCmd_CwdFilter(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--cwd", "/tmp/repro", "auth"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Should only match the /tmp/repro session
	assert.Contains(t, output, "CLI Auth Session")
	// Should NOT match the /home/user/project sessions
	assert.NotContains(t, output, "CLI Embedding Session")
	assert.NotContains(t, output, "CLI Misc Session")
}

func TestSearchCmd_Limit(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--limit", "1", "embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Should say "1 session matched" (singular — no "s" after session)
	assert.Contains(t, output, "1 session")
	assert.Contains(t, output, "matched")
}

func TestSearchCmd_Reindex(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--reindex", "embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Rebuild should still find results
	assert.Contains(t, output, "CLI Embedding Session")
}

func TestSearchCmd_DateFilters(t *testing.T) {
	setupCLISearchIndex(t)

	// --since in the past: should match
	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--since", "2020-01-01", "test"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})
	// "test" appears in session 2's messages ("test this auth error")
	assert.Contains(t, output, "CLI Auth Session")

	// --until in the past: should not match (sessions are recent)
	cmd2 := makeSearchCmd()
	cmd2.SetArgs([]string{"--until", "2000-01-01", "test"})
	output = testutil.CaptureStdout(t, func() {
		_ = cmd2.Execute()
	})
	// No matching sessions message from FormatResults
	assert.Contains(t, output, "No matching sessions.")
}

func TestSearchCmd_JsonNoResults(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--json", "xyznonexistentquery"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// Even with no results, should produce valid JSON (empty array)
	var results []search.SearchResult
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	assert.Empty(t, results)
}

func TestSearchCmd_SinceInvalidDate(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--since", "not-a-date", "test"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--since:")
	assert.Contains(t, err.Error(), "invalid date")
}

func TestSearchCmd_UntilInvalidDate(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--until", "not-a-date", "test"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--until:")
	assert.Contains(t, err.Error(), "invalid date")
}

func TestSearchCmd_ExactPhrase(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"embedding index"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "CLI Embedding Session")
}

func TestSearchCmd_MultiTermQuery(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"auth", "config"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "CLI Auth Session")
}

func TestSearchCmd_NoMatch(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"xyznonexistentquery"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "No matching sessions.")
}

// ---------------------------------------------------------------------------
// Tests — parseSearchDate directly
// ---------------------------------------------------------------------------

func TestParseSearchDate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"rfc3339", "2025-06-27T12:00:00Z", "2025-06-27", true},
		{"yyyy-mm-dd", "2025-01-01", "2025-01-01", true},
		{"invalid", "not-a-date", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSearchDate(tt.input)
			if tt.wantOK {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got.Format("2006-01-02"))
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid date")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests — getCLISessionsDir
// ---------------------------------------------------------------------------

func TestGetCLISessionsDir(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	dir := getCLISessionsDir()
	expected := filepath.Join(tmpDir, ".sprout", "sessions", "scoped")
	assert.Equal(t, expected, dir)
}

// ---------------------------------------------------------------------------
// Tests — stdout capture sanity
// ---------------------------------------------------------------------------

func TestCaptureStdout_Correctness(t *testing.T) {
	output := testutil.CaptureStdout(t, func() {
		buf := &bytes.Buffer{}
		buf.WriteString("hello world")
		os.Stdout.WriteString(buf.String())
	})
	assert.Equal(t, "hello world", output)
}

// ---------------------------------------------------------------------------
// Tests — combined flags
// ---------------------------------------------------------------------------

func TestSearchCmd_CombinedFlags(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--cwd", "/home/user/project", "--limit", "1", "--since", "2020-01-01", "help"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// "help" appears in session 1 (/home/user/project)
	assert.Contains(t, output, "CLI Embedding Session")
	assert.Contains(t, output, "1 session")
}

func TestSearchCmd_ReindexEmptyIndex(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create sessions dir with files but DON'T pre-build the index
	sessionsDir := filepath.Join(tmpDir, ".sprout", "sessions", "scoped", "hash1")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_cli-embed.json"), []byte(cliSession1JSON), 0o644))

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--reindex", "embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// --reindex auto-builds then searches
	assert.Contains(t, output, "CLI Embedding Session")
}

func TestSearchCmd_AutoBuildEmptyIndex(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create sessions dir with files but DON'T pre-build the index
	sessionsDir := filepath.Join(tmpDir, ".sprout", "sessions", "scoped", "hash1")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_cli-embed.json"), []byte(cliSession1JSON), 0o644))

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// runSearch auto-builds when index has 0 sessions
	assert.Contains(t, output, "CLI Embedding Session")
}

func TestSearchCmd_RFC3339Date(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"--since", "2020-01-01T00:00:00Z", "test"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	assert.Contains(t, output, "CLI Auth Session")
}

func TestSearchCmd_FormatOutput(t *testing.T) {
	setupCLISearchIndex(t)

	cmd := makeSearchCmd()
	cmd.SetArgs([]string{"embedding"})
	output := testutil.CaptureStdout(t, func() {
		_ = cmd.Execute()
	})

	// The formatted output should include the date in brackets
	assert.Contains(t, output, "]")
	// And the session name
	assert.Contains(t, output, "CLI Embedding Session")
	// And working directory
	assert.Contains(t, output, "/home/user/project")
	// And "matched" at the end
	assert.Contains(t, output, "matched")
}

// ---------------------------------------------------------------------------
// Tests — searchCmd is properly registered under rootCmd
// ---------------------------------------------------------------------------

func TestSearchCmd_Registered(t *testing.T) {
	// Verify searchCmd is a child of rootCmd
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "search <query>" {
			found = true
			break
		}
	}
	assert.True(t, found, "searchCmd should be registered under rootCmd")
}
