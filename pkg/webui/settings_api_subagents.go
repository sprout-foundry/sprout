package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Method router — Subagent type settings
// ---------------------------------------------------------------------------

// handleAPISettingsSubagentTypes dispatches GET/PUT on collection, PUT/DELETE on individual persona.
// Exact path (/api/settings/subagent-types) maps here for GET; trailing-slash
// (/api/settings/subagent-types/) maps here for PUT/DELETE individual persona.
func (ws *ReactWebServer) handleAPISettingsSubagentTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsSubagentTypesGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsSubagentTypesPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsSubagentTypesDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings/subagent-types
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSubagentTypesGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()

	// Get available providers (same format as /api/providers)
	providers := ws.listProviders(ws.resolveClientID(r))

	// Get current subagent provider and model from config
	currentProvider := cfg.GetSubagentProvider()
	currentModel := cfg.GetSubagentModel()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"subagent_types":      cfg.SubagentTypes,
		"available_providers": providers,
		"current_provider":    currentProvider,
		"current_model":       currentModel,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/subagent-types/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSubagentTypesPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/subagent-types/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "subagent type name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Accept only provider and model updates.
	// Use raw JSON to distinguish "field present (possibly empty string)" from "field absent".
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			return fmt.Errorf("subagent type %q not found", name)
		}
		if _, exists := cfg.SubagentTypes[name]; !exists {
			return fmt.Errorf("subagent type %q not found (use GET to list available types)", name)
		}

		existing := cfg.SubagentTypes[name]
		if v, ok := raw["provider"]; ok {
			s, ok := v.(string)
			if !ok {
				return errors.New("provider must be a string")
			}
			// Empty string means "inherit from default subagent settings"
			existing.Provider = strings.TrimSpace(s)
		}
		if v, ok := raw["model"]; ok {
			s, ok := v.(string)
			if !ok {
				return errors.New("model must be a string")
			}
			// Empty string means "inherit from default subagent settings"
			existing.Model = strings.TrimSpace(s)
		}
		cfg.SubagentTypes[name] = existing
		return nil
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "must be a string") {
			writeJSONError(w, http.StatusBadRequest, errMsg)
		} else {
			writeJSONError(w, http.StatusNotFound, errMsg)
		}
		return
	}

	updated := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"type":    updated.SubagentTypes[name],
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/subagent-types/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSubagentTypesDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/subagent-types/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "subagent type name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			return fmt.Errorf("subagent type %q not found", name)
		}
		if _, exists := cfg.SubagentTypes[name]; !exists {
			return fmt.Errorf("subagent type %q not found", name)
		}
		delete(cfg.SubagentTypes, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}
