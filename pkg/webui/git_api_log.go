package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// GitCommit is a single commit entry in the git log response.
type GitCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Author    string `json:"author"`
	Date      string `json:"date"`
	Message   string `json:"message"`
	RefNames  string `json:"ref_names,omitempty"`
}

// handleAPIGitLog returns a paginated list of past commits.
// Query params: limit (default 30, max 100), offset (default 0).
func (ws *ReactWebServer) handleAPIGitLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Verify git repo
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		http.Error(w, "Not a git repository", http.StatusBadRequest)
		return
	}

	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		fmt.Sscanf(raw, "%d", &limit)
		if limit <= 0 {
			limit = 30
		}
		if limit > 100 {
			limit = 100
		}
	}

	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		fmt.Sscanf(raw, "%d", &offset)
		if offset < 0 {
			offset = 0
		}
	}

	// Use a custom format to parse structured commit data.
	// Format: hash<NUL>author<NUL>date<NUL>ref_names<NUL>message
	// %H=full hash, %an=author name, %aI=author date (strict ISO 8601),
	// %D=ref names, %s=subject line.
	format := "%H%x00%an%x00%aI%x00%D%x00%s"
	args := []string{
		"log",
		fmt.Sprintf("--skip=%d", offset),
		fmt.Sprintf("-n%d", limit),
		fmt.Sprintf("--format=%s", format),
	}

	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get git log: %v", err), http.StatusInternalServerError)
		return
	}

	commits := make([]GitCommit, 0, limit)
	for _, block := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "\x00", 5)
		if len(parts) < 5 {
			continue
		}

		hash := parts[0]
		shortHash := hash
		if len(hash) > 7 {
			shortHash = hash[:7]
		}

		commits = append(commits, GitCommit{
			Hash:      hash,
			ShortHash: shortHash,
			Author:    parts[1],
			Date:      parts[2],
			Message:   parts[4],
			RefNames:  strings.TrimSpace(parts[3]),
		})
	}

	// Get total commit count for pagination — only needed when we received a full page.
	totalCount := 0
	if len(commits) == limit {
		countCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-list", "--count", "HEAD")
		countOutput, countErr := countCmd.Output()
		if countErr == nil {
			fmt.Sscanf(strings.TrimSpace(string(countOutput)), "%d", &totalCount)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"commits": commits,
		"offset":  offset,
		"limit":   limit,
		"total":   totalCount,
	})
}

// handleAPIGitCommitShow returns the full diff and metadata for a single commit.
func (ws *ReactWebServer) handleAPIGitCommitShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		http.Error(w, "hash is required", http.StatusBadRequest)
		return
	}

	// Validate that the hash refers to an actual commit.
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "cat-file", "-t", hash)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid commit hash: %s", strings.TrimSpace(string(output))), http.StatusBadRequest)
		return
	} else if strings.TrimSpace(string(output)) != "commit" {
		http.Error(w, "hash does not refer to a commit", http.StatusBadRequest)
		return
	}

	// Get commit metadata.
	format := "%H%x00%an%x00%aI%x00%D%x00%s"
	metaCmd := ws.gitCommandForWorkspace(workspaceRoot, "log", "-1", fmt.Sprintf("--format=%s", format), hash)
	metaOutput, err := metaCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get commit metadata: %v", err), http.StatusInternalServerError)
		return
	}
	metaStr := strings.TrimSpace(string(metaOutput))
	metaParts := strings.SplitN(metaStr, "\x00", 5)
	if len(metaParts) < 5 {
		http.Error(w, "Failed to parse commit metadata", http.StatusInternalServerError)
		return
	}

	fullHash := metaParts[0]
	shortHash := fullHash
	if len(fullHash) > 7 {
		shortHash = fullHash[:7]
	}

	// Get the diff.
	showCmd := ws.gitCommandForWorkspace(workspaceRoot, "show", "--format=", "--patch", hash)
	showOutput, err := showCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get commit diff: %v", err), http.StatusInternalServerError)
		return
	}
	diff := string(showOutput)
	diff = truncateDiffOutput(diff, 500000)

	// Get file list with name-status.
	nameStatusCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff-tree", "--no-commit-id", "--name-status", "-r", hash)
	nameStatusOutput, err := nameStatusCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get commit files: %v", err), http.StatusInternalServerError)
		return
	}

	files := make([]GitFile, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(nameStatusOutput)), "\n") {
		if statusChar, path, ok := parseNameStatusLine(line); ok {
			files = append(files, GitFile{
				Path:   path,
				Status: statusChar,
			})
		}
	}

	// Get stat summary.
	statCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff-tree", "--no-commit-id", "--stat", "-r", hash)
	statOutput, err := statCmd.CombinedOutput()
	stats := ""
	if err == nil {
		stats = strings.TrimSpace(string(statOutput))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "success",
		"hash":      fullHash,
		"short_hash": shortHash,
		"author":    metaParts[1],
		"date":      metaParts[2],
		"ref_names": strings.TrimSpace(metaParts[3]),
		"subject":   metaParts[4],
		"files":      files,
		"diff":       diff,
		"stats":      stats,
	})
}

// handleAPIGitCommitFileDiff returns the diff for a single file within a specific commit.
func (ws *ReactWebServer) handleAPIGitCommitFileDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		http.Error(w, "hash is required", http.StatusBadRequest)
		return
	}

	reqPath := normalizeGitPath(r.URL.Query().Get("path"))
	if reqPath == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	// Convert absolute paths to workspace-relative for git operations.
	reqPath = makeGitRelativePath(reqPath, workspaceRoot)

	// Path traversal protection.
	if strings.Contains(reqPath, "..") {
		http.Error(w, "path must not contain '..'", http.StatusBadRequest)
		return
	}

	// Validate that the hash refers to an actual commit.
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "cat-file", "-t", hash)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid commit hash: %s", strings.TrimSpace(string(output))), http.StatusBadRequest)
		return
	} else if strings.TrimSpace(string(output)) != "commit" {
		http.Error(w, "hash does not refer to a commit", http.StatusBadRequest)
		return
	}

	// Get the diff for just the requested file within this commit.
	showCmd := ws.gitCommandForWorkspace(workspaceRoot, "show", "--format=", "--patch", hash, "--", reqPath)
	showOutput, err := showCmd.CombinedOutput()
	if err != nil {
		errStr := strings.TrimSpace(string(showOutput))
		if strings.Contains(errStr, "bad default revision") || strings.Contains(errStr, "did not match any file") {
			http.Error(w, "File not found in this commit", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to get commit diff: %v", err), http.StatusInternalServerError)
		return
	}
	diff := truncateDiffOutput(string(showOutput), 500000)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"hash":    hash,
		"path":    reqPath,
		"diff":    diff,
	})
}

func gitReviewShouldSkipFileForContext(filePath string) bool {
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

func (ws *ReactWebServer) gitReviewIsValidRepoFilePath(workspaceRoot, relPath string) bool {
	if strings.Contains(relPath, "..") {
		return false
	}

	cleanRel := filepath.Clean(relPath)
	absPath, err := filepath.Abs(filepath.Join(workspaceRoot, cleanRel))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) || absPath == absRoot
}
