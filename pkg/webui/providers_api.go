//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	agentpkg "github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

type providerDescriptor struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

func (ws *ReactWebServer) handleAPIProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	providers := ws.listProviders(clientID)
	currentProvider := ""
	currentModel := ""

	// Resolve the active chat ID to get the session-scoped provider/model.
	activeChatID := ""
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	if agentInst, err := ws.getChatAgent(clientID, activeChatID); err == nil && agentInst != nil {
		currentProvider = strings.TrimSpace(agentInst.GetProvider())
		currentModel = strings.TrimSpace(agentInst.GetModel())
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"providers":        providers,
		"current_provider": currentProvider,
		"current_model":    currentModel,
	})
}

func (ws *ReactWebServer) listProviders(clientID string) []providerDescriptor {
	var configManager *configuration.Manager

	agentInst, err := ws.getClientAgent(clientID)
	if err == nil && agentInst != nil && agentInst.GetConfigManager() != nil {
		configManager = agentInst.GetConfigManager()
	} else {
		// Fall back to a layered config manager for any error or missing
		// config manager — covers no-provider, provider-config errors, agent
		// creation failures, and the nil-configManager case (err == nil).
		cm, createErr := ws.getLayeredConfigManager(clientID)
		if createErr != nil {
			return []providerDescriptor{}
		}
		configManager = cm
		agentInst = nil
	}

	if configManager == nil {
		return []providerDescriptor{}
	}

	providerTypes := configManager.GetAvailableProviders()
	descriptors := make([]providerDescriptor, 0, len(providerTypes))

	for _, providerType := range providerTypes {
		providerID := string(providerType)
		var models []string
		if agentInst != nil {
			models = ws.modelsForProvider(providerType, agentInst)
		} else {
			// No agent available - try to get models from the API or catalog
			models = ws.modelsForProviderNoAgent(providerType)
		}
		descriptors = append(descriptors, providerDescriptor{
			ID:     providerID,
			Name:   api.GetProviderName(providerType),
			Models: models,
		})
	}

	sort.SliceStable(descriptors, func(i, j int) bool {
		if descriptors[i].Name == descriptors[j].Name {
			return descriptors[i].ID < descriptors[j].ID
		}
		return descriptors[i].Name < descriptors[j].Name
	})

	return descriptors
}

// modelsForProviderFromAPI looks up models from the provider API and the
// embedded provider catalog. Returns nil when no models are found.
func modelsForProviderFromAPI(providerType api.ClientType) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := api.GetModelsForProviderCtx(ctx, providerType)
	if err == nil && len(models) > 0 {
		modelIDs := make([]string, 0, len(models))
		for _, model := range models {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				continue
			}
			modelIDs = append(modelIDs, id)
		}
		if len(modelIDs) > 0 {
			return modelIDs
		}
	}

	if provider, ok := providercatalog.FindProvider(string(providerType)); ok && len(provider.Models) > 0 {
		modelIDs := make([]string, 0, len(provider.Models))
		for _, model := range provider.Models {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				continue
			}
			modelIDs = append(modelIDs, id)
		}
		if len(modelIDs) > 0 {
			if err != nil {
				// Repeated discovery failures for the same provider get
				// rate-limited to avoid log spam — e.g. a misconfigured
				// ollama-local pointing at an unreachable host that the
				// WebUI polls periodically.
				logRateLimitedf(
					"model_discovery_catalog_fallback:"+string(providerType),
					"webui: using provider catalog fallback for %s after model discovery failure: %v",
					providerType, err,
				)
			}
			return modelIDs
		}
	}

	return nil
}

func (ws *ReactWebServer) modelsForProvider(providerType api.ClientType, agentInst *agentpkg.Agent) []string {
	if models := modelsForProviderFromAPI(providerType); len(models) > 0 {
		return models
	}

	// Agent-specific fallback when no API/catalog models are available.
	if agentInst == nil || agentInst.GetConfigManager() == nil {
		return []string{}
	}

	fallback := strings.TrimSpace(agentInst.GetConfigManager().GetModelForProvider(providerType))
	if fallback == "" && agentInst.GetProviderType() == providerType {
		fallback = strings.TrimSpace(agentInst.GetModel())
	}
	if fallback == "" {
		// Log only if the API/catalog lookup also failed with an error
		// (modelsForProviderFromAPI handles its own catalog-fallback log).
		return []string{}
	}

	_, err := api.GetModelsForProvider(providerType)
	if err != nil {
		// Rate-limited so a misconfigured-but-repeatedly-polled provider
		// (e.g. ollama-local pointed at an unreachable host) logs once,
		// stays quiet through routine polling, and re-surfaces every
		// logRateMinInterval if the failure is still happening.
		logRateLimitedf(
			"model_discovery_fail_fallback:"+string(providerType),
			"webui: model discovery failed for provider %s, using configured fallback model %q: %v",
			providerType, fallback, err,
		)
	}
	return []string{fallback}
}

// modelsForProviderNoAgent is like modelsForProvider but doesn't require an
// agent instance. Used during onboarding when no provider is configured yet.
func (ws *ReactWebServer) modelsForProviderNoAgent(providerType api.ClientType) []string {
	if models := modelsForProviderFromAPI(providerType); len(models) > 0 {
		return models
	}

	if _, err := api.GetModelsForProvider(providerType); err != nil {
		logRateLimitedf(
			"model_discovery_fail:"+string(providerType),
			"webui: model discovery failed for provider %s: %v",
			providerType, err,
		)
	}
	return []string{}
}

func (ws *ReactWebServer) publishProviderState(clientID string) {
	if ws.eventBus == nil {
		return
	}

	// Use the active chat's agent so each session reports its own provider/model.
	activeChatID := ""
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	// Fast check: if no provider is configured, skip the expensive
	// getChatAgent call and publish empty provider state immediately.
	if !isProviderAvailable() {
		stats := ws.gatherStatsForClientID(clientID)
		stats["provider"] = ""
		stats["model"] = ""
		stats["client_id"] = clientID
		ws.eventBus.Publish(events.EventTypeMetricsUpdate, stats)
		return
	}

	agentInst, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || agentInst == nil {
		// If no provider is configured, publish an empty provider state so
		// the frontend can immediately show the degraded UI instead of
		// waiting for the next stats poll.
		stats := ws.gatherStatsForClientID(clientID)
		stats["provider"] = ""
		stats["model"] = ""
		stats["client_id"] = clientID
		ws.eventBus.Publish(events.EventTypeMetricsUpdate, stats)
		return
	}

	stats := ws.gatherStatsForClientID(clientID)
	stats["provider"] = agentInst.GetProvider()
	stats["model"] = agentInst.GetModel()
	stats["persona"] = agentInst.GetActivePersona()
	stats["client_id"] = clientID
	ws.eventBus.Publish(events.EventTypeMetricsUpdate, stats)
}

// handleGetModels handles GET /api/providers/models?provider=<provider_type>
// Returns the list of available models for the given provider.
func (ws *ReactWebServer) handleGetModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider parameter is required")
		return
	}

	// Get config manager to map provider name to type
	var configManager *configuration.Manager
	agentInst, err := ws.getClientAgent(ws.resolveClientID(r))
	if err == nil && agentInst != nil && agentInst.GetConfigManager() != nil {
		configManager = agentInst.GetConfigManager()
	} else {
		cm, createErr := ws.getLayeredConfigManager(ws.resolveClientID(r))
		if createErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to create config manager")
			return
		}
		configManager = cm
	}

	if configManager == nil {
		writeJSONError(w, http.StatusInternalServerError, "config manager not available")
		return
	}

	clientType, err := configManager.MapStringToClientType(provider)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	models, err := api.GetModelsForProvider(clientType)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list models: %v", err))
		return
	}

	result := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		result = append(result, map[string]interface{}{
			"id":                m.ID,
			"name":              m.Name,
			"context_length":    m.ContextLength,
			"eligible_roles":    m.EligibleRoles,
			"recommended_roles": m.RecommendedRoles,
			"warnings":          m.Warnings,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": result,
		"total":  len(result),
	})
}

