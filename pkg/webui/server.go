// Package webui provides React web server with embedded assets
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/gorilla/websocket"
)

//go:embed static/*
var staticFiles embed.FS

// ReactWebServer provides the React web UI
type ReactWebServer struct {
	agent            *agent.Agent
	eventBus         *events.EventBus
	port             int
	server           *http.Server
	upgrader         websocket.Upgrader
	connections      sync.Map // map[*websocket.Conn]bool
	terminalManager  *TerminalManager
	isRunning        bool
	mutex            sync.RWMutex
	startTime        time.Time
	queryCount       int
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = 54321
	}

	return &ReactWebServer{
		agent:           agent,
		eventBus:        eventBus,
		port:            port,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow localhost connections
				origin := r.Header.Get("Origin")
				return origin == "http://localhost:"+strconv.Itoa(port) ||
					origin == "" // Allow same-origin and direct connections
			},
		},
		terminalManager: NewTerminalManager(),
		startTime:       time.Now(),
	}
}

// Start starts the web server
func (ws *ReactWebServer) Start(ctx context.Context) error {
	ws.mutex.Lock()
	if ws.isRunning {
		ws.mutex.Unlock()
		return fmt.Errorf("web server is already running")
	}
	ws.isRunning = true
	ws.mutex.Unlock()

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.HandleFunc("/terminal", ws.handleTerminalWebSocket)
	mux.HandleFunc("/api/query", ws.handleAPIQuery)
	mux.HandleFunc("/api/stats", ws.handleAPIStats)
	mux.HandleFunc("/api/files", ws.handleAPIFiles)
	mux.HandleFunc("/api/file", ws.handleAPIFile)
	mux.HandleFunc("/api/config", ws.handleAPIConfig)
	mux.HandleFunc("/api/terminal/history", ws.handleTerminalHistory)

	// Serve Service Worker with proper MIME type
	mux.HandleFunc("/sw.js", ws.handleServiceWorker)

	// Serve static files (React build assets) with proper MIME types
	mux.HandleFunc("/static/", ws.handleStaticFiles)

	ws.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ws.port),
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		log.Printf("🌐 Web UI starting at http://localhost:%d", ws.port)
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		ws.Shutdown()
	}()

	return nil
}

// Shutdown gracefully shuts down the web server
func (ws *ReactWebServer) Shutdown() error {
	ws.mutex.Lock()
	if !ws.isRunning {
		ws.mutex.Unlock()
		return nil
	}
	ws.isRunning = false
	ws.mutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Close all WebSocket connections
	ws.connections.Range(func(conn, value interface{}) bool {
		if wsConn, ok := conn.(*websocket.Conn); ok {
			wsConn.Close()
		}
		return true
	})

	return ws.server.Shutdown(ctx)
}

// IsRunning returns true if the web server is running
func (ws *ReactWebServer) IsRunning() bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.isRunning
}

// GetPort returns the port the web server is running on
func (ws *ReactWebServer) GetPort() int {
	return ws.port
}

// handleIndex serves the React application
func (ws *ReactWebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Try to serve from embedded filesystem first
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		// Fallback to filesystem
		http.ServeFile(w, r, "./pkg/webui/static/index.html")
		return
	}

	// Set proper HTML content type
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleStaticFiles serves static files with proper MIME types
func (ws *ReactWebServer) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	// Remove /static/ prefix to get the relative path
	filePath := r.URL.Path[len("/static/"):]

	// Prevent directory traversal
	if filePath == "" || filePath[0] == '.' || filePath[0] == '/' {
		http.NotFound(w, r)
		return
	}

	// Try to serve from embedded filesystem first
	embeddedPath := "static/static/" + filePath
	data, err := staticFiles.ReadFile(embeddedPath)
	if err != nil {
		// Fallback to filesystem
		fullPath := "./pkg/webui/static/static/" + filePath
		http.ServeFile(w, r, fullPath)
		return
	}

	// Set appropriate Content-Type header based on file extension
	ext := ""
	if lastDot := strings.LastIndex(filePath, "."); lastDot != -1 {
		ext = filePath[lastDot:]
	}

	switch ext {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		// Handle Service Worker files specifically
		if filePath == "sw.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Service-Worker-Allowed", "/")
		} else {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		// Let Go's DetectContentType handle unknown types
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	// Enable caching for static assets
	w.Header().Set("Cache-Control", "public, max-age=3600") // 1 hour cache

	// Serve the embedded data
	w.Write(data)
}

// handleServiceWorker serves the Service Worker with proper MIME type
func (ws *ReactWebServer) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	// Try to serve from embedded filesystem first
	data, err := staticFiles.ReadFile("static/sw.js")
	if err != nil {
		// Fallback to filesystem
		fallbackPath := "./pkg/webui/static/sw.js"
		data, err = os.ReadFile(fallbackPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	// Set proper headers for Service Worker
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	w.Write(data)
}

// handleWebSocket handles WebSocket connections for real-time events
func (ws *ReactWebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Store connection
	ws.connections.Store(conn, true)
	defer ws.connections.Delete(conn)

	// Send initial connection status
	conn.WriteJSON(map[string]interface{}{
		"type": "connection_status",
		"data": map[string]bool{"connected": true},
	})

	// Set up close handler to send disconnect status
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("WebSocket closing with code %d: %s", code, text)
		return nil
	})

	// Subscribe to events - EventBus should always be available in real deployments
	eventCh := ws.eventBus.Subscribe("webui")
	defer ws.eventBus.Unsubscribe("webui")

	// Send events to WebSocket
	for {
		select {
		case event := <-eventCh:
			if err := conn.WriteJSON(event); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		default:
			// Check for incoming messages
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}
			// Handle incoming WebSocket messages if needed
		}
	}
}

// handleAPIQuery handles API queries from the web UI
func (ws *ReactWebServer) handleAPIQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Query string `json:"query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Process query using real agent
	go func() {
		var provider, model string
		if ws.agent != nil {
			provider = ws.agent.GetProvider()
			model = ws.agent.GetModel()
		} else {
			provider = "test-provider"
			model = "test-model"
		}

		ws.eventBus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent(
			req.Query,
			provider,
			model,
		))

		startTime := time.Now()

		// Process with real agent if available
		var response string
		var tokensUsed int
		var cost float64

		if ws.agent != nil {
			result, err := ws.agent.ProcessQuery(req.Query)
			if err != nil {
				response = fmt.Sprintf("Error processing query: %v", err)
				tokensUsed = 0
				cost = 0.0
			} else {
				response = result
				tokensUsed = ws.agent.GetCurrentContextTokens()
				cost = ws.agent.GetTotalCost()
			}
		} else {
			// Fallback to simulated response if no agent is available
			time.Sleep(1 * time.Second)
			response = "This is a simulated response. The actual implementation would process the query using the CLI."
			tokensUsed = 100
			cost = 0.001
		}

		processingTime := time.Since(startTime)

		ws.eventBus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent(
			req.Query,
			response,
			tokensUsed,
			cost,
			processingTime,
		))

		ws.queryCount++
	}()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "processing",
		"query":  req.Query,
	})
}

// handleAPIStats returns current statistics
func (ws *ReactWebServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	var provider, model, sessionID string
	var totalTokens, currentContextTokens, maxContextTokens, promptTokens, completionTokens, cachedTokens int
	var totalCost, cachedCostSavings, lastTPS float64
	var contextWarningIssued, streamingEnabled, debugMode bool
	var currentIteration, maxIterations int

	if ws.agent != nil {
		provider = ws.agent.GetProvider()
		model = ws.agent.GetModel()
		sessionID = ws.agent.GetSessionID()
		totalTokens = ws.agent.GetTotalTokens()
		currentContextTokens = ws.agent.GetCurrentContextTokens()
		maxContextTokens = ws.agent.GetMaxContextTokens()
		promptTokens = ws.agent.GetPromptTokens()
		completionTokens = ws.agent.GetCompletionTokens()
		cachedTokens = ws.agent.GetCachedTokens()
		totalCost = ws.agent.GetTotalCost()
		cachedCostSavings = ws.agent.GetCachedCostSavings()
		lastTPS = ws.agent.GetLastTPS()
		contextWarningIssued = ws.agent.GetContextWarningIssued()
		streamingEnabled = ws.agent.IsStreamingEnabled()
		debugMode = ws.agent.IsDebugMode()
		currentIteration = ws.agent.GetCurrentIteration()
		maxIterations = ws.agent.GetMaxIterations()
	} else {
		// Test values
		provider = "test-provider"
		model = "test-model"
		sessionID = "test-session"
		totalTokens = 0
		currentContextTokens = 0
		maxContextTokens = 0
		promptTokens = 0
		completionTokens = 0
		cachedTokens = 0
		totalCost = 0.0
		cachedCostSavings = 0.0
		lastTPS = 0.0
		contextWarningIssued = false
		streamingEnabled = true
		debugMode = false
		currentIteration = 0
		maxIterations = 1000
	}

	// Calculate context usage percentage
	contextUsagePercent := float64(0)
	if maxContextTokens > 0 {
		contextUsagePercent = float64(currentContextTokens) / float64(maxContextTokens) * 100
	}

	// Calculate cache efficiency
	cacheEfficiency := float64(0)
	if totalTokens > 0 {
		cacheEfficiency = float64(cachedTokens) / float64(totalTokens) * 100
	}

	stats := map[string]interface{}{
		// Basic info
		"provider":     provider,
		"model":        model,
		"session_id":   sessionID,
		"query_count":  ws.queryCount,
		"uptime":       time.Since(ws.startTime).String(),
		"connections":  ws.countConnections(),

		// Token usage
		"total_tokens":        totalTokens,
		"prompt_tokens":       promptTokens,
		"completion_tokens":   completionTokens,
		"cached_tokens":       cachedTokens,
		"cache_efficiency":    cacheEfficiency,

		// Context usage
		"current_context_tokens": currentContextTokens,
		"max_context_tokens":     maxContextTokens,
		"context_usage_percent":  contextUsagePercent,
		"context_warning_issued": contextWarningIssued,

		// Cost tracking
		"total_cost":          totalCost,
		"cached_cost_savings": cachedCostSavings,

		// Performance metrics
		"last_tps": lastTPS,

		// Iteration tracking
		"current_iteration": currentIteration,
		"max_iterations":     maxIterations,

		// Configuration
		"streaming_enabled": streamingEnabled,
		"debug_mode":        debugMode,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAPIFiles returns file system information with enhanced security
func (ws *ReactWebServer) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	// Get the directory path from query parameter (default to current directory)
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// Security check - prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "Invalid path: directory traversal not allowed", http.StatusBadRequest)
		return
	}

	// Additional security: ensure path is within reasonable bounds
	if len(cleanPath) > 1000 {
		http.Error(w, "Path too long", http.StatusBadRequest)
		return
	}

	// Check if path exists and is a directory
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Directory not found", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Failed to access directory: %v", err), http.StatusInternalServerError)
		}
		return
	}

	if !info.IsDir() {
		http.Error(w, "Path is not a directory", http.StatusBadRequest)
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Build file tree response with security filtering
	var files []interface{}
	for _, entry := range entries {
		// Skip hidden files and directories (starting with .)
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Skip sensitive system files and directories
		if ws.isSensitivePath(entry.Name()) {
			continue
		}

		fileInfo := map[string]interface{}{
			"name":     entry.Name(),
			"path":     filepath.Join(cleanPath, entry.Name()),
			"isDir":    entry.IsDir(),
			"size":     info.Size(),
			"modified": info.ModTime().Unix(),
		}

		// Get file extension for files
		if !entry.IsDir() {
			fileInfo["ext"] = filepath.Ext(entry.Name())
			// Add file type categorization
			fileInfo["type"] = ws.categorizeFile(entry.Name())
		}

		files = append(files, fileInfo)
	}

	response := map[string]interface{}{
		"message": "success",
		"path":    cleanPath,
		"files":   files,
		"count":   len(files),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// isSensitivePath checks if a path should be hidden for security reasons
func (ws *ReactWebServer) isSensitivePath(name string) bool {
	sensitiveNames := []string{
		".git", ".svn", ".hg", ".bzr", // Version control
		"node_modules", "vendor", "target", "build", "dist", // Dependencies and build artifacts
		".env", ".env.local", ".env.production", // Environment files
		"*.key", "*.pem", "*.crt", "*.p12", // Certificates and keys
		"id_rsa", "id_ed25519", "id_dsa", // SSH keys
		".aws", ".azure", ".gcp", // Cloud credentials
		"secrets", "credentials", "private", // Obvious sensitive directories
		"*.log", "*.tmp", "*.cache", // Temporary files
	}

	lowerName := strings.ToLower(name)
	for _, sensitive := range sensitiveNames {
		if matched, _ := filepath.Match(sensitive, lowerName); matched {
			return true
		}
	}

	return false
}

// categorizeFile categorizes files by type for better UI handling
func (ws *ReactWebServer) categorizeFile(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".ts", ".tsx":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".cpp", ".cc", ".cxx", ".c++", ".hpp", ".hh", ".hxx", ".h++":
		return "cpp"
	case ".c":
		return "c"
	case ".rs":
		return "rust"
	case ".php":
		return "php"
	case ".rb":
		return "ruby"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".scss", ".sass":
		return "sass"
	case ".less":
		return "less"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md", ".markdown":
		return "markdown"
	case ".txt":
		return "text"
	case ".sql":
		return "sql"
	case ".dockerfile", "dockerfile":
		return "docker"
	case ".makefile", "makefile":
		return "makefile"
	case ".conf", ".config", ".ini":
		return "config"
	case ".lock":
		return "lock"
	case ".sum":
		return "checksum"
	case ".mod":
		return "module"
	case ".test", "_test.go":
		return "test"
	case ".proto":
		return "protobuf"
	case ".graphql", ".gql":
		return "graphql"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".angular":
		return "angular"
	default:
		return "unknown"
	}
}

// handleAPIFile handles reading and writing individual files
func (ws *ReactWebServer) handleAPIFile(w http.ResponseWriter, r *http.Request) {
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	// Security check - prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "Invalid path: directory traversal not allowed", http.StatusBadRequest)
		return
	}

	// Additional security: ensure path is within reasonable bounds
	if len(cleanPath) > 1000 {
		http.Error(w, "Path too long", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		// Check if file exists and get info
		info, err := os.Stat(cleanPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "File not found", http.StatusNotFound)
			} else {
				http.Error(w, fmt.Sprintf("Failed to access file: %v", err), http.StatusInternalServerError)
			}
			return
		}

		// Don't allow reading directories as files
		if info.IsDir() {
			http.Error(w, "Path is a directory, not a file", http.StatusBadRequest)
			return
		}

		// Check file size - don't read extremely large files
		const maxFileSize = 10 * 1024 * 1024 // 10MB
		if info.Size() > maxFileSize {
			http.Error(w, fmt.Sprintf("File too large (max %d MB)", maxFileSize/(1024*1024)), http.StatusRequestEntityTooLarge)
			return
		}

		// Read file content
		content, err := os.ReadFile(cleanPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"message":  "success",
			"path":     cleanPath,
			"content":  string(content),
			"size":     info.Size(),
			"modified": info.ModTime().Unix(),
			"ext":      filepath.Ext(cleanPath),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

	case "POST":
		// Parse request body
		var request struct {
			Content string `json:"content"`
			Backup  bool   `json:"backup"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Check content size
		const maxContentLength = 10 * 1024 * 1024 // 10MB
		if len(request.Content) > maxContentLength {
			http.Error(w, fmt.Sprintf("Content too large (max %d MB)", maxContentLength/(1024*1024)), http.StatusRequestEntityTooLarge)
			return
		}

		// Create backup if requested
		if request.Backup {
			backupPath := cleanPath + ".backup." + fmt.Sprintf("%d", time.Now().Unix())
			if err := ws.createFileBackup(cleanPath, backupPath); err != nil {
				// Log backup error but don't fail the operation
				log.Printf("Warning: failed to create backup: %v", err)
			}
		}

		// Create directory if it doesn't exist
		dir := filepath.Dir(cleanPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
			return
		}

		// Write file with proper permissions
		if err := os.WriteFile(cleanPath, []byte(request.Content), 0644); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
			return
		}

		// Verify file was written correctly
		writtenContent, err := os.ReadFile(cleanPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to verify written file: %v", err), http.StatusInternalServerError)
			return
		}

		if string(writtenContent) != request.Content {
			http.Error(w, "File verification failed: written content doesn't match", http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"message": "File saved successfully",
			"path":    cleanPath,
			"size":    len(request.Content),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

	case "DELETE":
		// Delete file functionality
		info, err := os.Stat(cleanPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "File not found", http.StatusNotFound)
			} else {
				http.Error(w, fmt.Sprintf("Failed to access file: %v", err), http.StatusInternalServerError)
			}
			return
		}

		if info.IsDir() {
			http.Error(w, "Cannot delete directories via this endpoint", http.StatusBadRequest)
			return
		}

		// Create backup before deletion
		backupPath := cleanPath + ".backup." + fmt.Sprintf("%d", time.Now().Unix())
		if err := ws.createFileBackup(cleanPath, backupPath); err != nil {
			log.Printf("Warning: failed to create backup before deletion: %v", err)
		}

		if err := os.Remove(cleanPath); err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete file: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"message": "File deleted successfully",
			"path":    cleanPath,
			"backup":  backupPath,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// createFileBackup creates a backup of the file
func (ws *ReactWebServer) createFileBackup(originalPath, backupPath string) error {
	// Read original file
	content, err := os.ReadFile(originalPath)
	if err != nil {
		return err
	}

	// Write backup
	return os.WriteFile(backupPath, content, 0644)
}

// handleAPIConfig handles provider and model configuration changes
func (ws *ReactWebServer) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Return current configuration
		ws.handleAPIStats(w, r)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.agent == nil {
		http.Error(w, "Agent not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"success": false,
		"message": "",
	}

	var messages []string

	// Set provider if specified
	if req.Provider != "" {
		provider := api.ClientType(req.Provider)
		if err := ws.agent.SetProvider(provider); err != nil {
			response["message"] = fmt.Sprintf("Failed to set provider: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(response)
			return
		}
		messages = append(messages, fmt.Sprintf("Provider set to %s", req.Provider))
	}

	// Set model if specified
	if req.Model != "" {
		if err := ws.agent.SetModel(req.Model); err != nil {
			response["message"] = fmt.Sprintf("Failed to set model: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(response)
			return
		}
		messages = append(messages, fmt.Sprintf("model set to %s", req.Model))
	}

	response["success"] = true
	if len(messages) == 0 {
		response["message"] = "Configuration updated"
	} else {
		response["message"] = strings.Join(messages, ", ")
	}

	// Publish configuration change event
	ws.eventBus.Publish("config_changed", map[string]interface{}{
		"provider": ws.agent.GetProvider(),
		"model":    ws.agent.GetModel(),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// countConnections returns the current number of WebSocket connections
func (ws *ReactWebServer) countConnections() int {
	count := 0
	ws.connections.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// handleTerminalWebSocket handles terminal WebSocket connections
func (ws *ReactWebServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Generate session ID
	sessionID := fmt.Sprintf("terminal_%d", time.Now().UnixNano())

	// Create terminal session
	session, err := ws.terminalManager.CreateSession(sessionID)
	if err != nil {
		log.Printf("Failed to create terminal session: %v", err)
		conn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Failed to create terminal session"},
		})
		return
	}

	// Store connection
	ws.connections.Store(conn, true)
	defer ws.connections.Delete(conn)

	// Send session created message
	conn.WriteJSON(map[string]interface{}{
		"type": "session_created",
		"data": map[string]string{"session_id": sessionID},
	})

	// Start output reader goroutine
	go func() {
		if session.OutputCh != nil {
			for output := range session.OutputCh {
				select {
				case <-r.Context().Done():
					return
				default:
					if err := conn.WriteJSON(map[string]interface{}{
						"type": "output",
						"data": map[string]string{
							"session_id": sessionID,
							"output":     string(output),
						},
					}); err != nil {
						log.Printf("Terminal WebSocket write error: %v", err)
						return
					}
				}
			}
		}
	}()

	// Handle incoming messages
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Terminal WebSocket read error: %v", err)
			}
			break
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}

		switch msgType {
		case "input":
			data, ok := msg["data"].(map[string]interface{})
			if !ok {
				continue
			}
			
			input, ok := data["input"].(string)
			if !ok {
				continue
			}

			if err := ws.terminalManager.ExecuteCommand(sessionID, input); err != nil {
				conn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{
						"session_id": sessionID,
						"message":    err.Error(),
					},
				})
			}

		case "resize":
			// Handle terminal resize if needed in the future
			// For now, we'll just acknowledge it
			conn.WriteJSON(map[string]interface{}{
				"type": "resize_ack",
				"data": map[string]string{"session_id": sessionID},
			})

		case "close":
			// Close the terminal session
			ws.terminalManager.CloseSession(sessionID)
			return
		}
	}

	// Clean up session when connection closes
	ws.terminalManager.CloseSession(sessionID)
}

// handleTerminalHistory handles terminal history requests
func (ws *ReactWebServer) handleTerminalHistory(w http.ResponseWriter, r *http.Request) {
	// Get session ID from query parameter
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id parameter required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		// Get terminal history
		history, err := ws.terminalManager.GetHistory(sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		
		response := map[string]interface{}{
			"message": "success",
			"history": history,
			"count":   len(history),
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		
	case "POST":
		// Add command to history
		var req struct {
			Command string `json:"command"`
		}
		
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		
		if req.Command == "" {
			http.Error(w, "Command cannot be empty", http.StatusBadRequest)
			return
		}
		
		if err := ws.terminalManager.AddToHistory(sessionID, req.Command); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		
		response := map[string]interface{}{
			"message": "success",
			"command": req.Command,
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
