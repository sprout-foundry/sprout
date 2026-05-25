//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
	"gopkg.in/yaml.v3"
)

// handleAPICreateFile handles API requests for creating new files
func (ws *ReactWebServer) handleAPICreateFile(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path      string `json:"path"`
		Directory string `json:"directory"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid request body: %v", err),
			"code":  "invalid_request_body",
		})
		return
	}

	// Determine the path to create
	var targetPath string
	if req.Path != "" {
		targetPath = req.Path
	} else if req.Directory != "" {
		targetPath = req.Directory
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Either path or directory must be specified",
			"code":  "missing_path_or_directory",
		})
		return
	}

	// Canonicalize and validate the path
	canonicalPath, err := canonicalizePath(targetPath, workspaceRoot, true)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid path: %v", err),
			"code":  "invalid_path",
		})
		return
	}

	if !isWithinWorkspace(canonicalPath, workspaceRoot) && !ws.allowExternalAccessForRequest(r, canonicalPath) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Path outside workspace",
			"code":  "path_outside_workspace",
		})
		return
	}

	// Check if path already exists
	if _, err := os.Stat(canonicalPath); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Path already exists",
			"code":  "path_already_exists",
		})
		return
	}

	// Create file or directory
	if strings.HasSuffix(canonicalPath, "/") || req.Directory != "" {
		// Create directory
		if err := os.MkdirAll(canonicalPath, 0755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Failed to create directory: %v", err),
				"code":  "failed_to_create_directory",
			})
			return
		}
	} else {
		// Create file
		parentDir := filepath.Dir(canonicalPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Failed to create parent directory: %v", err),
				"code":  "failed_to_create_parent_directory",
			})
			return
		}

		file, err := os.Create(canonicalPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Failed to create file: %v", err),
				"code":  "failed_to_create_file",
			})
			return
		}
		defer file.Close()
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(canonicalPath, "created", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"path":    canonicalPath,
	})
}

// handleAPIDeleteItem handles API requests for deleting files or directories
func (ws *ReactWebServer) handleAPIDeleteItem(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodDelete {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Method not allowed",
			"code":  "method_not_allowed",
		})
		return
	}

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid request body: %v", err),
			"code":  "invalid_request_body",
		})
		return
	}

	if req.Path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Path must be specified",
			"code":  "missing_path",
		})
		return
	}

	// Canonicalize and validate the path
	canonicalPath, err := canonicalizePath(req.Path, workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid path: %v", err),
			"code":  "invalid_path",
		})
		return
	}

	if !isWithinWorkspace(canonicalPath, workspaceRoot) && !ws.allowExternalAccessForRequest(r, canonicalPath) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Path outside workspace",
			"code":  "path_outside_workspace",
		})
		return
	}

	// Delete the file or directory
	if err := os.RemoveAll(canonicalPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Failed to delete: %v", err),
			"code":  "failed_to_delete",
		})
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(canonicalPath, "deleted", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"path":    canonicalPath,
	})
}

// handleAPIRenameItem handles API requests for renaming files or directories.
func (ws *ReactWebServer) handleAPIRenameItem(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Method not allowed",
			"code":  "method_not_allowed",
		})
		return
	}

	var req struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid request body: %v", err),
			"code":  "invalid_request_body",
		})
		return
	}

	if req.OldPath == "" || req.NewPath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Old and new paths must be specified",
			"code":  "missing_paths",
		})
		return
	}

	oldCanonicalPath, err := canonicalizePath(req.OldPath, workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid source path: %v", err),
			"code":  "invalid_old_path",
		})
		return
	}

	newCanonicalPath, err := canonicalizePath(req.NewPath, workspaceRoot, true)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid target path: %v", err),
			"code":  "invalid_new_path",
		})
		return
	}

	oldExternal := !isWithinWorkspace(oldCanonicalPath, workspaceRoot)
	newExternal := !isWithinWorkspace(newCanonicalPath, workspaceRoot)
	if (oldExternal && !ws.allowExternalAccessForRequest(r, oldCanonicalPath)) ||
		(newExternal && !ws.allowExternalAccessForRequest(r, newCanonicalPath)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Path outside workspace",
			"code":  "path_outside_workspace",
		})
		return
	}

	if _, err := os.Stat(oldCanonicalPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Source path does not exist: %v", err),
			"code":  "source_not_found",
		})
		return
	}

	if _, err := os.Stat(newCanonicalPath); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Target path already exists",
			"code":  "target_already_exists",
		})
		return
	}

	if err := os.MkdirAll(filepath.Dir(newCanonicalPath), 0755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Failed to create target parent directory: %v", err),
			"code":  "failed_to_create_parent_directory",
		})
		return
	}

	if err := os.Rename(oldCanonicalPath, newCanonicalPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Failed to rename: %v", err),
			"code":  "failed_to_rename",
		})
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(oldCanonicalPath, "deleted", ""))
	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(newCanonicalPath, "created", ""))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "success",
		"old_path": oldCanonicalPath,
		"new_path": newCanonicalPath,
	})
}

// handleAPIGetPrettierConfig handles API requests for Prettier config discovery.
func (ws *ReactWebServer) handleAPIGetPrettierConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get workspace root for this request
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "No workspace configured",
			"code":  "no_workspace",
		})
		return
	}

	// Prettier config filenames to check, in order of precedence.
	// Only includes formats the Go backend can actually parse.
	// JS/CJS/MJS and TOML config files are intentionally omitted because
	// they cannot be safely evaluated or parsed in Go.
	prettierConfigFiles := []string{
		".prettierrc",
		".prettierrc.json",
		".prettierrc.json5",
		".prettierrc.yaml",
		".prettierrc.yml",
	}

	// Merge config from all sources (later ones override earlier ones)
	mergedConfig := make(map[string]interface{})

	// Try to find config in workspace root
	for _, configFile := range prettierConfigFiles {
		configPath := filepath.Join(workspaceRoot, configFile)
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue // File doesn't exist, skip
		}

		var fileConfig map[string]interface{}
		content := strings.TrimSpace(string(data))

		// Parse based on file extension
		switch {
		case strings.HasSuffix(configFile, ".json") || strings.HasSuffix(configFile, ".json5"):
			// JSON or JSON5 (JSON5 is a super-set, plain JSON parser works for most cases)
			if err := json.Unmarshal(data, &fileConfig); err != nil {
				fmt.Printf("[debug] prettier config parse error in %s: %v\n", configFile, err)
				continue
			}
		case strings.HasSuffix(configFile, ".yaml") || strings.HasSuffix(configFile, ".yml"):
			if err := yaml.Unmarshal(data, &fileConfig); err != nil {
				fmt.Printf("[debug] prettier config parse error in %s: %v\n", configFile, err)
				continue
			}
		case configFile == ".prettierrc":
			// Plain .prettierrc - Prettier treats this as JSON by default.
			// Try JSON first, then fall back to key:value pairs.
			if err := json.Unmarshal(data, &fileConfig); err != nil {
				lines := strings.Split(content, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						key := strings.TrimSpace(parts[0])
						val := strings.TrimSpace(parts[1])
						if val == "true" {
							fileConfig[key] = true
						} else if val == "false" {
							fileConfig[key] = false
						} else if num, err := strconv.Atoi(val); err == nil {
							fileConfig[key] = num
						} else {
							fileConfig[key] = val
						}
					}
				}
			}
		default:
			continue
		}

		// Merge into mergedConfig
		for k, v := range fileConfig {
			mergedConfig[k] = v
		}
	}

	// Also check package.json for a "prettier" key
	pkgPath := filepath.Join(workspaceRoot, "package.json")
	pkgData, err := os.ReadFile(pkgPath)
	if err == nil {
		var pkgJSON map[string]interface{}
		if err := json.Unmarshal(pkgData, &pkgJSON); err == nil {
			if prettierKey, ok := pkgJSON["prettier"]; ok {
				if prettierCfg, ok := prettierKey.(map[string]interface{}); ok {
					for k, v := range prettierCfg {
						mergedConfig[k] = v
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mergedConfig)
}
