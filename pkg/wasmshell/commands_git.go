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

// ReadOnlyGitSubcommands is the allowlist of git subcommands the WASM
// shell answers in-browser. Anything else (add/commit/push/…) stays a
// 127 so the transactional escalation path can take it to a container.
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

// cmdGit implements read-only git subcommands against the registered
// GitExecutor. Unknown/write subcommands return 127 so the agent's
// escalation surface ("Run in cloud container") can pick them up.
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

	if !ReadOnlyGitSubcommands[sub] {
		return CmdResult{"", fmt.Sprintf("git: '%s' is not available in the browser shell (read-only subcommands only)\n", sub), 127}
	}

	if gitExecutor == nil {
		return CmdResult{"", "git: not available in this shell\n", 127}
	}

	return gitExecutor(sub, rest)
}

func gitUsageHint() CmdResult {
	return CmdResult{"", "usage: git <read-only subcommand> (status, diff, log, show, branch, remote, ls-files, rev-list, rev-parse)\n", 1}
}
