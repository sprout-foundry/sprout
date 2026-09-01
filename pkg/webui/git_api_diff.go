//go:build !js

package webui

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxDiffBytes = 200000

// handleAPIGitDiff handles git diff requests for a specific file
func (ws *ReactWebServer) handleAPIGitDiff(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	reqPath := normalizeGitPath(r.URL.Query().Get("path"))
	if reqPath == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "Path is required")
		return
	}

	// Convert absolute paths to workspace-relative for git operations.
	reqPath = makeGitRelativePath(reqPath, workspaceRoot)

	// Return empty diffs gracefully when not in a git repository.
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":       "success",
			"path":          reqPath,
			"has_staged":    false,
			"has_unstaged":  false,
			"staged_diff":   "",
			"unstaged_diff": "",
			"diff":          "No diff available for this file.",
		})
		return
	}

	stagedDiff, err := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--cached", "--", reqPath)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_get_staged_diff", fmt.Sprintf("Failed to get staged diff: %v", err))
		return
	}

	unstagedDiff, err := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--", reqPath)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_get_unstaged_diff", fmt.Sprintf("Failed to get unstaged diff: %v", err))
		return
	}

	// For untracked files, generate a synthetic diff against /dev/null.
	if strings.TrimSpace(stagedDiff) == "" && strings.TrimSpace(unstagedDiff) == "" {
		absPath := reqPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workspaceRoot, absPath)
		}
		// Only try the synthetic diff if the file actually exists on disk
		// (it may be clean/committed, in which case we just return empty diffs).
		if _, statErr := os.Stat(absPath); statErr == nil {
			// Check if the file is tracked by git. If it is, skip the synthetic diff.
			cmd := ws.gitCommandForWorkspace(workspaceRoot, "ls-files", "--error-unmatch", "--", reqPath)
			if cmd.Run() != nil {
				// File is not tracked, so it's untracked - generate synthetic diff
				untrackedDiff, untrackedErr := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--no-index", "--", "/dev/null", reqPath)
				if untrackedErr == nil {
					unstagedDiff = untrackedDiff
				}
			}
			// If the file IS tracked, we leave diffs empty (file is clean)
		}
	}

	stagedDiff = truncateDiffOutput(stagedDiff, maxDiffBytes)
	unstagedDiff = truncateDiffOutput(unstagedDiff, maxDiffBytes)

	var combined strings.Builder
	if strings.TrimSpace(stagedDiff) != "" {
		combined.WriteString("### Staged changes\n")
		combined.WriteString(stagedDiff)
		if !strings.HasSuffix(stagedDiff, "\n") {
			combined.WriteString("\n")
		}
	}
	if strings.TrimSpace(unstagedDiff) != "" {
		if combined.Len() > 0 {
			combined.WriteString("\n")
		}
		combined.WriteString("### Unstaged changes\n")
		combined.WriteString(unstagedDiff)
		if !strings.HasSuffix(unstagedDiff, "\n") {
			combined.WriteString("\n")
		}
	}
	if combined.Len() == 0 {
		combined.WriteString("No diff available for this file.")
	}

	// Full file contents for the editable merge view. Reconstructing
	// documents from the hunks alone produces fragments (context lines
	// glued together) — saving such a fragment back to disk destroys the
	// rest of the file. When extraction succeeds the frontend uses these
	// verbatim; when it fails (size cap) the merge view degrades to
	// read-only on the fragment reconstruction.
	originalContent, modifiedContent, contentsTruncated := ws.gitDiffFileContents(workspaceRoot, reqPath)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":            "success",
		"path":               reqPath,
		"has_staged":         strings.TrimSpace(stagedDiff) != "",
		"has_unstaged":       strings.TrimSpace(unstagedDiff) != "",
		"staged_diff":        stagedDiff,
		"unstaged_diff":      unstagedDiff,
		"diff":               combined.String(),
		"original_content":   originalContent,
		"modified_content":   modifiedContent,
		"contents_truncated": contentsTruncated,
	})
}

// maxDiffFileContentBytes caps the full-file payloads returned for the
// merge view. Above this, the editable view is not offered (read-only
// fragment view instead) rather than shipping megabytes of JSON.
const maxDiffFileContentBytes = 2 * 1024 * 1024

// looksBinary applies git's heuristic: a NUL byte within the first 8000
// bytes marks the content binary. Binary contents are omitted from the
// merge view — shipping raw bytes as a string produces garbage UTF-8 and
// an unreadable/mojibake editor buffer.
func looksBinary(data []byte) bool {
	scanLen := len(data)
	if scanLen > 8000 {
		scanLen = 8000
	}
	return bytes.IndexByte(data[:scanLen], 0) >= 0
}

// gitDiffFileContents returns the full before/after contents for a file in
// the working tree, for the editable merge view.
//
//	old = HEAD version (or "" when the file is new/untracked)
//	new = working-tree contents ("" when the file was deleted)
//
// The third return is true when either side was omitted for exceeding
// maxDiffFileContentBytes or being binary. Git errors leave strings empty —
// the diff text remains authoritative for display; only editability is
// affected.
func (ws *ReactWebServer) gitDiffFileContents(workspaceRoot, reqPath string) (string, string, bool) {
	truncated := false

	var original string
	if cmd := ws.gitCommandForWorkspace(workspaceRoot, "show", "HEAD:"+reqPath); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			if len(out) > maxDiffFileContentBytes || looksBinary(out) {
				truncated = true
			} else {
				original = string(out)
			}
		}
		// Errors (untracked file, no HEAD yet) leave original empty —
		// correct: a new file's "before" is the empty file.
	}

	var modified string
	absPath := reqPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspaceRoot, reqPath)
	}
	if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
		if info.Size() > maxDiffFileContentBytes {
			truncated = true
		} else {
			if data, readErr := os.ReadFile(absPath); readErr == nil {
				if looksBinary(data) {
					truncated = true
				} else {
					modified = string(data)
				}
			}
		}
	}
	// File missing from the working tree (deleted) — after-side stays empty.

	return original, modified, truncated
}
