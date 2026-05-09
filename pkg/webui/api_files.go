package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filediscovery"
	ignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"
)

// gatherStats collects server statistics
func (ws *ReactWebServer) gatherStats(r *http.Request) map[string]interface{} {
	return ws.gatherStatsForClientID(ws.resolveClientID(r))
}

// wslToWindowsPath is a package-level conversion function set once during init.
// On WSL systems it wraps "wslpath -w"; on non-WSL systems it is nil.
// Using a func var avoids calling exec.LookPath("wslpath") on every request.
var wslToWindowsPath func(linuxPath string) (string, error)

// wslToWindowsPathFunc creates the actual conversion function used by wslToWindowsPath.
func wslToWindowsPathFunc(linuxPath string) (string, error) {
	// Use wslpath to convert Linux path to Windows UNC path.
	// Handles /mnt/c/... → C:\... and /home/... → \\wsl.localhost\<distro>\...
	out, err := exec.Command("wslpath", "-w", linuxPath).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %q: %w", linuxPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	// Detect WSL and set up the path converter.
	// WSL_DISTRO_NAME is set by the WSL runtime environment.
	if os.Getenv("WSL_DISTRO_NAME") != "" && shellExists("wslpath") {
		wslToWindowsPath = wslToWindowsPathFunc
	}
}

// populateAgentStats populates a stats map with fields from the given agent instance.
func populateAgentStats(stats map[string]interface{}, agentInst *agent.Agent) {
	stats["provider"] = agentInst.GetProvider()
	stats["model"] = agentInst.GetModel()
	stats["persona"] = agentInst.GetActivePersona()
	stats["session_id"] = agentInst.GetSessionID()
	stats["total_tokens"] = agentInst.GetTotalTokens()
	stats["prompt_tokens"] = agentInst.GetPromptTokens()
	stats["completion_tokens"] = agentInst.GetCompletionTokens()
	stats["cached_tokens"] = agentInst.GetCachedTokens()
	stats["cache_efficiency"] = float64(0)
	if totalTokens := agentInst.GetTotalTokens(); totalTokens > 0 {
		stats["cache_efficiency"] = float64(agentInst.GetCachedTokens()) / float64(totalTokens) * 100
	}
	stats["cached_cost_savings"] = agentInst.GetCachedCostSavings()
	stats["current_context_tokens"] = agentInst.GetCurrentContextTokens()
	stats["max_context_tokens"] = agentInst.GetMaxContextTokens()
	stats["context_usage_percent"] = float64(0)
	if maxTokens := agentInst.GetMaxContextTokens(); maxTokens > 0 {
		stats["context_usage_percent"] = float64(agentInst.GetCurrentContextTokens()) / float64(maxTokens) * 100
	}
	stats["context_warning_issued"] = agentInst.GetContextWarningIssued()
	stats["total_cost"] = agentInst.GetTotalCost()
	stats["last_tps"] = agentInst.GetLastTPS()
	stats["current_iteration"] = agentInst.GetCurrentIteration()
	if agentInst.GetMaxIterations() == 0 {
		stats["max_iterations"] = "unlimited"
	} else {
		stats["max_iterations"] = agentInst.GetMaxIterations()
	}
	stats["streaming_enabled"] = agentInst.IsStreamingEnabled()
	stats["debug_mode"] = agentInst.IsDebugMode()
	stats["embedding_index_enabled"] = agentInst.IsEmbeddingIndexEnabled()
	if em := agentInst.GetEmbeddingManager(); em != nil {
		stats["embedding_index_building"] = em.IsBuilding()
		stats["embedding_index_initialized"] = em.IsInitialized()
		stats["embedding_index_size"] = em.IndexSize()
	}
}

func (ws *ReactWebServer) gatherStatsForClientID(clientID string) map[string]interface{} {
	ws.mutex.RLock()
	stats := ws.gatherStatsForClientIDLocked(clientID)
	ws.mutex.RUnlock()

	// If the locked function didn't find an agent (clientCtx.Agent was nil),
	// try to get or create one outside the lock to avoid deadlock —
	// getClientAgent acquires ws.mutex itself.
	if stats["provider"] == "" && clientID != "" {
		if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
			populateAgentStats(stats, agentInst)
		}
	}

	return stats
}

// gatherStatsForClientIDLocked gathers stats assuming ws.mutex is already held.
func (ws *ReactWebServer) gatherStatsForClientIDLocked(clientID string) map[string]interface{} {
	uptime := time.Since(ws.startTime)
	terminalSessions := 0
	for _, clientCtx := range ws.clientContexts {
		if clientCtx != nil && clientCtx.Terminal != nil {
			terminalSessions += clientCtx.Terminal.GetVisibleSessionCount()
		}
	}
	if terminalSessions == 0 && ws.terminalManager != nil {
		terminalSessions = ws.terminalManager.GetVisibleSessionCount()
	}

	// Get agent stats if available
	stats := map[string]interface{}{
		"uptime_seconds":                       int64(uptime.Seconds()),
		"connections":                          ws.countConnections(),
		"queries":                              ws.queryCount,
		"query_count":                          ws.queryCount,
		"terminal_sessions":                    terminalSessions,
		"client_context_count":                 len(ws.clientContexts),
		"client_context_cleanup_removed_last":  ws.lastClientContextCleanupRemoved,
		"client_context_cleanup_removed_total": ws.totalClientContextsRemoved,
		"server_time":                          time.Now().Unix(),
		"start_time":                           ws.startTime.Unix(),
		"uptime_formatted":                     uptime.String(),
		"uptime":                               uptime.String(),
	}
	if !ws.lastClientContextCleanupAt.IsZero() {
		stats["client_context_cleanup_last_unix"] = ws.lastClientContextCleanupAt.Unix()
	}

	clientCtx := ws.clientContexts[clientID]
	if clientCtx == nil {
		clientCtx = ws.clientContexts[defaultWebClientID]
	}

	// Report whether this client currently has an active query.
	// The frontend uses this during reconnect to immediately restore (or
	// clear) the processing indicator instead of relying on a 3-second
	// safety timer.
	stats["is_processing"] = clientCtx != nil && clientCtx.ActiveQuery
	if clientCtx != nil && clientCtx.ActiveQuery && clientCtx.CurrentQuery != "" {
		stats["current_query"] = clientCtx.CurrentQuery
	}

	var agentInst *agent.Agent
	if clientCtx != nil {
		agentInst = clientCtx.Agent
	}

	// Always include provider/model/persona in stats so the frontend can reliably
	// detect whether a provider is configured (empty = none).
	stats["provider"] = ""
	stats["model"] = ""
	stats["persona"] = ""
	stats["embedding_index_enabled"] = false

	// Add agent-specific stats if available
	if agentInst != nil {
		stats["provider"] = agentInst.GetProvider()
		stats["model"] = agentInst.GetModel()
		// persona is "" when the default orchestrator is active (no override applied).
		// The frontend hides the badge when empty to avoid showing "Orchestrator" for the default state.
		stats["persona"] = agentInst.GetActivePersona()
		stats["session_id"] = agentInst.GetSessionID()
		stats["total_tokens"] = agentInst.GetTotalTokens()
		stats["prompt_tokens"] = agentInst.GetPromptTokens()
		stats["completion_tokens"] = agentInst.GetCompletionTokens()
		stats["cached_tokens"] = agentInst.GetCachedTokens()
		stats["cache_efficiency"] = float64(0)
		if totalTokens := agentInst.GetTotalTokens(); totalTokens > 0 {
			stats["cache_efficiency"] = float64(agentInst.GetCachedTokens()) / float64(totalTokens) * 100
		}
		stats["cached_cost_savings"] = agentInst.GetCachedCostSavings()
		stats["current_context_tokens"] = agentInst.GetCurrentContextTokens()
		stats["max_context_tokens"] = agentInst.GetMaxContextTokens()
		stats["context_usage_percent"] = float64(0)
		if maxTokens := agentInst.GetMaxContextTokens(); maxTokens > 0 {
			stats["context_usage_percent"] = float64(agentInst.GetCurrentContextTokens()) / float64(maxTokens) * 100
		}
		stats["context_warning_issued"] = agentInst.GetContextWarningIssued()
		stats["total_cost"] = agentInst.GetTotalCost()
		stats["last_tps"] = agentInst.GetLastTPS()
		stats["current_iteration"] = agentInst.GetCurrentIteration()
		if agentInst.GetMaxIterations() == 0 {
			stats["max_iterations"] = "unlimited"
		} else {
			stats["max_iterations"] = agentInst.GetMaxIterations()
		}
		stats["streaming_enabled"] = agentInst.IsStreamingEnabled()
		stats["debug_mode"] = agentInst.IsDebugMode()
	} else {
		// Agent hasn't been lazily created yet. Fall back to the configured
		// provider/model from user settings so the frontend doesn't flash
		// "no provider" even though one is configured.
		cfg, cfgErr := configuration.Load()
		if cfgErr == nil && cfg != nil {
			if p := strings.TrimSpace(cfg.LastUsedProvider); p != "" && p != "editor" {
				stats["provider"] = p
				stats["model"] = cfg.GetModelForProvider(p)
			}
		}
	}
	if clientCtx != nil && len(clientCtx.AgentState) > 0 {
		var clientState agent.AgentState
		if err := json.Unmarshal(clientCtx.AgentState, &clientState); err == nil {
			stats["session_id"] = clientState.SessionID
			stats["total_tokens"] = clientState.TotalTokens
			stats["prompt_tokens"] = clientState.PromptTokens
			stats["completion_tokens"] = clientState.CompletionTokens
			stats["cached_tokens"] = clientState.CachedTokens
			stats["cached_cost_savings"] = clientState.CachedCostSavings
			stats["total_cost"] = clientState.TotalCost
		}
	}

	return stats
}

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

// handleAPIFiles handles API requests for file listing
func (ws *ReactWebServer) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Method not allowed",
			"code":  "method_not_allowed",
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
	canonicalDir, err := canonicalizePath(dir, workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid directory: %v", err),
			"code":  "invalid_directory",
		})
		return
	}
	if !isWithinWorkspace(canonicalDir, workspaceRoot) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Directory outside workspace",
			"code":  "directory_outside_workspace",
		})
		return
	}

	// Read directory
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Failed to read directory: %v", err),
			"code":  "failed_to_read_directory",
		})
		return
	}

	// Gather git status information for files in this directory
	var modifiedSet, untrackedSet map[string]bool
	var ignoreRules *ignore.GitIgnore
	if includeGitStatus {
		modifiedSet, untrackedSet = getGitFileStatusMap(workspaceRoot)
		ignoreRules = filediscovery.GetIgnoreRules(workspaceRoot)
	}

	// Convert to JSON response
	var files []map[string]interface{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		absPath := filepath.Join(canonicalDir, entry.Name())
		relPath, _ := filepath.Rel(workspaceRoot, absPath)

		fileInfo := map[string]interface{}{
			"name":     entry.Name(),
			"path":     absPath,
			"relative": relPath,
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
			"mod_time": info.ModTime().Unix(),
		}

		if includeGitStatus {
			gitStatus := getGitStatusForEntry(relPath, entry.IsDir(), modifiedSet, untrackedSet, ignoreRules, workspaceRoot)
			if gitStatus != "" {
				fileInfo["git_status"] = gitStatus
			}
		}

		files = append(files, fileInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "success",
		"files":     files,
		"path":      canonicalDir,
		"directory": canonicalDir,
	})
}

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

	if !isWithinWorkspace(canonicalPath, workspaceRoot) {
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
		file.Close()
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

	if !isWithinWorkspace(canonicalPath, workspaceRoot) {
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

	if !isWithinWorkspace(oldCanonicalPath, workspaceRoot) || !isWithinWorkspace(newCanonicalPath, workspaceRoot) {
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

// getGitFileStatusMap runs git status --porcelain once and returns sets of modified and untracked files.
func getGitFileStatusMap(workspaceRoot string) (modified, untracked map[string]bool) {
	modified = make(map[string]bool)
	untracked = make(map[string]bool)

	// Check if we're in a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = workspaceRoot
	if err := cmd.Run(); err != nil {
		return
	}

	cmd = exec.Command("git", "status", "--porcelain")
	cmd.Dir = workspaceRoot
	output, err := cmd.Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		// Format: XY<space><filename>  (X=staged, Y=unstaged, ??=untracked)
		if len(line) < 4 {
			continue
		}
		staged := string(line[0])
		unstaged := string(line[1])
		filePath := strings.TrimSpace(line[3:])

		// Untracked files
		if staged == "?" && unstaged == "?" {
			untracked[filePath] = true
			continue
		}

		// Modified, added, renamed, deleted files (both staged and unstaged)
		if unstaged == "M" || unstaged == "D" || staged == "M" || staged == "A" || staged == "D" || staged == "R" || staged == "C" {
			modified[filePath] = true
		}
	}

	return
}

// getGitStatusForEntry determines the git status for a single file or directory entry.
func getGitStatusForEntry(relPath string, isDir bool, modified, untracked map[string]bool, ignoreRules *ignore.GitIgnore, workspaceRoot string) string {
	// Special case: .git directory is always gitignored
	if isDir && relPath == ".git" {
		return "ignored"
	}

	if ignoreRules != nil {
		if isDir {
			if ignoreRules.MatchesPath(relPath) || ignoreRules.MatchesPath(relPath+"/") {
				return "ignored"
			}
		} else {
			if ignoreRules.MatchesPath(relPath) {
				return "ignored"
			}
		}
	}

	if modified[relPath] {
		return "modified"
	}

	if untracked[relPath] {
		return "untracked"
	}

	// For directories, check if any child has modified or untracked status
	if isDir {
		prefix := relPath + "/"
		for p := range modified {
			if strings.HasPrefix(p, prefix) {
				return "modified"
			}
		}
		for p := range untracked {
			if strings.HasPrefix(p, prefix) {
				return "untracked"
			}
		}
	}

	return ""
}

// handleAPIFile handles API requests for file operations (read/write)
func (ws *ReactWebServer) handleAPIFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleFileRead(w, r)
	case http.MethodPost:
		ws.handleFileWrite(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFileRead handles file read operations
func (ws *ReactWebServer) handleFileRead(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(path, workspaceRoot, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(canonicalPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found: %v", err), http.StatusNotFound)
		return
	}

	if info.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	if info.Size() > maxFileReadSize {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":    "file too large to open in editor",
			"size":     info.Size(),
			"max_size": maxFileReadSize,
		})
		return
	}

	if !isWithinWorkspace(canonicalPath, workspaceRoot) && !isAppConfigPath(canonicalPath) {
		consentToken := strings.TrimSpace(r.Header.Get(consentTokenHeader))
		if consentToken == "" {
			consentToken = strings.TrimSpace(r.URL.Query().Get("consent_token"))
		}
		if !fileConsents.consume(consentToken, canonicalPath, "read") {
			ws.writeExternalPathConsentRequired(w, canonicalPath, "read")
			return
		}
	}

	// Read file content
	content, err := os.ReadFile(canonicalPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusInternalServerError)
		return
	}

	// Determine content type
	// First, try to detect content type from the file content (magic bytes)
	contentType := http.DetectContentType(content)

	// Fallback to extension-based detection for types http.DetectContentType can't reliably detect
	// or for types that need specific MIME types (like .js, .svg)
	ext := strings.ToLower(filepath.Ext(canonicalPath))
	switch ext {
	case ".json":
		contentType = "application/json"
	case ".js":
		contentType = "application/javascript"
	case ".css":
		contentType = "text/css"
	case ".html":
		contentType = "text/html"
	case ".svg":
		contentType = "image/svg+xml"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".bmp":
		contentType = "image/bmp"
	case ".ico":
		contentType = "image/x-icon"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Write(content)
}

// handleFileWrite handles file write operations
func (ws *ReactWebServer) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(path, workspaceRoot, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	if !isWithinWorkspace(canonicalPath, workspaceRoot) && !isAppConfigPath(canonicalPath) {
		consentToken := strings.TrimSpace(r.Header.Get(consentTokenHeader))
		if consentToken == "" {
			consentToken = strings.TrimSpace(r.URL.Query().Get("consent_token"))
		}
		if !fileConsents.consume(consentToken, canonicalPath, "write") {
			ws.writeExternalPathConsentRequired(w, canonicalPath, "write")
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFileWriteBodySize)
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Parse JSON to extract content field
	var requestData struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &requestData); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse JSON: %v", err), http.StatusBadRequest)
		return
	}

	content := []byte(requestData.Content)

	// Create directory if it doesn't exist
	dir := filepath.Dir(canonicalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Write file
	if err := os.WriteFile(canonicalPath, content, 0644); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish file change event
	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(canonicalPath, "write", string(content)))

	// Stat the file to get actual filesystem mtime for the client
	modTime := int64(0)
	if info, err := os.Stat(canonicalPath); err == nil {
		modTime = info.ModTime().Unix()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  "File saved successfully",
		"path":     canonicalPath,
		"size":     len(content),
		"mod_time": modTime,
	})
}

func (ws *ReactWebServer) handleAPIFileConsent(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var req struct {
		Path      string `json:"path"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	if operation != "read" && operation != "write" {
		http.Error(w, "operation must be read or write", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(req.Path, workspaceRoot, operation == "write")
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	if isWithinWorkspace(canonicalPath, workspaceRoot) || isAppConfigPath(canonicalPath) {
		http.Error(w, "Path does not require external consent", http.StatusBadRequest)
		return
	}

	token, expiresAt, err := fileConsents.issue(canonicalPath, operation, defaultConsentTTL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create consent token: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"path":       canonicalPath,
		"operation":  operation,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

func (ws *ReactWebServer) writeExternalPathConsentRequired(w http.ResponseWriter, canonicalPath, operation string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":     "external path access requires explicit user consent",
		"code":      "external_path_consent_required",
		"path":      canonicalPath,
		"operation": operation,
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
