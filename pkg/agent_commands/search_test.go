//go:build !js

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/search"
	"github.com/sprout-foundry/sprout/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fixtures — session JSON content used by setupTestSearchIndex
// ---------------------------------------------------------------------------

const (
	session1JSON = `{
		"session_id": "test-embed",
		"name": "Embedding Index Session",
		"working_directory": "/home/user/project",
		"total_cost": 0.05,
		"messages": [
			{"role": "user", "content": "help me fix the embedding index"},
			{"role": "assistant", "content": "I can help with the embedding index issue"}
		]
	}`

	session2JSON = `{
		"session_id": "test-auth",
		"name": "Auth Error Session",
		"working_directory": "/tmp/repro",
		"total_cost": 0.03,
		"messages": [
			{"role": "user", "content": "test this auth error"},
			{"role": "assistant", "content": "checking the auth error in the config"}
		]
	}`

	session3JSON = `{
		"session_id": "test-misc",
		"name": "Miscellaneous Session",
		"working_directory": "/home/user/project",
		"total_cost": 0.01,
		"messages": [
			{"role": "user", "content": "just a general note"},
			{"role": "assistant", "content": "got it"}
		]
	}`
)

// ---------------------------------------------------------------------------
// setupTestSearchIndex isolates the search index into a temp directory.
//
// It overrides HOME so that search.DefaultIndexPath() points into the temp
// tree, creates the scoped sessions subdirectory, writes the three fixture
// session files, and pre-builds the index on disk.
//
// Returns the temp dir (callers typically only need it for assertions).
func setupTestSearchIndex(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Override HOME so DefaultIndexPath() resolves into our temp tree.
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	// Also restore HOME immediately at cleanup so sibling tests don't collide.
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create directory structure: ~/.sprout/sessions/scoped/<hash>/
	sessionsDir := filepath.Join(tmpDir, ".sprout", "sessions", "scoped", "hash1")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))

	// Write fixture session files.
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_test-embed.json"), []byte(session1JSON), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_test-auth.json"), []byte(session2JSON), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_test-misc.json"), []byte(session3JSON), 0o644))

	// Pre-build the index so runSearch() can find it.
	idx, err := search.LoadIndex(search.DefaultIndexPath())
	require.NoError(t, err)
	idx, err = search.BuildIndex(sessionsDir, idx)
	require.NoError(t, err)
	require.NoError(t, search.SaveIndex(search.DefaultIndexPath(), idx))

	return tmpDir
}

// ---------------------------------------------------------------------------
// Tests — structural (no index needed)
// ---------------------------------------------------------------------------

func TestSearchCommand_Name(t *testing.T) {
	cmd := &SearchCommand{}
	assert.Equal(t, "search", cmd.Name())
}

func TestSearchCommand_Description(t *testing.T) {
	cmd := &SearchCommand{}
	assert.Equal(t, "Search across saved sessions by content", cmd.Description())
}

func TestSearchCommand_Usage(t *testing.T) {
	cmd := &SearchCommand{}
	usage := cmd.Usage()
	assert.Contains(t, usage, "search")
	assert.Contains(t, usage, "--reindex")
	assert.Contains(t, usage, "--json")
}

// ---------------------------------------------------------------------------
// Tests — flag parsing errors (no index needed for flag-only failures)
// ---------------------------------------------------------------------------

func TestSearchCommand_MissingFlagValue_Cwd(t *testing.T) {
	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--cwd"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--cwd requires a value")
}

func TestSearchCommand_MissingFlagValue_Since(t *testing.T) {
	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--since"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--since requires a date value")
}

func TestSearchCommand_MissingFlagValue_Until(t *testing.T) {
	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--until"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--until requires a date value")
}

func TestSearchCommand_MissingFlagValue_Limit(t *testing.T) {
	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--limit"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit requires a numeric value")
}

func TestSearchCommand_InvalidLimitValue(t *testing.T) {
	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--limit", "abc", "test"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid integer")
}

func TestSearchCommand_UnknownFlag(t *testing.T) {
	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--unknown", "test"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

// ---------------------------------------------------------------------------
// Tests — search execution against pre-built index
// ---------------------------------------------------------------------------

func TestSearchCommand_NoQuery(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	err := cmd.Execute([]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage:")
}

func TestSearchCommand_SearchQuery(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"embedding"}, nil)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "session matched")
	assert.Contains(t, output, "Embedding Index Session")
}

func TestSearchCommand_ExactPhrase(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"embedding index"}, nil)
		require.NoError(t, err)
	})

	// "embedding index" is a phrase that appears in session 1
	assert.Contains(t, output, "Embedding Index Session")
}

func TestSearchCommand_MultiWordQuery(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"auth", "config"}, nil)
		require.NoError(t, err)
	})

	// "auth config" should match session 2 (auth error in the config)
	assert.Contains(t, output, "Auth Error Session")
}

func TestSearchCommand_NoMatch(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"xyznonexistentquery"}, nil)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No matching sessions.")
}

func TestSearchCommand_JsonOutput(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		ctx := &CommandContext{OutputFormat: OutputJSON}
		err := cmd.ExecuteWithJSONOutput([]string{"embedding"}, nil, ctx)
		require.NoError(t, err)
	})

	// Verify the output is valid JSON array
	var results []search.SearchResult
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	assert.NotEmpty(t, results)

	// Verify the result has the expected fields
	assert.Contains(t, results[0].SessionID, "test-embed")
	assert.Contains(t, results[0].Name, "Embedding")
	assert.Contains(t, results[0].Excerpt, "embedding")
}

func TestSearchCommand_JsonNoResults(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		ctx := &CommandContext{OutputFormat: OutputJSON}
		err := cmd.ExecuteWithJSONOutput([]string{"xyznonexistentquery"}, nil, ctx)
		require.NoError(t, err)
	})

	// Even with no results, should produce valid JSON (empty array)
	var results []search.SearchResult
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	assert.Empty(t, results)
}

func TestSearchCommand_CwdFilter(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--cwd", "/tmp/repro", "auth"}, nil)
		require.NoError(t, err)
	})

	// Should only match the /tmp/repro session
	assert.Contains(t, output, "Auth Error Session")
	// Should NOT match the /home/user/project sessions
	assert.NotContains(t, output, "Embedding Index Session")
	assert.NotContains(t, output, "Miscellaneous Session")
}

func TestSearchCommand_SinceFilter(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--since", "2020-01-01", "test"}, nil)
		require.NoError(t, err)
	})

	// All sessions were created "now" (temp dir), so they should all pass the since filter
	// "session matched" works for both singular and plural
	assert.Contains(t, output, "session matched")
}

func TestSearchCommand_UntilFilter(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--until", "2000-01-01", "test"}, nil)
		require.NoError(t, err)
	})

	// All sessions are recent, so nothing should match the until=2000 filter
	assert.Contains(t, output, "No matching sessions.")
}

func TestSearchCommand_Limit(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--limit", "1", "embedding"}, nil)
		require.NoError(t, err)
	})

	// Should be limited to 1 result (singular grammar)
	assert.Contains(t, output, "1 session matched")
}

func TestSearchCommand_Reindex(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--reindex", "embedding"}, nil)
		require.NoError(t, err)
	})

	// Rebuild should still find results — "session matched" covers both singular/plural
	assert.Contains(t, output, "session matched")
}

func TestSearchCommand_InvalidDate(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	err := cmd.Execute([]string{"--since", "invalid-date", "test"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid date")
}

func TestSearchCommand_RFC3339Date(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--since", "2020-01-01T00:00:00Z", "test"}, nil)
		require.NoError(t, err)
	})

	// "session matched" covers both singular and plural
	assert.Contains(t, output, "session matched")
}

func TestSearchCommand_CombinedFlags(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--cwd", "/home/user/project", "--limit", "1", "help"}, nil)
		require.NoError(t, err)
	})

	// "help" appears in session 1 ("/home/user/project"), and with --limit 1 we get 1 result (singular)
	assert.Contains(t, output, "1 session matched")
}

// ---------------------------------------------------------------------------
// Test parseSearchFlags directly (table-driven)
// ---------------------------------------------------------------------------

func TestParseSearchFlags(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantQuery    string
		wantReindex  bool
		wantCWD      string
		wantLimit    int
		wantSinceStr string
		wantUntilStr string
		wantErr      string
	}{
		{
			name:      "simple query",
			args:      []string{"hello", "world"},
			wantQuery: "hello world",
		},
		{
			name:        "reindex flag",
			args:        []string{"--reindex", "test"},
			wantQuery:   "test",
			wantReindex: true,
		},
		{
			name:      "cwd flag",
			args:      []string{"--cwd", "/tmp", "test"},
			wantQuery: "test",
			wantCWD:   "/tmp",
		},
		{
			name:      "limit flag",
			args:      []string{"--limit", "5", "test"},
			wantQuery: "test",
			wantLimit: 5,
		},
		{
			name:         "since flag date",
			args:         []string{"--since", "2025-01-01", "test"},
			wantQuery:    "test",
			wantSinceStr: "2025-01-01",
		},
		{
			name:         "until flag date",
			args:         []string{"--until", "2025-06-01", "test"},
			wantQuery:    "test",
			wantUntilStr: "2025-06-01",
		},
		{
			name:        "all flags combined",
			args:        []string{"--reindex", "--cwd", "/tmp", "--limit", "10", "--since", "2025-01-01", "query"},
			wantQuery:   "query",
			wantReindex: true,
			wantCWD:     "/tmp",
			wantLimit:   10,
			wantSinceStr: "2025-01-01",
		},
		{
			name:    "missing cwd value",
			args:    []string{"--cwd"},
			wantErr: "--cwd requires a value",
		},
		{
			name:    "missing since value",
			args:    []string{"--since"},
			wantErr: "--since requires a date value",
		},
		{
			name:    "missing until value",
			args:    []string{"--until"},
			wantErr: "--until requires a date value",
		},
		{
			name:    "missing limit value",
			args:    []string{"--limit"},
			wantErr: "--limit requires a numeric value",
		},
		{
			name:    "invalid limit",
			args:    []string{"--limit", "abc", "test"},
			wantErr: "invalid integer",
		},
		{
			name:    "unknown flag",
			args:    []string{"--bogus", "test"},
			wantErr: "unknown flag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, reindex, query, err := parseSearchFlags(tt.args)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, query)
			assert.Equal(t, tt.wantReindex, reindex)
			assert.Equal(t, tt.wantCWD, opts.WorkingDir)
			assert.Equal(t, tt.wantLimit, opts.Limit)

			if tt.wantSinceStr != "" {
				got := opts.Since.Format("2006-01-02")
				assert.Equal(t, tt.wantSinceStr, got)
			}
			if tt.wantUntilStr != "" {
				got := opts.Until.Format("2006-01-02")
				assert.Equal(t, tt.wantUntilStr, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test parseDate directly
// ---------------------------------------------------------------------------

func TestParseDate(t *testing.T) {
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
			got, err := parseDate(tt.input)
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
// Test getSessionsDir
// ---------------------------------------------------------------------------

func TestGetSessionsDir(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	dir := getSessionsDir()
	expected := filepath.Join(tmpDir, ".sprout", "sessions", "scoped")
	assert.Equal(t, expected, dir)
}

// ---------------------------------------------------------------------------
// Test testutil.CaptureStdout itself (sanity check)
// ---------------------------------------------------------------------------

func TestCaptureStdout_Correctness(t *testing.T) {
	output := testutil.CaptureStdout(t, func() {
		fmt.Fprint(os.Stdout, "hello world")
	})
	assert.Equal(t, "hello world", output)
}

// ---------------------------------------------------------------------------
// Edge case: query with mixed flag ordering
// ---------------------------------------------------------------------------

func TestSearchCommand_QueryBeforeAndAfterFlags(t *testing.T) {
	setupTestSearchIndex(t)

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"auth", "--cwd", "/tmp/repro", "error"}, nil)
		require.NoError(t, err)
	})

	// Query parts should be joined: "auth error"
	assert.Contains(t, output, "Auth Error Session")
}

// ---------------------------------------------------------------------------
// Edge case: reindex on empty index
// ---------------------------------------------------------------------------

func TestSearchCommand_ReindexOnEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create sessions dir with files but DON'T pre-build the index
	sessionsDir := filepath.Join(tmpDir, ".sprout", "sessions", "scoped", "hash1")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_test-embed.json"), []byte(session1JSON), 0o644))

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"--reindex", "embedding"}, nil)
		require.NoError(t, err)
	})

	// Should auto-build index on reindex and find results — "session matched" covers both
	assert.Contains(t, output, "session matched")
	assert.Contains(t, output, "Embedding Index Session")
}

// ---------------------------------------------------------------------------
// Edge case: auto-build when index is empty (no --reindex)
// ---------------------------------------------------------------------------

func TestSearchCommand_AutoBuildEmptyIndex(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create sessions dir with files but DON'T pre-build the index
	sessionsDir := filepath.Join(tmpDir, ".sprout", "sessions", "scoped", "hash1")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "session_test-embed.json"), []byte(session1JSON), 0o644))

	cmd := &SearchCommand{}
	output := testutil.CaptureStdout(t, func() {
		err := cmd.Execute([]string{"embedding"}, nil)
		require.NoError(t, err)
	})

	// runSearch auto-builds when index has 0 sessions — "session matched" covers both
	assert.Contains(t, output, "session matched")
}
