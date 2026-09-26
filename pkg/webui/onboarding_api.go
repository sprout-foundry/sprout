//go:build !js

package webui

// onboarding_api.go — the webui onboarding API handlers: the
// handleAPIOnboardingStatus / handleAPIOnboardingComplete /
// handleAPIOnboardingSkip handlers. The provider / environment /
// presentation types and the presentation table live in
// onboarding_presentation.go; the environment / WSL / model detection
// helpers live in onboarding_detect.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agentprovs "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

func (ws *ReactWebServer) handleAPIOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	// Derive a context from the request so model discovery is cancelled if
	// the client disconnects. Matches handleAPIProviders' timeout.
	listCtx, listCancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer listCancel()
	descriptors := ws.listProvidersCtx(listCtx, ws.resolveClientID(r))
	providers := make([]onboardingProvider, 0, len(descriptors))
	indexByID := make(map[string]onboardingProvider, len(descriptors))

	for _, desc := range descriptors {
		meta, _ := configuration.GetProviderAuthMetadata(desc.ID)
		hasCredential := configuration.HasProviderAuth(desc.ID)
		entry := onboardingProvider{
			ID:             desc.ID,
			Name:           desc.Name,
			Models:         desc.Models,
			RequiresAPIKey: meta.RequiresAPIKey,
			HasCredential:  hasCredential,
		}
		entry = applyOnboardingPresentation(entry)
		providers = append(providers, entry)
		indexByID[entry.ID] = entry
	}

	sort.SliceStable(providers, func(i, j int) bool {
		leftOrder, leftHasOrder := onboardingProviderOrder[providers[i].ID]
		rightOrder, rightHasOrder := onboardingProviderOrder[providers[j].ID]
		switch {
		case leftHasOrder && rightHasOrder:
			return leftOrder < rightOrder
		case leftHasOrder:
			return true
		case rightHasOrder:
			return false
		case providers[i].Recommended != providers[j].Recommended:
			return providers[i].Recommended
		case providers[i].Name == providers[j].Name:
			return providers[i].ID < providers[j].ID
		default:
			return providers[i].Name < providers[j].Name
		}
	})

	currentProvider := strings.TrimSpace(cfg.LastUsedProvider)
	if clientAgent, err := ws.getClientAgent(ws.resolveClientID(r)); err == nil && clientAgent != nil {
		if provider := strings.TrimSpace(clientAgent.GetProvider()); provider != "" && provider != "unknown" && provider != "test" {
			currentProvider = provider
		}
	}
	if currentProvider == "" || currentProvider == "test" {
		for _, provider := range providers {
			if provider.ID == "test" {
				continue
			}
			if !provider.RequiresAPIKey || provider.HasCredential {
				currentProvider = provider.ID
				break
			}
		}
	}
	currentModel := strings.TrimSpace(cfg.GetModelForProvider(currentProvider))
	if clientAgent, err := ws.getClientAgent(ws.resolveClientID(r)); err == nil && clientAgent != nil {
		if model := strings.TrimSpace(clientAgent.GetModel()); model != "" && model != "unknown" {
			currentModel = model
		}
	}
	if currentModel == "" {
		if p, ok := indexByID[currentProvider]; ok {
			if strings.TrimSpace(p.RecommendedModel) != "" {
				currentModel = strings.TrimSpace(p.RecommendedModel)
			} else if len(p.Models) > 0 {
				currentModel = strings.TrimSpace(p.Models[0])
			}
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
		"environment":      detectOnboardingEnvironment(),
	})
}

func (ws *ReactWebServer) handleAPIOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	clientID := ws.resolveClientID(r)

	cm := ws.getConfigManager(r, w)
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
	hasCredential := configuration.HasProviderAuth(req.Provider)

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

	// Reject test provider - it cannot be used as the active provider
	if providerType == api.TestClientType {
		writeJSONError(w, http.StatusBadRequest, "test provider cannot be used as the active provider")
		return
	}

	// Determine the model to persist. If req.Model is empty, use the provider's
	// default model. First check the provider catalog, then fall back to the
	// provider factory for custom/local providers.
	modelToPersist := req.Model
	if modelToPersist == "" {
		// Try the provider catalog first
		if provider, ok := providercatalog.FindProvider(req.Provider); ok {
			if provider.DefaultModel != "" {
				modelToPersist = provider.DefaultModel
			} else if len(provider.Models) > 0 {
				modelToPersist = provider.Models[0].ID
			}
		}

		// If still empty, try the provider factory for custom providers
		if modelToPersist == "" {
			factory := agentprovs.NewProviderFactory()
			if err := factory.LoadEmbeddedConfigs(); err == nil {
				if providerConfig, err := factory.GetProviderConfig(req.Provider); err == nil {
					if providerConfig.Models.DefaultModel != "" {
						modelToPersist = providerConfig.Models.DefaultModel
					} else if providerConfig.Defaults.Model != "" {
						modelToPersist = providerConfig.Defaults.Model
					}
				}
			}
		}
	}

	// Persist provider and model to config BEFORE agent creation so the
	// choice survives even if agent setup fails or times out.
	if setErr := cm.SetProvider(providerType); setErr != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to persist provider: %v", setErr))
		return
	}
	if modelToPersist != "" {
		if err := cm.SetModelForProvider(providerType, modelToPersist); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to persist model: %v", err))
			return
		}
	}
	if saveErr := cm.SaveConfig(); saveErr != nil {
		ws.log().Warn("failed to save onboarding config", slog.Any("err", saveErr))
	}

	// Clear any cached agent so it is re-created with the updated config
	// (real provider instead of "editor").
	ws.clearCachedAgent(clientID)

	// Pre-load the local model in-process when sprout-local is selected
	// so the first chat request is fast. On Apple Silicon this loads the
	// model directly via MLX — no HTTP server, no separate process.
	if req.Provider == "sprout-local" {
		if err := localmodel.EnsureServerForProviderWithCheck(r.Context(), "sprout-local"); err != nil {
			ws.log().Info("local model pre-load deferred (will lazy-load on first request)", "error", err)
		} else {
			ws.log().Info("local model loaded in-process")
		}
	}

	// Now create/get the agent with the newly configured provider.
	clientAgent, err := ws.getClientAgent(clientID)
	if err != nil || clientAgent == nil {
		writeJSONError(w, http.StatusServiceUnavailable, fmt.Sprintf("Agent is not available after configuration: %v", err))
		return
	}

	if err := clientAgent.SetProvider(providerType); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Model != "" {
		if err := clientAgent.SetModel(req.Model); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Re-persist the actual model (may differ from requested due to resolution)
		if actualModel := clientAgent.GetModel(); actualModel != "" {
			if persistErr := cm.SetModelForProvider(providerType, actualModel); persistErr != nil {
				ws.log().Warn("failed to re-persist resolved model", slog.String("model", actualModel), slog.Any("err", persistErr))
			}
			_ = cm.SaveConfig()
		}
	}

	_ = ws.syncAgentStateForClient(clientID)
	ws.publishProviderState(clientID)

	// The catalog default persisted earlier can diverge from the model the
	// agent actually resolved (e.g. ram-tiered local-model catalog on hosts
	// where the tier-0 default isn't runnable). Re-align persisted config
	// with the live agent so a restart doesn't silently change models.
	if req.Model == "" {
		if actualModel := clientAgent.GetModel(); actualModel != "" && actualModel != modelToPersist {
			if agentCM := clientAgent.GetConfigManager(); agentCM != nil {
				if persistErr := agentCM.SetModelForProvider(providerType, actualModel); persistErr != nil {
					ws.log().Warn("failed to re-persist resolved default model", slog.String("model", actualModel), slog.Any("err", persistErr))
				} else {
					_ = agentCM.SaveConfig()
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "Onboarding completed",
		"provider": clientAgent.GetProvider(),
		"model":    clientAgent.GetModel(),
	})
}

func (ws *ReactWebServer) handleAPIOnboardingSkip(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	clientID := ws.resolveClientID(r)

	// "Skip — use as editor" is a dismissal of the setup dialog, not a
	// durable configuration choice. Persisting "editor" to the GLOBAL
	// config gates agent creation for every workspace (see
	// isProviderAvailableInWorkspace), including workspaces whose own
	// config fully configures a provider — a workspace that never asked
	// to be editor-only. So:
	//
	//   • Workspace scope available → persist "editor" to the workspace
	//     layer only (sticky for this project, invisible elsewhere).
	//   • No workspace scope (daemon at $HOME before workspace selection)
	//     → session-only dismissal, persist nothing.
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if configuration.WorkspaceConfigDir(workspaceRoot) == "" {
		ws.log().Info("onboarding skipped without a workspace scope — editor mode not persisted",
			slog.String("client_id", clientID),
		)
		_ = ws.syncAgentStateForClient(clientID)
		ws.publishProviderState(clientID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"provider": "editor",
			"model":    "",
		})
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// The manager resolved here is layered (global + workspace); UpdateConfig
	// saves to the workspace layer, which is the intended scope.
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "editor"
		return nil
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to skip onboarding: %v", err))
		return
	}

	// Sync state and notify the client so the frontend picks up the
	// provider change without requiring a full status poll.
	_ = ws.syncAgentStateForClient(clientID)
	ws.publishProviderState(clientID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": "editor",
		"model":    "",
	})
}
