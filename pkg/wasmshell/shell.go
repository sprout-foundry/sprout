package wasmshell

import (
	"encoding/json"
	"strings"
)

// commandHistory stores the command history.
var commandHistory []string

const maxHistorySize = 1000

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

// chainSegment is one pipeline element of a && / || / ; chain.
type chainSegment struct {
	op   string // "", "&&", "||", ";"
	text string
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
