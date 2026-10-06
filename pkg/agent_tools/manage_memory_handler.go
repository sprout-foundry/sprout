package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// manageMemoryHandler implements ToolHandler for the consolidated manage_memory tool.
// Dispatches on `operation` to add/read/list/delete/search memories.
type manageMemoryHandler struct{}

func (h *manageMemoryHandler) Name() string { return "manage_memory" }

func (h *manageMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "manage_memory",
		Description: "Persistent cross-session memories that auto-load into the system prompt. " +
			"Operations: add, read, list, delete, search. " +
			"Use 'add' for durable preferences; 'search' to recall prior notes.",
		Required: []string{"operation"},
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true, Description: "One of: 'add', 'read', 'list', 'delete', 'search'."},
			{Name: "name", Type: "string", Description: "Memory name slug (required for add/read/delete)"},
			{Name: "content", Type: "string", Description: "Markdown content (required for add)"},
			{Name: "query", Type: "string", Description: "Search query (required for search)"},
			{Name: "threshold", Type: "number", Description: "Search: min similarity 0.0–1.0 (default 0.75)"},
			{Name: "top_k", Type: "integer", Description: "Search: max results (default 5)"},
		},
	}
}

func (h *manageMemoryHandler) Validate(args map[string]any) error {
	op, err := extractString(args, "operation")
	if err != nil {
		return err
	}
	op = strings.TrimSpace(strings.ToLower(op))
	switch op {
	case "add", "read", "list", "delete", "search":
		// valid
	default:
		return agenterrors.NewValidation(fmt.Sprintf("manage_memory: unknown operation %q (want add, read, list, delete, or search)", op), nil)
	}

	// Validate operation-specific required params
	switch op {
	case "add":
		if _, err := extractString(args, "name"); err != nil {
			return agenterrors.NewValidation("manage_memory: 'name' is required for add", nil)
		}
		if _, err := extractString(args, "content"); err != nil {
			return agenterrors.NewValidation("manage_memory: 'content' is required for add", nil)
		}
	case "read", "delete":
		if _, err := extractString(args, "name"); err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("manage_memory: 'name' is required for %s", op), nil)
		}
	case "search":
		if _, err := extractString(args, "query"); err != nil {
			return agenterrors.NewValidation("manage_memory: 'query' is required for search", nil)
		}
	}

	return nil
}

// resolveMemoryName sanitizes a memory name and rejects names that sanitize
// to nothing. The pkg/agent sanitize helper maps an empty result to the
// default name "untitled"; silently funneling every malformed call onto one
// shared file is exactly the bug this guard exists for (a bare delete would
// destroy that file with a success message), so inputs that would hit the
// default are rejected unless the caller literally asked for "untitled".
func resolveMemoryName(name string) (string, error) {
	// Models frequently pass the filename with its extension (any casing).
	trimmed := strings.TrimSpace(name)
	if lowered := strings.ToLower(trimmed); strings.HasSuffix(lowered, ".md") {
		trimmed = trimmed[:len(trimmed)-3]
	}

	sanitized := sanitizeMemoryName(trimmed)
	if sanitized == "untitled" && !strings.EqualFold(strings.TrimSpace(trimmed), "untitled") {
		return "", fmt.Errorf("memory name %q is empty after sanitization; pass a short slug (letters, digits, hyphens)", name)
	}

	return sanitized, nil
}

func (h *manageMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	op, _ := extractString(args, "operation")
	op = strings.TrimSpace(strings.ToLower(op))

	switch op {
	case "add":
		return h.executeAdd(env, args)
	case "read":
		return h.executeRead(args)
	case "list":
		return h.executeList()
	case "delete":
		return h.executeDelete(args)
	case "search":
		return h.executeSearch(env, args)
	default:
		return ToolResult{
			Output:  fmt.Sprintf("manage_memory: unknown operation %q", op),
			IsError: true,
		}, nil
	}
}

func (h *manageMemoryHandler) Aliases() []string      { return nil }
func (h *manageMemoryHandler) Timeout() time.Duration { return 0 }
func (h *manageMemoryHandler) MaxResultSize() int     { return 0 }
func (h *manageMemoryHandler) SafeForParallel() bool  { return false }
func (h *manageMemoryHandler) Interactive() bool      { return false }

const memoryDirName = "memories"

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
		if err := os.MkdirAll(memoryDir, 0o755); err != nil {
			return ""
		}
	}

	return memoryDir
}

// saveMemoryToDisk writes a memory file as <memoryDir>/<name>.md.
func saveMemoryToDisk(sanitized, content string) (string, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return "", agenterrors.NewConfig("unable to locate config directory for memories", nil)
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")

	err := os.WriteFile(filePath, []byte(content), 0o600)
	if err != nil {
		return "", agenterrors.NewTool("manage_memory", fmt.Sprintf("failed to write memory file %q: %v", sanitized, err), err)
	}

	return fmt.Sprintf("Memory '%s' saved to ~/.config/sprout/memories/%s.md. This memory will be loaded in all future conversations.", sanitized, sanitized), nil
}

// sanitizeMemoryName sanitizes a memory name for use as a filename:
// lowercase, spaces to hyphens, and only alphanumeric/hyphen/underscore
// characters kept, mirroring the pkg/agent copy. An input that sanitizes to
// nothing yields the historical "untitled" default; manage_memory callers
// must go through resolveMemoryName, which rejects that case instead of
// letting malformed calls collapse onto one shared file.
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

// executeAdd handles the "add" operation.
func (h *manageMemoryHandler) executeAdd(env ToolEnv, args map[string]any) (ToolResult, error) {
	name, _ := extractString(args, "name")
	content, _ := extractString(args, "content")

	// Redact secrets before persisting to memory files
	content = redact.String(content)

	sanitized, err := resolveMemoryName(name)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("manage_memory add: %v", err), IsError: true}, nil
	}

	notice := ""
	if _, statErr := os.Stat(filepath.Join(getMemoryDir(), sanitized+".md")); statErr == nil {
		notice = " (existing memory overwritten)"
	}

	result, err := saveMemoryToDisk(sanitized, content)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("failed to save memory '%s': %v", name, err),
			IsError: true,
		}, nil
	}

	return ToolResult{
		Output:     result + notice,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// executeRead handles the "read" operation.
func (h *manageMemoryHandler) executeRead(args map[string]any) (ToolResult, error) {
	name, _ := extractString(args, "name")

	sanitized, err := resolveMemoryName(name)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("manage_memory read: %v", err), IsError: true}, nil
	}

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return ToolResult{Output: "unable to locate config directory for memories", IsError: true}, nil
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Output: fmt.Sprintf("Memory '%s' not found", sanitized)}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to read memory '%s': %v", sanitized, err),
			IsError: true,
		}, nil
	}

	return ToolResult{
		Output:     string(data),
		TokenUsage: int64(estimateTokenUsage(string(data))),
	}, nil
}

// executeList handles the "list" operation.
func (h *manageMemoryHandler) executeList() (ToolResult, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return ToolResult{Output: "No memories directory found."}, nil
	}

	entries, err := readDirCompat(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Output: "No memories found."}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to read memories directory: %v", err),
			IsError: true,
		}, nil
	}

	type memoryEntry struct {
		name    string
		preview string
	}

	var memories []memoryEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		contentPath := filepath.Join(memoryDir, entry.Name())

		contentBytes, err := os.ReadFile(contentPath)
		if err != nil {
			continue // Skip unreadable files
		}

		preview := firstLine(string(contentBytes))
		if len(preview) > 120 {
			preview = truncateRunes(preview, 117)
		}

		memories = append(memories, memoryEntry{name: name, preview: preview})
	}

	if len(memories) == 0 {
		return ToolResult{Output: "No memories found."}, nil
	}

	// Sort alphabetically for deterministic output
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].name < memories[j].name
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory/memories:\n\n", len(memories)))
	for i, m := range memories {
		sb.WriteString(fmt.Sprintf("#%d — **%s**\n", i+1, m.name))
		if m.preview != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", m.preview))
		}
		sb.WriteString("\n")
	}

	return ToolResult{
		Output:     sb.String(),
		TokenUsage: int64(estimateTokenUsage(sb.String())),
	}, nil
}

// executeDelete handles the "delete" operation.
func (h *manageMemoryHandler) executeDelete(args map[string]any) (ToolResult, error) {
	name, _ := extractString(args, "name")

	sanitized, err := resolveMemoryName(name)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("manage_memory delete: %v", err), IsError: true}, nil
	}

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return ToolResult{Output: "unable to locate config directory for memories", IsError: true}, nil
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Output: fmt.Sprintf("Memory '%s' not found", sanitized)}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to delete memory '%s': %v", sanitized, err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("Memory '%s' deleted.", sanitized)

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// executeSearch handles the "search" operation: text matching over saved memories.
func (h *manageMemoryHandler) executeSearch(env ToolEnv, args map[string]any) (ToolResult, error) {
	query, _ := extractString(args, "query")

	topK := 5
	if tkRaw, exists := args["top_k"]; exists && tkRaw != nil {
		switch v := tkRaw.(type) {
		case int:
			topK = v
		case float64:
			topK = int(v)
		}
	}
	if topK < 1 {
		topK = 1
	}

	threshold := 0.75
	if tRaw, exists := args["threshold"]; exists && tRaw != nil {
		switch v := tRaw.(type) {
		case float64:
			threshold = v
		case float32:
			threshold = float64(v)
		case int:
			threshold = float64(v)
		}
	}
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 1 {
		threshold = 1
	}

	results, err := SearchMemoriesByText(query, topK, threshold)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("memory search failed: %v", err),
			IsError: true,
		}, nil
	}

	output := FormatMemorySearchResults(query, results, threshold)
	return ToolResult{
		Output:     output,
		TokenUsage: int64(estimateTokenUsage(output)),
	}, nil
}
