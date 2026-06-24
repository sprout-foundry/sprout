//go:build !js

package webui

import (
	"context"
	"net/http"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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
	// GET is best-effort: fall back to empty defaults rather than 503
	// when no config manager is available. See the matching comment on
	// handleAPISettingsProvidersGet for the rationale.
	cm := ws.resolveConfigManagerQuietly(r)
	// Derive a context from the request so model discovery is cancelled
	// if the client disconnects. Matches handleAPIProviders' timeout.
	listCtx, listCancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer listCancel()
	providers := ws.listProvidersCtx(listCtx, ws.resolveClientID(r))

	if cm == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			// subagent_types is a map[string]SubagentType in real config —
			// emit an empty map so the JSON shape matches what the UI
			// expects (`{}`, not `[]`).
			"subagent_types":      map[string]configuration.SubagentType{},
			"disabled_personas":   []string{},
			"available_providers": providers,
			"current_provider":    "",
			"current_model":       "",
		})
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"subagent_types":      cfg.SubagentTypes,
		"disabled_personas":   cfg.DisabledPersonas,
		"available_providers": providers,
		"current_provider":    cfg.GetSubagentProvider(),
		"current_model":       cfg.GetSubagentModel(),
	})
}
