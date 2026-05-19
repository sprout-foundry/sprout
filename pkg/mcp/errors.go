package mcp

import (
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// InvalidArgsError indicates that tool arguments failed JSON Schema validation.
type InvalidArgsError struct {
	// Tool is the name of the MCP tool whose arguments were invalid.
	Tool string

	// Server is the name of the MCP server that provides the tool.
	Server string

	// Wrapped is the underlying validation error from the JSON Schema
	// library. Callers can use Unwrap() or errors.Is/errors.As to
	// inspect the raw validation cause.
	Wrapped error
}

// Error returns a human-readable description of the validation failure.
func (e *InvalidArgsError) Error() string {
	return fmt.Sprintf("invalid arguments for tool %s/%s: %v", e.Server, e.Tool, e.Wrapped)
}

// Unwrap returns the underlying validation error so that callers can
// use errors.Is, errors.As, or errors.Unwrap for deeper inspection.
func (e *InvalidArgsError) Unwrap() error {
	return e.Wrapped
}

// FormatForLLM returns a concise, LLM-readable summary of the validation
// errors. Each error is listed as a bullet point showing the field path
// and reason. If Wrapped is nil, falls back to Error().
func (e *InvalidArgsError) FormatForLLM() string {
	if e.Wrapped == nil {
		return e.Error()
	}
	return formatValidationError(e.Wrapped, e.Tool, e.Server)
}

// formatPath converts a JSON pointer (e.g. "/nested/field") to a readable
// path string (e.g. "'nested.field'"). Numeric segments become array
// indices (e.g. "/items/0/name" → "'items[0].name'").
func formatPath(ptr string) string {
	if ptr == "" {
		return ""
	}
	// Remove leading slash, then split into segments.
	seg := strings.TrimPrefix(ptr, "/")
	parts := strings.Split(seg, "/")

	var b strings.Builder
	b.WriteByte('\'')
	for i, p := range parts {
		if i > 0 {
			// Determine separator: if p is a numeric index, use [N]; otherwise use .p
			// Check if the segment looks like an array index (digits only).
			isIndex := true
			for _, c := range p {
				if c < '0' || c > '9' {
					isIndex = false
					break
				}
			}
			if isIndex {
				b.WriteString("[")
				b.WriteString(p)
				b.WriteByte(']')
			} else {
				b.WriteByte('.')
				b.WriteString(p)
			}
		} else {
			b.WriteString(p)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// formatValidationError walks the ValidationError tree and returns a concise
// multi-line message suitable for an LLM to read.
func formatValidationError(err error, toolName, serverName string) string {
	verr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		// Not a validation error; fall back to a generic message.
		return fmt.Sprintf("Tool '%s' from server '%s' validation failed:\n  - %s",
			toolName, serverName, err.Error())
	}

	basic := verr.BasicOutput()

	var bullets []string
	for _, unit := range basic.Errors {
		if unit.Error == nil {
			continue
		}
		reason := unit.Error.String()
		path := formatPath(unit.InstanceLocation)

		if path != "" {
			bullets = append(bullets, fmt.Sprintf("%s %s", path, reason))
		} else {
			// Top-level error (e.g. "missing property 'url'") — use reason directly.
			bullets = append(bullets, reason)
		}
	}

	// Handle edge case where BasicOutput has no sub-errors but has a root error.
	if len(bullets) == 0 && basic.Error != nil {
		bullets = append(bullets, basic.Error.String())
	}

	if len(bullets) == 0 {
		return fmt.Sprintf("Tool '%s' from server '%s' validation failed: (unknown error)", toolName, serverName)
	}

	return fmt.Sprintf("Tool '%s' from server '%s' validation failed:\n  - %s",
		toolName, serverName, strings.Join(bullets, "\n  - "))
}
