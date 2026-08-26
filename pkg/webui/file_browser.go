//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
	"github.com/sprout-foundry/sprout/pkg/filediscovery"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// handleAPIBrowse handles API requests for directory browsing
func (ws *ReactWebServer) handleAPIBrowse(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	// Home-workspace hardening (SP-130). When the workspace root resolves
	// to the user's home directory without explicit consent, refuse the
	// listing. In daemon/service mode the CWD-derived workspace IS $HOME
	// until the user picks a project, and each directory listing of a
	// TCC-protected folder (Documents, Music, …) raises a macOS privacy
	// prompt. The frontend shows the workspace gate instead; until a
	// real workspace is selected there is nothing legitimate to browse.
	if isHomeWorkspace(workspaceRoot) && !hasHomeWorkspaceConsent() {
		writeJSONErr(w, http.StatusForbidden, "workspace_not_selected",
			"Workspace is the home directory and no consent was granted; select a workspace first")
		return
	}
	// Get directory from query parameter
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = "."
	}
	canonicalDir, err := canonicalizePath(dir, workspaceRoot, false)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_directory", fmt.Sprintf("Invalid directory: %v", err))
		return
	}
	if !isWithinWorkspace(canonicalDir, workspaceRoot) {
		writeJSONErr(w, http.StatusForbidden, "directory_outside_workspace", "Directory outside workspace")
		return
	}

	// Determine whether to filter out gitignored entries
	filterIgnored := r.URL.Query().Get("ignore") == "true"
	var ignoreRules *ignore.GitIgnore
	if filterIgnored {
		ignoreRules = filediscovery.GetIgnoreRules(workspaceRoot)
	}

	// Read directory
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_read_directory", fmt.Sprintf("Failed to read directory: %v", err))
		return
	}

	// Convert to JSON response
	var files []map[string]interface{}
	for _, entry := range entries {
		name := entry.Name()
		isDir := entry.IsDir()

		// Always skip the .git directory
		if isDir && name == ".git" {
			continue
		}

		// Skip entries that match gitignore rules when filtering is enabled
		if filterIgnored && ignoreRules != nil {
			absPath := filepath.Join(canonicalDir, name)
			relPath, _ := filepath.Rel(workspaceRoot, absPath)
			if ignoreRules.MatchesPath(relPath) || (isDir && ignoreRules.MatchesPath(relPath+"/")) {
				continue
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := map[string]interface{}{
			"name": name,
			"path": filepath.Join(canonicalDir, name),
			"type": "file",
		}

		if isDir {
			fileInfo["type"] = "directory"
		}

		if info != nil {
			fileInfo["size"] = info.Size()
			fileInfo["modified"] = info.ModTime().Unix()
		}

		files = append(files, fileInfo)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "success",
		"path":    canonicalDir,
		"files":   files,
	})
}

// handleAPIOpenInFileBrowser opens a path (or its parent directory for files)
// in the system file browser using the platform-appropriate command.
func (ws *ReactWebServer) handleAPIOpenInFileBrowser(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "path is required"})
		return
	}

	canonicalPath, err := canonicalizePath(req.Path, workspaceRoot, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": fmt.Sprintf("invalid path: %v", err)})
		return
	}

	var cmd *exec.Cmd
	info, statErr := os.Stat(canonicalPath)
	isDir := statErr == nil && info.IsDir()

	switch {
	case shellExists("open"):
		// macOS: "open -R <file>" reveals in Finder; "open <dir>" opens the dir
		if isDir {
			cmd = exec.Command("open", canonicalPath)
		} else {
			cmd = exec.Command("open", "-R", canonicalPath)
		}
	case shellExists("explorer.exe"):
		// Windows / WSL: convert Linux paths to Windows paths for WSL support.
		// On native Windows, canonicalPath is already a Windows path so wslToWindowsPath
		// returns it unchanged. On WSL, wslpath -w translates /home/... to
		// \\wsl.localhost\<distro>\... and /mnt/c/... to C:\...
		winPath := canonicalPath
		if wslToWindowsPath != nil {
			if converted, err := wslToWindowsPath(canonicalPath); err == nil {
				winPath = converted
			}
		}
		if isDir {
			cmd = exec.Command("explorer.exe", winPath)
		} else {
			cmd = exec.Command("explorer.exe", "/select,"+winPath)
		}
	case shellExists("xdg-open"):
		// Linux: open the containing directory (xdg-open can't select a file)
		target := canonicalPath
		if !isDir {
			target = filepath.Dir(canonicalPath)
		}
		cmd = exec.Command("xdg-open", target)
	case shellExists("nautilus"):
		cmd = exec.Command("nautilus", "--select", canonicalPath)
	default:
		writeJSON(w, http.StatusNotImplemented, map[string]interface{}{"error": "no file browser command available"})
		return
	}

	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": fmt.Sprintf("failed to open file browser: %v", err)})
		return
	}
	// Reap the child process to avoid zombies; file browsers detach on their own.
	utils.SafeGo(webuiLogger, "file browser wait", func() { _ = cmd.Wait() })

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "opened"})
}
