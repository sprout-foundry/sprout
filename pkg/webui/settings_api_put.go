//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	agentpkg "github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// PUT /api/settings
// ---------------------------------------------------------------------------

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

// handlePutSessionSettings writes settings to the current session's ConfigOverrides.
func (ws *ReactWebServer) handlePutSessionSettings(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)
	var incoming map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate provider if included
	if p, ok := incoming["provider"].(string); ok && p != "" {
		cm := ws.getConfigManager(r, w)
		if cm == nil {
			return
		}
		if _, err := cm.MapStringToClientType(p); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid provider: %v", err))
			return
		}
	}

	// Check for unknown keys and collect warnings
	knownSessionKeys := map[string]bool{
		"provider":           true,
		"model":              true,
		"temperature":        true,
		"max_tokens":         true,
		"reasoning_effort":   true,
		"system_prompt_text": true,
		"skip_prompt":        true,
		"web_search_enabled": true,
		"subagent_provider":  true,
		"subagent_model":     true,
		"disable_thinking":   true,
		"top_p":              true,
		"frequency_penalty":  true,
		"presence_penalty":   true,
		"stop_sequences":     true,
		"tool_choice":        true,
		"response_format":    true,
		"stream":             true,
	}

	var unknownKeys []string
	for k := range incoming {
		if !knownSessionKeys[k] {
			unknownKeys = append(unknownKeys, k)
		}
	}

	// Merge into session ConfigOverrides
	ws.mutex.Lock()
	ctx := ws.clientContexts[clientID]
	var cs *chatSession
	if ctx != nil {
		cs = ctx.getChatSession(ctx.getActiveChatID())
	}
	if ctx == nil || cs == nil {
		ws.mutex.Unlock()
		writeJSONError(w, http.StatusBadRequest, "No active session")
		return
	}
	cs.mu.Lock()
	if cs.ConfigOverrides == nil {
		cs.ConfigOverrides = make(map[string]interface{})
	}
	for k, v := range incoming {
		// Auto-truncate string values before storing
		if s, ok := v.(string); ok {
			switch k {
			case "provider", "model", "subagent_provider", "subagent_model":
				v = truncateString(s, maxSettingNameLength)
			case "reasoning_effort":
				v = truncateString(s, maxSettingEnumLength)
			case "system_prompt_text":
				v = truncateString(s, maxSettingPromptLength)
			default:
				v = truncateString(s, maxSettingGenericLength)
			}
		}
		if v == nil || v == "" || v == 0 || v == false {
			delete(cs.ConfigOverrides, k)
		} else {
			cs.ConfigOverrides[k] = v
		}
	}
	// Sync Provider/Model shortcuts
	if p, ok := cs.ConfigOverrides["provider"].(string); ok {
		cs.Provider = p
	}
	if m, ok := cs.ConfigOverrides["model"].(string); ok {
		cs.Model = m
	}
	savedOverrides := make(map[string]interface{}, len(cs.ConfigOverrides))
	for k, v := range cs.ConfigOverrides {
		savedOverrides[k] = v
	}
	cs.mu.Unlock()
	ws.mutex.Unlock()

	// Apply to live chat agent (not client-level agent) so the override
	// reaches the correct per-chat instance. Skip if the chat has an
	// active query — SetProvider swaps a.client without synchronization.
	providerOrModelChanged := false
	activeChatIDForAgent := ""
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		activeChatIDForAgent = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	canChangeAgent := activeChatIDForAgent != ""
	if canChangeAgent {
		ws.mutex.RLock()
		ctx := ws.clientContexts[clientID]
		if ctx != nil && ctx.hasActiveQueryForChat(activeChatIDForAgent) {
			canChangeAgent = false
		}
		ws.mutex.RUnlock()
	}

	if canChangeAgent {
		if agentInst, err := ws.getChatAgent(clientID, activeChatIDForAgent); err == nil && agentInst != nil {
			if p, ok := savedOverrides["provider"].(string); ok && p != "" {
				cm := ws.getConfigManager(r, w)
				if cm != nil {
					if pt, err := cm.MapStringToClientType(p); err == nil {
						agentInst.SetProvider(pt)
						providerOrModelChanged = true
					}
				}
			}
			if m, ok := savedOverrides["model"].(string); ok && m != "" {
				agentInst.SetModel(m)
				providerOrModelChanged = true
			}
			agentInst.SetConfigOverrides(savedOverrides)
		}
	}

	// Refresh the status bar for any provider/model change in session
	// scope, matching the default handler's behavior so the WebUI never
	// looks "stuck" on a stale model after the user picks a new one.
	if providerOrModelChanged {
		ws.publishProviderState(clientID)
		if p, ok := savedOverrides["provider"].(string); ok && p != "" {
			activeChatID := ""
			ws.mutex.RLock()
			if ctx := ws.clientContexts[clientID]; ctx != nil {
				activeChatID = ctx.getActiveChatID()
			}
			ws.mutex.RUnlock()
			ws.notifyMissingCredentialIfNeeded(clientID, activeChatID, p)
		}
	}

	resp := map[string]interface{}{
		"success": true,
		"config":  savedOverrides,
	}
	if len(unknownKeys) > 0 {
		resp["warnings"] = []string{fmt.Sprintf("Unknown fields ignored: %v", unknownKeys)}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePutWorkspaceSettings writes settings to the workspace config file.
func (ws *ReactWebServer) handlePutWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		writeJSONError(w, http.StatusBadRequest, "No workspace configured")
		return
	}
	ws.putConfigToFile(w, r, configuration.GetWorkspaceConfigPath(workspaceRoot))
}

// handlePutGlobalSettings writes settings to the global config file.
func (ws *ReactWebServer) handlePutGlobalSettings(w http.ResponseWriter, r *http.Request) {
	configPath, err := configuration.GetConfigPath()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Cannot determine global config path")
		return
	}
	ws.putConfigToFile(w, r, configPath)
}

// putConfigToFile is a helper that merges incoming settings into an existing
// config file and writes the result back.
func (ws *ReactWebServer) putConfigToFile(w http.ResponseWriter, r *http.Request, configPath string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)
	var incoming map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Load existing config or create default
	var cfg configuration.Config
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &cfg)
	} else {
		cfg = *configuration.NewConfig()
	}

	// Map session-style provider/model shortcuts to persisted config fields.
	// The frontend sends "provider" and "model" regardless of layer, but
	// applyPartialSettings expects "last_used_provider" and "provider_models".
	// Always delete these keys from incoming so they're never reported as unknown.
	if p, ok := incoming["provider"]; ok {
		if ps, ok := p.(string); ok {
			if ps != "" {
				incoming["last_used_provider"] = ps
			} else {
				// Empty string means clear the provider
				incoming["last_used_provider"] = ""
			}
		}
		delete(incoming, "provider")
	}
	if m, ok := incoming["model"]; ok {
		if ms, ok := m.(string); ok && ms != "" {
			// Determine which provider this model belongs to.
			provider := ""
			if p, ok := incoming["last_used_provider"].(string); ok && p != "" {
				provider = p
			} else if cfg.LastUsedProvider != "" {
				provider = cfg.LastUsedProvider
			}
			if provider != "" {
				if cfg.ProviderModels == nil {
					cfg.ProviderModels = make(map[string]string)
				}
				pm := make(map[string]interface{}, len(cfg.ProviderModels))
				for k, v := range cfg.ProviderModels {
					pm[k] = v
				}
				pm[provider] = ms
				incoming["provider_models"] = pm
			}
		}
		delete(incoming, "model")
	}

	// Apply patch and collect unknown keys
	unknownKeys, err := applyPartialSettings(&cfg, incoming)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Cannot create config directory: %v", err))
		return
	}

	// Write
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to marshal config: %v", err))
		return
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to write config: %v", err))
		return
	}

	// If the patch contained a primary provider/model change, also apply
	// it to the live agent and republish provider state. Without this,
	// the on-disk config is correct but the active session keeps running
	// against the old model — the dropdown "looks broken" because the
	// status bar doesn't move until the user reloads.
	newProvider, _ := incoming["last_used_provider"].(string)
	var newModel string
	if pm, ok := incoming["provider_models"].(map[string]interface{}); ok && newProvider != "" {
		if m, ok := pm[newProvider].(string); ok {
			newModel = m
		}
	}
	if newProvider != "" || newModel != "" {
		clientID := ws.resolveClientID(r)
		// Resolve the active chat ID so we apply to the correct per-chat agent.
		activeChatID := ""
		ws.mutex.RLock()
		if ctx := ws.clientContexts[clientID]; ctx != nil {
			activeChatID = ctx.getActiveChatID()
		}
		ws.mutex.RUnlock()

		if activeChatID != "" {
			if agentInst, err := ws.getChatAgent(clientID, activeChatID); err == nil && agentInst != nil {
				if newProvider != "" {
					if cm := ws.getConfigManager(r, w); cm != nil {
						if pt, err := cm.MapStringToClientType(newProvider); err == nil {
							if err := agentInst.SetProvider(pt); err != nil {
								ws.log().Warn("failed to set provider on live agent after persisted settings update", slog.Any("err", err))
							}
						}
					}
				}
				if newModel != "" {
					if err := agentInst.SetModel(newModel); err != nil {
						ws.log().Warn("failed to set model on live agent after persisted settings update", slog.Any("err", err))
					}
				}
			}
		}
		ws.publishProviderState(clientID)
		// Warn the user if the persisted provider needs a credential it
		// doesn't have — same warning path as the websocket
		// provider_change handler so the UX is consistent across all the
		// surfaces that can swap the active provider.
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
		"config":  sanitizedConfig(&cfg),
	}
	if len(unknownKeys) > 0 {
		resp["warnings"] = []string{fmt.Sprintf("Unknown fields ignored: %v", unknownKeys)}
	}
	writeJSON(w, http.StatusOK, resp)
}

// applyPartialSettings applies a partial JSON patch to the config struct.
// Only whitelisted top-level keys are accepted to prevent accidental
// overwrite of internal bookkeeping fields.
// Unknown keys are collected and returned so callers can warn the user.
// applyPartialSettings applies a partial JSON patch to the config struct.
// Only whitelisted top-level keys are accepted to prevent accidental
// overwrite of internal bookkeeping fields.
// Unknown keys are collected and returned so callers can warn the user.
//
// The actual per-domain work lives in settings_api_partial_settings.go so
// each domain can be reasoned about in isolation. This orchestrator just
// walks the appliers in order and collects any patch keys that none of
// them recognized.
func applyPartialSettings(cfg *configuration.Config, patch map[string]interface{}) ([]string, error) {
	knownKeys := make(map[string]bool, len(patch))
	for _, apply := range partialSettingsAppliers {
		if err := apply(cfg, patch, knownKeys); err != nil {
			return nil, err
		}
	}
	var unknown []string
	for k := range patch {
		if !knownKeys[k] {
			unknown = append(unknown, k)
		}
	}
	return unknown, nil
}
