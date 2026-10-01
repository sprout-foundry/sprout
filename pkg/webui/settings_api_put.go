//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	agentpkg "github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func (ws *ReactWebServer) handleAPISettingsPut(w http.ResponseWriter, r *http.Request) {
	// Check for explicit layer parameter
	layer := strings.TrimSpace(r.URL.Query().Get("layer"))
	switch layer {
	case "session":
		ws.handlePutSessionSettings(w, r)
		return
	case "workspace":
		ws.handlePutWorkspaceSettings(w, r)
		return
	case "global":
		ws.handlePutGlobalSettings(w, r)
		return
	}

	// Default (no layer): current backward-compatible behavior
	ws.handleAPISettingsPutDefault(w, r)
}

// handleAPISettingsPutDefault is the original PUT behavior:
// provider/model → session overrides, everything else → config manager.
func (ws *ReactWebServer) handleAPISettingsPutDefault(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var incoming map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Check for provider and model at the top level - these need special handling.
	// Provider/model changes are session-scoped: stored in chatSession.ConfigOverrides
	// and applied to the live agent in-memory, NOT persisted to config file.
	var newProvider string
	var newModel string
	if v, ok := incoming["provider"]; ok {
		newProvider, _ = v.(string)
		delete(incoming, "provider")
	}
	if v, ok := incoming["model"]; ok {
		newModel, _ = v.(string)
		delete(incoming, "model")
	}

	// Handle provider/model changes as session-scoped overrides
	clientID := ws.resolveClientID(r)
	if newProvider != "" || newModel != "" {
		// Validate provider if specified
		if newProvider != "" {
			providerType, err := cm.MapStringToClientType(newProvider)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid provider: %v", err))
				return
			}
			if providerType == api.TestClientType {
				writeJSONError(w, http.StatusBadRequest, "test provider cannot be set via API")
				return
			}
		}

		// Store overrides in the session's ConfigOverrides map
		ws.mutex.Lock()
		ctx := ws.clientContexts[clientID]
		activeChatID := ""
		if ctx != nil {
			activeChatID = ctx.getActiveChatID()
			// Reject provider/model changes while the active chat has a query
			// in flight — SetProvider swaps a.client without synchronization,
			// and swapping mid-query corrupts the in-flight LLM call.
			if ctx.hasActiveQueryForChat(activeChatID) {
				ws.mutex.Unlock()
				writeJSONError(w, http.StatusConflict, "Cannot change provider/model while this chat has an active run")
				return
			}
			if cs := ctx.getChatSession(activeChatID); cs != nil {
				cs.mu.Lock()
				if cs.ConfigOverrides == nil {
					cs.ConfigOverrides = make(map[string]interface{})
				}
				if newProvider != "" {
					cs.ConfigOverrides["provider"] = newProvider
					cs.Provider = newProvider
				}
				if newModel != "" {
					cs.ConfigOverrides["model"] = newModel
					cs.Model = newModel
				}
				cs.mu.Unlock()
			}
		}
		ws.mutex.Unlock()

		// Apply to the live chat agent (not the client-level agent) so the
		// override reaches the correct per-chat agent instance.
		if agentInst, err := ws.getChatAgent(clientID, activeChatID); err == nil && agentInst != nil {
			if newProvider != "" {
				providerType, _ := cm.MapStringToClientType(newProvider)
				if err := agentInst.SetProvider(providerType); err != nil {
					ws.log().Warn("failed to set provider on live agent", slog.Any("err", err))
				}
			}
			if newModel != "" {
				if err := agentInst.SetModel(newModel); err != nil {
					ws.log().Warn("failed to set model on live agent", slog.Any("err", err))
				}
			}
			// Sync overrides to the agent so they're persisted with session state
			ws.mutex.RLock()
			ctx := ws.clientContexts[clientID]
			var overrides map[string]interface{}
			if ctx != nil {
				if cs := ctx.getChatSession(ctx.getActiveChatID()); cs != nil {
					cs.mu.Lock()
					overrides = cs.ConfigOverrides
					cs.mu.Unlock()
				}
			}
			ws.mutex.RUnlock()
			if len(overrides) > 0 {
				agentInst.SetConfigOverrides(overrides)
			}
		}
	}

	// Apply patch and collect unknown keys
	var unknownKeys []string
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		unknown, err := applyPartialSettings(cfg, incoming)
		unknownKeys = unknown
		return err
	}); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A runtime max_context_tokens change must re-resolve the effective cap
	// on live agents immediately — otherwise reconcileContextCap's fast path
	// keeps serving the stale resolution until the next provider/model switch.
	if _, ok := incoming["max_context_tokens"]; ok {
		ws.refreshContextCapOnLiveAgents()
	}

	if _, ok := incoming["system_prompt_text"]; ok {
		cfg := cm.GetConfig()
		providerForPrompt := ""
		if reqAgent, err := ws.getClientAgent(ws.resolveClientID(r)); err == nil && reqAgent != nil {
			providerForPrompt = reqAgent.GetProvider()
		}
		systemPrompt, err := agentpkg.GetEmbeddedSystemPromptWithProvider(providerForPrompt)
		if err == nil {
			if prompt := strings.TrimSpace(cfg.SystemPromptText); prompt != "" {
				systemPrompt = prompt
			}
			ws.applySystemPromptToLiveAgents(systemPrompt)
		}
	}

	// Return the updated (sanitized) config.
	updated := cm.GetConfig()

	// Sync agent state after provider/model change
	if newProvider != "" || newModel != "" {
		if err := ws.syncAgentStateForClient(clientID); err != nil {
			ws.log().Warn("failed to sync agent state after provider or model change", slog.Any("err", err))
		}
		// Publish provider state so the WebUI status bar reflects the new
		// model/cost/ctx immediately. Without this the bar lags until the
		// next metrics event (e.g. the next tool_end), which after a user-
		// initiated provider switch reads as "the change didn't take".
		ws.publishProviderState(clientID)
		if newProvider != "" {
			activeChatID := ""
			ws.mutex.RLock()
			if ctx := ws.clientContexts[clientID]; ctx != nil {
				activeChatID = ctx.getActiveChatID()
			}
			ws.mutex.RUnlock()
			ws.notifyMissingCredentialIfNeeded(clientID, activeChatID, newProvider)
		}
	}

	resp := map[string]interface{}{
		"success": true,
		"config":  sanitizedConfig(updated),
	}
	if len(unknownKeys) > 0 {
		resp["warnings"] = []string{fmt.Sprintf("Unknown fields ignored: %v", unknownKeys)}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Scoped PUT handlers — /api/settings?layer=global|workspace|session
// ---------------------------------------------------------------------------
