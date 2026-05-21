package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// listDirHandler implements ToolHandler for the list_directory tool.
type listDirHandler struct{}

func (h *listDirHandler) Name() string {
	return "list_directory"
}

func (h *listDirHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "list_directory",
		Description: "List the contents of a directory. Returns file and directory names with their sizes and types. Use show_hidden to include dotfiles.",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    false,
				Description: "Directory path to list. Defaults to '.' (current directory) if not specified.",
			},
			{
				Name:        "show_hidden",
				Type:        "boolean",
				Required:    false,
				Description: "If true, show hidden files (dotfiles). Defaults to false.",
			},
		},
	}
}

func (h *listDirHandler) Validate(args map[string]any) error {
	// If path is provided, validate it's a string
	if path, exists := args["path"]; exists && path != nil {
		if _, ok := path.(string); !ok {
			return fmt.Errorf("parameter 'path' must be a string, got %T", path)
		}
	}

	// If show_hidden is provided, validate it's a boolean
	if sh, exists := args["show_hidden"]; exists && sh != nil {
		if _, ok := sh.(bool); !ok {
			return fmt.Errorf("parameter 'show_hidden' must be a boolean, got %T", sh)
		}
	}

	return nil
}

func (h *listDirHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// Extract parameters
	targetPath := "."
	if p, exists := args["path"]; exists && p != nil {
		if s, ok := p.(string); ok && strings.TrimSpace(s) != "" {
			targetPath = s
		}
	}

	showHidden := false
	if sh, exists := args["show_hidden"]; exists && sh != nil {
		if b, ok := sh.(bool); ok {
			showHidden = b
		}
	}

	// Resolve path securely
	resolvedPath, err := filesystem.SafeResolvePathWithBypass(ctx, targetPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("resolve directory path: %w", err)
	}

	// Check that it's a directory
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("access directory %s: %w", resolvedPath, err)
	}
	if !info.IsDir() {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("path is not a directory: %s", resolvedPath)
	}

	// Publish tool start event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool": "list_directory",
			"path": resolvedPath,
		})
	}

	// Read directory contents
	entries, err := os.ReadDir(resolvedPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read directory %s: %w", resolvedPath, err)
	}

	// Filter and build output
	var sb strings.Builder
	var structuredResults []map[string]any

	sb.WriteString(fmt.Sprintf("Directory contents of: %s\n\n", resolvedPath))

	// Add header
	sb.WriteString(fmt.Sprintf("%-45s %12s  %s\n", "NAME", "SIZE", "TYPE"))
	sb.WriteString(strings.Repeat("-", 65) + "\n")

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()

		// Filter hidden files if not requested
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		info, ierr := entry.Info()
		isDir := entry.IsDir()
		var size int64
		var sizeStr string
		if ierr == nil {
			size = info.Size()
			sizeStr = formatSize(size)
		} else {
			sizeStr = "-"
		}

		entryType := "file"
		if isDir {
			entryType = "dir"
		}

		sb.WriteString(fmt.Sprintf("%-45s %12s  %s\n", name, sizeStr, entryType))

		structuredResults = append(structuredResults, map[string]any{
			"name":    name,
			"isDir":   isDir,
			"size":    size,
			"type":    entryType,
		})
	}

	totalEntries := len(structuredResults)
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%d entries found\n", totalEntries))

	// Publish tool end event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":       "list_directory",
			"path":       resolvedPath,
			"total":      totalEntries,
			"showHidden": showHidden,
		})
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, sb.String())
	}

	return ToolResult{
		Output:        sb.String(),
		StructuredOut: structuredResults,
	}, nil
}

// formatSize formats a file size in bytes to a human-readable string.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
