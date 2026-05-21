// ListDirectoryHandler implements ToolHandler for the list_directory tool.
//
// This handler lists files and directories at the specified path,
// following the same security and validation patterns as ReadFileHandler.
package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// ListDirectoryHandler implements ToolHandler for listing directory contents.
type ListDirectoryHandler struct{}

// NewListDirectoryHandler returns a ready-to-use ListDirectoryHandler.
func NewListDirectoryHandler() *ListDirectoryHandler {
	return &ListDirectoryHandler{}
}

// Name returns the tool name "list_directory".
func (h *ListDirectoryHandler) Name() string {
	return "list_directory"
}

// Definition returns the LLM-facing tool definition.
func (h *ListDirectoryHandler) Definition() api.Tool {
	return api.Tool{
		Type: "function",
		Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{
			Name:        "list_directory",
			Description: "List files and directories at the specified path",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the directory to list",
						"minLength":   1,
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate checks that the arguments are suitable for the list_directory tool.
func (h *ListDirectoryHandler) Validate(args map[string]any) error {
	path, err := toString(args["path"], "path")
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("parameter 'path' is required")
	}
	return nil
}

// Execute lists the contents of the specified directory and returns formatted output.
func (h *ListDirectoryHandler) Execute(ctx context.Context, env *ToolEnv, args map[string]any) (*ToolResult, error) {
	path, err := toString(args["path"], "path")
	if err != nil {
		return &ToolResult{ErrorMessage: err.Error()}, err
	}

	// SECURITY: Validate path is within working directory (handles symlinks properly)
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		return &ToolResult{ErrorMessage: err.Error()}, err
	}

	// Check if path exists and is a directory
	info, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return &ToolResult{ErrorMessage: fmt.Sprintf("path does not exist: %s", cleanPath)}, fmt.Errorf("path does not exist: %s", cleanPath)
	}
	if err != nil {
		return &ToolResult{ErrorMessage: fmt.Sprintf("access path %s: %v", cleanPath, err)}, fmt.Errorf("access path %s: %w", cleanPath, err)
	}
	if !info.IsDir() {
		return &ToolResult{ErrorMessage: fmt.Sprintf("path is a file, not a directory: %s", cleanPath)}, fmt.Errorf("path is a file, not a directory: %s", cleanPath)
	}

	// Read directory entries
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return &ToolResult{ErrorMessage: fmt.Sprintf("read directory %s: %v", cleanPath, err)}, fmt.Errorf("read directory %s: %w", cleanPath, err)
	}

	// Sort entries by name for deterministic output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// Format output
	var sb strings.Builder
	sb.WriteString(cleanPath + ":\n")
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			sb.WriteString(fmt.Sprintf("  [ERROR] %s\n", entry.Name()))
			continue
		}
		if info.IsDir() {
			sb.WriteString(fmt.Sprintf("  [DIR]  %s\n", entry.Name()))
		} else {
			sb.WriteString(fmt.Sprintf("  [FILE] %s\n", entry.Name()))
		}
	}

	return &ToolResult{Output: sb.String()}, nil
}
