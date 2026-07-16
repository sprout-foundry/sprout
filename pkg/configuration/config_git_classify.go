// Package configuration: git-command classification and risk-pattern matching.
// (split from config_risk_subagent.go)
package configuration

import (
	"strings"
)

// categorizeGitCommand maps git subcommands to risk-category identifiers.
func categorizeGitCommand(cmdLower string) string {
	subcmd := firstFieldAfter(cmdLower, "git")
	switch subcmd {
	case "status":
		return "git_status"
	case "log":
		return "git_log"
	case "diff":
		return "git_diff"
	case "add":
		return "git_add"
	case "commit":
		return "git_commit"
	case "push":
		return "git_push"
	case "pull":
		return "git_pull"
	case "fetch":
		return "git_fetch"
	case "reset":
		return "git_reset_hard"
	case "clean":
		return "git_clean"
	case "branch":
		if strings.Contains(cmdLower, "-d") || strings.Contains(cmdLower, "--delete") {
			return "git_branch_delete" // Branch deletion is high risk
		}
		return "git_status" // Branch listing is low risk
	case "checkout":
		return "git_checkout" // Can discard changes
	case "switch":
		return "git_switch" // Can discard changes
	case "restore":
		return "git_restore" // Can discard changes
	case "stash":
		return "git_status" // Stash is relatively safe
	case "tag":
		return "git_add" // Tags are relatively safe
	case "merge", "rebase":
		return "git_commit" // Medium risk like commit
	case "cherry-pick", "cherry_pick", "am", "apply":
		return "git_commit" // Medium — applies patches, version-controlled
	case "rm", "mv":
		return "git_commit" // Medium — removes/moves tracked files, version-controlled
	default:
		return "shell_command" // Default to medium
	}
}

// matchesRiskPattern checks if a command matches a risk pattern identifier.
func matchesRiskPattern(cmdLower string, pattern string) bool {
	// Map pattern names to actual command matching. All patterns here
	// operate on tokenized fields rather than bare substring matches —
	// a path component like ".../platform &&" used to false-match
	// "rm " (the last two chars of "platform" + the space before "&&"),
	// and "-run" used to false-match "-r" — so a benign command like
	// `cd ~/.../platform && go test -run X` got classified as
	// high-risk rm_recursive. See the rm_recursive case below.
	fields := strings.Fields(cmdLower)
	hasToken := func(target string) bool {
		for _, f := range fields {
			if f == target {
				return true
			}
		}
		return false
	}
	switch pattern {
	case "force_flag":
		return containsForceFlag(cmdLower)
	case "rm_recursive":
		// Must actually invoke `rm` as a command — either at the very
		// start, after `sudo`, or after a `;` / `&&` / `||` / `|`
		// operator. A path component that happens to end in "rm"
		// (e.g. "platform") is NOT an invocation.
		if !invokesCommand(fields, "rm") {
			return false
		}
		// And a real recursive-mode flag must appear as its own token
		// or combined short flag (-r, -R, -rf, -fr, --recursive).
		for _, f := range fields {
			if f == "-r" || f == "-R" || f == "--recursive" {
				return true
			}
			// Combined short flag: -rf, -fr, -Rf, -fR (any order, any
			// length, must start with '-' and not be a long flag).
			if len(f) > 2 && f[0] == '-' && f[1] != '-' {
				hasR := strings.ContainsAny(f, "rR")
				hasF := strings.Contains(f, "f")
				if hasR && hasF {
					return true
				}
			}
		}
		return false
	case "git_reset_hard":
		return invokesGitSubcommand(fields, "reset") && hasToken("--hard")
	case "git_clean":
		return invokesGitSubcommand(fields, "clean")
	case "git_push_force":
		if !invokesGitSubcommand(fields, "push") {
			return false
		}
		// --force-with-lease is safer, don't match it
		for _, segment := range fields {
			if segment == "--force" || segment == "-f" {
				return true
			}
		}
		return false
	case "docker_prune":
		if !invokesCommand(fields, "docker") {
			return false
		}
		return hasToken("prune")
	case "git_checkout":
		return invokesGitSubcommand(fields, "checkout")
	case "git_switch":
		return invokesGitSubcommand(fields, "switch")
	case "git_restore":
		return invokesGitSubcommand(fields, "restore")
	case "git_branch_delete":
		if !invokesGitSubcommand(fields, "branch") {
			return false
		}
		return hasToken("-d") || hasToken("-D") || hasToken("--delete")
	default:
		return false
	}
}

// invokesCommand reports whether the tokenized command line actually
// invokes `name` as a command — i.e. as the first token, after `sudo`,
// or as the first token after a shell pipeline / chain operator
// (`;`, `&&`, `||`, `|`). This avoids substring matches inside paths,
// flag values, or arguments (the bug that made
// `cd .../platform && go test ... -run X` match `rm_recursive`).
func invokesCommand(fields []string, name string) bool {
	if len(fields) == 0 {
		return false
	}
	for i, f := range fields {
		if f != name {
			continue
		}
		if i == 0 {
			return true
		}
		// After a chain/pipe operator — first command in the next
		// segment. Walk backwards skipping `sudo`-like prefixes.
		prev := fields[i-1]
		switch prev {
		case ";", "&&", "||", "|":
			return true
		case "sudo":
			return true
		}
	}
	return false
}

// invokesGitSubcommand reports whether the tokenized command line
// invokes `git <subcmd>` — `git` must be an actual command invocation
// (at position 0, after a chain operator, or after sudo) AND immediately
// followed by the subcommand token. Catches `cd /repo && git checkout main`
// but not `timeout 110 git checkout -b x` (git is an argument to timeout)
// or path/argument substrings that happen to spell the same letters.
func invokesGitSubcommand(fields []string, subcmd string) bool {
	for i, f := range fields {
		if f != "git" {
			continue
		}
		// "git" must be an actual command invocation, not an argument
		// to another command (e.g. `timeout 110 git checkout`).
		if !isGitCommandAt(fields, i) {
			continue
		}
		if i+1 < len(fields) && fields[i+1] == subcmd {
			return true
		}
	}
	return false
}

// isGitCommandAt reports whether the "git" token at index i is an
// actual command invocation — at position 0, immediately after a chain
// operator (;, &&, ||, |), or after sudo. This prevents false positives
// where "git" appears as an argument to another command (e.g.
// `timeout 110 git checkout` — "git" follows "110", not a chain operator).
func isGitCommandAt(fields []string, i int) bool {
	if i == 0 {
		return true
	}
	prev := fields[i-1]
	return isChainOperator(prev) || prev == "sudo"
}

// isChainOperator reports whether a token is a shell pipeline / chain
// operator that ends one command and starts another.
func isChainOperator(tok string) bool {
	switch tok {
	case ";", "&&", "||", "|":
		return true
	}
	return false
}

// firstFieldAfter returns the first whitespace-delimited field after the given prefix.
func firstFieldAfter(s, prefix string) string {
	after := strings.TrimPrefix(s, prefix)
	after = strings.TrimSpace(after)
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
