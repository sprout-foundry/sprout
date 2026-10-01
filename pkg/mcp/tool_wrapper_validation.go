package mcp

// tool_wrapper_validation.go — the JSON-Schema argument-validation half of
// MCPToolWrapper (split from tool_wrapper.go): compileSchema, ValidateArgs,
// extractValidationFailures, extractLocationAndReason, and the string-parsing
// leaf helpers (splitLines, trimSpace, normalizePath, indexOf, replaceAll).
// The wrapper lifecycle / Execute / AgentTool conversion stays in
// tool_wrapper.go.
import (
	"fmt"
	"log/slog"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// compileSchema lazily compiles the tool's InputSchema into a jsonschema.Schema.
// Returns nil if there is no schema to compile (nil/empty InputSchema).
// Compile errors are logged once and fail-open to avoid breaking tools with
// non-standard schemas.
func (w *MCPToolWrapper) compileSchema() *jsonschema.Schema {
	if w.mcpTool.InputSchema == nil {
		return nil
	}

	// Check if the schema is effectively empty (only "type": "object" or empty map)
	schema := w.mcpTool.InputSchema
	if len(schema) == 0 {
		return nil
	}
	if len(schema) == 1 {
		if _, ok := schema["type"]; ok {
			return nil
		}
	}

	// Return cached schema if already compiled
	if s, ok := w.compiledSchema.(*jsonschema.Schema); ok {
		return s
	}

	w.schemaCompileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("schema", schema); err != nil {
			slog.Warn("failed to add schema for compilation, validation will be skipped",
				"tool", w.mcpTool.Name, "server", w.mcpTool.ServerName, "err", err)
			return
		}
		s, err := compiler.Compile("schema")
		if err != nil {
			slog.Warn("failed to compile schema, validation will be skipped",
				"tool", w.mcpTool.Name, "server", w.mcpTool.ServerName, "err", err)
			return
		}
		w.compiledSchema = s
	})

	if s, ok := w.compiledSchema.(*jsonschema.Schema); ok {
		return s
	}
	return nil
}

// ValidateArgs validates arguments against the tool's input schema using JSON
// Schema validation. Returns nil if the arguments are valid or if there is no
// schema to validate against. Returns an InvalidArgsError with structured
// failures on validation errors.
func (w *MCPToolWrapper) ValidateArgs(args map[string]interface{}) error {
	schema := w.compileSchema()
	if schema == nil {
		return nil
	}

	if args == nil {
		args = make(map[string]interface{})
	}

	err := schema.Validate(args)
	if err == nil {
		return nil
	}

	// Extract validation failures from the jsonschema result
	failures := extractValidationFailures(err)

	// Structured log entry for validation failure (cooperates with SP-008 structured logging)
	errMsgs := make([]string, len(failures))
	for i, f := range failures {
		errMsgs[i] = fmt.Sprintf("%s: %s", f.Path, f.Reason)
	}
	slog.Warn("MCP tool arguments failed schema validation",
		"tool", w.mcpTool.Name,
		"server", w.mcpTool.ServerName,
		"errors", errMsgs,
		"total_failures", mcpValidationFailures.Add(1),
	)

	return &InvalidArgsError{
		Tool:     w.mcpTool.Name,
		Server:   w.mcpTool.ServerName,
		Failures: failures,
		wrapped:  err,
	}
}

// extractValidationFailures converts a jsonschema validation error into
// structured ValidationFailure entries suitable for LLM consumption.
// jsonschema/v6 returns multi-line errors like:
//
//	"jsonschema validation failed with 'file:///.../schema#'\n- at '': missing property 'query'\n- at '/query': got number, want string"
func extractValidationFailures(err error) []ValidationFailure {
	if err == nil {
		return nil
	}

	detail := err.Error()
	lines := splitLines(detail)
	var failures []ValidationFailure

	for _, line := range lines {
		line = trimSpace(line)
		// Remove leading "- " prefix
		if len(line) >= 2 && line[:2] == "- " {
			line = line[2:]
		}

		if loc, ok := extractLocationAndReason(line); ok {
			failures = append(failures, loc)
		}
	}

	if len(failures) == 0 {
		// Fallback: treat the entire error as a root-level failure
		failures = append(failures, ValidationFailure{
			Path:   "(root)",
			Reason: detail,
		})
	}

	return failures
}

// extractLocationAndReason attempts to parse a jsonschema validation error
// string to extract the JSON pointer path and the validation reason.
func extractLocationAndReason(detail string) (ValidationFailure, bool) {
	// jsonschema/v6 error format is like:
	// "jsonschema validation failed with 'file:///.../schema#'\n- at '': missing property 'query'\n- at '/query': got number, want string"
	// or
	// "at '/query': got number, want string"

	lines := splitLines(detail)
	for _, line := range lines {
		// Skip lines that don't contain location info
		line = trimSpace(line)
		// Remove leading "- " prefix
		if len(line) >= 2 && line[:2] == "- " {
			line = line[2:]
		}

		// Check for "at '<path>': <message>" format (jsonschema/v6 standard)
		const atPrefix = "at '"
		if len(line) > len(atPrefix) && line[:len(atPrefix)] == atPrefix {
			// Find closing quote for path
			closeQuote := indexOf(line[len(atPrefix):], "': ")
			if closeQuote >= 0 {
				path := line[len(atPrefix) : len(atPrefix)+closeQuote]
				reason := line[len(atPrefix)+closeQuote+3:] // skip "': "
				path = normalizePath(path)
				return ValidationFailure{Path: path, Reason: reason}, true
			}
		}

		// Fallback: "at <location>: <message>" format (no quotes)
		const atPrefixNoQuote = "at "
		if len(line) > len(atPrefixNoQuote) && line[:len(atPrefixNoQuote)] == atPrefixNoQuote {
			colonIdx := indexOf(line[len(atPrefixNoQuote):], ": ")
			if colonIdx >= 0 {
				path := line[len(atPrefixNoQuote) : len(atPrefixNoQuote)+colonIdx]
				reason := line[len(atPrefixNoQuote)+colonIdx+2:]
				path = normalizePath(path)
				return ValidationFailure{Path: path, Reason: reason}, true
			}
		}

		// Fallback: "location: 'path'; error: 'message'" format
		const locPrefix = "location: '"
		locIdx := indexOf(line, locPrefix)
		if locIdx >= 0 {
			pathStart := locIdx + len(locPrefix)
			errIdx := indexOf(line[pathStart:], "'; error: '")
			if errIdx >= 0 {
				path := line[pathStart : pathStart+errIdx]
				reasonStart := pathStart + errIdx + len("'; error: '")
				// Trim trailing ' from the reason if present
				reasonEnd := len(line)
				if reasonEnd > reasonStart && line[reasonEnd-1] == '\'' {
					reasonEnd--
				}
				reason := line[reasonStart:reasonEnd]
				path = normalizePath(path)
				return ValidationFailure{Path: path, Reason: reason}, true
			}
		}
	}

	return ValidationFailure{}, false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	// Trim leading space/tab
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	// Trim trailing space/tab
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// normalizePath makes a JSON pointer path more readable for LLMs.
func normalizePath(path string) string {
	if path == "" || path == "#" || path == "." {
		return "(root)"
	}
	// Convert JSON pointer format (/foo/bar) to dot notation (.foo.bar)
	if len(path) > 0 && path[0] == '/' {
		path = "." + path[1:]
	}
	// Replace / with . for nested paths
	path = replaceAll(path, "/", ".")
	return path
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func replaceAll(s, old, new string) string {
	result := ""
	for len(s) > 0 {
		i := indexOf(s, old)
		if i < 0 {
			result += s
			break
		}
		result += s[:i] + new
		s = s[i+len(old):]
	}
	return result
}
