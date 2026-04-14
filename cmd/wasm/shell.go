//go:build js && wasm

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// commandHistory stores the command history.
var commandHistory []string

const maxHistorySize = 1000

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

// parseAndExecute is the main entry point for executing a command string.
// It handles pipes, redirects, and dispatches to the appropriate command.
func parseAndExecute(input string) CmdResult {
	input = strings.TrimSpace(input)
	if input == "" {
		return CmdResult{"", "", 0}
	}

	// Handle export as a special case (it's an assignment, not a command)
	// Handle comments
	if strings.HasPrefix(input, "#") {
		return CmdResult{"", "", 0}
	}

	addToHistory(input)

	// Expand environment variables in the input.
	input = os.ExpandEnv(input)

	// Handle tilde expansion in the input.
	if strings.HasPrefix(input, "~/") {
		input = shellEnv.Get("HOME") + input[1:]
	} else if input == "~" {
		return CmdResult{shellEnv.Get("HOME") + "\n", "", 0}
	}

	// Split by pipes, respecting quotes.
	pipeline := splitPipeline(input)

	if len(pipeline) == 1 {
		// No pipes — check for redirects only.
		return executeWithRedirects(pipeline[0])
	}

	// Execute pipeline.
	return executePipeline(pipeline)
}

// splitPipeline splits a command line by unquoted pipe characters.
func splitPipeline(input string) []string {
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
		name, args, _, _, _, _ := parseRedirects(seg)
		name = strings.TrimSpace(name)
		args = expandGlobs(args)

		if fn, ok := cmdRegistry[name]; ok {
			result := fn(args, stdin)
			if result.ExitCode != 0 {
				return result
			}
			stdin = result.Stdout
		} else {
			return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
		}
	}

	// Last segment gets redirect handling.
	return executeWithRedirects(lastSegment)
}

// executeWithRedirects parses and executes a single command with redirects.
func executeWithRedirects(input string) CmdResult {
	name, args, stdinFile, stdoutFile, stderrFile, appendStdout := parseRedirects(input)

	// Handle stdin redirect.
	var stdin string
	if stdinFile != "" {
		data, err := os.ReadFile(resolvePath(stdinFile))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("%s: %s: %s\n", name, stdinFile, err.Error()), 1}
		}
		stdin = string(data)
	}

	// Expand globs in args.
	name = strings.TrimSpace(name)
	args = expandGlobs(args)

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
			shellEnv.Set(parts[0], os.ExpandEnv(parts[1]))
			if len(args) > 0 {
				name = args[0]
				args = args[1:]
			} else {
				return CmdResult{"", "", 0}
			}
		}
	}

	fn, ok := cmdRegistry[name]
	if !ok {
		return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
	}

	result := fn(args, stdin)

	// Handle stdout redirect.
	if stdoutFile != "" {
		redirectPath := resolvePath(stdoutFile)
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
		redirectPath := resolvePath(stderrFile)
		SyncWriteFile(redirectPath, result.Stderr)
		result.Stderr = ""
	}

	return result
}

// parseRedirects extracts command name, args, and redirect operators from a line.
// Returns: name, args, stdinFile, stdoutFile, stderrFile, appendStdout
func parseRedirects(line string) (string, []string, string, string, string, bool) {
	tokens := tokenize(line, false)

	name := ""
	var args []string
	var stdinFile, stdoutFile, stderrFile string
	appendStdout := false
	expectStdin := false
	expectStdout := false
	expectStderr := false

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
			continue
		case "2>>":
			expectStderr = true
			appendStdout = true // reused flag for append
			continue
		case "&>":
			// Redirect both stdout and stderr
			expectStdout = true
			expectStderr = false
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

	return name, args, stdinFile, stdoutFile, stderrFile, appendStdout
}

// tokenize splits a command line into tokens, respecting quotes and escapes.
func tokenize(line string, keepQuotes bool) []string {
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
			if !inSingle && !keepQuotes {
				inSingle = true
			} else if inSingle && !keepQuotes {
				inSingle = false
			} else {
				current.WriteRune(ch)
			}
			if keepQuotes {
				current.WriteRune(ch)
			}
			continue
		}
		if ch == '"' && !inSingle {
			if !inDouble && !keepQuotes {
				inDouble = true
			} else if inDouble && !keepQuotes {
				inDouble = false
			} else {
				current.WriteRune(ch)
			}
			if keepQuotes {
				current.WriteRune(ch)
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

// historySearch searches command history for a prefix.
func historySearch(prefix string) []string {
	var results []string
	for i := len(commandHistory) - 1; i >= 0; i-- {
		if strings.HasPrefix(commandHistory[i], prefix) {
			results = append(results, commandHistory[i])
		}
	}
	return results
}

// scanLines is a utility alias kept for pipeline compatibility.
var _ = bufio.ScanLines

// jsonResult marshals a CmdResult to JSON string.
func jsonResult(r CmdResult) string {
	data, _ := json.Marshal(r)
	return string(data)
}
