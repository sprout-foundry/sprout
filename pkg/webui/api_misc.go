package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/console"
)

// handleAPIConfig handles API requests for configuration
func (ws *ReactWebServer) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientCtx := ws.getClientContextForRequest(r)
	// Get current configuration
	config := map[string]interface{}{
		"port":           ws.port,
		"daemon_root":    ws.GetDaemonRoot(),
		"workspace_root": clientCtx.WorkspaceRoot,
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
	terminalManager := ws.getTerminalManagerForRequest(r)

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
	history, err := terminalManager.GetHistory(sessionID)
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
	terminalManager := ws.getTerminalManagerForRequest(r)
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

	if err := terminalManager.AddToHistory(req.SessionID, command); err != nil {
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
	terminalManager := ws.getTerminalManagerForRequest(r)
	// Get list of session IDs
	sessionIDs := terminalManager.ListSessions()

	// Build detailed info for each session
	sessions := []map[string]interface{}{}
	activeCount := 0
	for _, sessionID := range sessionIDs {
		session, exists := terminalManager.GetSession(sessionID)
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
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

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
	relativePath, err := console.SavePastedImage(data, workspaceRoot)
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
