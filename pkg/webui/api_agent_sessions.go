//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

// handleAPIAgentSessions lists hidden agent sessions with status and output preview.
// This endpoint only returns background sessions (IsBackground=true).
// GET /api/terminal/agent-sessions
//
// Response format:
//
//	{
//	  "sessions": [
//	    {
//	      "id": "bg-npm-install-a1b2c3d4",
//	      "name": "npm install ...",
//	      "status": "active",
//	      "chat_id": "chat-123",
//	      "output_preview": "last 500 bytes of output",
//	      "started_at": 1234567890
//	    }
//	  ],
//	  "count": 1
//	}
func (ws *ReactWebServer) handleAPIAgentSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)

	// List all hidden sessions
	sessionIDs := terminalManager.ListHiddenSessions()

	type sessionEntry struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Status        string `json:"status"`
		ChatID        string `json:"chat_id"`
		OutputPreview string `json:"output_preview"`
		StartedAt     int64  `json:"started_at"` // Unix timestamp (seconds) when session was created
	}

	sessions := make([]sessionEntry, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		session, exists := terminalManager.GetSession(sessionID)
		if !exists {
			continue
		}

		session.mutex.RLock()
		// Only include background sessions (not regular hidden sessions)
		if !session.IsBackground {
			session.mutex.RUnlock()
			continue
		}

		name := session.Name
		chatID := session.ChatID
		active := session.Active

		var startedAt int64
		if !session.StartedAt.IsZero() {
			startedAt = session.StartedAt.Unix()
		} else if !session.LastUsed.IsZero() {
			// Fallback for sessions created before StartedAt was added.
			startedAt = session.LastUsed.Unix()
		}

		// Get output preview (last 500 bytes, stripped of ANSI)
		snapshot := session.ring.snapshot()
		preview := ""
		if len(snapshot) > 500 {
			start := len(snapshot) - 500
			for start < len(snapshot) && !utf8.RuneStart(snapshot[start]) {
				start++
			}
			preview = stripANSI(string(snapshot[start:]))
		} else {
			preview = stripANSI(string(snapshot))
		}

		session.mutex.RUnlock()

		status := "inactive"
		if active {
			status = "active"
		}

		sessions = append(sessions, sessionEntry{
			ID:            sessionID,
			Name:          name,
			Status:        status,
			ChatID:        chatID,
			OutputPreview: preview,
			StartedAt:     startedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// maxSessionIDLength limits session IDs from URL paths to prevent abuse.
const maxSessionIDLength = 128

// handleAPIAgentSessionActions dispatches requests for agent session sub-paths.
// This handler routes to the appropriate action based on the URL suffix:
//
//	GET  /api/terminal/agent-sessions/{id}/output  → returns accumulated output as plain text
//	POST /api/terminal/agent-sessions/{id}/attach  → promotes hidden session to visible
//
// The /output endpoint only works for background sessions (IsBackground=true).
// The /attach endpoint converts a hidden background session into a visible terminal
// session that appears in the terminal tab bar.
//
// Response format for /output: plain text output (ANSI stripped)
// Response format for /attach: {"id": "...", "status": "attached"}
func (ws *ReactWebServer) handleAPIAgentSessionActions(w http.ResponseWriter, r *http.Request) {
	// Extract the sub-path from the URL
	// Path format: /api/terminal/agent-sessions/{id}/{action}
	prefix := "/api/terminal/agent-sessions/"
	relativePath := strings.TrimPrefix(r.URL.Path, prefix)

	if relativePath == "" || strings.Count(relativePath, "/") != 1 {
		http.Error(w, "Invalid request path", http.StatusBadRequest)
		return
	}

	// Split into session ID and action (guaranteed 2 parts since we verified exactly 1 slash)
	parts := strings.SplitN(relativePath, "/", 2)
	sessionID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])

	if sessionID == "" || action == "" {
		http.Error(w, "Session ID and action are required", http.StatusBadRequest)
		return
	}

	// Validate session ID length (defense-in-depth against pathological inputs)
	if len(sessionID) > maxSessionIDLength {
		http.Error(w, "Session ID too long", http.StatusBadRequest)
		return
	}

	// Dispatch based on action
	switch action {
	case "output":
		ws.handleAgentSessionOutput(w, r, sessionID)
	case "attach":
		ws.handleAgentSessionAttach(w, r, sessionID)
	case "kill":
		ws.handleAgentSessionKill(w, r, sessionID)
	default:
		http.Error(w, fmt.Sprintf("Unknown action: %s", action), http.StatusNotFound)
	}
}

// handleAgentSessionOutput returns accumulated output for a specific agent session.
// This endpoint only works for background sessions (IsBackground=true).
// GET /api/terminal/agent-sessions/{id}/output
//
// Response format: plain text output (ANSI stripped)
func (ws *ReactWebServer) handleAgentSessionOutput(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)

	// Get background output (validates session exists and is a background session)
	output, err := terminalManager.GetBackgroundOutput(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) || errors.Is(err, ErrNotBackgroundSession) {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		log.Printf("Failed to get background output for session %s: %v", sessionID, err)
		http.Error(w, "Failed to get output", http.StatusInternalServerError)
		return
	}

	// Return as plain text
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(output))
}

// handleAgentSessionAttach promotes a hidden background session to a visible session.
// This clears the Hidden flag on the session so it appears in the terminal tab bar.
// POST /api/terminal/agent-sessions/{id}/attach
//
// Response format: {"id": "...", "status": "attached"}
func (ws *ReactWebServer) handleAgentSessionAttach(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)

	// Get the session
	session, exists := terminalManager.GetSession(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Atomically validate and mutate under a single write lock to prevent TOCTOU.
	// Without this, the session could become inactive between the validation read
	// and the Hidden flag write.
	session.mutex.Lock()
	isBackground := session.IsBackground
	active := session.Active
	isHidden := session.Hidden

	if !isBackground {
		session.mutex.Unlock()
		http.Error(w, "Session is not a background session", http.StatusBadRequest)
		return
	}
	if !active {
		session.mutex.Unlock()
		http.Error(w, "Session is not active", http.StatusBadRequest)
		return
	}
	if !isHidden {
		// Already visible — return idempotent success without mutation.
		session.mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     sessionID,
			"status": "attached",
		})
		return
	}

	// Clear the Hidden flag to make the session visible
	session.Hidden = false
	session.mutex.Unlock()

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     sessionID,
		"status": "attached",
	})
}

// handleAgentSessionKill terminates a background agent session.
// POST /api/terminal/agent-sessions/{id}/kill
func (ws *ReactWebServer) handleAgentSessionKill(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)

	// Verify the session exists and is a background session.
	// Use a write lock to mark the session inactive atomically, preventing
	// a concurrent /attach from promoting this session between our check and
	// the CloseSession call.
	session, exists := terminalManager.GetSession(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.mutex.Lock()
	if !session.IsBackground {
		session.mutex.Unlock()
		http.Error(w, "Session is not a background session", http.StatusBadRequest)
		return
	}
	// Mark inactive under the write lock so a concurrent attach sees !Active.
	session.Active = false
	session.mutex.Unlock()

	// Close the session (terminates the PTY process)
	if err := terminalManager.CloseSession(sessionID); err != nil {
		log.Printf("Failed to kill background session %s: %v", sessionID, err)
		http.Error(w, "Failed to kill session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     sessionID,
		"status": "killed",
	})
}
