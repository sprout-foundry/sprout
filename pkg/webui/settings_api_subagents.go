//go:build !js

package webui

import (
	"net/http"
)

// handleAPISettingsSubagentTypes dispatches GET on /api/settings/subagent-types.
// Personas are catalog-fixed: there is no PUT/DELETE — user customization of
// personas was removed in favor of skills (~/.config/sprout/skills/), which
// remain user-extensible.
func (ws *ReactWebServer) handleAPISettingsSubagentTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed (personas are catalog-fixed; PUT/DELETE removed)", http.StatusMethodNotAllowed)
		return
	}
	ws.handleAPISettingsSubagentTypesGet(w, r)
}

func (ws *ReactWebServer) handleAPISettingsSubagentTypesGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()

	providers := ws.listProviders(ws.resolveClientID(r))

	currentProvider := cfg.GetSubagentProvider()
	currentModel := cfg.GetSubagentModel()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"subagent_types":      cfg.SubagentTypes,
		"disabled_personas":   cfg.DisabledPersonas,
		"available_providers": providers,
		"current_provider":    currentProvider,
		"current_model":       currentModel,
	})
}
