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

	cm.EnrichCustomProviders()
	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"custom_providers": sanitizedCustomProviders(cfg.CustomProviders),
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/providers
// ---------------------------------------------------------------------------

// handleAPISettingsProvidersPost creates a new custom provider. The order of
// operations is transactional: the provider file is written first so a
// failure leaves the in-memory map untouched, then the map is mutated. If
// the in-memory commit fails after the file is on disk, the file is
// rolled back best-effort.
func (ws *ReactWebServer) handleAPISettingsProvidersPost(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cm.EnrichCustomProviders()

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

	duplicate := false
	checkErr := cm.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if _, exists := cfg.CustomProviders[provider.Name]; exists {
			duplicate = true
		}
		return nil
	})
	if checkErr != nil {
		writeJSONError(w, http.StatusInternalServerError, checkErr.Error())
		return
	}
	if duplicate {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("custom provider %q already exists (use PUT to update)", provider.Name))
		return
	}

	// Write the provider file first; a failure here leaves the in-memory
	// map untouched so the UI can retry without waiting for a reload.
	if saveErr := configuration.SaveCustomProvider(provider); saveErr != nil {
		log.Printf("webui: failed to persist provider %q: %v", provider.Name, saveErr)
		writeJSONError(w, http.StatusInternalServerError, "failed to persist provider")
		return
	}

	// Commit the in-memory mutation. If this fails (e.g. a concurrent
	// POST raced past the duplicate check), roll back the file we just
	// wrote so the next reload doesn't surface a phantom provider.
	commitErr := cm.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[provider.Name]; exists {
			return fmt.Errorf("custom provider %q already exists", provider.Name)
		}
		cfg.CustomProviders[provider.Name] = provider
		return nil
	})
	if commitErr != nil {
		if delErr := configuration.DeleteCustomProvider(provider.Name); delErr != nil {
			log.Printf("webui: failed to roll back provider %q after commit error: %v", provider.Name, delErr)
		}
		log.Printf("webui: failed to commit in-memory create for provider %q: %v", provider.Name, commitErr)
		writeJSONError(w, http.StatusInternalServerError, "failed to create provider")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/providers/{name}
// ---------------------------------------------------------------------------

// handleAPISettingsProvidersPut updates an existing custom provider.
// Transactional: the new file is written first, then the in-memory map is
// mutated. If the in-memory commit fails after the file is on disk, the
// previous version is restored from the captured snapshot.
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

	cm.EnrichCustomProviders()

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

	// Capture the existing provider under lock so we can roll back on
	// in-memory commit failure.
	var oldProvider configuration.CustomProviderConfig
	captureErr := cm.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		existing, ok := cfg.CustomProviders[name]
		if !ok {
			return fmt.Errorf("custom provider %q not found (use POST to create)", name)
		}
		oldProvider = existing
		return nil
	})
	if captureErr != nil {
		writeJSONError(w, http.StatusNotFound, captureErr.Error())
		return
	}

	// Persist the new provider file first; a failure here leaves the
	// in-memory map (and therefore any concurrent read) untouched.
	if saveErr := configuration.SaveCustomProvider(provider); saveErr != nil {
		log.Printf("webui: failed to persist provider %q: %v", name, saveErr)
		writeJSONError(w, http.StatusInternalServerError, "failed to persist provider")
		return
	}

	// Commit the in-memory mutation. If this fails (e.g. a concurrent
	// DELETE removed the entry between capture and commit), restore the
	// previous file so the provider's last-known-good value survives.
	commitErr := cm.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found during commit", name)
		}
		cfg.CustomProviders[name] = provider
		return nil
	})
	if commitErr != nil {
		if rollbackErr := configuration.SaveCustomProvider(oldProvider); rollbackErr != nil {
			log.Printf("webui: failed to roll back provider %q after commit error: %v", name, rollbackErr)
		}
		log.Printf("webui: failed to commit in-memory update for provider %q: %v", name, commitErr)
		writeJSONError(w, http.StatusInternalServerError, "failed to update provider state")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/providers/{name}
// ---------------------------------------------------------------------------

// handleAPISettingsProvidersDelete removes a custom provider. Transactional:
// the provider file is deleted first, then the in-memory map entry. If the
// in-memory commit fails after the file is gone, the previous version is
// restored from the captured snapshot.
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

	cm.EnrichCustomProviders()

	// Capture the existing provider so we can restore its file if the
	// in-memory commit fails after disk deletion.
	var oldProvider configuration.CustomProviderConfig
	captureErr := cm.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		existing, ok := cfg.CustomProviders[name]
		if !ok {
			return fmt.Errorf("custom provider %q not found", name)
		}
		oldProvider = existing
		return nil
	})
	if captureErr != nil {
		writeJSONError(w, http.StatusNotFound, captureErr.Error())
		return
	}

	// Remove the provider file first; a failure leaves the in-memory
	// map (and the manager's view of the provider) intact.
	if delErr := configuration.DeleteCustomProvider(name); delErr != nil {
		log.Printf("webui: failed to delete provider %q file: %v", name, delErr)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete provider")
		return
	}

	// Commit the in-memory removal. If this fails (e.g. a concurrent
	// PUT re-created the entry), restore the file we just deleted.
	commitErr := cm.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			return fmt.Errorf("custom provider %q not found during commit", name)
		}
		delete(cfg.CustomProviders, name)
		return nil
	})
	if commitErr != nil {
		if restoreErr := configuration.SaveCustomProvider(oldProvider); restoreErr != nil {
			log.Printf("webui: failed to restore provider %q file after commit error: %v", name, restoreErr)
		}
		log.Printf("webui: failed to commit in-memory delete for provider %q: %v", name, commitErr)
		writeJSONError(w, http.StatusInternalServerError, "failed to delete provider state")
		return
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
