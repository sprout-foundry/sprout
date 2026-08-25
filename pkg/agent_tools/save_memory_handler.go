package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

const memoryDirName = "memories"

// saveMemoryHandler implements ToolHandler for the save_memory tool.
//
// This handler saves a memory file to ~/.config/sprout/memories/<name>.md
// for persistence across conversations. It does NOT require an *Agent — the
// file is written directly to disk. Embedding/indexing of the memory for
// semantic search is handled separately (the agent's embedding manager
// picks it up on next index or via MigrateMemories).
type saveMemoryHandler struct{}

func (h *saveMemoryHandler) Name() string {
	return "save_memory"
}

func (h *saveMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "save_memory",
		Description: "Save a memory to persist across all conversations. The memory will be loaded into the system prompt automatically in future sessions.",
		Parameters: []ParameterDef{
			{
				Name:        "name",
				Type:        "string",
				Required:    true,
				Description: "Short descriptive name for the memory (e.g., 'git-safety', 'test-conventions')",
			},
			{
				Name:        "content",
				Type:        "string",
				Required:    true,
				Description: "Markdown content to store in the memory file",
			},
		},
		Required: []string{"name", "content"},
	}
}

func (h *saveMemoryHandler) Validate(args map[string]any) error {
	name, err := extractString(args, "name")
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return agenterrors.NewValidation("parameter 'name' must not be empty", nil)
	}

	content, err := extractString(args, "content")
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return agenterrors.NewValidation("parameter 'content' must not be empty", nil)
	}

	return nil
}

func (h *saveMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	name, err := extractString(args, "name")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	content, err := extractString(args, "content")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Redact secrets before persisting to memory files (same as handleAddMemory)
	content = redact.String(content)

	sanitized := sanitizeMemoryName(name)
	result, err := saveMemoryToDisk(sanitized, content)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("failed to save memory '%s': %v", name, err),
			IsError: true,
		}, agenterrors.NewTool("save_memory", fmt.Sprintf("save memory %q: %v", name, err), err)
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

func (h *saveMemoryHandler) Aliases() []string      { return nil }
func (h *saveMemoryHandler) Timeout() time.Duration { return 0 }
func (h *saveMemoryHandler) MaxResultSize() int     { return 0 }
func (h *saveMemoryHandler) SafeForParallel() bool  { return false }
func (h *saveMemoryHandler) Interactive() bool      { return false }

// saveMemoryToDisk writes a memory file to ~/.config/sprout/memories/<name>.md
// This is a standalone implementation that doesn't depend on *Agent.
func saveMemoryToDisk(sanitized, content string) (string, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return "", agenterrors.NewConfig("unable to locate config directory for memories", nil)
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")

	err := os.WriteFile(filePath, []byte(content), 0600)
	if err != nil {
		return "", agenterrors.NewTool("save_memory", fmt.Sprintf("failed to write memory file %q: %v", sanitized, err), err)
	}

	return fmt.Sprintf("Memory '%s' saved to ~/.config/sprout/memories/%s.md. This memory will be loaded in all future conversations.", sanitized, sanitized), nil
}

// getMemoryDir returns the path to the memory directory, creating it if needed.
// Returns "" if the config directory cannot be determined.
func getMemoryDir() string {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return ""
	}

	memoryDir := filepath.Join(configDir, memoryDirName)

	// Create directory if it doesn't exist
	if _, err := os.Stat(memoryDir); os.IsNotExist(err) {
		if err := os.MkdirAll(memoryDir, 0755); err != nil {
			return ""
		}
	}

	return memoryDir
}

// sanitizeMemoryName sanitizes a memory name for use as a filename.
func sanitizeMemoryName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces with hyphens
	name = strings.ReplaceAll(name, " ", "-")

	// Keep only alphanumeric, hyphens, and underscores
	matched := regexp.MustCompile(`[^a-z0-9\-_]+`).ReplaceAllString(name, "")

	// Remove leading/trailing hyphens and underscores
	matched = strings.Trim(matched, "-_")

	// Default name if empty
	if matched == "" {
		matched = "untitled"
	}

	return matched
}
