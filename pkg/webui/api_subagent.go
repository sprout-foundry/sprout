//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// handleAPISubagentCancel handles POST /api/subagent/{id}/cancel.
// Cancels a single running subagent in the active chat. Idempotent: if
// the ID is not currently running (already done / unknown), returns 404
// so the UI can clean up its row. Returns 200 with a payload on success.
//
// Why not piggy-back on /api/query/stop? Stop is whole-session and the
// UI needs per-row affordances (a subagent tree may have multiple
// siblings running). See SP-059 Phase 1a.
func (ws *ReactWebServer) handleAPISubagentCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Path is "/api/subagent/{id}/cancel"; strip the trailing "/cancel"
	// and the leading "/api/subagent/" to get the ID.
	path := strings.TrimSuffix(r.URL.Path, "/cancel")
	id := extractPathSegment(path, "/api/subagent/")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "subagent id is required")
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := r.URL.Query().Get("chat_id")
	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		}
		return
	}

	runner := clientAgent.GetSubagentRunner()
	if runner == nil {
		writeJSONError(w, http.StatusNotFound, "no subagent runner for this chat")
		return
	}

	if !runner.CancelSubagent(id) {
		// Not active — treat as "already done" so the UI can clean up
		// its row without surfacing a hard error to the user.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "ok",
			"already_completed": true,
			"id":                id,
			"timestamp":         time.Now().Unix(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"id":        id,
		"mode":      "cancel",
		"timestamp": time.Now().Unix(),
	})
}
