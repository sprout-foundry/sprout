//go:build !js

package webui

// git_api_review_heuristics.go — heuristic diff analysis for the
// deep-review endpoints: project-type detection, the staged-changes
// summary, key-comment extraction and importance, change categorization,
// and per-file context extraction. Split out of git_api_review.go.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/codereview"
)

func (ws *ReactWebServer) gitReviewDetectProjectType(workspaceRoot string) string {
	projectMarkers := []struct {
		name string
		file string
	}{
		{name: "Go project", file: "go.mod"},
		{name: "Node.js project", file: "package.json"},
		{name: "Python project", file: "requirements.txt"},
		{name: "Python project", file: "setup.py"},
		{name: "Python project", file: "pyproject.toml"},
		{name: "Rust project", file: "Cargo.toml"},
		{name: "Ruby project", file: "Gemfile"},
	}

	for _, marker := range projectMarkers {
		if _, err := os.Stat(filepath.Join(workspaceRoot, marker.file)); err == nil {
			return marker.name
		}
	}
	return ""
}

func (ws *ReactWebServer) gitReviewExtractStagedChangesSummary(workspaceRoot string) string {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	statLines := strings.Split(string(output), "\n")
	if len(statLines) > 0 && strings.TrimSpace(statLines[0]) != "" {
		return fmt.Sprintf("Staged changes summary: %s", strings.TrimSpace(statLines[0]))
	}
	return ""
}

func gitReviewExtractKeyCommentsFromDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	keyComments := make([]string, 0, 8)
	currentFile := ""

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
			continue
		}

		if strings.HasPrefix(line, "+") && (strings.Contains(line, "//") || strings.Contains(line, "#")) {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "+"))
			if gitReviewIsImportantComment(comment) {
				keyComments = append(keyComments, fmt.Sprintf("- %s: %s", currentFile, comment))
			}
		}
	}

	if len(keyComments) == 0 {
		return ""
	}
	if len(keyComments) > 10 {
		keyComments = keyComments[:10]
	}
	return strings.Join(keyComments, "\n")
}

func gitReviewIsImportantComment(comment string) bool {
	return codereview.IsImportantComment(comment)
}

func gitReviewCategorizeChanges(diff string) string {
	lines := strings.Split(diff, "\n")
	categories := make(map[string]int)

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index") {
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			addedLine := strings.TrimPrefix(line, "+")
			if strings.Contains(strings.ToUpper(addedLine), "SECURITY") ||
				strings.Contains(addedLine, "filesystem.ErrOutsideWorkingDirectory") ||
				strings.Contains(addedLine, "WithSecurityBypass") {
				categories["Security fixes/improvements"]++
			}
			if strings.Contains(addedLine, "error") ||
				strings.Contains(addedLine, "Err") ||
				strings.Contains(addedLine, "return nil") ||
				strings.Contains(addedLine, "if err") {
				categories["Error handling"]++
			}
			if strings.Contains(addedLine, "require(") ||
				strings.Contains(addedLine, "github.com/") ||
				strings.Contains(addedLine, "go.mod") {
				categories["Dependency updates"]++
			}
			if strings.Contains(addedLine, "Test") || strings.Contains(addedLine, "test") {
				categories["Test changes"]++
			}
		}

		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			categories["Code removal/refactoring"]++
		}
	}

	if len(categories) == 0 {
		return ""
	}

	linesOut := make([]string, 0, len(categories))
	for category, count := range categories {
		linesOut = append(linesOut, fmt.Sprintf("- %s (%d changes)", category, count))
	}
	return strings.Join(linesOut, "\n")
}

func (ws *ReactWebServer) gitReviewExtractFileContextForChanges(workspaceRoot, diff string) string {
	lines := strings.Split(diff, "\n")
	changedFiles := make(map[string]bool)

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				changedFiles[strings.TrimPrefix(parts[3], "b/")] = true
			}
		}
	}

	contextParts := make([]string, 0, len(changedFiles))
	for relPath := range changedFiles {
		if !ws.gitReviewIsValidRepoFilePath(workspaceRoot, relPath) || gitReviewShouldSkipFileForContext(relPath) {
			continue
		}

		absPath := filepath.Join(workspaceRoot, relPath)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}

		// Skip files that exceed the read size limit
		if fi, statErr := os.Stat(absPath); statErr == nil && fi.Size() > maxFileReadSize {
			continue
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		fileLines := strings.Split(string(content), "\n")
		maxLines := 500
		if len(fileLines) < maxLines {
			maxLines = len(fileLines)
		}
		if maxLines > 0 {
			contextParts = append(contextParts, fmt.Sprintf("### %s\n```go\n%s\n```", relPath, strings.Join(fileLines[:maxLines], "\n")))
		}
	}

	if len(contextParts) == 0 {
		return ""
	}
	return strings.Join(contextParts, "\n\n")
}
