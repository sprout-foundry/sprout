package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ContextFileInfo represents information about a discovered context file
type ContextFileInfo struct {
	Path        string
	Content     string
	Description string
	Priority    int
}

// DiscoverContextFiles looks for context files in the current directory and parent directories
// Returns the first matching file based on priority order
func DiscoverContextFiles() (*ContextFileInfo, error) {
	// Priority order for context files
	contextFiles := []struct {
		filename     string
		description  string
		priority     int
		searchUpward bool // whether to search parent directories
	}{
		{"AGENTS.md", "Agent configuration and context", 1, true},
		{"Claude.md", "Claude AI assistant context", 2, true},
		{"cursor.md", "Cursor editor context", 3, true},
		{"cursor-context.md", "Cursor editor context (alternative)", 4, true},
		{"github-copilot.md", "GitHub Copilot context", 5, true},
		{"copilot.md", "GitHub Copilot context (alternative)", 6, true},
		{"ai-context.md", "General AI context", 7, true},
		{"llm-context.md", "LLM context", 8, true},
		{"agent-context.md", "Agent context", 9, true},
		{"tool-context.md", "Tool context", 10, true},
		{"dev-context.md", "Development context", 11, true},
		{"project-context.md", "Project context", 12, true},
		{"README.md", "Project README (fallback)", 13, true},
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Search for context files
	for _, fileConfig := range contextFiles {
		var searchPath string
		var foundPath string

		if fileConfig.searchUpward {
			// Search in current directory and parent directories
			searchPath = cwd
			for {
				testPath := filepath.Join(searchPath, fileConfig.filename)
				if _, err := os.Stat(testPath); err == nil {
					foundPath = testPath
					break
				}

				// Move to parent directory
				parent := filepath.Dir(searchPath)
				if parent == searchPath {
					// Reached root directory
					break
				}
				searchPath = parent
			}
		} else {
			// Only search in current directory
			testPath := filepath.Join(cwd, fileConfig.filename)
			if _, err := os.Stat(testPath); err == nil {
				foundPath = testPath
			}
		}

		if foundPath != "" {
			// Read the file content
			content, err := os.ReadFile(foundPath)
			if err != nil {
				continue // Skip if we can't read the file
			}

			return &ContextFileInfo{
				Path:        foundPath,
				Content:     string(content),
				Description: fileConfig.description,
				Priority:    fileConfig.priority,
			}, nil
		}
	}

	return nil, nil // No context files found
}

// LoadContextFiles loads and formats context files for inclusion in system prompt
func LoadContextFiles() (string, error) {
	contextFile, err := DiscoverContextFiles()
	if err != nil {
		return "", err
	}

	if contextFile == nil {
		return "", nil // No context files found
	}

	// Format the context for inclusion
	var formatted strings.Builder

	formatted.WriteString(fmt.Sprintf("\n\n---\n\n## %s\n\n", contextFile.Description))
	formatted.WriteString(fmt.Sprintf("Loaded from: `%s`\n\n", contextFile.Path))

	// Process the content to extract relevant sections
	content := processContextContent(contextFile.Content)
	formatted.WriteString(content)

	return formatted.String(), nil
}

// processContextContent processes and cleans up context file content
func processContextContent(content string) string {
	lines := strings.Split(content, "\n")
	var processed []string
	var inCodeBlock bool
	var skipNextLine bool

	for i, line := range lines {
		// Skip table of contents and navigation sections
		if strings.Contains(strings.ToLower(line), "table of contents") ||
			strings.Contains(strings.ToLower(line), "contents") ||
			strings.Contains(strings.ToLower(line), "navigation") ||
			strings.Contains(strings.ToLower(line), "menu") ||
			strings.HasPrefix(strings.TrimSpace(line), "#") && 
			(strings.Contains(strings.ToLower(line), "index") || 
			 strings.Contains(strings.ToLower(line), "toc")) {
			skipNextLine = true
			continue
		}

		if skipNextLine {
			skipNextLine = false
			continue
		}

		// Track code blocks
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
		}

		// Skip empty lines at the beginning
		if i == 0 && strings.TrimSpace(line) == "" {
			continue
		}

		processed = append(processed, line)
	}

	// Join back and clean up
	result := strings.Join(processed, "\n")

	// Remove excessive empty lines
	result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	result = strings.ReplaceAll(result, "\n\n\n\n", "\n\n")

	return result
}

// regenerateContextCache forces regeneration of context cache
func regenerateContextCache() {
	// Remove any cached context files
	cachePath := filepath.Join(".ledit", "context_cache.md")
	os.Remove(cachePath)
}