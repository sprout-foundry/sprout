package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitInRepo runs a git subcommand inside dir and returns its output,
// failing the test on error.
func gitInRepo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: test-driven git invocations with controlled args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newTestGitRepo initializes a throwaway git repo in a temp dir with a
// configured identity and one commit on master. When staged is true it also
// stages a modification to a.txt so the repo carries a non-empty staged diff.
func newTestGitRepo(t *testing.T, staged bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitInRepo(t, dir, "init", "-q")
	gitInRepo(t, dir, "config", "user.email", "test@example.com")
	gitInRepo(t, dir, "config", "user.name", "Test")
	gitInRepo(t, dir, "config", "core.autocrlf", "false")
	gitInRepo(t, dir, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644))
	gitInRepo(t, dir, "add", "a.txt")
	gitInRepo(t, dir, "commit", "-q", "-m", "initial")
	if staged {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\nb\n"), 0o644))
		gitInRepo(t, dir, "add", "a.txt")
	}
	return dir
}

// setPackageGenerateCommitMessage points the package-level
// GenerateCommitMessageFunc at fn and restores the prior value on cleanup,
// under ToolFuncMu — mirroring setPackageRunSubagent so tests never leak
// package state.
func setPackageGenerateCommitMessage(t *testing.T, fn func(diff []byte, notes string) (string, error)) {
	t.Helper()
	ToolFuncMu.Lock()
	old := GenerateCommitMessageFunc
	GenerateCommitMessageFunc = fn
	ToolFuncMu.Unlock()
	t.Cleanup(func() {
		ToolFuncMu.Lock()
		GenerateCommitMessageFunc = old
		ToolFuncMu.Unlock()
	})
}

// conventionalSubjectPattern is the Conventional Commit title shape the
// sprout commit generator is prompted to produce.
var conventionalSubjectPattern = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|chore|ci)(\([^)]+\))?: .+`)

// TestCommitHandler_GeneratesConventionalSubjectWhenMessageOmitted covers
// the notes-only path: the generator is called once with the staged diff and
// the notes, its Conventional Commit message is committed verbatim, and the
// subject stays conventional and under 72 characters — never the legacy
// "Auto-commit" placeholder, never the notes verbatim.
func TestCommitHandler_GeneratesConventionalSubjectWhenMessageOmitted(t *testing.T) {
	dir := newTestGitRepo(t, true)

	var (
		calls    int
		gotDiff  []byte
		gotNotes string
	)
	const generated = "feat(commit): generate message from staged diff\n\nBody derived from the staged changes."
	gen := func(diff []byte, notes string) (string, error) {
		calls++
		gotDiff = diff
		gotNotes = notes
		return generated, nil
	}

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{GenerateCommitMessage: gen},
	}, map[string]any{"notes": "context notes"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Equal(t, 1, calls, "generator must be called exactly once")
	assert.Equal(t, "context notes", gotNotes)
	require.NotEmpty(t, gotDiff, "generator must receive the staged diff")
	assert.Contains(t, string(gotDiff), "a.txt")
	assert.Contains(t, string(gotDiff), "+b")

	subject := gitInRepo(t, dir, "log", "-1", "--format=%s")
	assert.True(t, conventionalSubjectPattern.MatchString(subject), "subject %q must look conventional", subject)
	assert.LessOrEqual(t, utf8.RuneCountInString(subject), 72, "subject must be at most 72 characters")

	body := gitInRepo(t, dir, "log", "-1", "--format=%B")
	assert.Equal(t, generated, body, "the generated message must be committed verbatim")
	assert.NotEqual(t, "Auto-commit", body)
	assert.NotEqual(t, "context notes", body)
}

// TestCommitHandler_MessageWinsOverNotes verifies that an explicit message
// is committed as-is and the generator is not consulted at all.
func TestCommitHandler_MessageWinsOverNotes(t *testing.T) {
	dir := newTestGitRepo(t, true)

	calls := 0
	gen := func([]byte, string) (string, error) {
		calls++
		return "must not be used", nil
	}

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{GenerateCommitMessage: gen},
	}, map[string]any{"message": "explicit: my message", "notes": "ignored context"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)

	assert.Zero(t, calls, "an explicit message must bypass the generator")
	assert.Equal(t, "explicit: my message", gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_GeneratorFailureCommitsNothing verifies that a generator
// error aborts the commit: the tool reports an error and the repository is
// untouched.
func TestCommitHandler_GeneratorFailureCommitsNothing(t *testing.T) {
	dir := newTestGitRepo(t, true)
	headBefore := gitInRepo(t, dir, "rev-parse", "HEAD")
	countBefore := gitInRepo(t, dir, "rev-list", "--count", "HEAD")

	gen := func([]byte, string) (string, error) {
		return "", errors.New("generation exploded")
	}

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{GenerateCommitMessage: gen},
	}, map[string]any{"notes": "context notes"})
	require.NoError(t, err)
	require.True(t, res.IsError, "expected an error result, got: %s", res.Output)
	assert.Contains(t, res.Output, "nothing was committed")
	assert.Contains(t, res.Output, "generation exploded")

	assert.Equal(t, headBefore, gitInRepo(t, dir, "rev-parse", "HEAD"), "HEAD must not move")
	assert.Equal(t, countBefore, gitInRepo(t, dir, "rev-list", "--count", "HEAD"), "no new commit may appear")
}

// TestCommitHandler_MissingGeneratorCommitsNothing verifies the unwired case
// (no per-agent ToolFuncs set and no package-level fallback): the tool
// refuses to commit anything rather than guessing a message.
func TestCommitHandler_MissingGeneratorCommitsNothing(t *testing.T) {
	dir := newTestGitRepo(t, true)
	headBefore := gitInRepo(t, dir, "rev-parse", "HEAD")

	setPackageGenerateCommitMessage(t, nil) // ensure the fallback var is clear too

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{}, // GenerateCommitMessage is nil
	}, map[string]any{"notes": "context notes"})
	require.NoError(t, err)
	require.True(t, res.IsError, "expected an error result, got: %s", res.Output)
	assert.Contains(t, res.Output, "no commit message generator")

	assert.Equal(t, headBefore, gitInRepo(t, dir, "rev-parse", "HEAD"), "HEAD must not move")
}

// TestCommitHandler_PackageLevelGeneratorFallback verifies the legacy
// single-agent path: a ToolEnv without ToolFuncs resolves the package-level
// GenerateCommitMessageFunc snapshot.
func TestCommitHandler_PackageLevelGeneratorFallback(t *testing.T) {
	dir := newTestGitRepo(t, true)
	const generated = "chore: fallback-generated message\n\nVia the package-level function pointer."

	setPackageGenerateCommitMessage(t, func([]byte, string) (string, error) {
		return generated, nil
	})

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir, // no ToolFuncs → package-var fallback
	}, map[string]any{"notes": "context notes"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, generated, gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_NoStagedChanges verifies the empty-diff guard: with a
// clean working tree the tool reports an error and the generator is never
// called.
func TestCommitHandler_NoStagedChanges(t *testing.T) {
	dir := newTestGitRepo(t, false) // clean tree, no staged changes
	headBefore := gitInRepo(t, dir, "rev-parse", "HEAD")

	calls := 0
	gen := func([]byte, string) (string, error) {
		calls++
		return "must not be used", nil
	}

	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{GenerateCommitMessage: gen},
	}, map[string]any{"notes": "context notes"})
	require.NoError(t, err)
	require.True(t, res.IsError, "expected an error result, got: %s", res.Output)
	assert.Contains(t, res.Output, "No staged changes")
	assert.Zero(t, calls, "the generator must not run without staged changes")
	assert.Equal(t, headBefore, gitInRepo(t, dir, "rev-parse", "HEAD"), "HEAD must not move")
}
