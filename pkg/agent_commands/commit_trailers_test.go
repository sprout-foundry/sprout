package commands

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCommitTrailersRepo initializes a throwaway repo with a staged change,
// chdirs into it, and points gitCommand at it. It returns the repo dir and a
// config manager isolated to a temp dir (so configuration.LoadOrInitConfig,
// which generateAndCommit calls, never reads a real config). Pass
// agentsMd to write an AGENTS.md into the repo root.
func newCommitTrailersRepo(t *testing.T, agentsMd string) (string, *configuration.Manager) {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "initial"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil { //nolint:gosec // G204: test-driven git invocations with controlled args
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "add", "a.txt").CombinedOutput(); err != nil { //nolint:gosec // G204: test-driven git invocations with controlled args
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if agentsMd != "" {
		if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(agentsMd), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	SetGitDir(repo)
	t.Chdir(repo)
	return repo, mgr
}

func commitTrailersRepoMessage(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "log", "-1", "--format=%B").Output() //nolint:gosec // G204: test-driven git invocations with controlled args
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func commitTrailersRepoCount(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").Output() //nolint:gosec // G204: test-driven git invocations with controlled args
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// runCommitTrailerFlow drives generateAndCommit with a piped manual message
// (no LLM client) and returns the captured stdout. It exercises the same
// trailer resolution and interpret-trailers writing the /commit and
// `sprout commit` surfaces use.
func runCommitTrailerFlow(t *testing.T, args []string, message string) string {
	t.Helper()
	cmd := &CommitCommand{}
	cmd.parseFlags(args)
	out := captureOutput(func() {
		require.NoError(t, cmd.generateAndCommit(nil, bufio.NewReader(strings.NewReader(message+"\n\n"))))
	})
	return out
}

// TestCommitCommand_TrailerFlagCommitted verifies a --trailer flag is
// committed onto the commit message with interpret-trailers semantics: a
// blank line before the trailer block.
func TestCommitCommand_TrailerFlagCommitted(t *testing.T) {
	repo, _ := newCommitTrailersRepo(t, "")
	runCommitTrailerFlow(t, []string{"--trailer", "Assisted-by: Sprout (dev/m)"}, "fix: manual")
	assert.Equal(t, "fix: manual\n\nAssisted-by: Sprout (dev/m)", commitTrailersRepoMessage(t, repo))
}

// TestCommitCommand_TrailersFromAgentsMd verifies the repo's AGENTS.md
// "Commit Trailers" section contributes defaults to the commit.
func TestCommitCommand_TrailersFromAgentsMd(t *testing.T) {
	agentsMd := "# Repo notes\n\n## Commit Trailers\n- Assisted-by: Sprout (ollama/llama3:8b)\n"
	repo, _ := newCommitTrailersRepo(t, agentsMd)
	runCommitTrailerFlow(t, nil, "fix: manual")
	assert.Equal(t, "fix: manual\n\nAssisted-by: Sprout (ollama/llama3:8b)", commitTrailersRepoMessage(t, repo))
}

// TestCommitCommand_TrailersConfigDefault verifies the commit.trailers config
// value contributes defaults.
func TestCommitCommand_TrailersConfigDefault(t *testing.T) {
	repo, mgr := newCommitTrailersRepo(t, "")
	require.NoError(t, mgr.UpdateConfig(func(c *configuration.Config) error {
		c.CommitTrailers = []string{"Assisted-by: Sprout (openai/gpt-5)"}
		return nil
	}))
	runCommitTrailerFlow(t, nil, "fix: manual")
	assert.Equal(t, "fix: manual\n\nAssisted-by: Sprout (openai/gpt-5)", commitTrailersRepoMessage(t, repo))
}

// TestCommitCommand_TrailersDryRunShowsMessage verifies --dry-run displays the
// final message (defaults and explicit trailers included) and creates no
// commit.
func TestCommitCommand_TrailersDryRunShowsMessage(t *testing.T) {
	agentsMd := "## Commit Trailers\n- Assisted-by: Sprout (dev/m)\n"
	repo, _ := newCommitTrailersRepo(t, agentsMd)
	out := runCommitTrailerFlow(t, []string{"--dry-run", "--trailer", "Refs: #42"}, "fix: manual")
	assert.Contains(t, out, "Assisted-by: Sprout (dev/m)")
	assert.Contains(t, out, "Refs: #42")
	assert.Contains(t, out, "Dry-run mode")
	assert.Equal(t, "1", commitTrailersRepoCount(t, repo), "dry-run must not create a commit")
}

// TestCommitCommand_TrailersDeduplicatedAcrossSources verifies a trailer
// present in both the config and AGENTS.md is committed once, and an explicit
// --trailer flag does not duplicate it either.
func TestCommitCommand_TrailersDeduplicatedAcrossSources(t *testing.T) {
	agentsMd := "## Commit Trailers\n- Refs: #42\n"
	repo, mgr := newCommitTrailersRepo(t, agentsMd)
	require.NoError(t, mgr.UpdateConfig(func(c *configuration.Config) error {
		c.CommitTrailers = []string{"Assisted-by: Sprout (dev/m)", "Refs: #42"}
		return nil
	}))
	runCommitTrailerFlow(t, []string{"--trailer", "Refs: #42"}, "fix: manual")
	assert.Equal(t, "fix: manual\n\nAssisted-by: Sprout (dev/m)\nRefs: #42", commitTrailersRepoMessage(t, repo))
}
