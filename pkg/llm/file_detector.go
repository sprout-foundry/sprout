package llm

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// FileDetector helps identify when files are mentioned in user prompts
type FileDetector struct {
	// Common file extensions to look for
	fileExtensions []string
	// Patterns that indicate file references
	filePatterns []*regexp.Regexp
}

// NewFileDetector creates a new file detector with common patterns
func NewFileDetector() *FileDetector {
	extensions := []string{
		".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".h", ".hpp",
		".rb", ".php", ".rs", ".swift", ".kt", ".cs", ".scala", ".sh", ".bash",
		".md", ".txt", ".json", ".yaml", ".yml", ".xml", ".html", ".css",
		".sql", ".dockerfile", ".makefile", ".toml", ".ini", ".cfg", ".conf",
	}

	patterns := []*regexp.Regexp{
		// Direct file mentions: "update main.go", "modify config.yaml"
		regexp.MustCompile(`\b\w+\.\w+\b`),
		// Path-like references: "src/main.go", "./config/app.yaml"
		regexp.MustCompile(`[./\w-]+/\w+\.\w+`),
		// Quoted file names: "main.go", 'config.yaml'
		regexp.MustCompile(`["']\w+\.\w+["']`),
		// File actions: "update the main.go file", "in config.yaml"
		regexp.MustCompile(`\b(?:update|modify|change|edit|fix|in|the)\s+\w+\.\w+`),
	}

	return &FileDetector{
		fileExtensions: extensions,
		filePatterns:   patterns,
	}
}

// DetectMentionedFiles extracts file names mentioned in the prompt
func (fd *FileDetector) DetectMentionedFiles(prompt string) []string {
	var files []string
	seen := make(map[string]bool)

	for _, pattern := range fd.filePatterns {
		matches := pattern.FindAllString(prompt, -1)
		for _, match := range matches {
			// Clean up the match
			file := strings.Trim(match, "\"'")
			file = strings.TrimSpace(file)

			// Remove common prefixes like "update ", "modify ", etc.
			prefixes := []string{"update ", "modify ", "change ", "edit ", "fix ", "in ", "the "}
			for _, prefix := range prefixes {
				if strings.HasPrefix(strings.ToLower(file), prefix) {
					file = file[len(prefix):]
					break
				}
			}

			// Check if it has a valid file extension
			ext := filepath.Ext(file)
			if fd.isValidExtension(ext) && !seen[file] {
				files = append(files, file)
				seen[file] = true
			}
		}
	}

	return files
}

// isValidExtension checks if the extension is in our list of common file types
func (fd *FileDetector) isValidExtension(ext string) bool {
	for _, validExt := range fd.fileExtensions {
		if strings.EqualFold(ext, validExt) {
			return true
		}
	}
	return false
}

// SuggestMissingFileReads analyzes a prompt and suggests read_file tool calls
func (fd *FileDetector) SuggestMissingFileReads(prompt string, providedFiles []string) []ToolCall {
	mentionedFiles := fd.DetectMentionedFiles(prompt)
	var suggestions []ToolCall

	// Create a map of provided files for quick lookup
	provided := make(map[string]bool)
	for _, file := range providedFiles {
		provided[strings.ToLower(file)] = true
	}

	callID := 1
	for _, file := range mentionedFiles {
		if !provided[strings.ToLower(file)] {
			suggestions = append(suggestions, ToolCall{
				ID:   fmt.Sprintf("suggested_call_%d", callID),
				Type: "function",
				Function: ToolCallFunction{
					Name:      "read_file",
					Arguments: fmt.Sprintf(`{"file_path": "%s"}`, file),
				},
			})
			callID++
		}
	}

	return suggestions
}

// GenerateFileReadPrompt creates a prompt that encourages the LLM to read mentioned files
func GenerateFileReadPrompt(mentionedFiles []string) string {
	if len(mentionedFiles) == 0 {
		return ""
	}

	prompt := "\n⚠️  IMPORTANT: The following files were mentioned but not provided:\n"
	for _, file := range mentionedFiles {
		prompt += fmt.Sprintf("- %s\n", file)
	}
	prompt += "\nYou MUST use the read_file tool to get their contents before proceeding.\n"

	return prompt
}
