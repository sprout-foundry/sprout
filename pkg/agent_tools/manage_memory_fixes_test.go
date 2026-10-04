package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// withMemoryTestDir points the memory directory at a temp dir for the test.
func withMemoryTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", dir)
	return filepath.Join(dir, "memories")
}

func TestResolveMemoryName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "git-safety", want: "git-safety"},
		{in: "Git Safety", want: "git-safety"},
		{in: "  docker-setup  ", want: "docker-setup"},
		{in: "commit-tool.md", want: "commit-tool"},
		{in: "commit-tool.MD", want: "commit-tool"},
		{in: "my_notes", want: "my_notes"},
		{in: "untitled", want: "untitled"},
		{in: "Untitled.md", want: "untitled"},
		{in: "café", want: "caf"},
		{in: "-x-", want: "x"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: ".md", wantErr: true},
		{in: "!!!", wantErr: true},
		{in: "___", wantErr: true},
		{in: "中文", wantErr: true},
	}

	for _, tc := range cases {
		got, err := resolveMemoryName(tc.in)
		if tc.wantErr {
			require.Error(t, err, "resolveMemoryName(%q) should fail", tc.in)
			continue
		}
		require.NoError(t, err, "resolveMemoryName(%q) should pass", tc.in)
		require.Equal(t, tc.want, got)
	}
}

func TestManageMemory_DeleteEmptyNameRejected(t *testing.T) {
	memDir := withMemoryTestDir(t)
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "untitled.md"), []byte("precious notes"), 0o600))

	h := &manageMemoryHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "delete",
		"name":      "   ",
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "delete with a blank name must be an error, not a silent delete")

	// The existing memory must be untouched.
	data, readErr := os.ReadFile(filepath.Join(memDir, "untitled.md"))
	require.NoError(t, readErr)
	require.Equal(t, "precious notes", string(data))
}

func TestManageMemory_ReadEmptyNameRejected(t *testing.T) {
	withMemoryTestDir(t)

	h := &manageMemoryHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "read",
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "empty")
}

func TestManageMemory_AddEmptyNameRejected(t *testing.T) {
	memDir := withMemoryTestDir(t)
	require.NoError(t, os.MkdirAll(memDir, 0o755))

	h := &manageMemoryHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "add",
		"name":      "!!!",
		"content":   "should not be written",
	})
	require.NoError(t, err)
	require.True(t, res.IsError)

	entries, listErr := os.ReadDir(memDir)
	require.NoError(t, listErr)
	require.Empty(t, entries, "no memory file should be created for an empty name")
}

func TestManageMemory_DeleteAcceptsMdSuffix(t *testing.T) {
	memDir := withMemoryTestDir(t)
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "git-safety.md"), []byte("rules"), 0o600))

	h := &manageMemoryHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "delete",
		"name":      "git-safety.md",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "deleted")

	_, statErr := os.Stat(filepath.Join(memDir, "git-safety.md"))
	require.True(t, os.IsNotExist(statErr))
}

func TestManageMemory_AddOverwriteNotice(t *testing.T) {
	memDir := withMemoryTestDir(t)
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "docker-setup.md"), []byte("old"), 0o600))

	h := &manageMemoryHandler{}
	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "add",
		"name":      "docker-setup",
		"content":   "new content",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "overwritten", "overwriting an existing memory must be surfaced to the caller")

	data, readErr := os.ReadFile(filepath.Join(memDir, "docker-setup.md"))
	require.NoError(t, readErr)
	require.Equal(t, "new content", string(data))

	// A fresh memory must NOT carry the overwrite notice.
	res2, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "add",
		"name":      "fresh-memory",
		"content":   "first save",
	})
	require.NoError(t, err)
	require.False(t, res2.IsError)
	require.NotContains(t, res2.Output, "overwritten")
}

func TestScoreMemoryMatch_ProseQueriesReachDefaultThreshold(t *testing.T) {
	t.Parallel()

	// The case the old normalization collapsed below any threshold and the
	// primary real-world shape: memory titles rarely contain the query's
	// words, so every word matches only in the content body. Content weight
	// (0.8) must stay above the default threshold (0.75).
	name := "docker-setup"
	preview := "Multi-stage builds, always." // none of the query words
	content := strings.Repeat("use multi stage builds for production containers ", 40)
	prose := []string{"multi", "stage", "builds", "production", "containers"}
	score := scoreMemoryMatch(name, preview, content, prose)
	require.GreaterOrEqual(t, score, 0.75,
		"content-only prose query must clear the default threshold, got %.2f", score)
	// "stage"/"builds" also hit the preview (0.9 each); the rest are
	// content-only (0.8) → (3×0.9 + 2×0.8) / 5 = 0.86.
	require.InDelta(t, 0.86, score, 0.001)

	// Pure content-only: no name or preview word overlap at all.
	pure := scoreMemoryMatch("x-y", "unrelated words here", content, prose)
	require.InDelta(t, 0.8, pure, 0.001)

	// A short query with name hits scores highest.
	score = scoreMemoryMatch(name, preview, content, []string{"docker", "setup"})
	require.Greater(t, score, 0.8)
}

func TestSearchMemoriesByText_EndToEnd(t *testing.T) {
	memDir := withMemoryTestDir(t)
	require.NoError(t, os.MkdirAll(memDir, 0o755))

	// Title does NOT contain the query words; the body does. This is the
	// content-only shape that must surface at the default threshold.
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "docker-setup.md"),
		[]byte("# Docker Setup\n\nAlways use multi stage builds for production containers.\n"), 0o600))
	// Unrelated memory must not match.
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "editor-colors.md"),
		[]byte("# Colors\n\nThe theme uses a solarized palette.\n"), 0o600))

	results, err := SearchMemoriesByText("multi stage builds production", 5, 0.75)
	require.NoError(t, err)
	require.NotEmpty(t, results, "content-only prose query must find the memory at the default threshold")
	require.Equal(t, "docker-setup", results[0].Name)
	require.GreaterOrEqual(t, results[0].Score, 0.75)

	// Unrelated query returns nothing.
	results, err = SearchMemoriesByText("solarized palette colors", 5, 0.75)
	require.NoError(t, err)
	for _, r := range results {
		require.NotEqual(t, "docker-setup", r.Name)
	}

	// Non-.md files are ignored.
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "notes.txt"), []byte("multi stage builds"), 0o600))
	results, err = SearchMemoriesByText("multi stage builds production", 5, 0.75)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// topK caps the result count.
	for i := 0; i < 4; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(memDir, fmt.Sprintf("build-notes-%d.md", i)),
			[]byte(fmt.Sprintf("# Notes %d\n\nmore multi stage builds production here\n", i)), 0o600))
	}
	results, err = SearchMemoriesByText("multi stage builds production", 2, 0.75)
	require.NoError(t, err)
	require.LessOrEqual(t, len(results), 2)
}

func TestFormatMemorySearchResults_Empty(t *testing.T) {
	t.Parallel()

	out := FormatMemorySearchResults("anything", nil, 0.75)
	require.Contains(t, out, "No memories found")
	require.Contains(t, out, "0.75")
}

func TestFirstLine_SkipsBlankLines(t *testing.T) {
	t.Parallel()

	require.Equal(t, "title", firstLine("\n\n  \ntitle\nbody"))
	require.Equal(t, "", firstLine("\n \n"))
}

func TestScoreMemoryMatch_NoMatchLow(t *testing.T) {
	t.Parallel()

	score := scoreMemoryMatch("docker-setup", "multi stage builds", strings.Repeat("docker content ", 40),
		[]string{"kubernetes", "deployment", "strategy"})
	require.Less(t, score, 0.75)
}

func TestScoreMemoryMatch_ShortWordsExcludedFromDenominator(t *testing.T) {
	t.Parallel()

	// "a" is too short to score and is excluded from both numerator and
	// denominator; "memory" and "tool" both hit the name (1.0 each).
	score := scoreMemoryMatch("memory-tool", "the memory tool", "the memory tool content",
		[]string{"a", "memory", "to", "tool"})
	require.InDelta(t, 1.0, score, 0.001)
}

func TestScoreMemoryMatch_EmptyQuery(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0.0, scoreMemoryMatch("name", "preview", "content", nil))
	require.Equal(t, 0.0, scoreMemoryMatch("name", "preview", "content", []string{"a", "b"}))
}

func TestTruncateRunes_MultibyteSafe(t *testing.T) {
	t.Parallel()

	// Box-drawing and arrow characters are 3 bytes each in UTF-8.
	s := strings.Repeat("─→", 100) // 200 runes, 600 bytes

	got := truncateRunes(s, 117)
	require.Equal(t, 117+3, len([]rune(got)), "117 runes + the 3-rune ellipsis")
	require.True(t, strings.HasSuffix(got, "..."))

	// The result must be valid UTF-8 — no split multi-byte tails.
	require.True(t, utf8.ValidString(got))

	// Strings at or under the limit pass through untouched (no ellipsis).
	short := strings.Repeat("─→", 58) + "─" // 117 runes
	require.Equal(t, short, truncateRunes(short, 117))
	require.Equal(t, short, truncateRunes(short, 200))
}
