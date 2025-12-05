// Package webui provides React web server with embedded assets
package webui

import (
	"context"
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

// ReactWebServer provides the React web UI
type ReactWebServer struct {
	agent       *agent.Agent
	eventBus    *events.EventBus
	port        int
	server      *http.Server
	upgrader    websocket.Upgrader
	connections sync.Map // map[*websocket.Conn]bool
	isRunning   bool
	mutex       sync.RWMutex
	startTime   time.Time
	queryCount  int
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = 54321
	}

	return &ReactWebServer{
		agent:    agent,
		eventBus: eventBus,
		port:     port,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow localhost connections
				origin := r.Header.Get("Origin")
				return origin == "http://localhost:"+strconv.Itoa(port) ||
					origin == "" // Allow same-origin and direct connections
			},
		},
		startTime: time.Now(),
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
	mux.HandleFunc("/api/query", ws.handleAPIQuery)
	mux.HandleFunc("/api/stats", ws.handleAPIStats)
	mux.HandleFunc("/api/files", ws.handleAPIFiles)
	mux.HandleFunc("/api/file", ws.handleAPIFile)
	mux.HandleFunc("/api/config", ws.handleAPIConfig)

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
	// Serve the React index.html - use correct path for production server
	http.ServeFile(w, r, "./pkg/webui/static/index.html")
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

	// Construct the full file path - use correct path for production server
	fullPath := "./pkg/webui/static/static/" + filePath

	// Set appropriate Content-Type header based on file extension
	ext := ""
	if lastDot := strings.LastIndex(filePath, "."); lastDot != -1 {
		ext = filePath[lastDot:]
	}

	switch ext {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
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

	// Serve the file
	http.ServeFile(w, r, fullPath)
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
	var provider, model string
	var totalTokens, totalCost interface{}

	if ws.agent != nil {
		provider = ws.agent.GetProvider()
		model = ws.agent.GetModel()
		totalTokens = ws.agent.GetCurrentContextTokens()
		totalCost = ws.agent.GetTotalCost()
	} else {
		provider = "test-provider"
		model = "test-model"
		totalTokens = 0
		totalCost = 0.0
	}

	stats := map[string]interface{}{
		"provider":     provider,
		"model":        model,
		"query_count":  ws.queryCount,
		"uptime":       time.Since(ws.startTime).String(),
		"total_tokens": totalTokens,
		"total_cost":   totalCost,
		"connections":  ws.countConnections(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAPIFiles returns file system information (placeholder)
func (ws *ReactWebServer) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	// Get the directory path from query parameter (default to current directory)
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// Security check - prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Build file tree response
	var files []interface{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
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
		}

		files = append(files, fileInfo)
	}

	response := map[string]interface{}{
		"message": "success",
		"path":    cleanPath,
		"files":   files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		// Read file
		content, err := os.ReadFile(cleanPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "File not found", http.StatusNotFound)
			} else {
				http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusInternalServerError)
			}
			return
		}

		// Get file info
		info, err := os.Stat(cleanPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get file info: %v", err), http.StatusInternalServerError)
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
		// Write file
		var request struct {
			Content string `json:"content"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Create directory if it doesn't exist
		dir := filepath.Dir(cleanPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
			return
		}

		// Write file
		if err := os.WriteFile(cleanPath, []byte(request.Content), 0644); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"message": "File saved successfully",
			"path":    cleanPath,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
