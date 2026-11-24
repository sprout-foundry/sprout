package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const memoryDirName = "memories"

// MemoryInfo represents information about a memory file
type MemoryInfo struct {
	Name    string // Memory name, derived from filename (without .md extension)
	Path    string // Full file path
	Content string // File content string
}

// getMemoryDir returns the path to ~/.config/sprout/memories/
// Creates the directory if it doesn't exist
func getMemoryDir() string {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return ""
	}

	memoryDir := filepath.Join(configDir, memoryDirName)

	// Check if directory exists
	if _, err := os.Stat(memoryDir); os.IsNotExist(err) {
		// Create the directory with 0755 permissions
		if err := os.MkdirAll(memoryDir, 0755); err != nil {
			return ""
		}
	}

	return memoryDir
}

// LoadAllMemories reads all .md files from the memories directory
// Returns a slice of MemoryInfo sorted by filename
// Returns empty slice (not error) if no memories exist
func LoadAllMemories() ([]MemoryInfo, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return []MemoryInfo{}, nil
	}

	// Read all entries in the directory
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []MemoryInfo{}, nil
		}
		return nil, agenterrors.NewTool("memory", "failed to read memories directory", err)
	}

	var memories []MemoryInfo

	for _, entry := range entries {
		// Only process .md files
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(memoryDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files that can't be read
		}

		// Remove .md extension for the name
		name := strings.TrimSuffix(entry.Name(), ".md")

		memories = append(memories, MemoryInfo{
			Name:    name,
			Path:    filePath,
			Content: string(content),
		})
	}

	// Sort by filename (Name field)
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Name < memories[j].Name
	})

	return memories, nil
}

// LoadMemoryContent reads a single memory file by name
// The name should be without the .md extension (e.g., "git-safety" reads git-safety.md)
func LoadMemoryContent(name string) (string, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return "", agenterrors.NewPermanentError("failed to get memory directory: unable to locate config directory", nil)
	}

	filePath := filepath.Join(memoryDir, name+".md")

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to read memory file %q", name)
	}

	return string(content), nil
}

// SaveMemory writes a memory file
// Sanitizes the name: lowercase, replace spaces with hyphens, strip special chars
// Keeps only alphanumeric, hyphens, and underscores
func SaveMemory(name string, content string) error {
	// Sanitize the name
	sanitized := tools.SanitizeMemoryName(name)

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return agenterrors.NewPermanentError("failed to get memory directory: unable to locate config directory", nil)
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")

	// Write the file
	err := os.WriteFile(filePath, []byte(content), 0600)
	if err != nil {
		return agenterrors.Wrapf(err, "failed to write memory file %q", sanitized)
	}

	return nil
}

// DeleteMemory deletes a memory file by name (with .md extension)
func DeleteMemory(name string) error {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return agenterrors.NewPermanentError("failed to get memory directory: unable to locate config directory", nil)
	}

	// Ensure .md extension
	if !strings.HasSuffix(name, ".md") {
		name = name + ".md"
	}

	filePath := filepath.Join(memoryDir, name)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return agenterrors.NewNotFound(fmt.Sprintf("memory file %q does not exist", name))
	}

	// Delete the file
	err := os.Remove(filePath)
	if err != nil {
		return agenterrors.Wrapf(err, "failed to delete memory file %q", name)
	}

	return nil
}

// ListMemories returns list of all memories with their name, path, and first line (title/heading)
// Sorts alphabetically by name
func ListMemories() ([]MemoryInfo, error) {
	memories, err := LoadAllMemories()
	if err != nil {
		return nil, agenterrors.NewTool("memory", "failed to load memories", err)
	}

	// Extract first line (title) for each memory
	for i := range memories {
		lines := strings.Split(memories[i].Content, "\n")
		if len(lines) > 0 {
			// Get the first non-empty line
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					memories[i].Content = strings.TrimSpace(line)
					break
				}
			}
		}
	}

	// Sort alphabetically by name
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Name < memories[j].Name
	})

	return memories, nil
}

// maxMemoryPromptBytes is the outer cap on memory bytes in the system prompt (~12 500 tokens).
const maxMemoryPromptBytes = 50_000

// maxMemoryEntryBytes is the per-memory cap: the largest rendered entry the block carries.
// It is a fixed constant derived from maxMemoryPromptBytes (one fifth of the default total,
// so a few full bodies fit under the outer guard) — not a share of the caller's maxBytes.
// The outer guard (memory_prompt_max_bytes, or maxBytes when set) remains the backstop.
// The per-memory value is fixed, so which entries are rendered never depends on how big
// a neighbour's entry happens to be.
const maxMemoryEntryBytes = maxMemoryPromptBytes / 5

// LoadMemoriesForPrompt formats every memory body for the system prompt (memory_prompt "full").
// Returns empty string if no memories exist.
func LoadMemoriesForPrompt() string {
	return LoadMemoriesForPromptMode(MemoryPromptFull, 0)
}
