package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// mcpValidationFailures tracks the total number of MCP tool argument validation
// failures across all servers. Use GetMCPValidationFailures() to read the counter.
var mcpValidationFailures atomic.Int64

// GetMCPValidationFailures returns the total count of MCP argument validation
// failures since process start. Useful for monitoring if a particular server
// is producing bad arguments at rate (cross-reference with structured logs).
func GetMCPValidationFailures() int64 {
	return mcpValidationFailures.Load()
}

// Local constants to avoid importing tools package and creating cycle
const (
	CategoryWeb             = "web"
	PermissionNetworkAccess = "network_access"
)

// Local type definitions to avoid import cycle
type Parameters struct {
	Args   []string
	Kwargs map[string]interface{}
}

type Result struct {
	Success       bool                   `json:"success"`
	Output        interface{}            `json:"output"`
	Errors        []string               `json:"errors"`
	Metadata      map[string]interface{} `json:"metadata"`
	ExecutionTime time.Duration          `json:"execution_time"`
}

// NewMCPToolWrapper creates a new wrapper for an MCP tool to implement the standard Tool interface
func NewMCPToolWrapper(mcpTool MCPTool, manager MCPManager) *MCPToolWrapper {
	return &MCPToolWrapper{
		mcpTool:   mcpTool,
		manager:   manager,
		category:  CategoryWeb,      // Default category for MCP tools
		timeout:   30 * time.Second, // Default timeout
		available: true,
	}
}

// Name returns the unique name of the tool
func (w *MCPToolWrapper) Name() string {
	return fmt.Sprintf("mcp_%s_%s", w.mcpTool.ServerName, w.mcpTool.Name)
}

// Description returns a human-readable description of what the tool does
func (w *MCPToolWrapper) Description() string {
	desc := w.mcpTool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from %s server", w.mcpTool.Name, w.mcpTool.ServerName)
	}
	return fmt.Sprintf("[MCP:%s] %s", w.mcpTool.ServerName, desc)
}

// Category returns the category this tool belongs to
func (w *MCPToolWrapper) Category() string {
	return w.category
}

// Execute runs the tool with the given context and parameters
func (w *MCPToolWrapper) Execute(ctx context.Context, params Parameters) (*Result, error) {
	startTime := time.Now()

	// Extract arguments from parameters
	args := params.Kwargs
	if args == nil {
		args = make(map[string]interface{})
	}

	// Validate arguments against the tool's input schema before making the call
	if err := w.ValidateArgs(args); err != nil {
		return &Result{
			Success:       false,
			Output:        FormatForLLM(err.(*InvalidArgsError)),
			Errors:        []string{err.Error()},
			Metadata:      map[string]interface{}{"validation_error": true},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Call the MCP tool
	result, err := w.manager.CallTool(ctx, w.mcpTool.ServerName, w.mcpTool.Name, args)
	if err != nil {
		return &Result{
			Success:       false,
			Errors:        []string{err.Error()},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Convert MCP result to standard tool result
	if result.IsError {
		var errors []string
		for _, content := range result.Content {
			if content.Type == "text" {
				errors = append(errors, content.Text)
			}
		}
		return &Result{
			Success:       false,
			Errors:        errors,
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Combine content into output
	var output interface{}
	if len(result.Content) == 1 {
		content := result.Content[0]
		switch content.Type {
		case "text":
			output = content.Text
		case "image":
			output = map[string]interface{}{
				"type":     content.Type,
				"data":     content.Data,
				"mimeType": content.MimeType,
			}
		case "resource":
			output = map[string]interface{}{
				"type":     content.Type,
				"text":     content.Text,
				"data":     content.Data,
				"mimeType": content.MimeType,
			}
		default:
			output = content.Text
		}
	} else if len(result.Content) > 1 {
		// Multiple content pieces
		outputs := make([]interface{}, len(result.Content))
		for i, content := range result.Content {
			switch content.Type {
			case "text":
				outputs[i] = content.Text
			case "image", "resource":
				outputs[i] = map[string]interface{}{
					"type":     content.Type,
					"text":     content.Text,
					"data":     content.Data,
					"mimeType": content.MimeType,
				}
			default:
				outputs[i] = content.Text
			}
		}
		output = outputs
	} else {
		output = ""
	}

	metadata := map[string]interface{}{
		"server_name":   w.mcpTool.ServerName,
		"tool_name":     w.mcpTool.Name,
		"content_count": len(result.Content),
		"mcp_source":    true,
	}

	// Add annotations if present
	for _, content := range result.Content {
		if content.Annotations != nil && len(content.Annotations) > 0 {
			metadata["annotations"] = content.Annotations
			break // Just take the first one with annotations
		}
	}

	return &Result{
		Success:       true,
		Output:        output,
		Metadata:      metadata,
		ExecutionTime: time.Since(startTime),
	}, nil
}

// CanExecute checks if the tool can be executed with the current context
func (w *MCPToolWrapper) CanExecute(ctx context.Context, params Parameters) bool {
	// Check if the server is available
	server, exists := w.manager.GetServer(w.mcpTool.ServerName)
	if !exists || !server.IsRunning() {
		return false
	}

	// Validate arguments against the tool's input schema
	args := params.Kwargs
	if w.ValidateArgs(args) != nil {
		return false
	}

	return true
}

// RequiredPermissions returns the permissions needed to execute this tool
func (w *MCPToolWrapper) RequiredPermissions() []string {
	// MCP tools typically need network access to communicate with servers
	permissions := []string{PermissionNetworkAccess}

	// Add permissions based on tool category or server type
	if w.mcpTool.ServerName == "github" {
		permissions = append(permissions, "mcp_github_access")
	}

	// Could analyze the tool's input schema to determine additional permissions
	return permissions
}

// EstimatedDuration returns an estimate of how long the tool will take to execute
func (w *MCPToolWrapper) EstimatedDuration() time.Duration {
	return w.timeout
}

// IsAvailable checks if the tool is available in the current environment
func (w *MCPToolWrapper) IsAvailable() bool {
	if !w.available {
		return false
	}

	// Check if the server is running
	server, exists := w.manager.GetServer(w.mcpTool.ServerName)
	return exists && server.IsRunning()
}

// SetCategory allows customizing the tool category
func (w *MCPToolWrapper) SetCategory(category string) {
	w.category = category
}

// SetTimeout allows customizing the tool timeout
func (w *MCPToolWrapper) SetTimeout(timeout time.Duration) {
	w.timeout = timeout
}

// SetAvailable allows enabling/disabling the tool
func (w *MCPToolWrapper) SetAvailable(available bool) {
	w.available = available
}

// GetMCPTool returns the underlying MCP tool
func (w *MCPToolWrapper) GetMCPTool() MCPTool {
	return w.mcpTool
}

// GetServerName returns the server name
func (w *MCPToolWrapper) GetServerName() string {
	return w.mcpTool.ServerName
}

// GetToolName returns the original tool name (without MCP prefix)
func (w *MCPToolWrapper) GetToolName() string {
	return w.mcpTool.Name
}

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

// AgentTool represents the agent's tool format for compatibility
type AgentTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

// ToAgentTool converts the MCP tool to the agent's Tool format
func (w *MCPToolWrapper) ToAgentTool() AgentTool {
	return AgentTool{
		Type: "function",
		Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{
			Name:        w.Name(),
			Description: w.Description(),
			Parameters:  w.mcpTool.InputSchema,
		},
	}
}
