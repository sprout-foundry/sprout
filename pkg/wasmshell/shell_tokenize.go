package wasmshell

// shell_tokenize.go — the WASM shell tokenizer and redirect layer:
// shellToken / stripQuotes / tokenizeMarked / ExpandGlobsMarked, the redirect
// parsing (parseRedirectsMarked, ParseRedirects, Tokenize) and
// executeWithRedirects. Split out of shell.go.

import (
	"fmt"
	"os"
	"strings"
)

// shellToken is a token with its quoting provenance — quoted tokens are
// exempt from glob expansion, matching POSIX shell semantics.
type shellToken struct {
	text   string
	quoted bool
}

// stripQuotes removes surrounding quote characters from a token.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// tokenizeMarked splits a command line into tokens, tracking which ones
// were quoted (single, double, or backslash-escaped characters).
func tokenizeMarked(line string) []shellToken {
	var tokens []shellToken
	var current strings.Builder
	quoted := false
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, shellToken{current.String(), quoted})
			current.Reset()
		}
		quoted = false
	}

	for _, ch := range line {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			quoted = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			quoted = true
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			quoted = true
			continue
		}
		if (ch == ' ' || ch == '\t') && !inSingle && !inDouble {
			flush()
			continue
		}
		current.WriteRune(ch)
	}

	flush()
	return tokens
}

// ExpandGlobsMarked expands glob patterns only in unquoted arguments —
// the reason `find . -name '*.txt'` keeps its pattern even when the
// working directory holds matching files.
func ExpandGlobsMarked(tokens []shellToken) []string {
	var result []string
	for _, tok := range tokens {
		arg := stripQuotes(tok.text)
		if tok.quoted {
			result = append(result, arg)
			continue
		}
		result = append(result, ExpandGlobs([]string{arg})...)
	}
	return result
}

// parseRedirectsMarked is ParseRedirects over quote-aware tokens: it
// returns the command name and marked args so glob expansion can skip
// quoted arguments (`find . -name '*.txt'` keeps its pattern).
func parseRedirectsMarked(line string) (string, []shellToken, string, string, string, bool, bool) {
	mtokens := tokenizeMarked(line)

	name := ""
	var args []shellToken
	var stdinFile, stdoutFile, stderrFile string
	appendStdout := false
	appendStderr := false
	expectStdin := false
	expectStdout := false
	expectStderr := false
	bothRedirect := false

	splitCompound := func(tok string) (op, rest string, fused bool) {
		for _, cand := range []string{"2>>", "2>", "&>>", "&>", ">>", ">", "<"} {
			if strings.HasPrefix(tok, cand) {
				return cand, tok[len(cand):], true
			}
		}
		return "", tok, false
	}

	first := true
	for _, mt := range mtokens {
		tok := mt.text
		if !mt.quoted && !expectStdin && !expectStdout && !expectStderr {
			if op, rest, fused := splitCompound(tok); fused && rest != "" && rest != "&1" {
				switch op {
				case "<":
					stdinFile = rest
				case "2>>":
					stderrFile, appendStderr = rest, true
				case "2>":
					stderrFile = rest
				case "&>>":
					stdoutFile, stderrFile, appendStdout, bothRedirect = rest, rest, true, true
				case "&>":
					stdoutFile, stderrFile, bothRedirect = rest, rest, true
				case ">>":
					stdoutFile, appendStdout = rest, true
				case ">":
					stdoutFile = rest
				}
				continue
			}
			switch tok {
			case "<":
				expectStdin = true
				continue
			case ">", "1>":
				expectStdout = true
				continue
			case ">>", "1>>":
				expectStdout = true
				appendStdout = true
				continue
			case "2>":
				expectStderr = true
				continue
			case "2>>":
				expectStderr = true
				appendStderr = true
				continue
			case "2>&1":
				stderrFile = mergeErrIntoOutSentinel
				continue
			case "&>":
				expectStdout = true
				expectStderr = true
				bothRedirect = true
				continue
			}
		}

		if expectStdin && stdinFile == "" {
			stdinFile = tok
			expectStdin = false
			continue
		}
		if expectStdout && stdoutFile == "" {
			stdoutFile = tok
			expectStdout = false
			if bothRedirect {
				stderrFile = tok
				expectStderr = false
				bothRedirect = false
			}
			continue
		}
		if expectStderr && stderrFile == "" {
			stderrFile = tok
			expectStderr = false
			continue
		}

		if first && !strings.HasPrefix(tok, "-") {
			name = stripQuotes(tok)
			first = false
		} else {
			args = append(args, mt)
			first = false
		}
	}

	return name, args, stdinFile, stdoutFile, stderrFile, appendStdout, appendStderr
}

// executeWithRedirects parses and executes a single command with redirects.
// If pipedStdin is non-empty, it takes precedence over any < redirect file.
func executeWithRedirects(input string, pipedStdin string) CmdResult {
	name, markedArgs, stdinFile, stdoutFile, stderrFile, appendStdout, appendStderr := parseRedirectsMarked(input)

	// 2>&1 — stderr merges into stdout within the result.
	mergeErr := stderrFile == mergeErrIntoOutSentinel
	if mergeErr {
		stderrFile = ""
	}

	// Handle stdin: prefer piped stdin, fall back to < redirect file.
	var stdin string
	if pipedStdin != "" {
		stdin = pipedStdin
	} else if stdinFile != "" {
		if stdinFile == devNullPath {
			stdin = ""
		} else {
			data, err := os.ReadFile(ResolvePath(stdinFile))
			if err != nil {
				return CmdResult{"", fmt.Sprintf("%s: %s: %s\n", name, stdinFile, err.Error()), 1}
			}
			stdin = string(data)
		}
	}

	// Expand globs in args — quoted arguments are exempt.
	name = strings.TrimSpace(name)
	args := ExpandGlobsMarked(markedArgs)

	// Handle "export" specially — it's handled as a command.
	if name == "export" {
		return cmdExport(args, stdin)
	}

	// Handle variable assignments (VAR=value command).
	if strings.Contains(name, "=") && !strings.HasPrefix(name, "-") {
		parts := strings.SplitN(name, "=", 2)
		if len(parts) == 2 {
			// This is a variable assignment before a command
			// e.g., FOO=bar echo $FOO
			ShellEnv.Set(parts[0], os.ExpandEnv(parts[1]))
			if len(args) > 0 {
				name = args[0]
				args = args[1:]
			} else {
				return CmdResult{"", "", 0}
			}
		}
	}

	fn, ok := CmdRegistry[name]
	if !ok {
		return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
	}

	result := fn(args, stdin)

	// Handle 2>&1 — stderr joins stdout in the result.
	if mergeErr {
		if result.Stderr != "" {
			if result.Stdout != "" && !strings.HasSuffix(result.Stdout, "\n") {
				result.Stdout += "\n"
			}
			result.Stdout += result.Stderr
			result.Stderr = ""
		}
	}

	// Handle stdout redirect.
	if stdoutFile != "" {
		if stdoutFile != devNullPath {
			redirectPath := ResolvePath(stdoutFile)
			if appendStdout {
				existing := ""
				if data, err := os.ReadFile(redirectPath); err == nil {
					existing = string(data)
				}
				SyncWriteFile(redirectPath, existing+result.Stdout)
			} else {
				SyncWriteFile(redirectPath, result.Stdout)
			}
		}
		result.Stdout = ""
	}

	// Handle stderr redirect.
	if stderrFile != "" {
		if stderrFile != devNullPath {
			redirectPath := ResolvePath(stderrFile)
			if appendStderr {
				existing := ""
				if data, err := os.ReadFile(redirectPath); err == nil {
					existing = string(data)
				}
				SyncWriteFile(redirectPath, existing+result.Stderr)
			} else {
				SyncWriteFile(redirectPath, result.Stderr)
			}
		}
		result.Stderr = ""
	}

	return result
}

// ParseRedirects extracts command name, args, and redirect operators from a line.
// Returns: name, args, stdinFile, stdoutFile, stderrFile, appendStdout, appendStderr
func ParseRedirects(line string) (string, []string, string, string, string, bool, bool) {
	tokens := Tokenize(line, false)

	name := ""
	var args []string
	var stdinFile, stdoutFile, stderrFile string
	appendStdout := false
	appendStderr := false
	expectStdin := false
	expectStdout := false
	expectStderr := false
	bothRedirect := false // &> means same file for stdout and stderr
	mergeErrIntoOut := false

	// splitCompound peels a redirect operator fused to its target
	// ("2>/dev/null", ">>out", "2>&1") into the operator and remainder.
	splitCompound := func(tok string) (op, rest string, fused bool) {
		for _, cand := range []string{"2>>", "2>", "&>>", "&>", ">>", ">", "<"} {
			if strings.HasPrefix(tok, cand) {
				return cand, tok[len(cand):], true
			}
		}
		return "", tok, false
	}

	for i, tok := range tokens {
		if !expectStdin && !expectStdout && !expectStderr {
			if op, rest, fused := splitCompound(tok); fused && rest != "" && rest != "&1" {
				switch op {
				case "<":
					stdinFile = rest
				case "2>>":
					stderrFile, appendStderr = rest, true
				case "2>":
					stderrFile = rest
				case "&>>":
					stdoutFile, stderrFile, appendStdout, bothRedirect = rest, rest, true, true
				case "&>":
					stdoutFile, stderrFile, bothRedirect = rest, rest, true
				case ">>":
					stdoutFile, appendStdout = rest, true
				case ">":
					stdoutFile = rest
				}
				continue
			}
		}

		switch tok {
		case "<":
			expectStdin = true
			continue
		case ">", "1>":
			expectStdout = true
			appendStdout = false
			continue
		case ">>", "1>>":
			expectStdout = true
			appendStdout = true
			continue
		case "2>":
			expectStderr = true
			appendStderr = false
			continue
		case "2>>":
			expectStderr = true
			appendStderr = true
			continue
		case "2>&1":
			mergeErrIntoOut = true
			continue
		case "&>":
			expectStdout = true
			expectStderr = true
			bothRedirect = true
			appendStdout = false
			appendStderr = false
			continue
		}

		if expectStdin && stdinFile == "" {
			stdinFile = tok
			expectStdin = false
			continue
		}
		if expectStdout && stdoutFile == "" {
			stdoutFile = tok
			expectStdout = false
			if bothRedirect {
				stderrFile = tok
				expectStderr = false
				bothRedirect = false
			}
			continue
		}
		if expectStderr && stderrFile == "" {
			stderrFile = tok
			expectStderr = false
			continue
		}

		if i == 0 && !strings.HasPrefix(tok, "-") {
			name = tok
		} else {
			args = append(args, tok)
		}
	}

	if mergeErrIntoOut {
		// 2>&1 — signal the caller through the stderrFile slot: a merge,
		// not a real file redirect.
		stderrFile = mergeErrIntoOutSentinel
	}

	return name, args, stdinFile, stdoutFile, stderrFile, appendStdout, appendStderr
}

// Tokenize splits a command line into tokens, respecting quotes and escapes.
func Tokenize(line string, keepQuotes bool) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range line {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			if keepQuotes {
				current.WriteRune(ch)
			}
			continue
		}
		if ch == '\'' && !inDouble {
			if keepQuotes {
				current.WriteRune(ch)
				inSingle = !inSingle
			} else {
				inSingle = !inSingle
			}
			continue
		}
		if ch == '"' && !inSingle {
			if keepQuotes {
				current.WriteRune(ch)
				inDouble = !inDouble
			} else {
				inDouble = !inDouble
			}
			continue
		}
		if (ch == ' ' || ch == '\t') && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}
