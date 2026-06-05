package git

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// SP-066-era defense: stop tests from running mutating git operations
// against the host repo.
//
// Context: a CommitFlow / CommitExecutor path used to fall back to the
// process CWD when no working directory was supplied (e.g., nil-agent
// constructions inside tests). On developer machines that CWD is the
// project repo itself, so a leaky test could (and did) land literal
// "test" commits on main. The testing-isolation layer at
// pkg/configuration/testing_isolation.go sanitizes the
// api.TestClientType="test" sentinel that produced the "test" message;
// this file is the second line of defense at the shell-out boundary so
// any future code path that builds a mutating git command without a
// working directory under `go test` fails fast instead of silently
// committing.
//
// Callers MUST go through one of the helpers in this file. Direct
// exec.Command("git", "commit", …) without a Dir is the pattern that
// caused the regression in the first place.

// mutatingGitSubcommands enumerates git subcommands that modify the
// repository state (working tree, index, refs, or object DB). Keep in
// sync with pkg/agent_commands/commit_git.go::mutatingGitSubcommands.
var mutatingGitSubcommands = map[string]bool{
	"commit":       true,
	"add":          true,
	"push":         true,
	"reset":        true,
	"restore":      true,
	"checkout":     true,
	"switch":       true,
	"rm":           true,
	"mv":           true,
	"merge":        true,
	"rebase":       true,
	"am":           true,
	"cherry-pick":  true,
	"revert":       true,
	"pull":         true,
	"stash":        true,
	"tag":          true,
	"branch":       true,
	"update-ref":   true,
	"update-index": true,
	"gc":           true,
	"prune":        true,
	"repack":       true,
}

// SafeGitCmd constructs an *exec.Cmd for a git invocation with built-in
// test-mode safety:
//
//   - Production: behaves like exec.Command("git", args...) with cmd.Dir
//     set to dir when dir != "".
//   - Test mode (`go test`): when dir is empty AND the subcommand is
//     mutating, returns a Cmd that points at an unrunnable sentinel path
//     so the test fails loudly instead of mutating the host repo.
//
// Pass dir="" only for read-only commands (status, diff, log, …) when
// you intentionally want the process CWD. Mutating commands MUST supply
// a working directory.
func SafeGitCmd(dir string, args ...string) *exec.Cmd {
	if testing.Testing() && dir == "" && len(args) > 0 && mutatingGitSubcommands[args[0]] {
		blocked := exec.Command("/dev/null/sprout-test-blocked-mutating-git", args...)
		blocked.Env = append(os.Environ(), "SPROUT_BLOCKED_GIT="+strings.Join(args, " "))
		return blocked
	}
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}
