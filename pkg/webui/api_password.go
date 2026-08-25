//go:build !js

package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// handleAPIPasswordRoutes dispatches /api/password/{id}/respond.
func (ws *ReactWebServer) handleAPIPasswordRoutes(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/respond") {
		ws.handleAPIPasswordRespond(w, r)
		return
	}
	http.NotFound(w, r)
}

// passwordRespondRequest is the JSON body for POST /api/password/{id}/respond.
type passwordRespondRequest struct {
	Password string `json:"password"`
}

// handleAPIPasswordRespond handles POST /api/password/{id}/respond — the
// WebUI submits the user's password for a pending password request.
//
// CRITICAL: The password value must NEVER appear in any log output.
func (ws *ReactWebServer) handleAPIPasswordRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Extract password request ID from path: /api/password/{id}/respond
	requestID := extractPasswordIDFromPath(r.URL.Path)
	if requestID == "" {
		writeJSONErr(w, http.StatusBadRequest, "password_request_id_required", "Password request ID is required")
		return
	}

	var req passwordRespondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ws.log().Warn("invalid password response JSON", slog.Any("err", err))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	// Deliver to the agent's broker — the goroutine blocked in
	// WebUIPasswordPrompter.Prompt is waiting on this channel.
	ag := ws.resolveEditAgent()
	delivered := false
	if ag != nil {
		delivered = ag.RespondToPasswordRequest(requestID, req.Password)
	}

	if !delivered {
		ws.log().Warn("password request not found or already responded", slog.String("request_id", requestID))
		writeJSONErr(w, http.StatusNotFound, "not_found", "Password request not found or already responded")
		return
	}

	ws.log().Info("delivered password response", slog.String("request_id", requestID))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"request_id": requestID,
		"responded":  true,
	})
}

// extractPasswordIDFromPath parses /api/password/{id}/respond and returns
// the {id} segment.
func extractPasswordIDFromPath(path string) string {
	// Expected: /api/password/{id}/respond or /api/password/{id}
	parts := splitPath(path)
	// parts[0]="api", parts[1]="password", parts[2]={id}, parts[3]=optional "respond"
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "password" {
		id := parts[2]
		if len(parts) == 4 && parts[3] == "respond" {
			return id
		}
		// /api/password/{id} (no /respond suffix)
		if len(parts) == 3 && id != "respond" {
			return id
		}
	}
	return ""
}
