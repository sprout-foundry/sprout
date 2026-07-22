//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// handleAPIQueryRewind handles POST /api/query/rewind to truncate the
// conversation history back to a prior turn, optionally reverting file
// changes made during the discarded turns.
//
// Request body:
//
//	{"to_turn": <int>, "revert_files": <bool>}
//
// to_turn is required (0-based: rewind to BEFORE this turn).
// revert_files defaults to true.
func (ws *ReactWebServer) handleAPIQueryRewind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req struct {
		ToTurn      *int  `json:"to_turn"`
		RevertFiles *bool `json:"revert_files"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("handleAPIQueryRewind: invalid JSON: %v", err)
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if req.ToTurn == nil {
		writeJSONErr(w, http.StatusBadRequest, "to_turn_required", "to_turn is required")
		return
	}

	revertFiles := true
	if req.RevertFiles != nil {
		revertFiles = *req.RevertFiles
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	// Reject if a query is currently running.
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.RUnlock()
		writeJSONErr(w, http.StatusBadRequest, "client_context_not_found", "Client context not found")
		return
	}
	if ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		writeJSONErr(w, http.StatusConflict, "query_in_progress", "Cannot rewind while a query is running")
		return
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if errors.Is(err, ErrNoProviderConfigured) || isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		}
		return
	}

	result, err := clientAgent.Rewind(agent.RewindOptions{
		ToTurnIndex: *req.ToTurn,
		RevertFiles: revertFiles,
	})
	if err != nil {
		log.Printf("handleAPIQueryRewind: rewind failed chat_id=%s err=%v", chatID, err)
		writeJSONErr(w, http.StatusBadRequest, "rewind_failed", fmt.Sprintf("Rewind failed: %v", err))
		return
	}

	// Sync agent state so the UI reflects the truncated history.
	if syncErr := ws.syncAgentStateForClientWithChat(clientID, chatID); syncErr != nil {
		log.Printf("handleAPIQueryRewind: state sync warning chat_id=%s err=%v", chatID, syncErr)
	}

	// Notify the UI that the session changed via rewind.
	ws.publishSessionChanged(clientID, chatID, "rewind", map[string]interface{}{
		"turns_discarded":     result.TurnsDiscarded,
		"messages_removed":    result.MessagesRemoved,
		"checkpoints_dropped": result.CheckpointsDropped,
	})

	log.Printf("handleAPIQueryRewind: completed chat_id=%s turns=%d messages=%d files_reverted=%d files_skipped=%d",
		chatID, result.TurnsDiscarded, result.MessagesRemoved,
		len(result.FilesReverted), len(result.FilesSkipped))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"turns_discarded":     result.TurnsDiscarded,
		"messages_removed":    result.MessagesRemoved,
		"files_reverted":      result.FilesReverted,
		"files_skipped":       result.FilesSkipped,
		"checkpoints_dropped": result.CheckpointsDropped,
	})
}
