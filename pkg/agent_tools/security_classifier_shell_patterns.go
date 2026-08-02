// Shell command pattern matching for security classification

package tools

import (
	"regexp"
	"strings"
)

// pipeToShellPattern matches pipe-to-shell patterns that can execute arbitrary code.
// Matches: | followed by optional whitespace, optional path prefix (e.g., /bin/, /usr/bin/),
// optional "env" wrapper, then shell/script interpreter name.
// The shell name must be followed by whitespace, shell metacharacters (;, |, &), or end of string.
// Examples matched: |bash, | bash, |  bash, | /bin/bash, | /usr/bin/env bash, |zsh, |bash -c 'cmd'
// NOT matched: |sort, |shasum, |shfmt (shell name must be followed by a valid boundary)
// standaloneSleepPattern matches `sleep N` and `sleep N{s,m,h,d}` where N is a
// positive integer or decimal. The anchors prevent matching chained or
// embedded forms — `cmd && sleep 5 && cmd2` will not match because the
// caller checks for compound separators first.
var standaloneSleepPattern = regexp.MustCompile(`^sleep\s+\d+(\.\d+)?[smhd]?$`)

// standaloneWaitPattern matches `wait` and `wait <pid>` (a single numeric arg).
// `wait` with no jobs to wait on returns immediately, so this is purely an
// antipattern when issued as a tool call.
var standaloneWaitPattern = regexp.MustCompile(`^wait(\s+\d+)?$`)

// compoundCommandSeparators are the operators that signal a chained or
// piped command. Their presence disqualifies a command from the standalone
// classification, even if part of the command line is a bare sleep/wait.
var compoundCommandSeparators = []string{"&&", "||", ";", "|", "\n"}

// isStandaloneSleepOrWaitCommand reports whether cmd is exactly a bare
// `sleep N[suffix]` or `wait [pid]` invocation with nothing else around it.
//
// Chained or scripted forms (`make && sleep 5`, `bash -c "sleep 60"`,
// `for i in 1 2 3; do sleep $i; done`) are NOT matched — legitimate
// scripting uses are preserved.
func isStandaloneSleepOrWaitCommand(cmd string) bool {
	for _, sep := range compoundCommandSeparators {
		if strings.Contains(cmd, sep) {
			return false
		}
	}
	return standaloneSleepPattern.MatchString(cmd) || standaloneWaitPattern.MatchString(cmd)
}

var pipeToShellPattern = regexp.MustCompile(`\|\s*(?:[^\s|&;]+/\s*)*(?:env\s+)?(?:bash|zsh|dash|fish|ksh|csh|tcsh|python[23]?|perl|ruby|node|sh)(?:\s|[;&|]|$)`)

// pipeToModulePattern matches a pipe into an interpreter run in module
// mode (python -m <module>). In that form stdin is consumed as DATA by
// the named module rather than executed as code — e.g.
// `curl … | python3 -m json.tool` pretty-prints JSON; it does not run
// the downloaded bytes. RE2 has no negative lookahead, so isPipeToShell
// subtracts these matches before deciding.
var pipeToModulePattern = regexp.MustCompile(`\|\s*(?:[^\s|&;]+/\s*)*(?:env\s+)?python[23]?\s+-m\s`)

// isPipeToShell reports whether s pipes output into a shell/interpreter
// that would EXECUTE the piped bytes as code. The python `-m <module>`
// form is treated as data-consuming, not code execution, so a command
// whose only interpreter pipe is a module run (e.g. json.tool) is not
// flagged. Any other pipe-to-interpreter (| bash, bare | python, | sh)
// still matches.
func isPipeToShell(s string) bool {
	lc := strings.ToLower(s)
	if !pipeToShellPattern.MatchString(lc) {
		return false
	}
	// Remove python -m module-mode pipes; if nothing code-executing
	// remains, the command only fed data to a module → not RCE.
	if pipeToModulePattern.MatchString(lc) {
		stripped := pipeToModulePattern.ReplaceAllString(lc, " ")
		if !pipeToShellPattern.MatchString(stripped) {
			return false
		}
	}
	return true
}

// SplitChainedCommand splits a command string on &&, ||, ;, | (quote-aware)
// and returns the individual subcommand strings. It respects single and
// double quotes so that separators inside quoted strings are not treated as
// chain boundaries.
func SplitChainedCommand(cmd string) []string {
	var parts []string
	current := &strings.Builder{}
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]

		if !inQuote && (c == '\'' || c == '"') {
			inQuote = true
			quoteChar = c
			current.WriteByte(c)
			continue
		}

		if inQuote && c == quoteChar {
			inQuote = false
			quoteChar = 0
			current.WriteByte(c)
			continue
		}

		if !inQuote {
			if c == '&' && i+1 < len(cmd) && cmd[i+1] == '&' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				i++
				continue
			}
			if c == '|' && i+1 < len(cmd) && cmd[i+1] == '|' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				i++
				continue
			}
			// Newline is a command separator — multi-line pastes (e.g.,
			// "echo hello\nrm -rf x") must be classified per-line so that
			// a dangerous command on the second line isn't hidden by a
			// safe command on the first line.
			if c == '\n' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				continue
			}
			if c == ';' || c == '|' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				continue
			}
		}
		current.WriteByte(c)
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

// classifyChainedCommand splits and classifies chained commands
func classifyChainedCommand(cmd string) []SecurityRisk {
	if risk, ok := classifyReadOnlyForLoop(cmd); ok {
		return []SecurityRisk{risk}
	}

	// Check for pipe-to-shell patterns (case-insensitive to prevent bypass).
	// Strip quoted sections first to avoid false positives from | characters
	// inside grep patterns, regex alternation, etc. (e.g., grep "a|b|c" | head).
	// Pipe-to-shell is CAUTION (prompt, don't block) — used for legitimate
	// install scripts. We add it as a risk but continue classifying parts so
	// genuinely dangerous commands in the chain (e.g., rm -rf /etc/) still
	// elevate to DANGEROUS.
	cmdLower := strings.ToLower(cmd)
	stripped := stripQuotedSections(cmdLower)
	pipeToShell := isPipeToShell(stripped)

	parts := SplitChainedCommand(cmd)

	var risks []SecurityRisk
	if pipeToShell {
		risks = append(risks, SecurityCaution)
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		risks = append(risks, classifySingleCommand(part))
	}
	return risks
}

// classifySingleCommand classifies a single command (no chaining)
func classifySingleCommand(cmd string) SecurityRisk {
	cmdLower := strings.ToLower(cmd)

	if risk, ok := classifyReadOnlyForLoop(cmd); ok {
		return risk
	}

	// Command substitution ($() or backticks) - cannot fully inspect inner commands
	if strings.Contains(cmd, "$(") || strings.ContainsAny(cmd, "`") {
		return SecurityCaution
	}

	// Heredoc syntax (<<) - cannot fully inspect heredoc content
	if strings.Contains(cmd, "<<") {
		return SecurityCaution
	}

	// Check for output redirection to system directories
	if strings.Contains(cmd, "> /etc/") || strings.Contains(cmd, ">> /etc/") ||
		strings.Contains(cmd, "> /usr/") || strings.Contains(cmd, ">> /usr/") ||
		strings.Contains(cmd, "> /bin/") || strings.Contains(cmd, ">> /bin/") ||
		strings.Contains(cmd, "> /sbin/") || strings.Contains(cmd, ">> /sbin/") ||
		strings.Contains(cmd, "> /var/") || strings.Contains(cmd, ">> /var/") ||
		strings.Contains(cmd, "> /opt/") || strings.Contains(cmd, ">> /opt/") ||
		strings.Contains(cmd, "> /root/") || strings.Contains(cmd, ">> /root/") ||
		strings.Contains(cmd, "> /boot/") || strings.Contains(cmd, ">> /boot/") {
		return SecurityDangerous
	}
	if (strings.Contains(cmd, "> /dev/") || strings.Contains(cmd, ">> /dev/")) &&
		!strings.Contains(cmd, "> /dev/null") && !strings.Contains(cmd, ">> /dev/null") &&
		!strings.Contains(cmd, "> /dev/stdout") && !strings.Contains(cmd, ">> /dev/stdout") &&
		!strings.Contains(cmd, "> /dev/stderr") && !strings.Contains(cmd, ">> /dev/stderr") {
		return SecurityDangerous
	}

	// Check for path traversal in redirection targets (e.g., > /tmp/../etc/passwd)
	if containsRedirection(cmd) && hasRedirectionTraversalToSystemDir(cmd) {
		return SecurityDangerous
	}

	if isPrivilegedPackageInstall(cmdLower) {
		return SecurityCaution
	}

	if isDangerousPattern(cmdLower) {
		return SecurityDangerous
	}

	// Safe rm -rf commands must be checked BEFORE isCautionPattern,
	// which now catches all non-whitelisted "rm -rf " / "rm -fr " commands.
	// Without this early return, safe nested paths like "rm -rf internal/api/dist/cache"
	// would be classified as CAUTION instead of SAFE.
	if isSafeRmRfPrefix(cmdLower) {
		return SecuritySafe
	}

	// Destructive find commands (find -delete, find -exec rm/chmod/chown) must be
	// intercepted BEFORE isCautionPattern because isCautionPattern catches "chmod 777"
	// and "rm " as sub-patterns within the -exec body. Without this ordering,
	// "find . -exec chmod 777 {} \;" would return CAUTION (matching chmod 777)
	// instead of DANGEROUS (bulk destructive operation across unknown file set).
	if isDestructiveFind(cmdLower) {
		return SecurityDangerous
	}

	// Check caution patterns BEFORE safe patterns, so that specific
	// caution-level commands (like rm -rf, eval, docker rm) override
	// broad safe matches.
	if isCautionPattern(cmdLower) {
		return SecurityCaution
	}

	// Interpreter command escapes (bash -c '...', python -c '...', etc.)
	// have opaque inline code bodies that we can't statically inspect.
	// They must return CAUTION, not SAFE — the safe-list matches "bash",
	// "python", etc. but doesn't know the -c/-e body could be destructive.
	if isInterpreterCommandEscape(cmdLower) {
		return SecurityCaution
	}

	if isSafeShellCommand(cmdLower) {
		return SecuritySafe
	}

	// xargs is safe iff the command it invokes is safe. Delegate to a
	// structured sub-classifier that strips xargs flags and recursively
	// classifies the inner command. Without this carve-out, every
	// `xargs <safe-cmd>` invocation defaults to CAUTION via the
	// fallback below — including safe pipelines like `find … | xargs
	// du -sh`. `isCautionPattern` does not match xargs-prefixed
	// commands (it checks prefixes like "rm -rf ", "eval ", etc., not
	// "xargs rm -rf"), so this check correctly handles dangerous
	// `xargs rm` / `xargs chmod` cases by recursion into the inner
	// command's classification.
	if risk, ok := classifyXargsInvocation(cmdLower); ok {
		return risk
	}

	// Default: SAFE. The classifier is behavior-based, not name-based.
	// All known risky patterns (rm, chmod 777, sudo, kill, eval, pipe to
	// shell, command substitution, heredoc, interpreter escapes, output
	// redirection, system-dir targeting, critical operations) are checked
	// above. An unrecognized command that matches none of those patterns
	// has no detectable risky behavior and should not prompt the user.
	// This eliminates the need to maintain an ever-growing whitelist of
	// safe command names.
	return SecuritySafe
}

func classifyReadOnlyForLoop(cmd string) (SecurityRisk, bool) {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "for ") || !strings.Contains(lower, " do ") || !strings.HasSuffix(lower, "done") {
		return SecuritySafe, false
	}

	max := SecuritySafe

	for _, sub := range extractCommandSubstitutions(trimmed) {
		risk := maxRisk(classifyChainedCommand(sub))
		if risk > max {
			max = risk
		}
	}

	doIndex := strings.Index(lower, " do ")
	doneIndex := strings.LastIndex(lower, " done")
	if doIndex == -1 || doneIndex == -1 || doneIndex <= doIndex+4 {
		return SecurityCaution, true
	}

	body := strings.TrimSpace(trimmed[doIndex+4 : doneIndex])
	if body == "" {
		return SecurityCaution, true
	}

	bodyRisk := classifyReadOnlyLoopBody(body)
	if bodyRisk > max {
		max = bodyRisk
	}

	return max, true
}

func classifyReadOnlyLoopBody(body string) SecurityRisk {
	parts := strings.Split(body, ";")
	max := SecuritySafe

	for _, raw := range parts {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		for _, branch := range strings.Split(part, "&&") {
			for _, option := range strings.Split(branch, "||") {
				cmd := strings.TrimSpace(option)
				if cmd == "" {
					continue
				}
				risk := classifySingleCommand(cmd)
				if risk > max {
					max = risk
				}
			}
		}
	}

	return max
}

// ChainedClassification is a per-subcommand classification result.
// The existing []SecurityRisk return type from classifyChainedCommand
// is preserved for backwards compatibility; new code uses this richer
// type. SP-124b.
type ChainedClassification struct {
	Subcommand string // the subcommand text (trimmed, not normalized)
	Risk       SecurityRisk
	Reasoning  string // human-readable why
	Category   RiskCategory
}

// ClassifyChainedCommand is the exported wrapper around the internal
// classifyChainedCommand. It returns one ChainedClassification per
// subcommand with populated Subcommand, Risk, Reasoning, and Category.
//
// This is a thin adapter — the heavy lifting (splitting, per-subcommand
// classification) is done by classifyChainedCommand and SplitChainedCommand
// from SP-122, which are not modified.
//
// Implementation:
//   - parts := SplitChainedCommand(cmd)
//   - For each part, call classifySingleCommand(part) to get the SecurityRisk
//   - Call classifyShellCommand({"command": part}) to populate Reasoning and Category
//   - Skip empty/blank subcommands (SplitChainedCommand already drops them,
//     but we are defensive)
func ClassifyChainedCommand(cmd string) []ChainedClassification {
	parts := SplitChainedCommand(cmd)
	var results []ChainedClassification
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		risk := classifySingleCommand(part)
		// classifyShellCommand populates Reasoning and Category.
		// It has special-case early returns for empty/invalid commands
		// and for check_background/stop_background modes — both are
		// safe fallbacks for a classification table cell.
		result := classifyShellCommand(map[string]interface{}{"command": part})
		results = append(results, ChainedClassification{
			Subcommand: part,
			Risk:       risk,
			Reasoning:  result.Reasoning,
			Category:   result.Category,
		})
	}
	return results
}
