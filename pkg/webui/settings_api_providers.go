//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Method router — Provider settings
// ---------------------------------------------------------------------------

// handleAPISettingsProviders dispatches all methods for /api/settings/providers and /api/settings/providers/{name}.
// Exact path (/api/settings/providers) maps here for GET/POST; trailing-slash (/api/settings/providers/) maps here for PUT/DELETE.
func (ws *ReactWebServer) handleAPISettingsProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsProvidersGet(w, r)
	case http.MethodPost:
		ws.handleAPISettingsProvidersPost(w, r)
	case http.MethodPut:
		ws.handleAPISettingsProvidersPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsProvidersDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings/providers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersGet(w http.ResponseWriter, r *http.Request) {
	// GET is best-effort: when no config manager is available (no agent,
	// no layered config), return an empty list with 200 rather than
	// 503. The UI can still render its providers panel and let the user
	// add a first custom provider via POST. POST itself uses the strict
	// getConfigManager since it needs a persistable target.
	cm := ws.resolveConfigManagerQuietly(r)
	if cm == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			// custom_providers is a map[string]CustomProviderConfig in
			// real config — emit an empty map so the JSON shape matches
			// (`{}`, not `[]`).
			"custom_providers": map[string]configuration.CustomProviderConfig{},
		})
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"custom_providers": sanitizedCustomProviders(cfg.CustomProviders),
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/providers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersPost(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Auto-truncate string fields that exceed backend limits.
	provider = truncateCustomProvider(provider)

	if provider.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	if err := validateCustomProvider(provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := provider.Name
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[key]; exists {
			return fmt.Errorf("custom provider %q already exists (use PUT to update)", key)
		}
		cfg.CustomProviders[key] = provider
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	// Persist the custom provider to its own file (~/.config/sprout/providers/{name}.json)
	// so it survives config.json saves (which strip CustomProviders).
	if saveErr := configuration.SaveCustomProvider(provider); saveErr != nil {
		log.Printf("webui: warning: failed to persist custom provider %q to file: %v", key, saveErr)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/providers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/providers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Ensure name in body matches URL
	provider.Name = name

	// Auto-truncate string fields that exceed backend limits.
	provider = truncateCustomProvider(provider)

	if err := validateCustomProvider(provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found (use POST to create)", name)
		}
		cfg.CustomProviders[name] = provider
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	// Persist the updated custom provider to its own file
	if saveErr := configuration.SaveCustomProvider(provider); saveErr != nil {
		log.Printf("webui: warning: failed to persist custom provider %q to file: %v", name, saveErr)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/providers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/providers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			return fmt.Errorf("custom provider %q not found", name)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found", name)
		}
		delete(cfg.CustomProviders, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	// Remove the provider's individual file
	if delErr := configuration.DeleteCustomProvider(name); delErr != nil {
		log.Printf("webui: warning: failed to delete custom provider file %q: %v", name, delErr)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateCustomProvider(p configuration.CustomProviderConfig) error {
	if p.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	if p.Endpoint == "" {
		return fmt.Errorf("provider endpoint is required")
	}
	if p.ContextSize < 0 {
		return fmt.Errorf("context_size must be non-negative")
	}
	return nil
}
