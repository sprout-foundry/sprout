package commands

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// With no AI agent and --yes there is no way to obtain a message; the command
// must fail (non-zero exit) instead of waiting on stdin and reporting success.
func TestCommitCommand_YesWithoutAgentFails(t *testing.T) {
	_, cleanup := configuration.NewTestManager(t)
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
	t.Chdir(repo)

	cmd := &CommitCommand{}
	cmd.SetAgentError(errors.New("no provider configured"))
	var err error
	out := captureOutput(func() { err = cmd.Execute([]string{"--yes"}, nil) })
	if !strings.Contains(out, "no provider configured") {
		t.Errorf("manual-mode notice should name the agent error, got: %q", out)
	}
	if err == nil || !strings.Contains(err.Error(), "no AI provider available") {
		t.Fatalf("Execute(--yes) with no agent = %v, want the no-provider error", err)
	}
	if out, _ := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").CombinedOutput(); strings.TrimSpace(string(out)) != "1" { //nolint:gosec // G204: test-driven git invocations with controlled args
		t.Errorf("expected no new commit, rev-list count = %s", out)
	}
}
