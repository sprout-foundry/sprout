package codereview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// diff_context.go — heuristic diff analysis for the WebUI deep-review
// endpoints (pkg/webui). The CLI's single-call review uses the richer
// staged_context.go (StagedContext); DetectProjectType and CategorizeChanges
// live there — this file keeps the webui-shaped helpers that have no
// staged_context counterpart.

// maxContextFileBytes bounds the per-file read in ExtractFileContext. The
// WebUI enforced its own 10 MiB limit before this consolidation; the CLI had
// none (a pathologically large staged file was read whole). The shared bound
// applies to both.
const maxContextFileBytes = 10 << 20

// ExtractKeyCommentsFromDiff scans added diff lines for comments ("//" or
// "#") judged important by IsImportantComment and returns up to 10 of them,
// each prefixed with the file it appeared in. "" when none are found.
func ExtractKeyCommentsFromDiff(diff string) string {
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
			if IsImportantComment(comment) {
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

// ExtractFileContextForChanges returns up to the first 500 lines of each
// changed file (skipping files excluded by ShouldSkipFileForContext and paths
// escaping workspaceRoot), formatted as markdown code blocks. "" when no
// readable files remain.
func ExtractFileContextForChanges(workspaceRoot, diff string) string {
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
		if !IsValidRepoFilePath(workspaceRoot, relPath) || ShouldSkipFileForContext(relPath) {
			continue
		}

		absPath := filepath.Join(workspaceRoot, relPath)
		fi, statErr := os.Stat(absPath)
		if statErr != nil || os.IsNotExist(statErr) {
			continue
		}
		if fi.Size() > maxContextFileBytes {
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

// ShouldSkipFileForContext reports whether relPath should be excluded from
// review context extraction: lockfiles, minified bundles, generated code,
// coverage artifacts, binaries/images, and vendored or git-internal paths.
func ShouldSkipFileForContext(filePath string) bool {
	if utils.ClassifyReviewFile(filePath).SkipForReview {
		return true
	}

	if strings.HasSuffix(filePath, ".sum") ||
		strings.HasSuffix(filePath, ".lock") ||
		strings.HasSuffix(filePath, "package-lock.json") ||
		strings.HasSuffix(filePath, "yarn.lock") {
		return true
	}
	if strings.Contains(filePath, ".min.") ||
		strings.HasSuffix(filePath, ".map") ||
		strings.Contains(filePath, "node_modules/") {
		return true
	}
	if strings.HasSuffix(filePath, ".pb.go") ||
		strings.Contains(filePath, "_generated.go") ||
		strings.Contains(filePath, "_generated.") {
		return true
	}
	if strings.HasSuffix(filePath, "coverage.out") ||
		strings.HasSuffix(filePath, "coverage.html") ||
		strings.HasSuffix(filePath, ".test") ||
		strings.HasSuffix(filePath, ".out") {
		return true
	}
	if strings.HasSuffix(filePath, ".svg") ||
		strings.HasSuffix(filePath, ".png") ||
		strings.HasSuffix(filePath, ".jpg") ||
		strings.HasSuffix(filePath, ".ico") {
		return true
	}
	return strings.Contains(filePath, "vendor/") || strings.Contains(filePath, ".git/")
}

// IsValidRepoFilePath reports whether relPath is a safe in-repo path: no
// ".." traversal and the cleaned absolute path stays inside workspaceRoot.
// An absolute relPath is checked as-is (not re-anchored under workspaceRoot),
// so "/etc/passwd" against any workspace root is rejected.
func IsValidRepoFilePath(workspaceRoot, relPath string) bool {
	if strings.Contains(relPath, "..") {
		return false
	}

	cleanRel := filepath.Clean(relPath)
	var absPath string
	if filepath.IsAbs(cleanRel) {
		absPath = cleanRel
	} else {
		var err error
		absPath, err = filepath.Abs(filepath.Join(workspaceRoot, cleanRel))
		if err != nil {
			return false
		}
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) || absPath == absRoot
}
