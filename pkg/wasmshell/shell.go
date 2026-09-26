package wasmshell

// shell.go — the WASM shell core: command history, the entry point
// (ParseAndExecute), and the pipeline / chain execution layer (SplitPipeline,
// SplitChains, executeChain, executePipeline). The tokenizer, glob expansion,
// and redirect parsing / execution live in shell_tokenize.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// commandHistory stores the command history.
var commandHistory []string

const maxHistorySize = 1000

// devNullPath is the WASM shell's discard sink — writes are dropped, reads
// return empty. Agents habitually redirect noise here ("2>/dev/null"), so
// the shell must understand it without touching the MEMFS tree.
const devNullPath = "/dev/null"

// mergeErrIntoOutSentinel is what ParseRedirects records for a 2>&1 that
// has no real file target — a merge, not a redirect.
const mergeErrIntoOutSentinel = "__merge__"

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
// It handles chains (&&, ||, ;), pipes, redirects, and dispatches to the
// appropriate command.
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

	// Split by chain operators (&&, ||, ;) — the top grammar level.
	chains := SplitChains(input)
	if len(chains) > 1 {
		return executeChain(chains)
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

// SplitChains splits a command line by unquoted && / || / ; separators.
// Quotes and escapes suppress splitting, and separator characters inside
// quotes are preserved in the segment text. The first segment carries an
// empty op; later ones carry the operator that joins them to the previous
// segment.
func SplitChains(input string) []chainSegment {
	var segments []chainSegment
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	nextOp := ""
	i := 0
	runes := []rune(input)

	flush := func() {
		text := strings.TrimSpace(current.String())
		current.Reset()
		if text == "" && len(segments) == 0 {
			// Leading separator with no preceding text — nothing to record.
			return
		}
		segments = append(segments, chainSegment{op: nextOp, text: text})
		nextOp = ""
	}

	for i < len(runes) {
		ch := runes[i]

		if escaped {
			current.WriteRune(ch)
			escaped = false
		} else if ch == '\\' {
			current.WriteRune(ch)
			escaped = true
		} else if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
		} else if !inSingle && !inDouble {
			switch {
			case ch == ';':
				flush()
				nextOp = ";"
				i++
				continue
			case ch == '&' && i+1 < len(runes) && runes[i+1] == '&':
				flush()
				nextOp = "&&"
				i += 2
				continue
			case ch == '|' && i+1 < len(runes) && runes[i+1] == '|':
				flush()
				nextOp = "||"
				i += 2
				continue
			default:
				current.WriteRune(ch)
			}
		} else {
			current.WriteRune(ch)
		}
		i++
	}

	flush()

	return segments
}

// executeChain runs a sequence of pipeline segments joined by && / || / ;.
// Semantics match POSIX: && runs when the previous exit code is 0, || runs
// when it is non-zero, ; always runs. Empty segments are skipped. stdout
// and stderr accumulate; the exit code is the last executed command's.
func executeChain(chains []chainSegment) CmdResult {
	var stdout, stderr strings.Builder
	var lastExit int
	executed := false

	for _, seg := range chains {
		if seg.op == "&&" && lastExit != 0 {
			continue
		}
		if seg.op == "||" && lastExit == 0 {
			continue
		}
		if seg.text == "" {
			continue
		}

		result := parseAndExecutePipeline(seg.text)
		executed = true
		stdout.WriteString(result.Stdout)
		stderr.WriteString(result.Stderr)
		lastExit = result.ExitCode
	}

	if !executed {
		return CmdResult{"", "", 0}
	}
	return CmdResult{stdout.String(), stderr.String(), lastExit}
}

// parseAndExecutePipeline handles the pipeline-with-redirects layer under
// the chain layer: split by pipes, run each stage, honor redirects.
func parseAndExecutePipeline(segment string) CmdResult {
	pipeline := SplitPipeline(segment)
	if len(pipeline) == 1 {
		return executeWithRedirects(pipeline[0], "")
	}
	return executePipeline(pipeline)
}

// chainSegment is one pipeline element of a && / || / ; chain.
type chainSegment struct {
	op   string // "", "&&", "||", ";"
	text string
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
