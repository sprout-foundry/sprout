//go:build !js

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAgentsMdTrailers writes an AGENTS.md with a "Commit Trailers" section
// into dir, used to pin the repo-level default resolution.
func writeAgentsMdTrailers(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0o644))
}

// TestCommitHandler_AppendsExplicitTrailers verifies the trailers parameter
// is committed with git interpret-trailers semantics: a blank line before
// the trailer block and no duplicate.
func TestCommitHandler_AppendsExplicitTrailers(t *testing.T) {
	dir := newTestGitRepo(t, true)

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
	}, map[string]any{
		"message":  "fix: login timeout",
		"trailers": []any{"Assisted-by: Claude Code (anthropic/claude-opus-5-5)", "Signed-off-by: Dev <dev@example.com>"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t,
		"fix: login timeout\n\nAssisted-by: Claude Code (anthropic/claude-opus-5-5)\nSigned-off-by: Dev <dev@example.com>",
		gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_TrailersDeduplicateAgainstMessage verifies a trailer
// already present in the explicit message is not duplicated. Comparison
// follows git: the key is case-insensitive, the value compared after
// trimming (case-sensitive, as git does).
func TestCommitHandler_TrailersDeduplicateAgainstMessage(t *testing.T) {
	dir := newTestGitRepo(t, true)

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
	}, map[string]any{
		"message":  "fix: login timeout\n\nSigned-off-by: Dev <dev@example.com>",
		"trailers": []any{"signed-off-by: Dev <dev@example.com>", "Assisted-by: Sprout (ollama/llama3:8b)"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t,
		"fix: login timeout\n\nSigned-off-by: Dev <dev@example.com>\nAssisted-by: Sprout (ollama/llama3:8b)",
		gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_TrailersFromAgentsMd verifies the repo's AGENTS.md
// "Commit Trailers" section contributes defaults to an explicit commit.
func TestCommitHandler_TrailersFromAgentsMd(t *testing.T) {
	dir := newTestGitRepo(t, true)
	writeAgentsMdTrailers(t, dir, "# Agent notes\n\n## Commit Trailers\nTrailer lines to append:\n- Assisted-by: Sprout (ollama/llama3:8b)\n\n## Other section\nstuff\n")

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
	}, map[string]any{"message": "fix: login timeout"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t,
		"fix: login timeout\n\nAssisted-by: Sprout (ollama/llama3:8b)",
		gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_TrailersFromConfig verifies the commit.trailers config
// (the .sprout config list) contributes defaults to an explicit commit.
func TestCommitHandler_TrailersFromConfig(t *testing.T) {
	dir := newTestGitRepo(t, true)

	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	require.NoError(t, mgr.UpdateConfig(func(c *configuration.Config) error {
		c.CommitTrailers = []string{"Assisted-by: Sprout (openai/gpt-5)"}
		return nil
	}))

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ConfigManager: mgr,
	}, map[string]any{"message": "fix: login timeout"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t,
		"fix: login timeout\n\nAssisted-by: Sprout (openai/gpt-5)",
		gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_TrailersGeneratedMessage verifies trailers are appended
// to generated (message-omitted) commits as well.
func TestCommitHandler_TrailersGeneratedMessage(t *testing.T) {
	dir := newTestGitRepo(t, true)
	const generated = "feat(commit): generated subject\n\nGenerated body."

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{GenerateCommitMessage: func([]byte, string) (string, error) { return generated, nil }},
	}, map[string]any{
		"notes":    "context notes",
		"trailers": []any{"Assisted-by: Sprout (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t,
		generated+"\n\nAssisted-by: Sprout (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)",
		gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_TrailersBadListType verifies a trailers value that is
// not a list of strings is rejected before anything is committed.
func TestCommitHandler_TrailersBadListType(t *testing.T) {
	dir := newTestGitRepo(t, true)
	headBefore := gitInRepo(t, dir, "rev-parse", "HEAD")

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
	}, map[string]any{"message": "fix: x", "trailers": []any{"ok: v", 42}})
	require.NoError(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "trailers")
	assert.Equal(t, headBefore, gitInRepo(t, dir, "rev-parse", "HEAD"), "HEAD must not move")
}

// TestCommitHandler_TrailersRepoDir verifies trailers are resolved from the
// repo_dir worktree, not just the workspace root.
func TestCommitHandler_TrailersRepoDir(t *testing.T) {
	root := newTestGitRepo(t, false)
	worktreeDir := filepath.Join(root, "wt")
	// A real worktree of the workspace repo (shared .git), so
	// ValidateRepoDir admits it.
	gitInRepo(t, root, "worktree", "add", "-b", "feat", worktreeDir)
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "a.txt"), []byte("a\nb\n"), 0o644))
	gitInRepo(t, worktreeDir, "add", "a.txt")
	writeAgentsMdTrailers(t, worktreeDir, "## Commit Trailers\n- Assisted-by: Sprout (ollama/qwen3:8b)\n")

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: root,
	}, map[string]any{"message": "fix: x", "repo_dir": worktreeDir})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t,
		"fix: x\n\nAssisted-by: Sprout (ollama/qwen3:8b)",
		gitInRepo(t, worktreeDir, "log", "-1", "--format=%B"))
}
