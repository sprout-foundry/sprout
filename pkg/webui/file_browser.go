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
)

// handleAPIBrowse handles API requests for directory browsing
func (ws *ReactWebServer) handleAPIBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	// Get directory from query parameter
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = "."
	}
	canonicalDir, err := canonicalizePath(dir, workspaceRoot, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid directory: %v", err), http.StatusBadRequest)
		return
	}
	if !isWithinWorkspace(canonicalDir, workspaceRoot) {
		http.Error(w, "Directory outside workspace", http.StatusForbidden)
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
		http.Error(w, fmt.Sprintf("Failed to read directory: %v", err), http.StatusInternalServerError)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Method not allowed"})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "path is required"})
		return
	}

	canonicalPath, err := canonicalizePath(req.Path, workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("invalid path: %v", err)})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "no file browser command available"})
		return
	}

	if err := cmd.Start(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("failed to open file browser: %v", err)})
		return
	}
	// Reap the child process to avoid zombies; file browsers detach on their own.
	go func() { _ = cmd.Wait() }()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "opened"})
}
