package mcp

import (
	"fmt"
	"strings"
)

// InvalidArgsError is returned when tool arguments fail JSON Schema validation.
// It contains structured validation failures that can be formatted for LLM consumption.
type InvalidArgsError struct {
	// Tool is the name of the tool that failed validation
	Tool string
	// Server is the MCP server name hosting the tool
	Server string
	// Failures contains the individual validation errors
	Failures []ValidationFailure
	// wrapped holds the underlying error from the jsonschema library (if any)
	wrapped error
}

// ValidationFailure represents a single validation error at a specific field path.
type ValidationFailure struct {
	// Path is the JSON pointer path to the failing field (e.g., ".query", ".filters[0]")
	Path string
	// Reason is a human-readable explanation of why validation failed
	Reason string
}

// Error returns a concise, LLM-friendly validation error message enumerating
// field paths and reasons instead of raw jsonschema output. This format enables
// the model to self-correct on the next iteration.
func (e *InvalidArgsError) Error() string {
	if len(e.Failures) == 0 {
		return fmt.Sprintf("invalid arguments for tool %q on server %q", e.Tool, e.Server)
	}

	var parts []string
	for _, f := range e.Failures {
		path := f.Path
		if path == "" || path == "." || path == "#" {
			path = "(root)"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", path, f.Reason))
	}

	return fmt.Sprintf("invalid arguments for tool %q on server %q:\n- %s",
		e.Tool, e.Server, strings.Join(parts, "\n- "))
}

// Unwrap returns the underlying error for errors.Is/errors.As compatibility.
func (e *InvalidArgsError) Unwrap() error {
	return e.wrapped
}

// FormatForLLM returns a structured, LLM-friendly message that enumerates
// failing field paths and human-readable reasons. This is designed to be used
// as the tool result so the model can self-correct on the next iteration.
func FormatForLLM(err *InvalidArgsError) string {
	if err == nil {
		return ""
	}

	if len(err.Failures) == 0 {
		return fmt.Sprintf("Validation failed for tool %q on server %q", err.Tool, err.Server)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Validation failed for tool %q on server %q:\n", err.Tool, err.Server))
	sb.WriteString("The following arguments are invalid:\n")
	for i, f := range err.Failures {
		path := f.Path
		if path == "" || path == "." || path == "#" {
			path = "(root)"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s: %s\n", i+1, path, f.Reason))
	}
	sb.WriteString("\nPlease correct these arguments and try again.")
	return sb.String()
}
