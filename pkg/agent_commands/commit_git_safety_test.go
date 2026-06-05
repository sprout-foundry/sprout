package commands

import (
	"os"
	"strings"
	"testing"
)

// TestGitCommand_BlocksMutatingOpsInTestWithoutTempDir guards against the
// regression where a production-path `gitCommand("commit", ...)` execution
// from inside `go test` runs against the host repo. The actual exploit
// happened when api.TestClientType "test" leaked into config.CommitProvider
// and the mock client returned "test" as the commit message — two
// "test"-titled commits landed on the user's main branch before being
// noticed. The fix in pkg/configuration/testing_isolation.go sanitizes
// that sentinel; this defense at the shell-out layer guarantees that no
// other path producing the same chain can silently commit again.
func TestGitCommand_BlocksMutatingOpsInTestWithoutTempDir(t *testing.T) {
	// Make sure no prior test left a directory set on the package global.
	priorDir := currentDir
	SetGitDir("")
	t.Cleanup(func() { SetGitDir(priorDir) })

	mutating := []string{
		"commit", "add", "push", "reset", "restore",
		"checkout", "switch", "rm", "merge", "rebase",
	}
	for _, sub := range mutating {
		t.Run(sub, func(t *testing.T) {
			cmd := gitCommand(sub, "anything")
			// The blocked sentinel: path is /dev/null/... which can't
			// resolve, so .Run() returns an error rather than executing
			// git. Verify the sentinel before .Run() ever fires.
			if !strings.Contains(cmd.Path, "/dev/null/sprout-test-blocked-mutating-git") {
				t.Fatalf("expected blocked sentinel for %q, got cmd.Path=%q", sub, cmd.Path)
			}
			// The env should carry the offending args so test infrastructure
			// can surface a useful failure message.
			var found bool
			for _, e := range cmd.Env {
				if strings.HasPrefix(e, "SPROUT_BLOCKED_GIT=") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("blocked %s command missing SPROUT_BLOCKED_GIT env", sub)
			}
			// Running it must fail (not exec git, not commit anything).
			if err := cmd.Run(); err == nil {
				t.Errorf("blocked %s command unexpectedly ran successfully", sub)
			}
		})
	}
}

// TestGitCommand_AllowsReadOnlyOpsInTest covers the inverse: read-only
// subcommands like diff/status/rev-parse still need to work for legit
// test paths (and for code that runs `git status` to detect a clean
// tree before doing real work).
func TestGitCommand_AllowsReadOnlyOpsInTest(t *testing.T) {
	priorDir := currentDir
	SetGitDir("")
	t.Cleanup(func() { SetGitDir(priorDir) })

	readOnly := []string{"diff", "status", "log", "rev-parse", "show", "ls-files"}
	for _, sub := range readOnly {
		t.Run(sub, func(t *testing.T) {
			cmd := gitCommand(sub)
			if strings.Contains(cmd.Path, "sprout-test-blocked-mutating-git") {
				t.Errorf("read-only %q was unexpectedly blocked: cmd.Path=%q", sub, cmd.Path)
			}
			// We don't .Run() because git may or may not be installed in
			// the test environment and the cwd may not be a repo; the
			// behavior we care about is the gate decision, not the exec.
		})
	}
}

// TestGitCommand_AllowsMutatingOpsWhenTempDirSet verifies the opt-in
// pathway: tests that legitimately need mutating git ops can call
// SetGitDir("…tempdir…") first and then run anything they want against
// the throwaway repo. This is the pattern used by coverage_boost_test.go.
func TestGitCommand_AllowsMutatingOpsWhenTempDirSet(t *testing.T) {
	priorDir := currentDir
	tmp := t.TempDir()
	SetGitDir(tmp)
	t.Cleanup(func() { SetGitDir(priorDir) })

	cmd := gitCommand("commit", "-m", "irrelevant")
	if strings.Contains(cmd.Path, "sprout-test-blocked-mutating-git") {
		t.Fatalf("commit blocked despite SetGitDir(tmp); cmd.Path=%q", cmd.Path)
	}
	if cmd.Dir != tmp {
		t.Errorf("cmd.Dir = %q, want %q (the temp dir)", cmd.Dir, tmp)
	}
}

// TestMutatingGitSubcommands_CoversKnownMutators sanity-checks the
// allowlist so a refactor that drops one of these accidentally fails
// the build, not the next user's working tree.
func TestMutatingGitSubcommands_CoversKnownMutators(t *testing.T) {
	required := []string{"commit", "add", "push", "reset"}
	for _, sub := range required {
		if !mutatingGitSubcommands[sub] {
			t.Errorf("mutatingGitSubcommands missing %q", sub)
		}
	}
}

// Build-time unused-import guards to keep the file self-contained even
// when most tests don't .Run() the returned exec.Cmd.
var _ = os.Stdout
