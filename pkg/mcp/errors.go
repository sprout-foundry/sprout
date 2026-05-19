package mcp

import (
	"fmt"
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
