package tools

import (
	"fmt"
	"strings"
)

// blockedGitArg represents a dangerous git flag pattern that must be blocked.
type blockedGitArg struct {
	// check is a function that returns true if the args string matches this
	// dangerous pattern. The args string is already lowercased.
	check    func(argsLower string) bool
	pattern  string // Human-readable pattern for error messages and auditing
	category string // High-level category for error messages
	reason   string // Human-readable explanation of why this is dangerous
}

// gitBlocklist defines all dangerous git flags/patterns that are rejected.
//
// Each entry has a check function that validates the lowercased args string.
// We use field-level prefix matching where possible (to avoid false positives
// from substrings) and fall back to substring matching for multi-token patterns.
//
// Based on research into git argument injection CVEs:
//   - CVE-2025-66032 (Claude Code abbreviation bypass via flag abbreviation)
//   - CVE-2024-32002 (git clone --recursive via malicious submodules)
//   - CVE-2025-48384 (git clone --recursive path traversal)
//   - CVE-2022-24826 (simple-git --upload-pack RCE)
//   - CVE-2026-25053 (n8n git node RCE via clean/smudge filters)
var gitBlocklist = []blockedGitArg{
	// ---- Command execution via remote helper options (CRITICAL) ----

	// --upload-pack allows arbitrary command execution. Git accepts abbreviations,
	// so we check for "--upload-pa" which is the shortest commonly exploitable prefix.
	flagPrefix("--upload-pa", "command execution", "allows arbitrary command execution via remote helper"),
	// --receive-pack allows arbitrary command execution.
	flagPrefix("--receive-pa", "command execution", "allows arbitrary command execution via remote helper"),
	// --exec specifies arbitrary program for git-shell. We use "--exe" as the prefix
	// to catch abbreviations; the only git flags starting with "--exe" are variants
	// of --exec (--exec-path, etc.) which are all potentially dangerous.
	flagPrefix("--exe", "command execution", "specifies arbitrary program for git-shell execution"),

	// ---- Config injection via -c with dangerous key prefix (CRITICAL) ----
	// We block specific dangerous config keys rather than blanket "core.*" to
	// allow legitimate keys like core.autocrlf, core.pager, core.compression.
	//
	// configKeyPrefix checks both forms:
	//   "-c core.hooksPath=..." (space-separated)
	//   "-ccore.hooksPath=..."  (compact, no space)

	// System-level execution config keys
	configKeyPrefix("-c", "core.hooksPath", "config injection", "redirects hook directory to arbitrary location, enabling command execution"),
	configKeyPrefix("-c", "core.gitProxy", "config injection", "executes arbitrary command as git network proxy"),
	configKeyPrefix("-c", "core.sshCommand", "config injection", "replaces SSH with arbitrary command for data exfiltration"),
	configKeyPrefix("-c", "core.fsmonitor", "config injection", "executes arbitrary command as filesystem monitor"),

	// Credential exfiltration
	configKeyPrefix("-c", "credential.", "config injection", "credential.* config can execute arbitrary commands via ! prefix"),

	// Remote manipulation — override upload-pack/receive-pack/proxy
	configKeyPrefix("-c", "remote.", "config injection", "remote.* config can override upload-pack/receive-pack to arbitrary commands"),

	// URL rewriting — redirect push/fetch to attacker server
	configKeyPrefix("-c", "url.", "config injection", "url.* config can redirect pushes/fetches to attacker-controlled servers"),

	// Protocol settings — enable dangerous protocols
	configKeyPrefix("-c", "protocol.", "config injection", "protocol.* config can enable dangerous protocols (e.g., file:// for local RCE)"),

	// Clean/smudge filter execution (CVE-2026-25053)
	configKeyPrefix("-c", "filter.", "config injection", "filter.*.clean/smudge can execute arbitrary commands during git add/checkout"),

	// ---- Config injection via --config (CRITICAL) ----

	// Both "--config=key=value" (equals form, single token) and
	// "--config key=value" (space form, two tokens) are checked.
	flagPrefix("--config=", "config injection", "--config flag can set dangerous git config values enabling RCE"),
	substringMatch("--config ", "config injection", "--config flag can set dangerous git config values enabling RCE"),

	// ---- Directory escape (HIGH) ----

	// --git-dir overrides .git directory location
	flagPrefix("--git-d", "directory escape", "overrides .git directory, potentially writing to arbitrary locations"),
	// --work-tree overrides working tree directory
	flagPrefix("--work-t", "directory escape", "overrides working tree, potentially writing files outside workspace"),
	// --separate-git-dir places .git data in separate directory
	flagPrefix("--separate-git", "directory escape", "overrides git directory location, potentially writing to arbitrary paths"),
	// --prefix overrides pathspec prefix
	flagPrefix("--pref", "directory escape", "overrides prefix path, can be combined for path traversal"),

	// ---- Submodule execution (HIGH) ----

	// --recursive and --recurse-submodules trigger submodule checkout/operations
	// which can execute malicious hooks in submodules.
	flagPrefix("--recur", "submodule execution", "triggers submodule operations which can execute malicious hooks"),

	// ---- Other dangerous flags ----

	flagPrefix("--filter", "filter injection", "partial clone filter can be exploited for arbitrary behavior"),
	flagPrefix("--remote=", "remote manipulation", "can specify a local script path that gets executed"),
	// --template sets template directory for new repos, could inject malicious hooks
	flagPrefix("--template", "hook injection", "sets template directory for new repos, could inject malicious hooks"),
}

// flagPrefix returns a blockedGitArg that checks if any whitespace-delimited
// token in args starts with the given prefix (case-insensitive).
func flagPrefix(prefix, category, reason string) blockedGitArg {
	prefixLower := strings.ToLower(prefix)
	return blockedGitArg{
		check: func(argsLower string) bool {
			for _, field := range strings.Fields(argsLower) {
				if strings.HasPrefix(field, prefixLower) {
					return true
				}
			}
			return false
		},
		pattern:  prefix,
		category: category,
		reason:   reason,
	}
}

// configKeyPrefix returns a blockedGitArg that checks if args contain the
// given flag followed (with or without a space) by the key prefix.
// It catches these forms:
//   - "-c core.hooksPath=..." (single space between flag and key)
//   - "-ccore.hooksPath=..."  (compact, no separator)
//   - "-c\tcore.hooksPath=..." (tab separator — git accepts tabs/newlines/multi-space)
//
// Rather than enumerate every whitespace separator git accepts, we normalize
// the args by collapsing all runs of whitespace to a single space before the
// substring checks. This also defeats tab/newline injection attempts that
// would bypass a literal " " (space) substring match.
func configKeyPrefix(flag, keyPrefix, category, reason string) blockedGitArg {
	// Space-separated form: "-c core."
	spacePattern := strings.ToLower(flag + " " + keyPrefix)
	// Compact form: "-ccore." (no space between flag and key)
	compactPattern := strings.ToLower(flag + keyPrefix)

	return blockedGitArg{
		check: func(argsLower string) bool {
			// Collapse all whitespace (tabs, newlines, multi-space) to single
			// spaces so "-c\tcore." and "-c  core." are caught like "-c core.".
			// strings.Fields splits on any Unicode whitespace and drops empty
			// tokens, so re-joining yields single-space-separated args.
			normalized := strings.Join(strings.Fields(argsLower), " ")
			return strings.Contains(normalized, spacePattern) ||
				strings.Contains(normalized, compactPattern)
		},
		pattern:  flag + " " + keyPrefix,
		category: category,
		reason:   reason,
	}
}

// substringMatch returns a blockedGitArg that checks if args contain the
// given substring (case-insensitive).
func substringMatch(pattern, category, reason string) blockedGitArg {
	patternLower := strings.ToLower(pattern)
	return blockedGitArg{
		check:    func(argsLower string) bool { return strings.Contains(argsLower, patternLower) },
		pattern:  pattern,
		category: category,
		reason:   reason,
	}
}

// ValidateGitArgs validates that the provided git arguments string does not
// contain any dangerous flags or patterns.
//
// It uses a combination of matching strategies:
//   - Field prefix matching: splits args into whitespace-delimited tokens and
//     checks if any token starts with a blocklisted prefix. Catches abbreviations.
//   - Substring matching: for multi-token patterns like "-c core.", checks
//     containment in the full args string.
//
// Returns nil if all arguments are safe, or an error describing which flag
// was blocked and why.
func ValidateGitArgs(args string) error {
	if args == "" {
		return nil
	}

	argsLower := strings.ToLower(args)

	// Check -C flag (uppercase) separately because -c (lowercase) is the config
	// flag and must not be blocked. We need the original case to distinguish them.
	for _, field := range strings.Fields(args) {
		// -C as standalone flag or -C<path> (attached path)
		if field == "-C" || (len(field) > 2 && strings.HasPrefix(field, "-C")) {
			return fmt.Errorf("rejected dangerous git flag %q: overrides working directory, bypassing workspace restriction (directory escape)",
				field)
		}
	}

	for _, flag := range gitBlocklist {
		if flag.check(argsLower) {
			return fmt.Errorf("rejected dangerous git flag %q: %s (%s)",
				flag.pattern, flag.reason, flag.category)
		}
	}

	return nil
}
