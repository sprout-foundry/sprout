package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

// handleAPIQuery handles API queries to the agent
func (ws *ReactWebServer) handleAPIQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var query struct {
		Query string `json:"query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if query.Query == "" {
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	// Increment query count
	ws.mutex.Lock()
	ws.queryCount++
	ws.mutex.Unlock()

	// Create a channel to receive the response
	responseCh := make(chan string, 1)
	errorCh := make(chan error, 1)

	// Run the query in a goroutine
	go func() {
		response, err := ws.agent.ProcessQuery(query.Query)
		if err != nil {
			errorCh <- err
			return
		}
		responseCh <- response
	}()

	// Wait for response or timeout
	select {
	case response := <-responseCh:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"response":  response,
			"timestamp": time.Now().Unix(),
		})
	case err := <-errorCh:
		log.Printf("Agent query error: %v", err)
		http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
	case <-time.After(30 * time.Second):
		http.Error(w, "Query timeout", http.StatusRequestTimeout)
	}
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
		"terminal_sessions": ws.terminalManager.SessionCount(),
		"server_time":       time.Now().Unix(),
		"start_time":        ws.startTime.Unix(),
		"uptime_formatted":  uptime.String(),
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
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "."
	}

	// Prevent directory traversal
	if strings.Contains(dir, "..") {
		http.Error(w, "Invalid directory", http.StatusBadRequest)
		return
	}

	// Read directory
	entries, err := os.ReadDir(dir)
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
			"path": filepath.Join(dir, entry.Name()),
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
		"files":   files,
	})
}

// handleAPIGitStatus handles API requests for git status
func (ws *ReactWebServer) handleAPIGitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// For now, return mock data - in real implementation this would call git commands
	status := map[string]interface{}{
		"branch": "main",
		"ahead":  0,
		"behind": 0,
		"staged": []map[string]interface{}{
			{
				"path":    "pkg/webui/api.go",
				"status":  "M",
				"changes": map[string]int{"additions": 10, "deletions": 5},
			},
		},
		"modified": []map[string]interface{}{
			{
				"path":    "webui/src/App.tsx",
				"status":  "M",
				"changes": map[string]int{"additions": 5, "deletions": 2},
			},
		},
		"untracked": []map[string]interface{}{},
		"clean":     false,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"status":  status,
	})
}

// handleAPIFiles handles API requests for file listing
func (ws *ReactWebServer) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get directory from query parameter
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "."
	}

	// Prevent directory traversal
	if strings.Contains(dir, "..") {
		http.Error(w, "Invalid directory", http.StatusBadRequest)
		return
	}

	// Read directory
	entries, err := os.ReadDir(dir)
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
			"name":     entry.Name(),
			"path":     filepath.Join(dir, entry.Name()),
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
			"mod_time": info.ModTime().Unix(),
		}

		files = append(files, fileInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"files":     files,
		"directory": dir,
	})
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

	// Prevent directory traversal
	if strings.Contains(path, "..") {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found: %v", err), http.StatusNotFound)
		return
	}

	if info.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusInternalServerError)
		return
	}

	// Determine content type
	contentType := "text/plain"
	if strings.HasSuffix(path, ".json") {
		contentType = "application/json"
	} else if strings.HasSuffix(path, ".js") {
		contentType = "application/javascript"
	} else if strings.HasSuffix(path, ".css") {
		contentType = "text/css"
	} else if strings.HasSuffix(path, ".html") {
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

	// Prevent directory traversal
	if strings.Contains(path, "..") {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	// Read request body
	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Write file
	if err := os.WriteFile(path, content, 0644); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish file change event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "write", string(content)))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"path":    path,
		"size":    len(content),
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
		"port": ws.port,
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session ID from query parameter
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
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
	})
}
