package webui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Method router — Skill settings
// ---------------------------------------------------------------------------

// handleAPISettingsSkills dispatches GET and PUT /api/settings/skills.
func (ws *ReactWebServer) handleAPISettingsSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsSkillsGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsSkillsPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings/skills
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSkillsGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"skills": cfg.Skills,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/skills
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSkillsPut(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Accept either:
	//   { "skills": { "id": { "enabled": true/false, ... }, ... } }
	// or a flat list of toggles:
	//   { "toggles": { "id": true/false, ... } }
	var incoming struct {
		Skills  map[string]configuration.Skill `json:"skills"`
		Toggles map[string]bool                `json:"toggles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.Skills == nil {
			cfg.Skills = make(map[string]configuration.Skill)
		}

		// Apply full skill entries
		for id, skill := range incoming.Skills {
			existing, exists := cfg.Skills[id]
			if exists {
				// Preserve existing metadata that wasn't provided
				skill.Path = existing.Path
				if skill.Metadata == nil {
					skill.Metadata = existing.Metadata
				}
				if skill.AllowedTools == "" {
					skill.AllowedTools = existing.AllowedTools
				}
				if skill.Description == "" {
					skill.Description = existing.Description
				}
				if skill.Name == "" {
					skill.Name = existing.Name
				}
			}
			skill.ID = id
			cfg.Skills[id] = skill
		}

		// Apply simple enable/disable toggles
		for id, enabled := range incoming.Toggles {
			if existing, exists := cfg.Skills[id]; exists {
				existing.Enabled = enabled
				cfg.Skills[id] = existing
			}
		}

		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"skills":  updated.Skills,
	})
}
