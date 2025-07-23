package workspace

import (
	"fmt"
	"os"
	"path/filepath" // Added for filepath.Ext
	"sort"          // Added for sort.Strings
	"strings"
)

// getWorkspaceInfo formats the workspace information for the LLM.
// It lists all files, provides full content for selected files, and summaries for others.
func getWorkspaceInfo(workspace WorkspaceFile, fullContextFiles, summaryContextFiles []string) string {
	var b strings.Builder
	b.WriteString("--- Start of full content from workspace ---\n")

	// Convert slices to maps for efficient lookup
	fullContextMap := make(map[string]bool)
	for _, f := range fullContextFiles {
		fullContextMap[f] = true
	}

	summaryContextMap := make(map[string]bool)
	for _, f := range summaryContextFiles {
		summaryContextMap[f] = true
	}

	// 1. List all files in the workspace
	b.WriteString("--- Workspace File System Structure ---\n")
	var allFilePaths []string
	for filePath := range workspace.Files {
		allFilePaths = append(allFilePaths, filePath)
	}
	// Sort for consistent output
	sort.Strings(allFilePaths)

	for _, filePath := range allFilePaths {
		b.WriteString(fmt.Sprintf("%s\n", filePath))
	}
	b.WriteString("\n")

	// 2. Add selected file context
	b.WriteString("--- Selected File Context ---\n\n")

	// Full Context Files
	b.WriteString("### Full Context Files:\n")
	fullContextAdded := false
	for _, filePath := range allFilePaths { // Iterate through all files to maintain order
		if fullContextMap[filePath] {
			fileInfo, exists := workspace.Files[filePath]
			if !exists {
				// This should ideally not happen if workspace is consistent
				b.WriteString(fmt.Sprintf("Warning: File %s selected for full context not found in workspace.\n", filePath))
				continue
			}

			if fileInfo.Summary == "File is too large to analyze." {
				b.WriteString(fmt.Sprintf("Warning: File %s was selected for full context but is too large. Only summary provided:\n", filePath))
				b.WriteString(fmt.Sprintf("Summary: %s\n", fileInfo.Summary))
				if fileInfo.Exports != "" {
					b.WriteString(fmt.Sprintf("Exports: %s\n", fileInfo.Exports))
				}
				if len(fileInfo.SecurityConcerns) > 0 { // New: Add security concerns
					b.WriteString(fmt.Sprintf("Security Concerns: %s\n", strings.Join(fileInfo.SecurityConcerns, ", ")))
				}
				b.WriteString("\n")
				fullContextAdded = true // Mark as added even if only summary is provided due to size
				continue
			}

			content, err := os.ReadFile(filePath)
			if err != nil {
				b.WriteString(fmt.Sprintf("Warning: Could not read content for %s: %v. Skipping full context.\n", filePath, err))
				continue
			}

			lang := getLanguageFromFilename(filePath)
			b.WriteString(fmt.Sprintf("%s\n", filePath))
			b.WriteString(fmt.Sprintf("```%s\n%s\n```\n", lang, string(content))) // Added newline after code block
			if len(fileInfo.SecurityConcerns) > 0 { // New: Add security concerns
				b.WriteString(fmt.Sprintf("Security Concerns: %s\n", strings.Join(fileInfo.SecurityConcerns, ", ")))
			}
			b.WriteString("\n") // Added newline after security concerns
			fullContextAdded = true
		}
	}
	if !fullContextAdded {
		b.WriteString("No files selected for full context.\n\n")
	}

	// Summary Context Files
	b.WriteString("### Summary Context Files:\n")
	summaryContextAdded := false
	for _, filePath := range allFilePaths { // Iterate through all files to maintain order
		// Only add as summary if it wasn't already added as full context (or attempted as full context)
		if summaryContextMap[filePath] && !fullContextMap[filePath] {
			fileInfo, exists := workspace.Files[filePath]
			if !exists {
				b.WriteString(fmt.Sprintf("Warning: File %s selected for summary context not found in workspace.\n", filePath))
				continue
			}
			b.WriteString(fmt.Sprintf("%s\n", filePath))
			b.WriteString(fmt.Sprintf("Summary: %s\n", fileInfo.Summary))
			if fileInfo.Exports != "" {
				b.WriteString(fmt.Sprintf("Exports: %s\n", fileInfo.Exports))
			}
			if len(fileInfo.SecurityConcerns) > 0 { // New: Add security concerns
				b.WriteString(fmt.Sprintf("Security Concerns: %s\n", strings.Join(fileInfo.SecurityConcerns, ", ")))
			}
			b.WriteString("\n")
			summaryContextAdded = true
		}
	}
	if !summaryContextAdded {
		b.WriteString("No files selected for summary context.\n\n")
	}
	b.WriteString("--- End of full content from workspace ---\n")
	return b.String()
}

// getLanguageFromFilename infers the programming language from the file extension.
func getLanguageFromFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".hpp":
		return "cpp"
	case ".sh", ".bash":
		return "bash"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".xml":
		return "xml"
	case ".sql":
		return "sql"
	case ".rb":
		return "ruby"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".rs":
		return "rust"
	case ".dart":
		return "dart"
	case ".pl", ".pm":
		return "perl"
	case ".lua":
		return "lua"
	case ".vim":
		return "vimscript"
	case ".toml":
		return "toml"
	default:
		return "text"
	}
}