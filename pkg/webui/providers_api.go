//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
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
	// Derive a context from the request so model discovery is cancelled if
	// the client disconnects. Add an overall cap so the parallel fetches
	// can't run indefinitely even if the request context never fires.
	listCtx, listCancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer listCancel()
	providers := ws.listProvidersCtx(listCtx, clientID)
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
	return ws.listProvidersCtx(context.Background(), clientID)
}

// listProvidersCtx is the context-aware variant. When derived from an HTTP
// request, model discovery is cancelled when the client disconnects so the
// server doesn't keep making N provider API calls for a closed browser tab.
// It also fetches models for all providers concurrently instead of
// sequentially, turning a 12×5s worst case into a single 5s timeout.
func (ws *ReactWebServer) listProvidersCtx(ctx context.Context, clientID string) []providerDescriptor {
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

	// Fetch models for all providers concurrently. Each provider gets its
	// own sub-context with a per-provider timeout, so one slow provider
	// can't block the entire response. The parent context (derived from
	// the HTTP request when available) cancels all fetches if the client
	// disconnects.
	type providerResult struct {
		index int
		desc  providerDescriptor
	}

	results := make([]providerDescriptor, len(providerTypes))
	var wg sync.WaitGroup

	for i, providerType := range providerTypes {
		wg.Add(1)
		go func(idx int, pt api.ClientType) {
			defer wg.Done()
			var models []string
			if agentInst != nil {
				models = ws.modelsForProviderCtx(ctx, pt, agentInst)
			} else {
				models = ws.modelsForProviderNoAgentCtx(ctx, pt)
			}
			results[idx] = providerDescriptor{
				ID:     string(pt),
				Name:   api.GetProviderName(pt),
				Models: models,
			}
		}(i, providerType)
	}
	wg.Wait()

	// Filter out zero-value entries (shouldn't happen since we pre-sized,
	// but the results slice was zero-initialized and we wrote to each index).
	descriptors := results[:]

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
	return modelsForProviderFromAPICtx(ctx, providerType)
}

// modelsForProviderFromAPICtx is the context-aware variant. The caller
// controls the timeout/cancellation — pass a context derived from the HTTP
// request so in-flight API calls are cancelled when the client disconnects.
func modelsForProviderFromAPICtx(ctx context.Context, providerType api.ClientType) []string {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ws.modelsForProviderCtx(ctx, providerType, agentInst)
}

// modelsForProviderCtx is the context-aware variant. Derives the model-list
// timeout from the parent context so a client disconnect cancels the fetch.
func (ws *ReactWebServer) modelsForProviderCtx(ctx context.Context, providerType api.ClientType, agentInst *agentpkg.Agent) []string {
	if models := modelsForProviderFromAPICtx(ctx, providerType); len(models) > 0 {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ws.modelsForProviderNoAgentCtx(ctx, providerType)
}

// modelsForProviderNoAgentCtx is the context-aware variant.
func (ws *ReactWebServer) modelsForProviderNoAgentCtx(ctx context.Context, providerType api.ClientType) []string {
	if models := modelsForProviderFromAPICtx(ctx, providerType); len(models) > 0 {
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

	// Derive a context from the request so model discovery is cancelled if
	// the client disconnects, and cap it so the upstream API call can't run
	// indefinitely. Mirrors handleAPIProviders' timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

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

	if clientType == api.TestClientType {
		writeJSONError(w, http.StatusBadRequest, "test provider models cannot be listed via API")
		return
	}

	models, err := api.GetModelsForProviderCtx(ctx, clientType)
	if err != nil {
		// Fall back to the provider catalog if the provider is known. This
		// keeps the model picker modal consistent with the settings dropdown
		// (which uses modelsForProviderFromAPICtx and already falls back).
		if provider, ok := providercatalog.FindProvider(string(clientType)); ok && len(provider.Models) > 0 {
			result := make([]map[string]interface{}, 0, len(provider.Models))
			for _, m := range provider.Models {
				id := strings.TrimSpace(m.ID)
				if id == "" {
					continue
				}
				result = append(result, map[string]interface{}{
					"id":                id,
					"name":              m.Name,
					"context_length":    m.ContextLength,
					"eligible_roles":    nil,
					"recommended_roles": nil,
					"warnings":          nil,
				})
			}
			if len(result) > 0 {
				logRateLimitedf(
					"model_discovery_catalog_fallback_get:"+string(clientType),
					"webui: using provider catalog fallback for %s in handleGetModels after model discovery failure: %v",
					clientType, err,
				)
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"models": result,
					"total":  len(result),
				})
				return
			}
		}
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
