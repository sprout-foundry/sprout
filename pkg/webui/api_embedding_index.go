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
		http.Error(w, "Agent not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		ws.writeEmbeddingIndexStatus(w, agentInst)
	case http.MethodPost:
		ws.handleSetEmbeddingIndex(w, r, agentInst)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Enabled {
		if !agentInst.IsEmbeddingIndexEnabled() {
			if err := agentInst.EnableEmbeddingIndex(); err != nil {
				http.Error(w, fmt.Sprintf("Failed to enable indexing: %v", err), http.StatusInternalServerError)
				return
			}
		}
	} else {
		agentInst.DisableEmbeddingIndex()
	}

	ws.writeEmbeddingIndexStatus(w, agentInst)
}
