package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	agent_commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/filediscovery"
	ignore "github.com/sabhiram/go-gitignore"
)

const (
	maxQueryBodyBytes    = 1 << 20  // 1 MiB
	maxFileWriteBodySize = 10 << 20 // 10 MiB
	consentTokenHeader   = "X-Ledit-Consent-Token"
)

func (ws *ReactWebServer) incrementActiveQueries() {
	ws.mutex.Lock()
	ws.activeQueries++
	ws.mutex.Unlock()
}

func (ws *ReactWebServer) decrementActiveQueries() {
	ws.mutex.Lock()
	if ws.activeQueries > 0 {
		ws.activeQueries--
	}
	ws.mutex.Unlock()
}

func (ws *ReactWebServer) hasActiveQuery() bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.activeQueries > 0
}

// handleAPIQuery handles API queries to the agent
func (ws *ReactWebServer) handleAPIQuery(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAPIQuery called")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var query struct {
		Query string `json:"query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		log.Printf("handleAPIQuery: invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if query.Query == "" {
		log.Printf("handleAPIQuery: empty query")
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	log.Printf("handleAPIQuery: processing query: %s", query.Query)

	// Increment query count
	ws.mutex.Lock()
	ws.queryCount++
	ws.mutex.Unlock()

	// Run the query asynchronously. The web UI consumes progress and completion via WebSocket.
	ws.incrementActiveQueries()
	go func() {
		defer ws.decrementActiveQueries()
		startedAt := time.Now()
		registry := agent_commands.NewCommandRegistry()

		if registry.IsSlashCommand(query.Query) {
			log.Printf("handleAPIQuery: executing slash command: %s", query.Query)
			if ws.eventBus != nil {
				ws.eventBus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent(
					query.Query,
					ws.agent.GetProvider(),
					ws.agent.GetModel(),
				))
			}

			err := registry.Execute(query.Query, ws.agent)
			if err != nil {
				log.Printf("handleAPIQuery: slash command error: %v", err)
				if ws.eventBus != nil {
					ws.eventBus.Publish(events.EventTypeError, events.ErrorEvent("Slash command failed", err))
				}
				return
			}

			if ws.eventBus != nil {
				trimmed := strings.TrimSpace(query.Query)
				ws.eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(
					fmt.Sprintf("Executed command: `%s`\n", trimmed),
					"assistant_text",
				))
				ws.eventBus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent(
					query.Query,
					fmt.Sprintf("Executed command: %s", trimmed),
					0,
					0,
					time.Since(startedAt),
				))
			}
			return
		}

		log.Printf("handleAPIQuery: calling ProcessQuery")
		_, err := ws.agent.ProcessQuery(query.Query)
		if err != nil {
			log.Printf("handleAPIQuery: ProcessQuery error: %v", err)
			if ws.eventBus != nil {
				ws.eventBus.Publish(events.EventTypeError, events.ErrorEvent("Query failed", err))
			}
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"query":     query.Query,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIQuerySteer injects user input into the currently running query loop.
func (ws *ReactWebServer) handleAPIQuerySteer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var query struct {
		Query string `json:"query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	if strings.HasPrefix(query.Query, "/") {
		http.Error(w, "Slash commands cannot be steered while a query is running", http.StatusBadRequest)
		return
	}

	if !ws.hasActiveQuery() {
		http.Error(w, "No active query to steer", http.StatusConflict)
		return
	}

	if err := ws.agent.InjectInputContext(query.Query); err != nil {
		http.Error(w, fmt.Sprintf("Failed to steer active query: %v", err), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"mode":      "steer",
		"query":     query.Query,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIStats handles API requests for server statistics
func (ws *ReactWebServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := ws.gatherStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAPIWorkspace handles API requests for reading and updating the active workspace root.
func (ws *ReactWebServer) handleAPIWorkspace(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPIWorkspaceGet(w, r)
	case http.MethodPost:
		ws.handleAPIWorkspaceSet(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ws *ReactWebServer) handleAPIWorkspaceGet(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"daemon_root":    ws.GetDaemonRoot(),
		"workspace_root": ws.GetWorkspaceRoot(),
	})
}

func (ws *ReactWebServer) handleAPIWorkspaceSet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	// Check if there's an active query - reject workspace changes while queries are running
	if ws.hasActiveQuery() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "cannot change workspace while an agent query is active. Wait for the query to complete before switching.",
			"code":           "query_in_progress",
			"active_queries": ws.getActiveQueryCount(),
		})
		return
	}

	// Capture the previous workspace root before setting the new one
	previousWorkspaceRoot := ws.GetWorkspaceRoot()

	workspaceRoot, err := ws.SetWorkspaceRoot(req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to set workspace: %v", err), http.StatusBadRequest)
		return
	}

	// Broadcast workspace change event to all WebSocket clients
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeWorkspaceChanged, events.WorkspaceChangedEvent(
			ws.GetDaemonRoot(),
			workspaceRoot,
			previousWorkspaceRoot,
		))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"daemon_root":    ws.GetDaemonRoot(),
		"message":        "Workspace updated",
		"workspace_root": workspaceRoot,
	})
}

func (ws *ReactWebServer) handleAPIWorkspaceBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	daemonRoot := ws.GetDaemonRoot()
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = daemonRoot
	}

	canonicalDir, err := canonicalizePath(dir, daemonRoot, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid directory: %v", err), http.StatusBadRequest)
		return
	}
	if canonicalDir != daemonRoot && !isWithinWorkspace(canonicalDir, daemonRoot) {
		http.Error(w, "Directory outside daemon root", http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := map[string]interface{}{
			"name": entry.Name(),
			"path": filepath.Join(canonicalDir, entry.Name()),
			"type": "file",
		}
		if entry.IsDir() {
			fileInfo["type"] = "directory"
		}
		fileInfo["size"] = info.Size()
		fileInfo["modified"] = info.ModTime().Unix()
		files = append(files, fileInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "success",
		"path":           canonicalDir,
		"daemon_root":    daemonRoot,
		"workspace_root": ws.GetWorkspaceRoot(),
		"files":          files,
	})
}

// gatherStats collects server statistics
func (ws *ReactWebServer) gatherStats() map[string]interface{} {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	uptime := time.Since(ws.startTime)

	// Get agent stats if available
	stats := map[string]interface{}{
		"uptime_seconds":    int64(uptime.Seconds()),
		"connections":       ws.countConnections(),
		"queries":           ws.queryCount,
		"query_count":       ws.queryCount,
		"terminal_sessions": ws.terminalManager.SessionCount(),
		"server_time":       time.Now().Unix(),
		"start_time":        ws.startTime.Unix(),
		"uptime_formatted":  uptime.String(),
		"uptime":            uptime.String(),
	}

	// Add agent-specific stats if available
	if ws.agent != nil {
		stats["provider"] = ws.agent.GetProvider()
		stats["model"] = ws.agent.GetModel()
		stats["session_id"] = ws.agent.GetSessionID()
		stats["total_tokens"] = ws.agent.GetTotalTokens()
		stats["prompt_tokens"] = ws.agent.GetPromptTokens()
		stats["completion_tokens"] = ws.agent.GetCompletionTokens()
		stats["cached_tokens"] = ws.agent.GetCachedTokens()
		stats["cache_efficiency"] = float64(0)
		if totalTokens := ws.agent.GetTotalTokens(); totalTokens > 0 {
			stats["cache_efficiency"] = float64(ws.agent.GetCachedTokens()) / float64(totalTokens) * 100
		}
		stats["cached_cost_savings"] = ws.agent.GetCachedCostSavings()
		stats["current_context_tokens"] = ws.agent.GetCurrentContextTokens()
		stats["max_context_tokens"] = ws.agent.GetMaxContextTokens()
		stats["context_usage_percent"] = float64(0)
		if maxTokens := ws.agent.GetMaxContextTokens(); maxTokens > 0 {
			stats["context_usage_percent"] = float64(ws.agent.GetCurrentContextTokens()) / float64(maxTokens) * 100
		}
		stats["context_warning_issued"] = ws.agent.GetContextWarningIssued()
		stats["total_cost"] = ws.agent.GetTotalCost()
		stats["last_tps"] = ws.agent.GetLastTPS()
		stats["current_iteration"] = ws.agent.GetCurrentIteration()
		stats["max_iterations"] = ws.agent.GetMaxIterations()
		stats["streaming_enabled"] = ws.agent.IsStreamingEnabled()
		stats["debug_mode"] = ws.agent.IsDebugMode()
	}

	return stats
}

// handleAPIBrowse handles API requests for directory browsing
func (ws *ReactWebServer) handleAPIBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get directory from query parameter
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = "."
	}
	canonicalDir, err := canonicalizePath(dir, ws.workspaceRoot, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid directory: %v", err), http.StatusBadRequest)
		return
	}
	if !isWithinWorkspace(canonicalDir, ws.workspaceRoot) {
		http.Error(w, "Directory outside workspace", http.StatusForbidden)
		return
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
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := map[string]interface{}{
			"name": entry.Name(),
			"path": filepath.Join(canonicalDir, entry.Name()),
			"type": "file",
		}

		if entry.IsDir() {
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
	canonicalDir, err := canonicalizePath(dir, ws.workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid directory: %v", err),
			"code":  "invalid_directory",
		})
		return
	}
	if !isWithinWorkspace(canonicalDir, ws.workspaceRoot) {
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
		modifiedSet, untrackedSet = getGitFileStatusMap(ws.workspaceRoot)
		ignoreRules = filediscovery.GetIgnoreRules(ws.workspaceRoot)
	}

	// Convert to JSON response
	var files []map[string]interface{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		absPath := filepath.Join(canonicalDir, entry.Name())
		relPath, _ := filepath.Rel(ws.workspaceRoot, absPath)

		fileInfo := map[string]interface{}{
			"name":     entry.Name(),
			"path":     absPath,
			"relative": relPath,
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
			"mod_time": info.ModTime().Unix(),
		}

		if includeGitStatus {
			gitStatus := getGitStatusForEntry(relPath, entry.IsDir(), modifiedSet, untrackedSet, ignoreRules, ws.workspaceRoot)
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
	canonicalPath, err := canonicalizePath(targetPath, ws.workspaceRoot, true)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid path: %v", err),
			"code":  "invalid_path",
		})
		return
	}

	if !isWithinWorkspace(canonicalPath, ws.workspaceRoot) {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"path":    canonicalPath,
	})
}

// handleAPIDeleteItem handles API requests for deleting files or directories
func (ws *ReactWebServer) handleAPIDeleteItem(w http.ResponseWriter, r *http.Request) {
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
	canonicalPath, err := canonicalizePath(req.Path, ws.workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid path: %v", err),
			"code":  "invalid_path",
		})
		return
	}

	if !isWithinWorkspace(canonicalPath, ws.workspaceRoot) {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"path":    canonicalPath,
	})
}

// handleAPIRenameItem handles API requests for renaming files or directories.
func (ws *ReactWebServer) handleAPIRenameItem(w http.ResponseWriter, r *http.Request) {
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

	oldCanonicalPath, err := canonicalizePath(req.OldPath, ws.workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid source path: %v", err),
			"code":  "invalid_old_path",
		})
		return
	}

	newCanonicalPath, err := canonicalizePath(req.NewPath, ws.workspaceRoot, false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("Invalid target path: %v", err),
			"code":  "invalid_new_path",
		})
		return
	}

	if !isWithinWorkspace(oldCanonicalPath, ws.workspaceRoot) || !isWithinWorkspace(newCanonicalPath, ws.workspaceRoot) {
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "success",
		"old_path": oldCanonicalPath,
		"new_path": newCanonicalPath,
	})
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
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(path, ws.workspaceRoot, false)
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

	if !isWithinWorkspace(canonicalPath, ws.workspaceRoot) {
		consentToken := strings.TrimSpace(r.Header.Get(consentTokenHeader))
		if consentToken == "" {
			consentToken = strings.TrimSpace(r.URL.Query().Get("consent_token"))
		}
		if !ws.fileConsents.consume(consentToken, canonicalPath, "read") {
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
	contentType := "text/plain"
	if strings.HasSuffix(canonicalPath, ".json") {
		contentType = "application/json"
	} else if strings.HasSuffix(canonicalPath, ".js") {
		contentType = "application/javascript"
	} else if strings.HasSuffix(canonicalPath, ".css") {
		contentType = "text/css"
	} else if strings.HasSuffix(canonicalPath, ".html") {
		contentType = "text/html"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Write(content)
}

// handleFileWrite handles file write operations
func (ws *ReactWebServer) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(path, ws.workspaceRoot, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	if !isWithinWorkspace(canonicalPath, ws.workspaceRoot) {
		consentToken := strings.TrimSpace(r.Header.Get(consentTokenHeader))
		if consentToken == "" {
			consentToken = strings.TrimSpace(r.URL.Query().Get("consent_token"))
		}
		if !ws.fileConsents.consume(consentToken, canonicalPath, "write") {
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

	// Validate hotkeys config before writing (prevent broken config)
	hotkeysPath, _ := GetHotkeysPath()
	if hotkeysPath != "" && canonicalPath == hotkeysPath {
		var hotkeyCheck HotkeyConfig
		if err := json.Unmarshal(content, &hotkeyCheck); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Invalid hotkeys JSON: %v", err),
				"path":    canonicalPath,
			})
			return
		}
		if err := ValidateHotkeyConfig(&hotkeyCheck); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Hotkeys validation failed: %v", err),
				"path":    canonicalPath,
			})
			return
		}
	}

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
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(canonicalPath, "write", string(content)))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "File saved successfully",
		"path":    canonicalPath,
		"size":    len(content),
	})
}

func (ws *ReactWebServer) handleAPIFileConsent(w http.ResponseWriter, r *http.Request) {
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

	canonicalPath, err := canonicalizePath(req.Path, ws.workspaceRoot, operation == "write")
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	if isWithinWorkspace(canonicalPath, ws.workspaceRoot) {
		http.Error(w, "Path does not require external consent", http.StatusBadRequest)
		return
	}

	token, expiresAt, err := ws.fileConsents.issue(canonicalPath, operation, defaultConsentTTL)
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

// handleAPIConfig handles API requests for configuration
func (ws *ReactWebServer) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current configuration
	config := map[string]interface{}{
		"port":           ws.port,
		"daemon_root":    ws.GetDaemonRoot(),
		"workspace_root": ws.GetWorkspaceRoot(),
		"agent": map[string]interface{}{
			"name":    "ledit",
			"version": "1.0.0", // This should come from actual version info
		},
		"features": map[string]interface{}{
			"terminal":          true,
			"file_operations":   true,
			"real_time_updates": true,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// handleTerminalHistory handles API requests for terminal history
func (ws *ReactWebServer) handleTerminalHistory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleTerminalHistoryGet(w, r)
	case http.MethodPost:
		ws.handleTerminalHistoryPost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ws *ReactWebServer) handleTerminalHistoryGet(w http.ResponseWriter, r *http.Request) {

	// Get session ID from query parameter (optional)
	sessionID := r.URL.Query().Get("session_id")

	// If no session ID provided, return empty history
	if sessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"history":    []string{},
			"session_id": "",
			"count":      0,
		})
		return
	}

	// Get history from terminal manager
	history, err := ws.terminalManager.GetHistory(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get history: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"history":    history,
		"session_id": sessionID,
		"count":      len(history),
	})
}

func (ws *ReactWebServer) handleTerminalHistoryPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Command   string `json:"command"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	command := strings.TrimSpace(req.Command)
	if command == "" {
		http.Error(w, "Command is required", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":    "Command accepted without active terminal session",
			"command":    command,
			"session_id": "",
			"stored":     false,
		})
		return
	}

	if err := ws.terminalManager.AddToHistory(req.SessionID, command); err != nil {
		http.Error(w, fmt.Sprintf("Failed to add history: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "History updated",
		"command":    command,
		"session_id": req.SessionID,
		"stored":     true,
	})
}

// handleAPITerminalSessions returns list of active terminal sessions
func (ws *ReactWebServer) handleAPITerminalSessions(w http.ResponseWriter, r *http.Request) {
	// Get list of session IDs
	sessionIDs := ws.terminalManager.ListSessions()

	// Build detailed info for each session
	sessions := []map[string]interface{}{}
	activeCount := 0
	for _, sessionID := range sessionIDs {
		session, exists := ws.terminalManager.GetSession(sessionID)
		if exists {
			session.mutex.RLock()
			size := session.Size
			if session.Active {
				activeCount++
			}
			sessions = append(sessions, map[string]interface{}{
				"id":        sessionID,
				"active":    session.Active,
				"last_used": session.LastUsed,
				"has_size":  size != nil,
			})
			session.mutex.RUnlock()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions":     sessions,
		"count":        len(sessions),
		"active_count": activeCount,
	})
}

// tryParseMultipartFile attempts to extract image data from a multipart form.
// Returns the file data and true if successful, or nil and false otherwise.
func tryParseMultipartFile(body []byte, contentType string) ([]byte, bool) {
	if !strings.Contains(contentType, "multipart/form-data") {
		return nil, false
	}

	r := &http.Request{
		Header: http.Header{"Content-Type": []string{contentType}},
		Body:   io.NopCloser(bytes.NewReader(body)),
	}

	if err := r.ParseMultipartForm(int64(len(body))); err != nil {
		return nil, false
	}

	file, _, formErr := r.FormFile("image")
	if formErr != nil {
		return nil, false
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, false
	}

	return data, true
}

// handleUploadImage handles image upload requests
func (ws *ReactWebServer) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the entire body once into a buffer
	r.Body = http.MaxBytesReader(w, r.Body, console.MaxPastedImageSize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Try to parse as multipart form if content type indicates multipart
	contentType := r.Header.Get("Content-Type")
	data, ok := tryParseMultipartFile(body, contentType)

	// If multipart parsing failed or content type is not multipart, use raw body
	if !ok {
		data = body
	}

	// Validate image format
	ext, _ := console.DetectImageMagic(data)
	if ext == "" {
		http.Error(w, "Not a recognized image format", http.StatusBadRequest)
		return
	}

	// Save the image
	relativePath, err := console.SavePastedImage(data)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save image: %v", err), http.StatusInternalServerError)
		return
	}

	filename := filepath.Base(relativePath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path":     relativePath,
		"filename": filename,
	})
}
