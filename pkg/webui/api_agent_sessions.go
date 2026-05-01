package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleAPIAgentSessions lists hidden agent sessions with status and output preview.
// This endpoint only returns background sessions (IsBackground=true).
// GET /api/terminal/agent-sessions
//
// Response format:
// {
//   "sessions": [
//     {
//       "id": "bg-npm-install-a1b2c3d4",
//       "name": "npm install ...",
//       "status": "active",
//       "chat_id": "chat-123",
//       "output_preview": "last 500 bytes of output"
//     }
//   ],
//   "count": 1
// }
func (ws *ReactWebServer) handleAPIAgentSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)

	// List all hidden sessions
	sessionIDs := terminalManager.ListHiddenSessions()

	sessions := []map[string]interface{}{}
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

		// Get output preview (last 500 bytes, stripped of ANSI)
		snapshot := session.ring.snapshot()
		preview := ""
		if len(snapshot) > 500 {
			preview = stripANSI(string(snapshot[len(snapshot)-500:]))
		} else {
			preview = stripANSI(string(snapshot))
		}

		session.mutex.RUnlock()

		sessions = append(sessions, map[string]interface{}{
			"id":             sessionID,
			"name":           name,
			"status":         map[bool]string{true: "active", false: "inactive"}[active],
			"chat_id":        chatID,
			"output_preview":  preview,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleAPIAgentSessionOutput returns accumulated output for a specific agent session.
// This endpoint only works for background sessions (IsBackground=true).
// GET /api/terminal/agent-sessions/{id}/output
//
// Response format: plain text output (ANSI stripped)
func (ws *ReactWebServer) handleAPIAgentSessionOutput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	// Path format: /api/terminal/agent-sessions/{id}/output
	path := strings.TrimPrefix(r.URL.Path, "/api/terminal/agent-sessions/")
	path = strings.TrimSuffix(path, "/output")
	sessionID := strings.TrimSpace(path)

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)

	// Get background output (validates session exists and is a background session)
	output, err := terminalManager.GetBackgroundOutput(sessionID)
	if err != nil {
		// Map known errors to appropriate HTTP status codes
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "not a background") {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to get output: %v", err), http.StatusInternalServerError)
		return
	}

	// Return as plain text
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(output))
}
