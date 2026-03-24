// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

// handleAPISessions handles GET /api/sessions - lists all saved sessions with metadata
func (ws *ReactWebServer) handleAPISessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get scope query parameter ("all" or "current")
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "current" // Default to current working directory sessions
	}

	var sessions []map[string]interface{}

	if scope == "current" {
		// Get sessions for current working directory
		workingDir, _ := os.Getwd()
		sessionInfos, err := agent.ListSessionsWithTimestampsScoped(workingDir)
		if err != nil {
			log.Printf("handleAPISessions: failed to list scoped sessions: %v", err)
			http.Error(w, fmt.Sprintf("Failed to list sessions: %v", err), http.StatusInternalServerError)
			return
		}
		if len(sessionInfos) > 100 {
			sessionInfos = sessionInfos[:100]
		}
		sessions = ws.buildSessionList(sessionInfos)
	} else {
		// Get all sessions across all scopes
		sessionInfos, err := agent.ListAllSessionsWithTimestamps()
		if err != nil {
			log.Printf("handleAPISessions: failed to list all sessions: %v", err)
			http.Error(w, fmt.Sprintf("Failed to list sessions: %v", err), http.StatusInternalServerError)
			return
		}
		if len(sessionInfos) > 100 {
			sessionInfos = sessionInfos[:100]
		}
		sessions = ws.buildSessionList(sessionInfos)
	}

	// Get current session ID
	currentSessionID := ""
	if ws.agent != nil {
		currentSessionID = ws.agent.GetSessionID()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":           "success",
		"sessions":          sessions,
		"current_session_id": currentSessionID,
	})
}

// buildSessionList builds the session list with message count and token info
func (ws *ReactWebServer) buildSessionList(sessionInfos []agent.SessionInfo) []map[string]interface{} {
	sessions := make([]map[string]interface{}, 0, len(sessionInfos))

	for _, info := range sessionInfos {
		// Load session state to get message count and token info
		messageCount := 0
		totalTokens := 0

		state, err := agent.LoadStateWithoutAgentScoped(info.SessionID, info.WorkingDirectory)
		if err == nil && state != nil {
			messageCount = len(state.Messages)
			totalTokens = state.TotalTokens
		}

		session := map[string]interface{}{
			"session_id":        info.SessionID,
			"name":              info.Name,
			"working_directory": info.WorkingDirectory,
			"last_updated":      info.LastUpdated.Format(time.RFC3339),
			"message_count":     messageCount,
			"total_tokens":      totalTokens,
		}
		sessions = append(sessions, session)
	}

	return sessions
}

// handleAPIRestoreSession handles POST /api/sessions/restore - restores a specific session
func (ws *ReactWebServer) handleAPIRestoreSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req struct {
		SessionID string `json:"session_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("handleAPIRestoreSession: invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.SessionID) == "" {
		log.Printf("handleAPIRestoreSession: session_id is required")
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)

	// Load the session state
	state, err := agent.LoadStateWithoutAgentScoped(sessionID, ws.workspaceRoot)
	if err != nil {
		log.Printf("handleAPIRestoreSession: failed to load session %s: %v", sessionID, err)
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusBadRequest)
		return
	}

	// Marshal state to JSON
	stateData, err := agent.ExportStateToJSON(&agent.ConversationState{
		Messages:                state.Messages,
		TotalCost:               state.TotalCost,
		TotalTokens:             state.TotalTokens,
		PromptTokens:            state.PromptTokens,
		CompletionTokens:        state.CompletionTokens,
		EstimatedTokenResponses: state.EstimatedTokenResponses,
		CachedTokens:            state.CachedTokens,
		CachedCostSavings:       state.CachedCostSavings,
		SessionID:               state.SessionID,
		Name:                    state.Name,
		WorkingDirectory:        state.WorkingDirectory,
	})
	if err != nil {
		log.Printf("handleAPIRestoreSession: failed to marshal state: %v", err)
		http.Error(w, "Failed to prepare session data", http.StatusInternalServerError)
		return
	}

	// Import state into agent
	if ws.agent != nil {
		if err := ws.agent.ImportState(stateData); err != nil {
			log.Printf("handleAPIRestoreSession: failed to import state: %v", err)
			http.Error(w, "Failed to import session state", http.StatusInternalServerError)
			return
		}
	}

	// Publish connection_status event to notify frontend of session change
	if ws.eventBus != nil {
		ws.eventBus.Publish("connection_status", map[string]interface{}{
			"connected":     true,
			"session_id":    state.SessionID,
			"restored":      true,
			"message_count": len(state.Messages),
		})
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":           "Session restored",
		"session_id":        state.SessionID,
		"message_count":     len(state.Messages),
		"total_tokens":      state.TotalTokens,
		"name":              state.Name,
		"working_directory": state.WorkingDirectory,
	})
}