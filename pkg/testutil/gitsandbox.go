package testutil

import (
	"os/exec"
	"testing"
)

// GitSandbox returns an initialized throwaway git repository suitable for
// tests that execute real git commands.
//
// History-rewriting or side-effecting git commands (rebase, reset, push,
// clean, checkout …) must never be able to escape into a real checkout:
// a test whose "it will fail harmlessly in the temp workspace" assumption
// silently breaks (cwd fallback, discovery walk-up) ends up operating on
// the developer's actual repository. Two defenses:
//
//  1. The sandbox is an initialized repo with zero commits, so most
//     history-rewrite commands fail inside it ("no commits yet", "unknown
//     revision") instead of touching anything else.
//  2. GIT_CEILING_DIRECTORIES is set to the sandbox root for the duration
//     of the test, so git never walks UP past the sandbox looking for an
//     enclosing .git — even if the command's working directory resolves
//     somewhere unexpected.
//
// The env var is set via t.Setenv, so it is restored automatically; tests
// using GitSandbox must not call t.Parallel (t.Setenv panics if they do).
func GitSandbox(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("testutil.GitSandbox: git init: %v: %s", err, out)
	}

	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	return dir
}
