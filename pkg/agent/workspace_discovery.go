package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// findRelevantFilesByContent searches for files containing relevant content based on the user intent
func findRelevantFilesByContent(userIntent string, logger *utils.Logger) []string {
	intentLower := strings.ToLower(userIntent)
	searchTerms := extractSearchTerms(intentLower)
	if len(searchTerms) == 0 {
		logger.Logf("No search terms extracted from intent, returning empty list")
		return []string{}
	}
	logger.Logf("Searching for files containing terms: %v", searchTerms)

	relevantFiles := make(map[string]int)
	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !isSourceFile(path) {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		contentLower := strings.ToLower(string(content))
		score := 0
		for _, term := range searchTerms {
			if strings.Contains(contentLower, term) {
				score += 10
				if strings.Contains(contentLower, "func "+term) ||
					strings.Contains(contentLower, "type "+term) ||
					strings.Contains(contentLower, term+"(") {
					score += 20
				}
			}
		}
		pathLower := strings.ToLower(path)
		for _, term := range searchTerms {
			if strings.Contains(pathLower, term) {
				score += 15
			}
		}
		if score > 0 {
			relevantFiles[path] = score
			logger.Logf("Found relevant file: %s (score: %d)", path, score)
		}
		return nil
	})

	type fileScore struct {
		path  string
		score int
	}
	var scored []fileScore
	for file, score := range relevantFiles {
		scored = append(scored, fileScore{file, score})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

	var result []string
	maxFiles := 5
	for i, fs := range scored {
		if i >= maxFiles {
			break
		}
		result = append(result, fs.path)
	}
	if len(result) == 0 {
		logger.Logf("No files found by content search")
		return []string{}
	}
	logger.Logf("Content search found %d relevant files: %v", len(result), result)
	return result
}

// extractSearchTerms extracts key search terms from user intent
func extractSearchTerms(intentLower string) []string {
	var terms []string
	words := strings.Fields(intentLower)
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if len(word) > 3 && (strings.Contains(word, "orchestration") ||
			strings.Contains(word, "editing") || strings.Contains(word, "model") ||
			strings.Contains(word, "review") || strings.Contains(word, "code") ||
			strings.Contains(word, "llm") || strings.Contains(word, "api") ||
			strings.Contains(word, "config") || strings.Contains(word, "prompt") ||
			strings.Contains(word, "editor") || strings.Contains(word, "embedding")) {
			terms = append(terms, word)
		}
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
		cmd := exec.Command("sh", "-c", fmt.Sprintf("command -v rg >/dev/null 2>&1 && rg -n -l -i --glob '*.go' %q . || grep -r -n -l -i --include=*.go %q .", term, term))
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
			if strings.HasSuffix(line, ".go") {
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
