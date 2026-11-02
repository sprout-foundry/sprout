//go:build !js

package webui

import (
	"context"
	"fmt"
	"log/slog"
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

// providerListWait caps how long a provider listing waits on the first live
// lookup of a custom provider (an endpoint the user configured, usually
// local) that has no embedded catalog models to fall back on. Built-in
// providers never wait: the listing serves their catalog models (or none)
// and the live list replaces them once the background lookup lands.
const providerListWait = 1500 * time.Millisecond

// providerModelsPickerWait caps the model picker's wait for one provider's
// first live lookup before it falls back to the catalog.
const providerModelsPickerWait = 5 * time.Second

func (ws *ReactWebServer) handleAPIProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	clientID := ws.resolveClientID(r)
	configManager, agentInst := ws.providerListingSource(clientID)
	providers := ws.listProvidersWith(r.Context(), configManager, agentInst)

	// The session-scoped provider/model of the active chat when it has an
	// agent; otherwise what an agent built now would resolve to. Reading
	// the config rather than building an agent keeps this endpoint off the
	// provider connection check.
	currentProvider := ""
	currentModel := ""
	if chatAgent := ws.peekChatAgent(clientID, ""); chatAgent != nil {
		currentProvider = strings.TrimSpace(chatAgent.GetProvider())
		currentModel = strings.TrimSpace(chatAgent.GetModel())
	} else if configManager != nil {
		if pt, model, err := configManager.ResolveProviderModel("", ""); err == nil {
			currentProvider = strings.TrimSpace(string(pt))
			currentModel = strings.TrimSpace(model)
			if currentModel == "" {
				if custom, ok := configManager.GetConfig().CustomProviders[currentProvider]; ok {
					currentModel = strings.TrimSpace(custom.ModelName)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers":        providers,
		"current_provider": currentProvider,
		"current_model":    currentModel,
	})
}

func (ws *ReactWebServer) listProviders(clientID string) []providerDescriptor {
	return ws.listProvidersCtx(context.Background(), clientID)
}

// listProvidersCtx lists every available provider with its models. Model
// lists come from the process-lifetime provider model cache, so the listing
// never waits on a remote catalog: see providerModelIDs.
func (ws *ReactWebServer) listProvidersCtx(ctx context.Context, clientID string) []providerDescriptor {
	configManager, agentInst := ws.providerListingSource(clientID)
	return ws.listProvidersWith(ctx, configManager, agentInst)
}

// providerListingSource returns the config manager a provider listing reads
// — the client's existing agent's, or a fresh layered (global + workspace)
// manager — plus that agent when there is one. It never builds an agent.
func (ws *ReactWebServer) providerListingSource(clientID string) (*configuration.Manager, *agentpkg.Agent) {
	if agentInst := ws.peekClientAgent(clientID); agentInst != nil && agentInst.GetConfigManager() != nil {
		return agentInst.GetConfigManager(), agentInst
	}
	// Make sure the client context exists so the layered manager includes
	// this client's workspace layer.
	ws.getOrCreateClientContext(clientID)
	cm, err := ws.getLayeredConfigManager(clientID)
	if err != nil {
		return nil, nil
	}
	return cm, nil
}

func (ws *ReactWebServer) listProvidersWith(ctx context.Context, configManager *configuration.Manager, agentInst *agentpkg.Agent) []providerDescriptor {
	if configManager == nil {
		return []providerDescriptor{}
	}

	providerTypes := configManager.GetAvailableProviders()
	customProviders := configManager.GetConfig().CustomProviders

	// Resolve all providers concurrently so the providers that do wait
	// (no cached result, no catalog fallback) share one providerListWait.
	results := make([]providerDescriptor, len(providerTypes))
	var wg sync.WaitGroup
	for i, providerType := range providerTypes {
		wg.Add(1)
		go func(idx int, pt api.ClientType) {
			defer wg.Done()
			custom, isCustom := customProviders[string(pt)]
			var customPtr *configuration.CustomProviderConfig
			if isCustom {
				customPtr = &custom
			}
			results[idx] = providerDescriptor{
				ID:     string(pt),
				Name:   api.GetProviderName(pt),
				Models: ws.providerModelIDs(ctx, pt, configManager, agentInst, customPtr),
			}
		}(i, providerType)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Name == results[j].Name {
			return results[i].ID < results[j].ID
		}
		return results[i].Name < results[j].Name
	})
	return results
}

// providerModelIDs returns the model IDs to list for one provider, in order
// of preference: the configured model alone when the custom provider
// disables model discovery; the cached live list; the embedded catalog; the
// configured model. It waits on a live lookup only for a custom provider
// with no cached result and no catalog models, and then at most
// providerListWait.
func (ws *ReactWebServer) providerModelIDs(ctx context.Context, pt api.ClientType, configManager *configuration.Manager, agentInst *agentpkg.Agent, custom *configuration.CustomProviderConfig) []string {
	if custom != nil && custom.DisableModelDiscovery {
		if model := strings.TrimSpace(custom.ModelName); model != "" {
			return []string{model}
		}
	}

	catalogIDs := catalogModelIDs(pt)
	var wait time.Duration
	if custom != nil && len(catalogIDs) == 0 {
		wait = providerListWait
	}
	models, err, ok := ws.providerModels.lookup(ctx, pt, wait)
	if ok && err == nil {
		if ids := modelInfoIDs(models); len(ids) > 0 {
			return ids
		}
	}

	if len(catalogIDs) > 0 {
		if err != nil {
			// Rate-limited: the listing is polled, and a provider host
			// that stays unreachable would otherwise log on every poll.
			logRateLimitedWarn(
				"model_discovery_catalog_fallback:"+string(pt),
				"using provider catalog fallback after model discovery failure",
				slog.String("provider", string(pt)),
				slog.Any("err", err),
			)
		}
		return catalogIDs
	}

	fallback := ""
	if configManager != nil {
		fallback = strings.TrimSpace(configManager.GetModelForProvider(pt))
	}
	if fallback == "" && agentInst != nil && agentInst.GetProviderType() == pt {
		fallback = strings.TrimSpace(agentInst.GetModel())
	}
	if fallback == "" && custom != nil {
		fallback = strings.TrimSpace(custom.ModelName)
	}
	if fallback == "" {
		return []string{}
	}
	if err != nil {
		logRateLimitedWarn(
			"model_discovery_fail_fallback:"+string(pt),
			"model discovery failed; using configured fallback model",
			slog.String("provider", string(pt)),
			slog.String("fallback_model", fallback),
			slog.Any("err", err),
		)
	}
	return []string{fallback}
}

// modelInfoIDs returns the sorted, non-empty IDs of models.
func modelInfoIDs(models []api.ModelInfo) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// catalogModelIDs returns the sorted model IDs the embedded provider
// catalog lists for pt, or nil.
func catalogModelIDs(pt api.ClientType) []string {
	provider, ok := providercatalog.FindProvider(string(pt))
	if !ok || len(provider.Models) == 0 {
		return nil
	}
	ids := make([]string, 0, len(provider.Models))
	for _, model := range provider.Models {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return ids
}

func (ws *ReactWebServer) publishProviderState(clientID string) {
	if ws.eventBus == nil {
		return
	}

	// Use the active chat's agent so each session reports its own provider/model.
	activeChatID := ""
	workspaceRoot := ""
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
		workspaceRoot = ctx.WorkspaceRoot
	}
	ws.mutex.RUnlock()

	// Fast check: if no provider is configured, skip the expensive
	// getChatAgent call and publish empty provider state immediately.
	// Cost/token keys are dropped for the same reason as the nil-agent
	// path below: zeroed/stale totals flash the status bar.
	if !isProviderAvailableInWorkspace(workspaceRoot) {
		stats := ws.gatherStatsForClientID(clientID)
		stats["provider"] = ""
		stats["model"] = ""
		stats["client_id"] = clientID
		for _, k := range []string{"total_cost", "total_tokens", "prompt_tokens", "completion_tokens", "cached_tokens", "cached_cost_savings"} {
			delete(stats, k)
		}
		ws.eventBus.Publish(events.EventTypeMetricsUpdate, stats)
		return
	}

	agentInst, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || agentInst == nil {
		// If no provider is configured, publish an empty provider state so
		// the frontend can immediately show the degraded UI instead of
		// waiting for the next stats poll. Drop the cost/token keys: a
		// gather with a nil agent can carry a zeroed or stale snapshot
		// total, and merging that would flash the status bar to $0.00
		// until the next real event restores it.
		stats := ws.gatherStatsForClientID(clientID)
		stats["provider"] = ""
		stats["model"] = ""
		stats["client_id"] = clientID
		for _, k := range []string{"total_cost", "total_tokens", "prompt_tokens", "completion_tokens", "cached_tokens", "cached_cost_savings"} {
			delete(stats, k)
		}
		ws.eventBus.Publish(events.EventTypeMetricsUpdate, stats)
		return
	}

	stats := ws.gatherStatsForClientID(clientID)
	stats["provider"] = agentInst.GetProvider()
	stats["model"] = agentInst.GetModel()
	stats["persona"] = agentInst.GetActivePersona()
	agentInst.PublishEvent(events.EventTypeMetricsUpdate, stats)
}

// handleGetModels handles GET /api/providers/models?provider=<provider_type>
// Returns the list of available models for the given provider.
func (ws *ReactWebServer) handleGetModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// The live lookup runs in the provider model cache, detached from this
	// request; the request only waits up to providerModelsPickerWait (less
	// if the client disconnects) before falling back to the catalog.
	ctx := r.Context()

	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		writeJSONErr(w, http.StatusBadRequest, "provider_required", "provider parameter is required")
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
			writeJSONErr(w, http.StatusInternalServerError, "config_manager_failed", "failed to create config manager")
			return
		}
		configManager = cm
	}

	if configManager == nil {
		writeJSONErr(w, http.StatusInternalServerError, "config_manager_unavailable", "config manager not available")
		return
	}

	clientType, err := configManager.MapStringToClientType(provider)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_provider", err.Error())
		return
	}

	if clientType == api.TestClientType {
		writeJSONErr(w, http.StatusBadRequest, "test_provider_blocked", "test provider models cannot be listed via API")
		return
	}

	models, err, ok := ws.providerModels.lookup(ctx, clientType, providerModelsPickerWait)
	if !ok && err == nil {
		err = fmt.Errorf("model discovery for %s is still running", clientType)
	}
	if err != nil {
		// Fall back to the provider catalog if the provider is known. This
		// keeps the model picker modal consistent with the settings dropdown
		// (which uses modelsForProviderFromAPICtx and already falls back).
		if provider, ok := providercatalog.FindProvider(string(clientType)); ok && len(provider.Models) > 0 {
			// Sort a copy so the picker is alphabetical without mutating the
			// shared catalog (FindProvider returns a live slice).
			catalogModels := make([]providercatalog.Model, len(provider.Models))
			copy(catalogModels, provider.Models)
			sort.SliceStable(catalogModels, func(i, j int) bool {
				return catalogModels[i].ID < catalogModels[j].ID
			})
			result := make([]map[string]interface{}, 0, len(catalogModels))
			for _, m := range catalogModels {
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
				logRateLimitedWarn(
					"model_discovery_catalog_fallback_get:"+string(clientType),
					"using provider catalog fallback after model discovery failure",
					slog.String("provider", string(clientType)),
					slog.String("operation", "get_models"),
					slog.Any("err", err),
				)
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"models": result,
					"total":  len(result),
				})
				return
			}
		}
		writeJSONErr(w, http.StatusInternalServerError, "model_list_failed", fmt.Sprintf("failed to list models: %v", err))
		return
	}

	// Sort a copy so the picker lists models alphabetically without mutating
	// the slice returned by GetModelsForProviderCtx (other consumers read it in
	// provider order for default-model selection).
	displayModels := make([]api.ModelInfo, len(models))
	copy(displayModels, models)
	sort.SliceStable(displayModels, func(i, j int) bool {
		return displayModels[i].ID < displayModels[j].ID
	})

	result := make([]map[string]interface{}, 0, len(displayModels))
	for _, m := range displayModels {
		result = append(result, map[string]interface{}{
			"id":                m.ID,
			"name":              m.Name,
			"context_length":    m.ContextLength,
			"eligible_roles":    m.EligibleRoles,
			"recommended_roles": m.RecommendedRoles,
			"warnings":          m.Warnings,
			"tags":              m.Tags,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": result,
		"total":  len(result),
	})
}
