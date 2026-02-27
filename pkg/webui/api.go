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

const (
	maxQueryBodyBytes    = 1 << 20  // 1 MiB
	maxFileWriteBodySize = 10 << 20 // 10 MiB
	consentTokenHeader   = "X-Ledit-Consent-Token"
)

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

	// Create a channel to receive the response
	responseCh := make(chan string, 1)
	errorCh := make(chan error, 1)

	// Run the query in a goroutine
	go func() {
		log.Printf("handleAPIQuery: calling ProcessQuery")
		response, err := ws.agent.ProcessQuery(query.Query)
		if err != nil {
			log.Printf("handleAPIQuery: ProcessQuery error: %v", err)
			errorCh <- err
			return
		}
		log.Printf("handleAPIQuery: ProcessQuery returned: %s", response)
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
	for _, sessionID := range sessionIDs {
		session, exists := ws.terminalManager.GetSession(sessionID)
		if exists {
			session.mutex.RLock()
			size := session.Size
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
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleAPIProviders returns list of available providers and their models
func (ws *ReactWebServer) handleAPIProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Define default providers with their models
	providers := []map[string]interface{}{
		{
			"id":     "openai",
			"name":   "OpenAI",
			"models": []string{"gpt-4", "gpt-4-turbo", "gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"},
		},
		{
			"id":     "anthropic",
			"name":   "Anthropic",
			"models": []string{"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022", "claude-3-opus-20240229"},
		},
		{
			"id":     "ollama",
			"name":   "Ollama",
			"models": []string{"llama3.2", "llama3.1", "codellama", "mistral", "deepseek-coder"},
		},
		{
			"id":     "deepinfra",
			"name":   "DeepInfra",
			"models": []string{"mistralai/Mixtral-8x7B-Instruct-v0.1", "deepseek-ai/DeepSeek-V3"},
		},
		{
			"id":     "cerebras",
			"name":   "Cerebras",
			"models": []string{"llama3.1-70b", "llama3.1-8b"},
		},
		{
			"id":     "zai",
			"name":   "Z.AI",
			"models": []string{"glm-5", "glm-4", "glm-3-turbo"},
		},
		{
			"id":     "minimax",
			"name":   "MiniMax",
			"models": []string{"MiniMax-M2", "MiniMax-M2.1", "MiniMax-M2.5", "MiniMax-M2-Stable"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": providers,
	})
}
