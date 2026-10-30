package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Shared utility functions for tool handlers

// extractGitSubcommand extracts the subcommand from a git command string for display purposes.
func extractGitSubcommand(command string) string {
	parts := strings.Fields(strings.TrimSpace(command))
	if len(parts) < 2 || parts[0] != "git" {
		return "unknown"
	}
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if strings.HasPrefix(part, "-") {
			if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
				i++ // skip the flag value
			}
			continue
		}
		return part
	}
	return "unknown"
}

// isGitCommitSubcommand checks if a git command is specifically a commit operation
// (as opposed to other write operations like push, merge, etc.)
func isGitCommitSubcommand(command string) bool {
	parts := shellSplit(strings.TrimSpace(command))
	if len(parts) < 2 || parts[0] != "git" {
		return false
	}
	// Skip leading flags and -c key=value config options to find the actual subcommand
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "-c" {
			// -c takes the next argument as key=value, skip it too
			i++
			continue
		}
		if strings.HasPrefix(part, "-") {
			continue
		}
		subcommand := strings.TrimPrefix(strings.TrimPrefix(part, "--"), "-")
		return subcommand == "commit"
	}
	return false
}

// positiveIntArgMax bounds a numeric argument so a hallucinated or hostile
// value cannot overflow a later int or Duration conversion.
const positiveIntArgMax = 1_000_000_000

// positiveIntArg reads an optional positive integer argument, returning 0
// when it is absent, zero, negative, non-integral, or implausibly large. JSON
// numbers arrive as float64, so an integral float is accepted; a fractional
// value is rejected rather than truncated, since "max_iterations": 200.5 is a
// caller mistake.
func positiveIntArg(args map[string]interface{}, key string) int {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		if n > 0 && n <= positiveIntArgMax && n == float64(int64(n)) {
			return int(n)
		}
	case int:
		if n > 0 && n <= positiveIntArgMax {
			return n
		}
	case int64:
		if n > 0 && n <= positiveIntArgMax {
			return int(n)
		}
	}
	return 0
}

// subagentTimeoutArg reads an optional timeout in seconds, clamped to
// subagentTimeoutCeiling. Returns 0 when unset or invalid.
func subagentTimeoutArg(args map[string]interface{}, key string) time.Duration {
	n := positiveIntArg(args, key)
	if n <= 0 {
		return 0
	}
	if maxSecs := int(subagentTimeoutCeiling / time.Second); n > maxSecs {
		n = maxSecs
	}
	return time.Duration(n) * time.Second
}

// convertToString safely converts a parameter to string with proper error handling
func convertToString(param interface{}, paramName string) (string, error) {
	switch v := param.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case map[string]interface{}:
		// If it's a map, try to convert to JSON string
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", agenterrors.Wrapf(err, "parameter '%s' is an object that cannot be converted to string", paramName)
		}
		return string(jsonBytes), nil
	case nil:
		return "", agenterrors.NewValidation("parameter '"+paramName+"' is missing or null", nil)
	default:
		return "", agenterrors.NewValidation(fmt.Sprintf("parameter '%s' has invalid type %T, expected string", paramName, param), nil)
	}
}

// extractGitCommitArgs parses a `git commit ...` command line and extracts
// the message from -m or --message flags. The command comes from an LLM tool
// argument, which may include shell-style quoting (single or double quotes).
// We support basic shell quoting so that `git commit -m "fix: typo"` correctly
// extracts `fix: typo`.
//
// Returns the extracted message (may be empty if no -m/--message flag found).
func extractGitCommitArgs(command string) string {
	tokens := shellSplit(command)
	message := ""

	for i := 0; i < len(tokens)-1; i++ {
		switch tokens[i] {
		case "-m", "--message":
			// Git supports multiple -m flags to build multi-paragraph messages.
			// Each -m becomes a separate paragraph in the commit message.
			if message != "" {
				message += "\n\n"
			}
			message += tokens[i+1]
			i++ // skip the next token (it's the message value)
		}
	}

	return message
}

// shellSplit performs basic shell-style word splitting that respects
// single and double quotes. This is intentionally minimal — it handles
// the common patterns LLMs use when constructing git commit commands.
// It does NOT handle escape sequences, backticks, or variable expansion.
func shellSplit(s string) []string {
	var tokens []string
	var current strings.Builder
	var inQuote rune // 0 = not in quote, '"' or '\'' == in quote
	justClosedQuote := false

	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
				justClosedQuote = true
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if current.Len() > 0 || justClosedQuote {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			justClosedQuote = false
		default:
			current.WriteRune(r)
			justClosedQuote = false
		}
	}

	if current.Len() > 0 || justClosedQuote {
		tokens = append(tokens, current.String())
	}

	return tokens
}
