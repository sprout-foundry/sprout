package tools

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// shell_patterns_xargs.go — the xargs invocation classifiers (flag
// separation, unbounded-destructive detection), split out of the original
// shell_patterns.go monolith. Pure move.

// xargsShortFlagsWithSeparateValue is the set of xargs short flags whose
// value is supplied as the next token (e.g. `xargs -n 4 cmd`). Other
// short flags either take an embedded value (`-I{}`, `-n4`, `-eEOF`)
// or no value (`-0`, `-r`, `-t`, `-v`, `-x`).
//
// Keys are lowercase; the caller always passes cmdLower.
//
// `-i` (lowercase) is included despite its GNU xargs(1) semantics of
// "optional value" (shorthand for `-I {}`). We treat it as value-taking
// because the more common usage `xargs -i REPL cmd ...` benefits from
// consuming REPL — that lets the stripper correctly identify `cmd` as
// the inner command. The cost: `xargs -i grep p` parses as inner `p`
// (because we consume `grep` as REPL) instead of inner `grep p`. The
// resulting CAUTION is conservative (false negative in the safe
// direction), which we accept.
var xargsShortFlagsWithSeparateValue = map[string]bool{
	"-a": true, // --arg-file=FILE
	"-d": true, // --delimiter=DELIM
	"-e": true, // --eof[=EOF-STR] (lowercased from -E)
	"-i": true, // --replace[=REPLSTR] (shorthand; GNU spec says optional)
	"-I": true, // --replace=REPLSTR (uppercase I is required-argument)
	"-l": true, // --max-lines[=MAX-LINES]
	"-L": true, // --max-lines=MAX-LINES
	"-n": true, // --max-args=MAX-ARGS
	"-s": true, // --max-chars=MAX-CHARS
}

// xargsLongFlagsWithSeparateValue is the set of xargs long flags whose
// value is supplied as the next token (e.g. `xargs --max-args 4 cmd`).
// Long flags may also use the embedded `--flag=value` form, which is
// detected by a literal `=` in the token.
var xargsLongFlagsWithSeparateValue = map[string]bool{
	"--arg-file":         true,
	"--delimiter":        true,
	"--eof":              true,
	"--max-args":         true,
	"--max-chars":        true,
	"--max-lines":        true,
	"--max-procs":        true,
	"--open-tty":         true,
	"--process-slot-var": true,
	"--replace":          true,
}

// classifyXargsInvocation returns the risk level of `xargs <inner-cmd>` and
// a bool indicating whether the input matched an xargs invocation at all.
// When ok is false, the caller should fall through to other classifiers.
//
// xargs is dangerous only insofar as the command it invokes is dangerous.
// We strip xargs flags and recursively classify the inner command. For
// shell-interpreter invocations (xargs sh -c "...") we can't statically
// inspect the body, so we return CAUTION regardless of the inner script.
//
// Critical-system check: if the inner command would be a critical operation
// when run directly (e.g. `rm -rf /`), we elevate the xargs invocation to
// DANGEROUS. Without this, `xargs rm -rf /` would slip through as CAUTION
// because the inner classification (rm -rf /) itself doesn't reach the
// critical-operation detector — that detector runs at the top level of
// classifyShellCommand and uses invokesCommand semantics that don't know
// about xargs. Adding the inner check here preserves the DANGEROUS
// classification for cases the top-level detector handles coincidentally
// (via the sudo-prefix path) and extends it to bare `xargs rm -rf /`.
//
// Recursion is bounded to depth 1: a second `xargs` keyword in the inner
// command is classified by the recursive call (which will fall through to
// default-CAUTION there, which is the safe behavior).
func classifyXargsInvocation(cmdLower string) (SecurityRisk, bool) {
	if cmdLower == "xargs" {
		// Bare `xargs` reads stdin until EOF and runs whatever lines it
		// finds — ambiguous, stay cautious.
		return SecurityCaution, true
	}
	const prefix = "xargs "
	if !strings.HasPrefix(cmdLower, prefix) {
		return SecuritySafe, false
	}

	inner := stripXargsFlags(cmdLower[len(prefix):])
	inner = strings.TrimSpace(inner)
	if inner == "" {
		// `xargs` with only flags and no inner command — degenerate,
		// treat like bare xargs.
		return SecurityCaution, true
	}

	// Shell-interpreter carve-out. xargs sh -c "...", xargs bash -c "...",
	// etc. The script body is opaque to a static classifier.
	// Strip a leading "--" (end-of-options separator) so that
	// "xargs -- sh -c ..." is still caught.
	interpreterCmd := inner
	if strings.HasPrefix(interpreterCmd, "-- ") {
		interpreterCmd = interpreterCmd[3:]
	}
	head := strings.Fields(interpreterCmd)[0]
	switch head {
	case "sh", "bash", "zsh", "dash", "fish", "ksh", "csh", "tcsh":
		return SecurityCaution, true
	}

	// Critical-system elevation. If the inner command is a known critical
	// operation (rm -rf /, mkfs, dd to a block device, fork bomb, etc.),
	// the xargs invocation is also critical — there is no scenario where
	// running `xargs rm -rf /` should be less dangerous than running
	// `rm -rf /` directly. This is the canonical check used by
	// classifyShellCommand's isCriticalSystemOperation path; we replicate
	// it here because the recursion strips xargs before that gate sees
	// the command.
	if configuration.IsCriticalOperation(inner) {
		return SecurityDangerous, true
	}

	// Destructive inner command with no explicit target (e.g. `rm -rf`,
	// `rm -r`, `chmod -R`). When fed by xargs, the targets come from
	// arbitrary piped input — the destruction is unbounded. Elevate to
	// DANGEROUS because there's no safe-path whitelist check on the
	// piped arguments. This closes the `ls | xargs rm -rf` gap where
	// `rm -rf` alone classifies as CAUTION (no target → no critical hit)
	// but the actual effect is mass deletion of whatever `ls` emitted.
	if isUnboundedDestructiveXargs(inner) {
		return SecurityDangerous, true
	}

	return classifySingleCommand(inner), true
}

// isUnboundedDestructiveXargs reports whether inner is a destructive command
// (rm -rf, rm -r, rm -f, rmdir, chmod -R, chown -R) without an explicit file
// target. In an xargs context the targets are piped input — unbounded and
// not checked by the safe-path whitelist.
func isUnboundedDestructiveXargs(inner string) bool {
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return false
	}
	cmd := fields[0]
	flags := strings.Join(fields[1:], " ")
	// rm with recursive/force flags but no path argument (only flags).
	if cmd == "rm" && (strings.Contains(flags, "-r") || strings.Contains(flags, "-f") || strings.Contains(flags, "-rf") || strings.Contains(flags, "-fr")) {
		// Check that there are no non-flag arguments (paths).
		hasPathArg := false
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") {
				hasPathArg = true
				break
			}
		}
		return !hasPathArg
	}
	// rmdir with no path argument.
	if cmd == "rmdir" && len(fields) == 1 {
		return true
	}
	// chmod -R / chown -R with no path argument.
	if (cmd == "chmod" || cmd == "chown") && strings.Contains(flags, "-R") {
		hasPathArg := false
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") {
				hasPathArg = true
				break
			}
		}
		return !hasPathArg
	}
	return false
}

// stripXargsFlags removes xargs flag tokens from the front of cmdLower and
// returns the remaining inner command as a single space-joined string.
//
// Recognized forms:
//   - bare short flags:       -0 -r -t -v -x
//   - short flag + value:     -n 4, -I REPL, -d DELIM, -s SIZE, ...
//   - short flag with embedded value: -n4, -I{}, -eEOF
//   - bare long flags:        --null --no-run-if-empty --verbose --help ...
//   - long flag + value:      --max-args 4 --replace REPLSTR ...
//   - long flag with =value:  --max-args=4 --replace=REPLSTR
//
// The token `--` ends flag parsing and is preserved as part of the inner
// command (xargs treats `--` as a positional-args separator).
//
// This is intentionally narrow — we don't try to be a complete xargs parser.
// The goal is to handle the common forms seen in agent-generated pipelines:
//
//	xargs du -sh
//	xargs -n 1 wc -l
//	xargs -I{} grep pattern {}
//	xargs -0 du -shc --files0-from=-
//	xargs --null du -sh
//	xargs --max-args=4 du -sh
//
// Unknown flags consume the token only (conservative: anything we don't
// recognize as value-taking is treated as bare, so we never accidentally
// classify the flag's value as the inner command).
func stripXargsFlags(s string) string {
	tokens := strings.Fields(s)
	out := make([]string, 0, len(tokens))
	consumeNext := false
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if consumeNext {
			consumeNext = false
			continue
		}
		// `--` ends flag parsing; preserve in inner command.
		if tok == "--" {
			out = append(out, tokens[i:]...)
			return strings.Join(out, " ")
		}
		// Single `-` (stdin) or non-flag token: start of inner command.
		if !strings.HasPrefix(tok, "-") {
			out = append(out, tokens[i:]...)
			return strings.Join(out, " ")
		}
		// Long flag.
		if strings.HasPrefix(tok, "--") {
			name := tok
			if eq := strings.IndexByte(tok, '='); eq >= 0 {
				name = tok[:eq]
			}
			if xargsLongFlagsWithSeparateValue[name] && !strings.Contains(tok, "=") {
				// value is the next token
				consumeNext = true
			}
			// else: --flag=value (embedded) or bare long flag — token consumed
			continue
		}
		// Short flag. Form A: bare single letter (`-0`, `-r`, `-t`, `-v`, `-x`).
		// Form B: clustered with embedded value (`-n4`, `-I{}`, `-eEOF`).
		// Form C: clustered bare (`-0rt`, `-rt`).
		//
		// We can't always disambiguate B vs C from the cluster alone, but
		// GNU getopt-style clusters always embed the value with the FIRST
		// short option in the cluster, and the rest of the cluster is that
		// option's value. For our purposes: if the second char is a digit
		// or non-letter, treat the whole rest of the cluster as the value
		// of the first flag (Form B). Otherwise treat as a bare flag
		// cluster (Form C).
		if len(tok) == 2 {
			// Single-letter short flag.
			if xargsShortFlagsWithSeparateValue[tok] {
				consumeNext = true
			}
			// else: bare flag, token consumed
			continue
		}
		// Cluster. Inspect first char.
		firstChar := "-" + string(tok[1])
		if xargsShortFlagsWithSeparateValue[firstChar] {
			// Form B: value is embedded in the cluster (e.g., `-n4`).
			// Token consumed.
			continue
		}
		// Form C: bare flag cluster (`-0rt`, `-rv`). Token consumed.
		continue
	}
	return strings.Join(out, " ")
}
