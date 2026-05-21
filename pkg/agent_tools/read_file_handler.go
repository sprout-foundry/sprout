// ReadFileHandler implements ToolHandler for the read_file tool.
//
// This is the new-style, registry-based handler that replaces the legacy
// switch-based dispatch in pkg/agent/tool_handlers_file.go.
package tools

import (
	"context"
	"fmt"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ReadFileHandler implements ToolHandler for reading file contents.
// It supports optional line ranges via the view_range parameter.
type ReadFileHandler struct{}

// NewReadFileHandler returns a ready-to-use ReadFileHandler.
func NewReadFileHandler() *ReadFileHandler {
	return &ReadFileHandler{}
}

// Name returns the tool name "read_file".
func (h *ReadFileHandler) Name() string {
	return "read_file"
}

// Definition returns the LLM-facing tool definition.
func (h *ReadFileHandler) Definition() api.Tool {
	return api.Tool{
		Type: "function",
		Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{
			Name:        "read_file",
			Description: "Read contents of a file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
						"minLength":   1,
					},
					"view_range": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "integer"},
						"description": "Line range as [start, end] array (1-based)",
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate checks that the arguments are suitable for the read_file tool.
func (h *ReadFileHandler) Validate(args map[string]any) error {
	// Check path — supports "path" and "file_path" aliases.
	path, err := extractFilePath(args)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("parameter 'path' is required")
	}

	// Check view_range if present.
	if vr, ok := args["view_range"]; ok {
		if err := validateViewRange(vr); err != nil {
			return err
		}
	}

	return nil
}

// Execute reads the file and returns its contents.
func (h *ReadFileHandler) Execute(ctx context.Context, env *ToolEnv, args map[string]any) (*ToolResult, error) {
	path, err := extractFilePath(args)
	if err != nil {
		return &ToolResult{ErrorMessage: err.Error()}, err
	}

	// Parse optional view_range.
	var startLine, endLine int
	if vr, ok := args["view_range"]; ok {
		lines, ok := vr.([]any)
		if !ok {
			return &ToolResult{ErrorMessage: "view_range must be an array"}, fmt.Errorf("view_range must be an array, got %T", vr)
		}
		if len(lines) == 2 {
			if s, ok := toIntFromAny(lines[0]); ok {
				startLine = s
			}
			if e, ok := toIntFromAny(lines[1]); ok {
				endLine = e
			}
		}
	}

	var content string
	if startLine > 0 || endLine > 0 {
		content, err = ReadFileWithRange(ctx, path, startLine, endLine)
	} else {
		content, err = ReadFile(ctx, path)
	}

	if err != nil {
		return &ToolResult{ErrorMessage: err.Error()}, err
	}

	return &ToolResult{Output: content}, nil
}

// extractFilePath extracts the file path from args, supporting both
// "path" and "file_path" aliases for backward compatibility.
func extractFilePath(args map[string]any) (string, error) {
	if v, ok := args["path"]; ok {
		return toString(v, "path")
	}
	if v, ok := args["file_path"]; ok {
		return toString(v, "file_path")
	}
	return "", nil
}

func toString(v any, name string) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		return "", fmt.Errorf("parameter '%s' must be a string, got %T", name, v)
	}
}

// toIntFromAny converts a value that may be float64, int, or int64 to int.
// Returns (result, true) on success or (0, false) if the type is unsupported.
func toIntFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// validateViewRange checks that view_range is a two-element array of numbers.
func validateViewRange(vr any) error {
	lines, ok := vr.([]any)
	if !ok {
		return fmt.Errorf("parameter 'view_range' must be an array, got %T", vr)
	}
	if len(lines) != 2 {
		return fmt.Errorf("parameter 'view_range' must have exactly 2 elements, got %d", len(lines))
	}
	for i, elem := range lines {
		switch elem.(type) {
		case float64, int, int64:
			// JSON numbers decode as float64; both are acceptable.
		default:
			return fmt.Errorf("view_range[%d] must be an integer, got %T", i, elem)
		}
	}
	return nil
}
