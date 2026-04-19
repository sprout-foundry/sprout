package wasmshell

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// commandHistory stores the command history.
var commandHistory []string

const maxHistorySize = 1000

// ResetHistory clears the command history (useful for testing).
func ResetHistory() {
	commandHistory = nil
}

// addToHistory adds a command to history if it's not a duplicate of the last entry.
func addToHistory(cmd string) {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return
	}
	if len(commandHistory) > 0 && commandHistory[len(commandHistory)-1] == trimmed {
		return
	}
	commandHistory = append(commandHistory, trimmed)
	if len(commandHistory) > maxHistorySize {
		commandHistory = commandHistory[len(commandHistory)-maxHistorySize:]
	}
}

// ParseAndExecute is the main entry point for executing a command string.
// It handles pipes, redirects, and dispatches to the appropriate command.
func ParseAndExecute(input string) CmdResult {
	input = strings.TrimSpace(input)
	if input == "" {
		return CmdResult{"", "", 0}
	}

	// Handle comments
	if strings.HasPrefix(input, "#") {
		return CmdResult{"", "", 0}
	}

	addToHistory(input)

	// Expand environment variables in the input.
	input = os.ExpandEnv(input)

	// Handle tilde expansion in the input.
	if strings.HasPrefix(input, "~/") {
		input = ShellEnv.Get("HOME") + input[1:]
	} else if input == "~" {
		return CmdResult{ShellEnv.Get("HOME") + "\n", "", 0}
	}

	// Split by pipes, respecting quotes.
	pipeline := SplitPipeline(input)

	if len(pipeline) == 1 {
		// No pipes — check for redirects only.
		return executeWithRedirects(pipeline[0], "")
	}

	// Execute pipeline.
	return executePipeline(pipeline)
}

// SplitPipeline splits a command line by unquoted pipe characters.
func SplitPipeline(input string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range input {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			current.WriteRune(ch)
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
			continue
		}
		if ch == '|' && !inSingle && !inDouble {
			segments = append(segments, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}

	if current.Len() > 0 {
		segments = append(segments, strings.TrimSpace(current.String()))
	}

	return segments
}

// executePipeline runs commands connected by pipes.
func executePipeline(segments []string) CmdResult {
	// The last segment may have redirects.
	lastIdx := len(segments) - 1
	pipeSegments := segments[:lastIdx]
	lastSegment := segments[lastIdx]

	var stdin string

	for _, seg := range pipeSegments {
		name, args, _, _, _, _, _ := ParseRedirects(seg)
		name = strings.TrimSpace(name)
		args = ExpandGlobs(args)

		if fn, ok := CmdRegistry[name]; ok {
			result := fn(args, stdin)
			if result.ExitCode != 0 {
				return result
			}
			stdin = result.Stdout
		} else {
			return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
		}
	}

	// Last segment gets redirect handling, passing piped stdin.
	return executeWithRedirects(lastSegment, stdin)
}

// executeWithRedirects parses and executes a single command with redirects.
// If pipedStdin is non-empty, it takes precedence over any < redirect file.
func executeWithRedirects(input string, pipedStdin string) CmdResult {
	name, args, stdinFile, stdoutFile, stderrFile, appendStdout, appendStderr := ParseRedirects(input)

	// Handle stdin: prefer piped stdin, fall back to < redirect file.
	var stdin string
	if pipedStdin != "" {
		stdin = pipedStdin
	} else if stdinFile != "" {
		data, err := os.ReadFile(ResolvePath(stdinFile))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("%s: %s: %s\n", name, stdinFile, err.Error()), 1}
		}
		stdin = string(data)
	}

	// Expand globs in args.
	name = strings.TrimSpace(name)
	args = ExpandGlobs(args)

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

	// Handle stdout redirect.
	if stdoutFile != "" {
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
		result.Stdout = ""
	}

	// Handle stderr redirect.
	if stderrFile != "" {
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

	for i, tok := range tokens {
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

// HistorySearch searches command history for a prefix.
func HistorySearch(prefix string) []string {
	var results []string
	for i := len(commandHistory) - 1; i >= 0; i-- {
		if strings.HasPrefix(commandHistory[i], prefix) {
			results = append(results, commandHistory[i])
		}
	}
	return results
}

// JSONResult marshals a CmdResult to JSON string.
func JSONResult(r CmdResult) string {
	data, _ := json.Marshal(r)
	return string(data)
}
