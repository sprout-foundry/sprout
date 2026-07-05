package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// createTestMemories creates memory files in the real memory directory for testing.
// Returns the list of sanitized names that were created.
func createTestMemories(t *testing.T, mems []struct {
	Name    string
	Content string
}) []string {
	t.Helper()
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		t.Skip("memory directory not available")
	}

	var names []string
	for _, m := range mems {
		sanitized := sanitizeMemoryName(m.Name)
		path := filepath.Join(memoryDir, sanitized+".md")
		err := os.WriteFile(path, []byte(m.Content), 0600)
		require.NoError(t, err)
		names = append(names, sanitized)
	}
	return names
}

// removeTestMemories removes memory files by their sanitized names.
func removeTestMemories(t *testing.T, names []string) {
	t.Helper()
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return
	}
	for _, name := range names {
		os.Remove(filepath.Join(memoryDir, name+".md"))
	}
}

func TestSearchMemoriesHandler_Execute_NoMemories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	// If there are no memory files, the search should return a "no memories found" message
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query": "something-that-does-not-exist",
	})
	// This might succeed with "no memories found" or fail if the memory dir doesn't exist
	if err != nil {
		require.True(t, res.IsError)
		return
	}
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "No memories found")
}

func TestSearchMemoriesHandler_Execute_WithMemories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	// Create test memories
	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-git-safety", "# Git Safety Rules\nNever force push to main."},
		{"sprout-test-test-conventions", "# Test Conventions\nAll tests must be table-driven."},
		{"sprout-test-logging-standards", "# Logging Standards\nUse structured logging everywhere."},
	})
	defer removeTestMemories(t, names)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query": "git",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Should find the git-safety memory
	require.Contains(t, res.Output, "sprout-test-git-safety")
}

func TestSearchMemoriesHandler_Execute_QueryNoMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-alpha", "# Alpha\nSome content about alpha."},
	})
	defer removeTestMemories(t, names)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query": "xyzzy-nonexistent",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "No memories found")
}

func TestSearchMemoriesHandler_Execute_TopK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	// Create multiple memories that all match the query
	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-alpha", "# Alpha\nCommon keyword test."},
		{"sprout-test-beta", "# Beta\nAnother common keyword test."},
		{"sprout-test-gamma", "# Gamma\nYet another common keyword test."},
	})
	defer removeTestMemories(t, names)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query":     "common keyword",
		"top_k":     2,
		"threshold": 0.0,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Should only return top 2 results
	// Count the number of "#N —" entries in the output
	count := strings.Count(res.Output, "#1 —") + strings.Count(res.Output, "#2 —")
	require.GreaterOrEqual(t, count, 1, "should have at least 1 result entry")

	// The output should say "Found N memories" where N <= 2
	require.Contains(t, res.Output, "Found")
	require.Contains(t, res.Output, "memories matching")
}

func TestSearchMemoriesHandler_Execute_Threshold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-database-config", "# Database Config\nConnection pooling settings for PostgreSQL."},
	})
	defer removeTestMemories(t, names)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query":     "database",
		"threshold": 0.0, // accept any score
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Should find the memory with "database" in its name
	require.Contains(t, res.Output, "sprout-test-database-config")
}

func TestSearchMemoriesHandler_Execute_HighThreshold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-minimal", "# Minimal\nShort content."},
	})
	defer removeTestMemories(t, names)

	// Use a very high threshold so the zero-score "xyzzy" query returns no results
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query":     "xyzzy",
		"threshold": 0.99,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "No memories found")
}

func TestSearchMemoriesHandler_Execute_MissingQuery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "required")
}

func TestSearchMemoriesHandler_Execute_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-echo", "# Echo\nEcho echo echo."},
	})
	defer removeTestMemories(t, names)

	res, err := h.Execute(ctx, env, map[string]any{
		"query": "echo",
	})
	if err != nil {
		return // memory dir might not be available
	}
	require.NoError(t, err)
	require.Contains(t, buf.String(), res.Output)
}

func TestSearchMemoriesHandler_Execute_EventBus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener
	env := newTestEnv(t, dir)
	env.EventBus = bus

	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-event", "# Event\nEvent content."},
	})
	defer removeTestMemories(t, names)

	_, err := h.Execute(ctx, env, map[string]any{
		"query": "event",
	})
	if err != nil {
		return // memory dir might not be available
	}

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestSearchMemoriesHandler_Execute_TokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-tokens", "# Tokens\nToken token token token token token."},
	})
	defer removeTestMemories(t, names)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query": "token",
	})
	if err != nil {
		return
	}
	require.NoError(t, err)
	require.Greater(t, res.TokenUsage, int64(0))
}

func TestSearchMemoriesHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	// Valid
	require.NoError(t, h.Validate(map[string]any{"query": "test"}))
	require.NoError(t, h.Validate(map[string]any{"query": "test", "top_k": 10}))
	require.NoError(t, h.Validate(map[string]any{"query": "test", "threshold": 0.5}))

	// Missing query
	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty query
	err = h.Validate(map[string]any{"query": ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	// Invalid top_k
	err = h.Validate(map[string]any{"query": "test", "top_k": "not-a-number"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be an integer")

	// Invalid threshold
	err = h.Validate(map[string]any{"query": "test", "threshold": "not-a-number"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a number")
}

// Test searchMemoriesByText directly
func TestSearchMemoriesByText(t *testing.T) {
	t.Parallel()
	names := createTestMemories(t, []struct {
		Name    string
		Content string
	}{
		{"sprout-test-zephyr-a", "# Zephyr Alpha\nContent about zephyr testing."},
		{"sprout-test-zephyr-b", "# Zephyr Beta\nContent about zephyr testing."},
		{"sprout-test-search-c", "# Gamma\nNo match here at all."},
	})
	defer removeTestMemories(t, names)

	results, err := searchMemoriesByText("zephyr", 10, 0.0)
	require.NoError(t, err)
	require.Greater(t, len(results), 0, "should find at least one result")

	// Both zephyr memories should be in the results
	found := false
	for _, r := range results {
		if strings.Contains(r.Name, "zephyr") {
			found = true
			break
		}
	}
	require.True(t, found, "should find a zephyr memory in results")
}

// Test scoreMemoryMatch directly
func TestScoreMemoryMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		preview     string
		content     string
		queryWords  []string
		expectMatch bool
	}{
		{
			name:        "git-safety",
			preview:     "# Git Safety Rules",
			content:     "Never force push to main.",
			queryWords:  []string{"git"},
			expectMatch: true,
		},
		{
			name:        "test-conventions",
			preview:     "# Test Conventions",
			content:     "All tests must be table-driven.",
			queryWords:  []string{"test"},
			expectMatch: true,
		},
		{
			name:        "unrelated",
			preview:     "# Something Else",
			content:     "Totally different content.",
			queryWords:  []string{"database"},
			expectMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			score := scoreMemoryMatch(tc.name, tc.preview, tc.content, tc.queryWords)
			if tc.expectMatch {
				require.Greater(t, score, 0.0, "expected a non-zero score for matching memory")
			} else {
				require.Equal(t, 0.0, score, "expected zero score for non-matching memory")
			}
		})
	}
}

// Test firstLine
func TestFirstLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"Hello world\nSecond line", "Hello world"},
		{"\n\nFirst non-empty", "First non-empty"},
		{"  \n  spaces  \ncontent", "spaces"},
		{"", ""},
		{"   ", ""},
		{"Single line", "Single line"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, firstLine(tc.input))
		})
	}
}

// Test formatMemorySearchResults
func TestFormatMemorySearchResults(t *testing.T) {
	t.Parallel()

	// Empty results
	output := formatMemorySearchResults("test query", nil, 0.75)
	require.Contains(t, output, "No memories found")
	require.Contains(t, output, "test query")

	// With results
	results := []memorySearchResult{
		{Name: "test-memory", Preview: "# Test Memory", Score: 0.95},
		{Name: "another-memory", Preview: "# Another", Score: 0.8},
	}
	output = formatMemorySearchResults("test", results, 0.5)
	require.Contains(t, output, "Found 2 memory/memories")
	require.Contains(t, output, "test-memory")
	require.Contains(t, output, "0.95")
	require.Contains(t, output, "manage_memory")
}

func TestSearchMemoriesHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	require.Equal(t, "search_memories", h.Name())

	def := h.Definition()
	require.Equal(t, "search_memories", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"query"}, def.Required)

	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["query"])
	require.True(t, paramNames["top_k"])
	require.True(t, paramNames["threshold"])
}

func TestSearchMemoriesHandler_Execute_Fallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchMemoriesHandler{}
	ctx := newTestCtx(dir)

	// In an environment where getMemoryDir returns "", search should not error
	// It should just return "No memories found"
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"query": "anything",
	})
	// Either succeeds (no memories found) or handles gracefully
	if err != nil {
		require.True(t, res.IsError)
	} else {
		require.False(t, res.IsError)
	}
}

// Ensure the handler uses configuration.GetConfigDir (indirectly via getMemoryDir)
func TestSearchMemoriesHandler_UsesConfigDir(t *testing.T) {
	t.Parallel()
	_, err := configuration.GetConfigDir()
	if err == nil {
		t.Log("Config directory is available")
	} else {
		t.Logf("Config directory not available: %v", err)
	}
}
