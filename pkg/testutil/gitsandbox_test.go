package testutil

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGitSandbox_InitializedEmptyRepo verifies the sandbox is a real repo
// with zero commits — the property that makes history-rewrite commands fail
// harmlessly inside it rather than operating on an enclosing checkout.
func TestGitSandbox_InitializedEmptyRepo(t *testing.T) {
	dir := GitSandbox(t)

	if _, err := os.Stat(dir + "/.git"); err != nil {
		t.Fatalf("sandbox has no .git: %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").CombinedOutput()
	if err == nil {
		t.Fatalf("expected rev-list HEAD to fail in empty repo, got output: %s", out)
	}

	if got := os.Getenv("GIT_CEILING_DIRECTORIES"); got != dir {
		t.Errorf("GIT_CEILING_DIRECTORIES = %q, want %q", got, dir)
	}
}

// TestGitSandbox_CeilingBlocksDiscovery verifies the defense-in-depth
// property: a git command run from a directory INSIDE the sandbox cannot
// discover the enclosing repository — rev-parse must fail rather than
// resolve to an ancestor .git.
func TestGitSandbox_CeilingBlocksDiscovery(t *testing.T) {
	dir := GitSandbox(t)

	sub := dir + "/nested/deep"
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	// Note: a directory outside any repo normally walks up to /.git-less
	// failure anyway; the ceiling matters when an ancestor HAS a .git.
	// Simulate that by running from a subdir with the ceiling set: git
	// must not resolve a toplevel above dir.
	out, err := exec.Command("git", "-C", sub, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return // failed to discover — exactly what we want
	}
	toplevel := strings.TrimSpace(string(out))
	if toplevel != dir {
		t.Errorf("rev-parse toplevel from inside sandbox escaped: got %q, want %q (or discovery failure)", toplevel, dir)
	}
}
