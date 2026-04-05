package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

const memoryDirName = "memories"

// MemoryInfo represents information about a memory file
type MemoryInfo struct {
	Name    string // Memory name, derived from filename (without .md extension)
	Path    string // Full file path
	Content string // File content string
}

// getMemoryDir returns the path to ~/.ledit/memories/
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
		return nil, fmt.Errorf("failed to read memories directory: %w", err)
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
		return "", fmt.Errorf("failed to get memory directory")
	}

	filePath := filepath.Join(memoryDir, name+".md")

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read memory file %s: %w", name, err)
	}

	return string(content), nil
}

// SaveMemory writes a memory file
// Sanitizes the name: lowercase, replace spaces with hyphens, strip special chars
// Keeps only alphanumeric, hyphens, and underscores
func SaveMemory(name string, content string) error {
	// Sanitize the name
	sanitized := sanitizeMemoryName(name)

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return fmt.Errorf("failed to get memory directory")
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")

	// Write the file
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write memory file: %w", err)
	}

	return nil
}

// sanitizeMemoryName sanitizes a memory name
// - Converts to lowercase
// - Replaces spaces with hyphens
// - Strips special characters (keeps only alphanumeric, hyphens, underscores)
// - Ensures .md extension is not part of the name
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

// DeleteMemory deletes a memory file by name (with .md extension)
func DeleteMemory(name string) error {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return fmt.Errorf("failed to get memory directory")
	}

	// Ensure .md extension
	if !strings.HasSuffix(name, ".md") {
		name = name + ".md"
	}

	filePath := filepath.Join(memoryDir, name)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("memory file does not exist: %s", name)
	}

	// Delete the file
	err := os.Remove(filePath)
	if err != nil {
		return fmt.Errorf("failed to delete memory file: %w", err)
	}

	return nil
}

// ListMemories returns list of all memories with their name, path, and first line (title/heading)
// Sorts alphabetically by name
func ListMemories() ([]MemoryInfo, error) {
	memories, err := LoadAllMemories()
	if err != nil {
		return nil, err
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

// LoadMemoriesForPrompt loads all memories and formats them for inclusion in the system prompt
// Returns empty string if no memories exist
func LoadMemoriesForPrompt() string {
	memories, err := LoadAllMemories()
	if err != nil || len(memories) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("\n\n---\n\n")
	sb.WriteString("## Memories\n\n")
	sb.WriteString("The following memories capture user preferences and learned patterns from previous sessions. Use them to guide your behavior.\n\n")

	for _, memory := range memories {
		sb.WriteString(fmt.Sprintf("### %s\n", memory.Name))

		// Process content: skip leading H1 title if present
		content := memory.Content
		lines := strings.Split(content, "\n")

		// Check if first line is an H1 heading (starts with # followed by space)
		startIdx := 0
		if len(lines) > 0 {
			firstLine := strings.TrimSpace(lines[0])
			if strings.HasPrefix(firstLine, "# ") {
				startIdx = 1
			}
		}

		// Join remaining lines
		if startIdx < len(lines) {
			sb.WriteString(strings.Join(lines[startIdx:], "\n"))
		}

		sb.WriteString("\n\n")
	}

	return sb.String()
}
