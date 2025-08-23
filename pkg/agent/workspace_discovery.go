package agent

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// Note: WorkspaceInfo is defined in types.go in this package

// extractSearchTerms extracts key search terms from user intent
func extractSearchTerms(intentLower string) []string {
	var terms []string
	words := strings.Fields(intentLower)
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if len(word) > 2 {
			terms = append(terms, word)
		}
	}
	// Add crude function name heuristics when phrases are present
	if strings.Contains(intentLower, "greet") {
		terms = append(terms, "greet")
	}
	if strings.Contains(intentLower, "println") {
		terms = append(terms, "println")
	}
	if strings.Contains(intentLower, "codereviews") || strings.Contains(intentLower, "codereview") {
		terms = append(terms, "review", "code", "getcodereview")
	}
	if strings.Contains(intentLower, "orchestration model") {
		terms = append(terms, "orchestration", "model")
	}
	if strings.Contains(intentLower, "editing model") {
		terms = append(terms, "editing", "model", "editor")
	}
	uniqueTerms := make(map[string]bool)
	var result []string
	for _, term := range terms {
		if !uniqueTerms[term] && len(term) > 2 {
			uniqueTerms[term] = true
			result = append(result, term)
		}
	}
	return result
}

// findFilesUsingShellCommands uses shell commands to find relevant files when other methods fail
func findFilesUsingShellCommands(userIntent string, workspaceInfo *WorkspaceInfo, logger *utils.Logger) []string {
	logger.Logf("Using shell commands to find files for: %s", userIntent)
	var foundFiles []string
	intentLower := strings.ToLower(userIntent)
	searchTerms := extractSearchTerms(intentLower)
	logger.Logf("Shell search terms: %v", searchTerms)
	for _, term := range searchTerms {
		if len(term) < 3 {
			continue
		}
		logger.Logf("Searching for files containing: %s", term)
		// Search both code and docs (go and markdown/text)
		cmd := exec.Command("sh", "-c", fmt.Sprintf("command -v rg >/dev/null 2>&1 && rg -n -l -i --glob '*.go' --glob '*.md' --glob '*.txt' %q . || (grep -r -n -l -i --include=*.go --include=*.md --include=*.txt %q .)", term, term))
		output, err := cmd.Output()
		if err != nil {
			logger.Logf("Search for '%s' failed: %v", term, err)
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			if idx := strings.Index(line, ":"); idx > -1 {
				line = line[:idx]
			}
			if strings.HasSuffix(line, ".go") || strings.HasSuffix(strings.ToLower(line), ".md") || strings.HasSuffix(strings.ToLower(line), ".txt") {
				cleanPath := strings.TrimPrefix(line, "./")
				foundFiles = append(foundFiles, cleanPath)
				logger.Logf("Shell search found: %s (contains '%s')", cleanPath, term)
			}
		}
	}
	seen := make(map[string]bool)
	var unique []string
	for _, file := range foundFiles {
		if !seen[file] && len(unique) < 5 {
			seen[file] = true
			unique = append(unique, file)
		}
	}
	if len(unique) == 0 {
		logger.Logf("No content matches, trying filename search...")
		for _, term := range searchTerms {
			cmd := exec.Command("find", ".", "-name", "*.go", "-path", fmt.Sprintf("*%s*", term))
			output, err := cmd.Output()
			if err != nil {
				continue
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				if line != "" && strings.HasSuffix(line, ".go") {
					cleanPath := strings.TrimPrefix(line, "./")
					if !seen[cleanPath] && len(unique) < 5 {
						seen[cleanPath] = true
						unique = append(unique, cleanPath)
						logger.Logf("Filename search found: %s", cleanPath)
					}
				}
			}
		}
	}
	logger.Logf("Shell commands found %d unique files: %v", len(unique), unique)
	return unique
}
