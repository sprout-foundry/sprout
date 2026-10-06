package wasmshell

import (
	"fmt"
	"strings"
)

// GhExecutor runs a `gh` subcommand (e.g. "pr list") with the raw args and
// returns its result. The browser build installs an implementation backed by
// isomorphic-git (clone/checkout) plus the GitHub REST API over the same
// egress path git uses; without one, `gh` reports itself unavailable (exit
// 127) — the same signal the escalation surface treats as "run in container".
type GhExecutor func(subcommand string, args []string) CmdResult

// ghExecutor holds the installed executor; nil means gh is unavailable.
var ghExecutor GhExecutor

// RegisterGhExecutor installs the executor backing the shell's "gh" command.
// Passing nil uninstalls it.
func RegisterGhExecutor(fn GhExecutor) {
	ghExecutor = fn
}

// GhSubcommands is the set of `gh` subcommands (as space-joined paths, e.g.
// "pr checkout") the WASM shell answers in-browser. Anything else stays a 127
// so the transactional escalation path can take it to a container.
var GhSubcommands = map[string]bool{
	"repo clone":  true,
	"repo view":   true,
	"pr list":     true,
	"pr view":     true,
	"pr checkout": true,
	"pr create":   true,
	"pr diff":     true,
	"pr status":   true,
	"auth status": true,
}

// cmdGh implements the in-browser gh subcommands against the registered
// GhExecutor. Unknown subcommands return 127 so the agent's escalation
// surface ("Run in cloud container") picks them up.
func cmdGh(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return ghUsageHint()
	}

	// Peel leading global flags (gh --help, gh --version, etc.).
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		flag := rest[0]
		rest = rest[1:]
		switch flag {
		case "-h", "--help":
			return CmdResult{Stdout: ghHelpText(), ExitCode: 0}
		case "--version":
			return CmdResult{Stdout: "gh version 2.0.0 (sprout browser shell)\n", ExitCode: 0}
		}
	}

	if len(rest) == 0 {
		return ghUsageHint()
	}

	// A subcommand path is up to two words (e.g. "pr checkout", "repo clone").
	sub := rest[0]
	consumed := 1
	if len(rest) > 1 && !strings.HasPrefix(rest[1], "-") && GhSubcommands[sub+" "+rest[1]] {
		sub = sub + " " + rest[1]
		consumed = 2
	}

	if !GhSubcommands[sub] {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("gh: '%s' is not available in the browser shell (run it in a cloud container)\n", sub), ExitCode: 127}
	}

	if ghExecutor == nil {
		return CmdResult{Stdout: "", Stderr: "gh: not available in this shell\n", ExitCode: 127}
	}

	return ghExecutor(sub, rest[consumed:])
}

func ghUsageHint() CmdResult {
	return CmdResult{Stdout: "", Stderr: "usage: gh <command> <subcommand> (repo clone, pr list, pr view, pr checkout, pr create, pr diff, pr status, auth status)\n", ExitCode: 1}
}

func ghHelpText() string {
	return "Work with GitHub from the browser shell.\n\n" +
		"Available commands:\n" +
		"  gh repo clone <owner/name|url>   Clone a repository\n" +
		"  gh pr list [--state open]       List pull requests\n" +
		"  gh pr view <number>             Show a pull request\n" +
		"  gh pr checkout <number>         Fetch and check out a PR branch\n" +
		"  gh pr create --title T --body B Create a pull request from the current branch\n" +
		"  gh pr diff <number>             Show a pull request's diff\n" +
		"  gh pr status                    Show pull requests for the current branch\n" +
		"  gh auth status                  Show the GitHub connection state\n"
}
