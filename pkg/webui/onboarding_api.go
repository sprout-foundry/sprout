package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

type onboardingProvider struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Models        []string `json:"models"`
	RequiresAPIKey bool    `json:"requires_api_key"`
	HasCredential bool     `json:"has_credential"`
}

func (ws *ReactWebServer) handleAPIOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cm := ws.getConfigManager(w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	apiKeys := cm.GetAPIKeys()
	descriptors := ws.listProviders()
	providers := make([]onboardingProvider, 0, len(descriptors))
	indexByID := make(map[string]onboardingProvider, len(descriptors))

	for _, desc := range descriptors {
		meta, _ := configuration.GetProviderAuthMetadata(desc.ID)
		hasCredential := configuration.HasProviderCredential(desc.ID, apiKeys)
		entry := onboardingProvider{
			ID:             desc.ID,
			Name:           desc.Name,
			Models:         desc.Models,
			RequiresAPIKey: meta.RequiresAPIKey,
			HasCredential:  hasCredential,
		}
		providers = append(providers, entry)
		indexByID[entry.ID] = entry
	}

	currentProvider := strings.TrimSpace(cfg.LastUsedProvider)
	if ws.agent != nil {
		if provider := strings.TrimSpace(ws.agent.GetProvider()); provider != "" && provider != "unknown" {
			currentProvider = provider
		}
	}
	currentModel := strings.TrimSpace(cfg.GetModelForProvider(currentProvider))
	if ws.agent != nil {
		if model := strings.TrimSpace(ws.agent.GetModel()); model != "" && model != "unknown" {
			currentModel = model
		}
	}

	setupRequired := false
	reason := ""
	if currentProvider == "" || currentProvider == "test" {
		setupRequired = true
		reason = "provider_not_configured"
	} else if p, ok := indexByID[currentProvider]; ok && p.RequiresAPIKey && !p.HasCredential {
		setupRequired = true
		reason = "missing_provider_credential"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_required":   setupRequired,
		"reason":           reason,
		"current_provider": currentProvider,
		"current_model":    currentModel,
		"providers":        providers,
	})
}

func (ws *ReactWebServer) handleAPIOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.agent == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Agent is not available")
		return
	}

	cm := ws.getConfigManager(w)
	if cm == nil {
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.Provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	providerType, err := cm.MapStringToClientType(req.Provider)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	meta, _ := configuration.GetProviderAuthMetadata(req.Provider)
	hasCredential := configuration.HasProviderCredential(req.Provider, cm.GetAPIKeys())

	if meta.RequiresAPIKey && !hasCredential && req.APIKey == "" {
		writeJSONError(w, http.StatusBadRequest, "api_key is required for this provider")
		return
	}

	if req.APIKey != "" {
		keys := cm.GetAPIKeys()
		keys.SetAPIKey(req.Provider, req.APIKey)
		if err := cm.SaveAPIKeys(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save API key: %v", err))
			return
		}
	}

	if err := ws.agent.SetProvider(providerType); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Model != "" {
		if err := ws.agent.SetModel(req.Model); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	ws.publishProviderState()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "Onboarding completed",
		"provider": ws.agent.GetProvider(),
		"model":    ws.agent.GetModel(),
	})
}
