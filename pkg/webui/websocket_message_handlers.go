//go:build !js

package webui

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func (ws *ReactWebServer) handleProviderChangeMessage(safeConn *SafeConn, data *ProviderChangeData, clientID string) {
	// Use the active chat's agent for provider changes.
	ctx, activeChatID := ws.getActiveChatContext(clientID)

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		// If no provider is configured (editor mode) or the configured model is not available,
		// update the config first so that a new agent can be created with the requested provider.
		if errors.Is(err, ErrNoProviderConfigured) || errors.Is(err, agent.ErrModelNotAvailable) || (err != nil && isProviderConfigError(err)) {
			// Use a layered config manager to update the provider directly.
			cm, createErr := ws.getLayeredConfigManager(clientID)
			if createErr != nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": "Failed to create config manager"},
				})
				return
			}
			providerType, parseErr := cm.MapStringToClientType(data.Provider)
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
				if envelope, ok := configConflictEnvelope(saveErr, cm); ok {
					_ = safeConn.WriteJSON(envelope)
					return
				}
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

	providerType, err := clientAgent.GetConfigManager().MapStringToClientType(data.Provider)
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
		if envelope, ok := configConflictEnvelope(err, clientAgent.GetConfigManager()); ok {
			_ = safeConn.WriteJSON(envelope)
		}
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

	// Emit a warning notice if the newly active provider needs an API
	// key but doesn't have one — without this the change "succeeds"
	// silently and the user only discovers the problem on the next
	// query, when the underlying provider call returns 401.
	ws.notifyMissingCredentialIfNeeded(clientID, activeChatID, data.Provider)
}

func (ws *ReactWebServer) handleModelChangeMessage(safeConn *SafeConn, data *ModelChangeData, clientID string) {
	// Use the active chat's agent for model changes.
	ctx, activeChatID := ws.getActiveChatContext(clientID)

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

	if data.Provider != "" {
		providerType, err := clientAgent.GetConfigManager().MapStringToClientType(data.Provider)
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

	if err := clientAgent.SetModel(data.Model); err != nil {
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
		if envelope, ok := configConflictEnvelope(err, clientAgent.GetConfigManager()); ok {
			_ = safeConn.WriteJSON(envelope)
		}
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
func (ws *ReactWebServer) handlePersonaChangeMessage(safeConn *SafeConn, data *PersonaChangeData, clientID string) {
	// Use the active chat's agent for persona changes.
	ctx, activeChatID := ws.getActiveChatContext(clientID)

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
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

	if err := clientAgent.ApplyPersona(data.Persona); err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

// notifyMissingCredentialIfNeeded publishes a dedicated provider_no_credential
// event if the just-activated provider requires an API key but doesn't
// have one configured. Without this the user only learns about the
// missing credential when their next query fails with a provider-level
// 401 — surfacing it at the moment of change lets them open Settings →
// Credentials and fix it before sending traffic.
//
// The event has its own type (vs the prior agent_message warning) so the
// frontend can render a sticky toast with a "configure credential" action
// instead of inlining the warning into the active assistant bubble (which
// silently drops the notice when no chat is in flight).
//
// Best-effort: any lookup failure is silently skipped (we'd rather miss
// a warning than block a legitimate provider change on transient state).
func (ws *ReactWebServer) notifyMissingCredentialIfNeeded(clientID, chatID, providerID string) {
	if providerID == "" {
		return
	}
	meta, err := configuration.GetProviderAuthMetadata(providerID)
	if err != nil || !meta.RequiresAPIKey {
		return
	}
	if configuration.HasProviderAuth(providerID) {
		return
	}
	msg := fmt.Sprintf(
		"Provider %q requires an API key. Configure it in Settings → Credentials before sending messages.",
		providerID,
	)
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeProviderNoCredential,
		events.ProviderNoCredentialEvent(providerID, msg))
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
