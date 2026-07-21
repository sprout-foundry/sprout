package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

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
	// Wire the agent's filesystem gate into ctx before the resolve
	// step so off-workspace directories prompt for approval (matching
	// the file handlers' behavior). Without this, list_directory on a
	// sibling directory would hard-error with the bare sentinel.
	ctx = WithFilesystemGateFromEnv(ctx, env)

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

	// Resolve path securely through the FilesystemGate so off-workspace
	// directories prompt for approval (matching the file handlers'
	// behavior). Without this wrap, list_directory on a sibling
	// directory would still hard-error with the bare sentinel — the
	// gate goes into ctx but pkg/filesystem has no awareness of it,
// so the resolve call needs the explicit hook here. See the
// FilesystemGate interface in handler.go and withFilesystemApproval
// in filesystem_gate.go for the contract.
	resolvedPath, err := withFilesystemApproval(ctx, FilesystemGateFromContext(ctx), "list_directory", targetPath,
		func(ctx context.Context) (string, error) {
			return filesystem.SafeResolvePathWithBypass(ctx, targetPath)
		},
	)
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

	// Read directory contents (readDirCompat works on js/wasm too).
	entries, err := readDirCompat(resolvedPath)
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
			"name":  name,
			"isDir": isDir,
			"size":  size,
			"type":  entryType,
		})
	}

	totalEntries := len(structuredResults)
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%d entries found\n", totalEntries))

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, sb.String())
	}

	return ToolResult{
		Output:        sb.String(),
		StructuredOut: structuredResults,
	}, nil
}

func (h *listDirHandler) Aliases() []string      { return nil }
func (h *listDirHandler) Timeout() time.Duration { return 0 }
func (h *listDirHandler) MaxResultSize() int     { return 0 }
func (h *listDirHandler) SafeForParallel() bool  { return false }
func (h *listDirHandler) Interactive() bool      { return false }
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
