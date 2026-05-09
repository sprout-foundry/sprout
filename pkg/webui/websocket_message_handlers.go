package webui

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func (ws *ReactWebServer) handleProviderChangeMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Use the active chat's agent for provider changes.
	activeChatID := ""
	ws.mutex.RLock()
	var ctx *webClientContext
	if ctx = ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		// If no provider is configured (editor mode) or the configured model is not available,
		// update the config first so that a new agent can be created with the requested provider.
		if errors.Is(err, ErrNoProviderConfigured) || errors.Is(err, agent.ErrModelNotAvailable) || (err != nil && isProviderConfigError(err)) {
			data, ok := msg["data"].(map[string]interface{})
			if !ok {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": "Invalid provider change payload"},
				})
				return
			}
			providerName, _ := data["provider"].(string)
			providerName = strings.TrimSpace(providerName)
			if providerName == "" {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": "Provider is required"},
				})
				return
			}

			// Use a layered config manager to update the provider directly.
			cm, createErr := ws.getLayeredConfigManager(clientID)
			if createErr != nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": "Failed to create config manager"},
				})
				return
			}
			providerType, parseErr := cm.MapStringToClientType(providerName)
			if parseErr != nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": parseErr.Error()},
				})
				return
			}

			// Persist the provider to config and clear cached agent.
			// Skip the mock test provider — it should never be saved as last_used.
			if providerType == api.TestClientType {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": "test provider cannot be used as the active provider"},
				})
				return
			}
			if setErr := cm.SetProvider(providerType); setErr != nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": fmt.Sprintf("Failed to set provider: %v", setErr)},
				})
				return
			}
			if saveErr := cm.SaveConfig(); saveErr != nil {
				log.Printf("webui: failed to save provider change config: %v", saveErr)
			}
			ws.clearCachedAgent(clientID)
			clientAgent, err = ws.getChatAgent(clientID, activeChatID)
			if err != nil || clientAgent == nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": "Agent is not available after provider update"},
				})
				return
			}
		} else {
			_ = safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": "Agent is not available"},
			})
			return
		}
	}

	if clientAgent.GetConfigManager() == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent config is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid provider change payload"},
		})
		return
	}

	providerName, _ := data["provider"].(string)
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Provider is required"},
		})
		return
	}

	providerType, err := clientAgent.GetConfigManager().MapStringToClientType(providerName)
	if err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	// Check active query for the active chat, not the global client
	if ctx != nil && activeChatID != "" && ctx.hasActiveQueryForChat(activeChatID) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Cannot change provider while this chat has an active run"},
		})
		return
	}

	if err := clientAgent.SetProvider(providerType); err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	// Attempt to persist the provider/model to config as the last used.
	// This fails gracefully — we log the error but don't fail the operation
	// since the session-scoped provider/model is already set in the chat session.
	// Skip the test provider — it's not a real API provider.
	if providerType == api.TestClientType {
		log.Printf("webui: skipping persistence for test provider")
	} else if err := persistProviderModelToConfig(clientAgent, providerType); err != nil {
		log.Printf("webui: failed to persist provider/model to config: %v", err)
	}

	// Store provider on the chat session for per-session tracking.
	ws.mutex.RLock()
	if ctx != nil && activeChatID != "" {
		if cs := ctx.getChatSession(activeChatID); cs != nil {
			cs.mu.Lock()
			cs.Provider = api.GetProviderName(clientAgent.GetProviderType())
			cs.Model = clientAgent.GetModel()
			cs.mu.Unlock()
		}
	}
	ws.mutex.RUnlock()

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

func (ws *ReactWebServer) handleModelChangeMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Use the active chat's agent for model changes.
	activeChatID := ""
	ws.mutex.RLock()
	var ctx *webClientContext
	if ctx = ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		// Return a specific error for model-not-found so the web UI can show model selection
		if errors.Is(err, agent.ErrModelNotAvailable) {
			_ = safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{
					"message": "Configured model is not available",
					"code":    "model_not_available",
				},
			})
			return
		}
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid model change payload"},
		})
		return
	}

	modelName, _ := data["model"].(string)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Model is required"},
		})
		return
	}

	// Check active query for the active chat, not the global client
	if ctx != nil && activeChatID != "" && ctx.hasActiveQueryForChat(activeChatID) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Cannot change model while this chat has an active run"},
		})
		return
	}

	previousProvider := clientAgent.GetProviderType()
	previousModel := clientAgent.GetModel()
	providerChanged := false

	if providerName, _ := data["provider"].(string); strings.TrimSpace(providerName) != "" {
		providerType, err := clientAgent.GetConfigManager().MapStringToClientType(providerName)
		if err == nil && providerType != clientAgent.GetProviderType() {
			if err := clientAgent.SetProvider(providerType); err != nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": err.Error()},
				})
				return
			}
			providerChanged = true
		}
	}

	if err := clientAgent.SetModel(modelName); err != nil {
		if providerChanged && previousProvider != "" {
			if rollbackErr := clientAgent.SetProvider(previousProvider); rollbackErr != nil {
				log.Printf("webui: failed to rollback provider change after model switch failure: provider=%s model=%s rollback_err=%v", previousProvider, previousModel, rollbackErr)
			} else if strings.TrimSpace(previousModel) != "" {
				if rollbackModelErr := clientAgent.SetModel(previousModel); rollbackModelErr != nil {
					log.Printf("webui: provider rollback succeeded but failed to restore model %q: %v", previousModel, rollbackModelErr)
				}
			}
		}
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	// Attempt to persist the provider/model to config as the last used.
	// This fails gracefully — we log the error but don't fail the operation
	// since the session-scoped provider/model is already set in the chat session.
	//
	// Skip persistence for the test/mock provider — it's not a real API endpoint
	// and should never survive an app restart.
	if clientAgent.GetProviderType() == api.TestClientType {
		log.Printf("webui: skipping persistence for test provider")
	} else if err := persistProviderModelToConfig(clientAgent, clientAgent.GetProviderType()); err != nil {
		log.Printf("webui: failed to persist provider/model to config: %v", err)
	}

	// Store model on the chat session for per-session tracking.
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && activeChatID != "" {
		if cs := ctx.getChatSession(activeChatID); cs != nil {
			cs.mu.Lock()
			cs.Provider = api.GetProviderName(clientAgent.GetProviderType())
			cs.Model = clientAgent.GetModel()
			cs.mu.Unlock()
		}
	}
	ws.mutex.RUnlock()

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

// handlePersonaChangeMessage handles persona change requests from the webui.
func (ws *ReactWebServer) handlePersonaChangeMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Use the active chat's agent for persona changes.
	activeChatID := ""
	ws.mutex.RLock()
	var ctx *webClientContext
	if ctx = ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid persona change payload"},
		})
		return
	}

	personaID, _ := data["persona"].(string)
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Persona is required"},
		})
		return
	}

	// Check active query for the active chat, not the global client
	if ctx != nil && activeChatID != "" && ctx.hasActiveQueryForChat(activeChatID) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Cannot change persona while this chat has an active run"},
		})
		return
	}

	if err := clientAgent.ApplyPersona(personaID); err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

// persistProviderModelToConfig attempts to persist the provider and model to the
// global config file. This gracefully fails on error — the session is already updated,
// but the preference won't survive a full app restart.
//
// Returns nil if successful, or the error from SaveConfig() if it fails.
func persistProviderModelToConfig(chatAgent *agent.Agent, provider api.ClientType) error {
	configManager := chatAgent.GetConfigManager()
	if configManager == nil {
		return fmt.Errorf("config manager not available")
	}

	// Update the in-memory config
	configManager.SetProvider(provider)
	model := chatAgent.GetModel()
	if model != "" {
		configManager.SetModelForProvider(provider, model)
	}

	// Attempt to persist — this may fail due to file permissions, read-only config, etc.
	// The caller logs the error but doesn't fail the operation.
	return configManager.SaveConfig()
}
