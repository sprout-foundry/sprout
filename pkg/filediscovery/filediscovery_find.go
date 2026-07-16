// Package filediscovery: shell-based file finding (split from filediscovery.go)

package filediscovery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// parseQueryTerms parses a query into file patterns and search terms
func (fd *FileDiscovery) parseQueryTerms(query string) *QueryTerms {
	terms := &QueryTerms{
		FilePatterns: []string{},
		SearchTerms:  []string{},
	}

	// Split query by spaces
	parts := strings.Fields(query)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a file pattern (contains wildcards)
		if strings.Contains(part, "*") || strings.Contains(part, "?") {
			terms.FilePatterns = append(terms.FilePatterns, part)
		} else {
			// Treat as search term
			terms.SearchTerms = append(terms.SearchTerms, part)
		}
	}

	return terms
}

// QueryTerms represents parsed query terms
type QueryTerms struct {
	FilePatterns []string
	SearchTerms  []string
}

// findFilesUsingShellCommands finds files using shell commands
func (fd *FileDiscovery) findFilesUsingShellCommands(query string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	// Parse the query to extract file patterns and search terms
	terms := fd.parseQueryTerms(query)

	// Use different strategies based on the query type
	if len(terms.FilePatterns) > 0 {
		// Use find command for file pattern matching
		found = fd.findWithFindCommand(terms.FilePatterns, workspaceInfo)
	} else if len(terms.SearchTerms) > 0 {
		// Use grep command for content searching
		found = fd.findWithGrepCommand(terms.SearchTerms, workspaceInfo)
	} else {
		// Fallback to directory walk with basic filtering
		found = fd.findWithDirectoryWalk(query, workspaceInfo)
	}

	// Remove duplicates and apply security filtering
	return fd.deduplicateAndFilter(found, workspaceInfo)
}

// findWithFindCommand uses the find command to locate files by pattern
func (fd *FileDiscovery) findWithFindCommand(patterns []string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	for _, pattern := range patterns {
		// Build find command
		cmd := exec.Command("find", workspaceInfo.RootDir,
			"-type", "f", // Only files
			"-name", pattern, // Match pattern
			"-not", "-path", "*/.*", // Exclude hidden files
			"-not", "-path", "*/node_modules/*",
			"-not", "-path", "*/vendor/*",
			"-not", "-path", "*/target/*",
			"-not", "-path", "*/build/*",
			"-not", "-path", "*/dist/*")

		output, err := cmd.Output()
		if err != nil {
			// Log error but continue with other patterns
			continue
		}

		// Parse output
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				found = append(found, line)
			}
		}
	}

	return found
}

// findWithGrepCommand uses grep to search file contents
func (fd *FileDiscovery) findWithGrepCommand(searchTerms []string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	for _, term := range searchTerms {
		// Build grep command
		// Note: -l lists filenames only, -H includes filename headers
		cmd := exec.Command("grep", "-r", // Recursive
			"--include=*.go", "--include=*.js", "--include=*.ts", "--include=*.py",
			"--include=*.java", "--include=*.cpp", "--include=*.c", "--include=*.rs",
			"--include=*.php", "--include=*.rb", "--include=*.swift", "--include=*.kt",
			"--include=*.html", "--include=*.css", "--include=*.json", "--include=*.xml",
			"--include=*.yaml", "--include=*.yml", "--include=*.md", "--include=*.txt",
			"-l", // Only list filenames
			term, workspaceInfo.RootDir)

		output, err := cmd.Output()
		if err != nil {
			// Log error but continue with other terms
			continue
		}

		// Parse output (format: filename per line when using -l)
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				found = append(found, line)
			}
		}
	}

	return found
}

// findWithDirectoryWalk falls back to directory walking
func (fd *FileDiscovery) findWithDirectoryWalk(query string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	// Simple directory walk as fallback
	err := filepath.Walk(workspaceInfo.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip directories and hidden files
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Skip common non-source directories
		if strings.Contains(path, "node_modules") ||
			strings.Contains(path, "vendor") ||
			strings.Contains(path, "target") ||
			strings.Contains(path, "build") ||
			strings.Contains(path, "dist") {
			return nil
		}

		// Basic filename matching
		if strings.Contains(strings.ToLower(info.Name()), strings.ToLower(query)) {
			found = append(found, path)
		}

		return nil
	})

	if err != nil {
		// Log error but return what we found
	}

	return found
}

// deduplicateAndFilter removes duplicates and applies security filtering
func (fd *FileDiscovery) deduplicateAndFilter(files []string, workspaceInfo *WorkspaceInfo) []string {
	seen := make(map[string]bool)
	var result []string

	for _, file := range files {
		// Convert to absolute path
		absPath, err := filepath.Abs(file)
		if err != nil {
			continue
		}

		// Skip if already seen
		if seen[absPath] {
			continue
		}
		seen[absPath] = true

		// Security: ensure file is within workspace
		if !strings.HasPrefix(absPath, workspaceInfo.RootDir) {
			continue
		}

		// Check if file exists and is readable
		if info, err := os.Stat(absPath); err != nil || info.IsDir() {
			continue
		}

		result = append(result, absPath)
	}

	return result
}

// findFilesUsingShellCommandsFallback finds files using shell commands (fallback method)
func (fd *FileDiscovery) findFilesUsingShellCommandsFallback(query string, options *DiscoveryOptions) []string {
	found := []string{}

	// Use find command as fallback
	root := "."
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			// Simple string matching
			if strings.Contains(strings.ToLower(path), strings.ToLower(query)) {
				found = append(found, path)
			}
		}
		return nil
	}); err != nil {
		if fd.logger != nil {
			fd.logger.LogError(utils.WrapError(err, "shell command file discovery failed"))
		}
	}

	return found
}
