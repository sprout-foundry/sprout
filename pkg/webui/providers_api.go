package webui

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
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

	providers := ws.listProviders()
	currentProvider := ""
	currentModel := ""
	if ws.agent != nil {
		currentProvider = strings.TrimSpace(ws.agent.GetProvider())
		currentModel = strings.TrimSpace(ws.agent.GetModel())
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"providers":        providers,
		"current_provider": currentProvider,
		"current_model":    currentModel,
	})
}

func (ws *ReactWebServer) listProviders() []providerDescriptor {
	if ws.agent == nil || ws.agent.GetConfigManager() == nil {
		return []providerDescriptor{}
	}

	configManager := ws.agent.GetConfigManager()
	providerTypes := configManager.GetAvailableProviders()
	descriptors := make([]providerDescriptor, 0, len(providerTypes))

	for _, providerType := range providerTypes {
		providerID := string(providerType)
		models := ws.modelsForProvider(providerType)
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

func (ws *ReactWebServer) modelsForProvider(providerType api.ClientType) []string {
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

	if ws.agent == nil || ws.agent.GetConfigManager() == nil {
		return []string{}
	}

	fallback := strings.TrimSpace(ws.agent.GetConfigManager().GetModelForProvider(providerType))
	if fallback == "" && ws.agent.GetProviderType() == providerType {
		fallback = strings.TrimSpace(ws.agent.GetModel())
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

func (ws *ReactWebServer) publishProviderState() {
	if ws.agent == nil || ws.eventBus == nil {
		return
	}

	stats := ws.gatherStats()
	stats["provider"] = ws.agent.GetProvider()
	stats["model"] = ws.agent.GetModel()
	ws.eventBus.Publish(events.EventTypeMetricsUpdate, stats)
}
