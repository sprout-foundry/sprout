package webui

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"

	agentpkg "github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/providercatalog"
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
	} else if errors.Is(err, ErrNoProviderConfigured) {
		// If no provider is configured, create a config manager directly
		// so we can still list available providers during onboarding.
		cm, createErr := configuration.NewManagerSilent()
		if createErr != nil {
			return []providerDescriptor{}
		}
		configManager = cm
		agentInst = nil
	} else {
		return []providerDescriptor{}
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

func (ws *ReactWebServer) modelsForProvider(providerType api.ClientType, agentInst *agentpkg.Agent) []string {
	models, err := api.GetModelsForProvider(providerType)
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
				log.Printf("webui: using provider catalog fallback for %s after model discovery failure: %v", providerType, err)
			}
			return modelIDs
		}
	}

	if agentInst == nil || agentInst.GetConfigManager() == nil {
		return []string{}
	}

	fallback := strings.TrimSpace(agentInst.GetConfigManager().GetModelForProvider(providerType))
	if fallback == "" && agentInst.GetProviderType() == providerType {
		fallback = strings.TrimSpace(agentInst.GetModel())
	}
	if fallback == "" {
		if err != nil {
			log.Printf("webui: model discovery failed for provider %s and no fallback model is configured: %v", providerType, err)
		}
		return []string{}
	}
	if err != nil {
		log.Printf("webui: model discovery failed for provider %s, using configured fallback model %q: %v", providerType, fallback, err)
	}
	return []string{fallback}
}

// modelsForProviderNoAgent is like modelsForProvider but doesn't require an
// agent instance. Used during onboarding when no provider is configured yet.
func (ws *ReactWebServer) modelsForProviderNoAgent(providerType api.ClientType) []string {
	models, err := api.GetModelsForProvider(providerType)
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
				log.Printf("webui: using provider catalog fallback for %s after model discovery failure: %v", providerType, err)
			}
			return modelIDs
		}
	}

	if err != nil {
		log.Printf("webui: model discovery failed for provider %s: %v", providerType, err)
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
