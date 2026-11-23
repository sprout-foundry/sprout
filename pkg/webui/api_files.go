//go:build !js

package webui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
	"github.com/sprout-foundry/sprout/pkg/filediscovery"
)

// handleAPIFiles handles API requests for file listing
func (ws *ReactWebServer) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	projectFolder := ws.getProjectFolderForRequest(r)
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Home-project-folder hardening (SP-130): same gate as handleAPIBrowse.
	// Listing the daemon's CWD-derived home project folder raises macOS TCC
	// prompts for protected folders and is exactly what the project folder
	// gate exists to prevent.
	if isHomeProjectFolder(projectFolder) && !hasHomeProjectFolderConsent() {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error": "Workspace is the home directory and no consent was granted; select a workspace first",
			"code":  "workspace_not_selected",
		})
		return
	}

	// Optionally skip git status computation (for performance when not needed)
	includeGitStatus := r.URL.Query().Get("git_status") != "false"

	// Get directory from query parameter
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = strings.TrimSpace(r.URL.Query().Get("dir"))
	}
	if dir == "" {
		dir = "."
	}
	canonicalDir, err := canonicalizePath(dir, projectFolder, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": fmt.Sprintf("Invalid directory: %v", err),
			"code":  "invalid_directory",
		})
		return
	}
	if !isWithinProjectFolder(canonicalDir, projectFolder) && !ws.allowExternalAccessForRequest(r, canonicalDir) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error": "Directory outside workspace",
			"code":  "directory_outside_workspace",
		})
		return
	}

	// Read directory
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": fmt.Sprintf("Failed to read directory: %v", err),
			"code":  "failed_to_read_directory",
		})
		return
	}

	// Gather git status information for files in this directory
	var modifiedSet, untrackedSet map[string]bool
	var ignoreRules *ignore.GitIgnore
	if includeGitStatus {
		modifiedSet, untrackedSet = getGitFileStatusMap(projectFolder)
		ignoreRules = filediscovery.GetIgnoreRules(projectFolder)
	}

	// Convert to JSON response
	var files []map[string]interface{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		absPath := filepath.Join(canonicalDir, entry.Name())
		relPath, _ := filepath.Rel(projectFolder, absPath)

		fileInfo := map[string]interface{}{
			"name":     entry.Name(),
			"path":     absPath,
			"relative": relPath,
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
			"mod_time": info.ModTime().Unix(),
		}

		if includeGitStatus {
			gitStatus := getGitStatusForEntry(relPath, entry.IsDir(), modifiedSet, untrackedSet, ignoreRules, projectFolder)
			if gitStatus != "" {
				fileInfo["git_status"] = gitStatus
			}
		}

		files = append(files, fileInfo)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "success",
		"files":     files,
		"path":      canonicalDir,
		"directory": canonicalDir,
	})
}
