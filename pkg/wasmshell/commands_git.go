package wasmshell

import (
	"fmt"
	"strings"
)

// GitExecutor runs a read-only git subcommand ("status", "diff", "log", …)
// with the raw subcommand args and returns its result. The browser build
// installs an implementation backed by isomorphic-git; without one, the
// shell reports git as unavailable (exit 127) — the same signal a WASM
// shell gives for commands it cannot run, which the escalation surface
// treats as "run in container".
type GitExecutor func(subcommand string, args []string) CmdResult

// gitExecutor holds the installed executor; nil means git is unavailable.
var gitExecutor GitExecutor

// RegisterGitExecutor installs the executor backing the shell's "git"
// command. Passing nil uninstalls it.
func RegisterGitExecutor(fn GitExecutor) {
	gitExecutor = fn
}

// GitSubcommands is the set of git subcommands the WASM shell answers
// in-browser via the registered GitExecutor (isomorphic-git). It covers the
// read-only commands plus the local/remote write commands a user runs in the
// browser IDE (add, commit, checkout, branch, fetch, push, pull, init,
// clone, rm, mv). Subcommands not listed here (rebase, merge, reset, stash,
// …) stay a 127 so the transactional escalation path can take them to a
// container.
var GitSubcommands = map[string]bool{
	// read-only
	"status":       true,
	"diff":         true,
	"log":          true,
	"show":         true,
	"branch":       true,
	"remote":       true,
	"ls-files":     true,
	"rev-list":     true,
	"rev-parse":    true,
	"blame":        true,
	"describe":     true,
	"shortlog":     true,
	"tag":          true,
	"symbolic-ref": true,
	"config":       true,
	"cat-file":     true,
	// local write
	"add":      true,
	"commit":   true,
	"checkout": true,
	"switch":   true,
	"fetch":    true,
	"push":     true,
	"pull":     true,
	"init":     true,
	"clone":    true,
	"rm":       true,
	"mv":       true,
}

// ReadOnlyGitSubcommands is the read-only subset of GitSubcommands. Kept for
// callers (and tests) that need the narrower set.
var ReadOnlyGitSubcommands = map[string]bool{
	"status":       true,
	"diff":         true,
	"log":          true,
	"show":         true,
	"branch":       true,
	"remote":       true,
	"ls-files":     true,
	"rev-list":     true,
	"rev-parse":    true,
	"blame":        true,
	"describe":     true,
	"shortlog":     true,
	"tag":          true,
	"symbolic-ref": true,
	"config":       true,
	"cat-file":     true,
}

// cmdGit implements the in-browser git subcommands against the registered
// GitExecutor. Unknown subcommands (rebase, merge, reset, stash, …) return
// 127 so the agent's escalation surface ("Run in cloud container") picks
// them up.
func cmdGit(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return gitUsageHint()
	}

	// Peel global flags that precede the subcommand (git -C dir status…).
	sub := ""
	rest := args
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "-C" && i+1 < len(rest) {
			rest = append(append([]string{}, rest[:i]...), rest[i+2:]...)
			i = -1
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		sub = a
		rest = append(append([]string{}, rest[:i]...), rest[i+1:]...)
		break
	}

	if sub == "" {
		return gitUsageHint()
	}

	if !GitSubcommands[sub] {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("git: '%s' is not available in the browser shell (run it in a cloud container)\n", sub), ExitCode: 127}
	}

	if gitExecutor == nil {
		return CmdResult{Stdout: "", Stderr: "git: not available in this shell\n", ExitCode: 127}
	}

	return gitExecutor(sub, rest)
}

func gitUsageHint() CmdResult {
	return CmdResult{Stdout: "", Stderr: "usage: git <subcommand> (status, diff, log, show, branch, remote, add, commit, checkout, fetch, push, pull, clone, init, ls-files, rev-list, rev-parse)\n", ExitCode: 1}
}
