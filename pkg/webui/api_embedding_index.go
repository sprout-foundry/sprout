//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// handleAPIEmbeddingIndex handles GET/POST /api/embedding-index
// GET returns current indexing status.
// POST accepts { "enabled": bool } to toggle indexing on/off.
func (ws *ReactWebServer) handleAPIEmbeddingIndex(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_unavailable", "Agent not available")
		return
	}

	switch r.Method {
	case http.MethodGet:
		ws.writeEmbeddingIndexStatus(w, agentInst)
	case http.MethodPost:
		ws.handleSetEmbeddingIndex(w, r, agentInst)
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

func (ws *ReactWebServer) writeEmbeddingIndexStatus(w http.ResponseWriter, agentInst *agent.Agent) {
	enabled := agentInst.IsEmbeddingIndexEnabled()
	response := map[string]interface{}{
		"enabled": enabled,
	}

	em := agentInst.GetEmbeddingManager()
	if em != nil {
		response["index_size"] = em.IndexSize()
		response["initialized"] = em.IsInitialized()
		response["building"] = em.IsBuilding()
	} else {
		response["index_size"] = 0
		response["initialized"] = false
		response["building"] = false
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (ws *ReactWebServer) handleSetEmbeddingIndex(w http.ResponseWriter, r *http.Request, agentInst *agent.Agent) {
	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if req.Enabled {
		if !agentInst.IsEmbeddingIndexEnabled() {
			if err := agentInst.EnableEmbeddingIndex(); err != nil {
				writeJSONErr(w, http.StatusInternalServerError, "embedding_index_enable_failed", fmt.Sprintf("Failed to enable indexing: %v", err))
				return
			}
		}
	} else {
		agentInst.DisableEmbeddingIndex()
	}

	ws.writeEmbeddingIndexStatus(w, agentInst)
}
