//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	agentpkg "github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
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
					log.Printf("webui: failed to set provider on live agent: %v", err)
				}
			}
			if newModel != "" {
				if err := agentInst.SetModel(newModel); err != nil {
					log.Printf("webui: failed to set model on live agent: %v", err)
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
			log.Printf("webui: failed to sync agent state after provider/model change: %v", err)
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
		"provider":                       true,
		"model":                          true,
		"temperature":                    true,
		"max_tokens":                     true,
		"reasoning_effort":               true,
		"system_prompt_text":             true,
		"skip_prompt":                    true,
		"web_search_enabled":             true,
		"subagent_provider":              true,
		"subagent_model":                 true,
		"disable_thinking":               true,
		"top_p":                          true,
		"frequency_penalty":              true,
		"presence_penalty":               true,
		"stop_sequences":                 true,
		"tool_choice":                    true,
		"response_format":                true,
		"stream":                         true,
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
								log.Printf("webui: failed to set provider on live agent after persisted PUT: %v", err)
							}
						}
					}
				}
				if newModel != "" {
					if err := agentInst.SetModel(newModel); err != nil {
						log.Printf("webui: failed to set model on live agent after persisted PUT: %v", err)
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
func applyPartialSettings(cfg *configuration.Config, patch map[string]interface{}) ([]string, error) {
	// knownKeys tracks which keys were recognized and consumed.
	// After processing, any key NOT in this set is returned as unknown.
	knownKeys := make(map[string]bool, len(patch))
	// Simple scalar fields
	if v, ok := patch["reasoning_effort"]; ok {
		knownKeys["reasoning_effort"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateReasoningEffort(s); err != nil {
			return nil, fmt.Errorf("validate reasoning_effort: %w", err)
		}
		cfg.ReasoningEffort = s
	}
	if v, ok := patch["system_prompt_text"]; ok {
		knownKeys["system_prompt_text"] = true
		s, _ := v.(string)
		cfg.SystemPromptText = truncateString(s, maxSettingPromptLength)
	}
	if v, ok := patch["skip_prompt"]; ok {
		knownKeys["skip_prompt"] = true
		cfg.SkipPrompt, _ = v.(bool)
	}
	if v, ok := patch["resource_directory"]; ok {
		knownKeys["resource_directory"] = true
		s, _ := v.(string)
		cfg.ResourceDirectory = truncateString(s, maxSettingPathLength)
	}
	if v, ok := patch["history_scope"]; ok {
		knownKeys["history_scope"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateHistoryScope(s); err != nil {
			return nil, fmt.Errorf("validate history_scope: %w", err)
		}
		cfg.HistoryScope = s
	}
	if v, ok := patch["risk_profile"]; ok {
		knownKeys["risk_profile"] = true
		s, _ := v.(string)
		// Allow empty (= unset → resolves to "default") and any
		// built-in profile name. User-defined names in
		// cfg.RiskProfiles are also accepted. Reject names that
		// match neither so a typo in the dropdown doesn't silently
		// fall back to default.
		if s != "" && !configuration.IsValidRiskProfile(s) {
			if _, ok := cfg.RiskProfiles[s]; !ok {
				return nil, fmt.Errorf("validate risk_profile: unknown profile %q (built-in: readonly, cautious, default, permissive, unrestricted; or define your own in risk_profiles)", s)
			}
		}
		cfg.RiskProfile = s
	}
	if v, ok := patch["risk_profiles"]; ok {
		knownKeys["risk_profiles"] = true
		// Accept a map[name]AutoApproveRules. Round-trip via JSON so
		// we get type-safe decoding without depending on map[string]any
		// gymnastics for the nested struct. nil clears the override
		// map and falls back to baked-in defaults for all profiles.
		if v == nil {
			cfg.RiskProfiles = nil
		} else {
			raw, mErr := json.Marshal(v)
			if mErr != nil {
				return nil, fmt.Errorf("validate risk_profiles: encode incoming value: %w", mErr)
			}
			var decoded map[string]configuration.AutoApproveRules
			if uErr := json.Unmarshal(raw, &decoded); uErr != nil {
				return nil, fmt.Errorf("validate risk_profiles: %w", uErr)
			}
			cfg.RiskProfiles = decoded
		}
	}
	if v, ok := patch["self_review_gate_mode"]; ok {
		knownKeys["self_review_gate_mode"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateSelfReviewGateMode(s); err != nil {
			return nil, fmt.Errorf("validate self_review_gate_mode: %w", err)
		}
		cfg.SelfReviewGateMode = s
	}
	if v, ok := patch["output_verbosity"]; ok {
		knownKeys["output_verbosity"] = true
		s, _ := v.(string)
		s = strings.ToLower(strings.TrimSpace(truncateString(s, maxSettingEnumLength)))
		switch s {
		case "", "compact", "default", "verbose":
			cfg.OutputVerbosity = s
		default:
			return nil, fmt.Errorf("validate output_verbosity: must be 'compact', 'default', or 'verbose' (got %q)", s)
		}
	}
	if v, ok := patch["subagent_provider"]; ok {
		knownKeys["subagent_provider"] = true
		s, _ := v.(string)
		cfg.SubagentProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["subagent_model"]; ok {
		knownKeys["subagent_model"] = true
		s, _ := v.(string)
		cfg.SubagentModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["subagent_max_parallel"]; ok {
		knownKeys["subagent_max_parallel"] = true
		n, ok2 := asInt(v)
		if ok2 && n >= 0 {
			cfg.SubagentMaxParallel = n
		}
	}
	if v, ok := patch["subagent_parallel_enabled"]; ok {
		knownKeys["subagent_parallel_enabled"] = true
		b, ok2 := v.(bool)
		if ok2 {
			cfg.SubagentParallelEnabled = &b
		}
	}
	if v, ok := patch["default_subagent_persona"]; ok {
		knownKeys["default_subagent_persona"] = true
		s, _ := v.(string)
		s = strings.TrimSpace(truncateString(s, maxSettingNameLength))
		// Empty string clears the override (falls back to "general").
		// A non-empty value must reference a known persona; otherwise reject
		// rather than silently fail at spawn time.
		if s != "" && cfg.GetSubagentType(s) == nil {
			return nil, fmt.Errorf("default_subagent_persona %q is not a known persona ID or alias", s)
		}
		cfg.DefaultSubagentPersona = s
	}
	if v, ok := patch["disabled_personas"]; ok {
		knownKeys["disabled_personas"] = true
		// Accept either []string or []interface{} (JSON unmarshals to the latter).
		var ids []string
		switch list := v.(type) {
		case []string:
			ids = list
		case []interface{}:
			for _, item := range list {
				if s, ok := item.(string); ok {
					ids = append(ids, s)
				}
			}
		case nil:
			ids = nil
		default:
			return nil, fmt.Errorf("disabled_personas must be a list of persona IDs")
		}
		// Validate each entry resolves to a known persona. Unknown IDs would
		// silently no-op the disable, which is a quiet bug.
		var cleaned []string
		for _, id := range ids {
			trimmed := strings.TrimSpace(truncateString(id, maxSettingNameLength))
			if trimmed == "" {
				continue
			}
			if cfg.GetSubagentType(trimmed) == nil && !cfg.IsPersonaDisabled(trimmed) {
				// Allow re-listing an already-disabled persona (so the list is
				// stable across PUTs even after a catalog change removes one).
				return nil, fmt.Errorf("disabled_personas: %q is not a known persona ID or alias", trimmed)
			}
			cleaned = append(cleaned, trimmed)
		}
		cfg.DisabledPersonas = cleaned
	}
	if v, ok := patch["commit_provider"]; ok {
		knownKeys["commit_provider"] = true
		s, _ := v.(string)
		cfg.CommitProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["commit_model"]; ok {
		knownKeys["commit_model"] = true
		s, _ := v.(string)
		cfg.CommitModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["review_provider"]; ok {
		knownKeys["review_provider"] = true
		s, _ := v.(string)
		cfg.ReviewProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["review_model"]; ok {
		knownKeys["review_model"] = true
		s, _ := v.(string)
		cfg.ReviewModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["pdf_ocr_enabled"]; ok {
		knownKeys["pdf_ocr_enabled"] = true
		cfg.PDFOCREnabled, _ = v.(bool)
	}
	if v, ok := patch["pdf_ocr_provider"]; ok {
		knownKeys["pdf_ocr_provider"] = true
		s, _ := v.(string)
		cfg.PDFOCRProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["pdf_ocr_model"]; ok {
		knownKeys["pdf_ocr_model"] = true
		s, _ := v.(string)
		cfg.PDFOCRModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["enable_zsh_command_detection"]; ok {
		knownKeys["enable_zsh_command_detection"] = true
		cfg.EnableZshCommandDetection, _ = v.(bool)
	}
	if v, ok := patch["auto_execute_detected_commands"]; ok {
		knownKeys["auto_execute_detected_commands"] = true
		cfg.AutoExecuteDetectedCommands, _ = v.(bool)
	}
	if v, ok := patch["disable_thinking"]; ok {
		knownKeys["disable_thinking"] = true
		cfg.DisableThinking, _ = v.(bool)
	}
	if v, ok := patch["max_context_tokens"]; ok {
		knownKeys["max_context_tokens"] = true
		if v == nil {
			cfg.MaxContextTokens = nil
		} else {
			n, ok2 := asInt(v)
			if !ok2 || n < 0 {
				return nil, fmt.Errorf("max_context_tokens must be a non-negative integer (0 = no limit)")
			}
			if n == 0 {
				cfg.MaxContextTokens = nil
			} else if n < 1024 {
				return nil, fmt.Errorf("max_context_tokens must be at least 1024 when set (got %d)", n)
			} else {
				cfg.MaxContextTokens = &n
			}
		}
	}
	if v, ok := patch["ea_mode"]; ok {
		knownKeys["ea_mode"] = true
		s, _ := v.(string)
		s = strings.ToLower(strings.TrimSpace(truncateString(s, maxSettingEnumLength)))
		switch s {
		case "", "interactive", "queue":
			cfg.EAMode = s
		default:
			return nil, fmt.Errorf("validate ea_mode: must be 'interactive' or 'queue' (got %q)", s)
		}
	}
	if v, ok := patch["subagent_max_depth"]; ok {
		knownKeys["subagent_max_depth"] = true
		n, ok2 := asInt(v)
		if ok2 && n >= 0 && n <= 32 {
			cfg.SubagentMaxDepth = n
		}
	}
	if v, ok := patch["approved_shell_commands"]; ok {
		knownKeys["approved_shell_commands"] = true
		if arr, ok := v.([]interface{}); ok {
			out := make([]string, 0, len(arr))
			seen := make(map[string]struct{}, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					trimmed := strings.TrimSpace(truncateString(s, maxSettingPathLength))
					if trimmed == "" {
						continue
					}
					if _, dup := seen[trimmed]; dup {
						continue
					}
					seen[trimmed] = struct{}{}
					out = append(out, trimmed)
				}
			}
			cfg.ApprovedShellCommands = out
		} else if v == nil {
			cfg.ApprovedShellCommands = nil
		}
	}

	// APITimeouts
	if at, ok := patch["api_timeouts"]; ok {
		knownKeys["api_timeouts"] = true
		if atMap, ok := at.(map[string]interface{}); ok {
			if existing := cfg.APITimeouts; existing == nil {
				cfg.APITimeouts = &configuration.APITimeoutConfig{}
			}
			if v2, ok2 := atMap["connection_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return nil, fmt.Errorf("api_timeouts.connection_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return nil, fmt.Errorf("validate connection_timeout_sec: %w", err)
				}
				cfg.APITimeouts.ConnectionTimeoutSec = n
			}
			if v2, ok2 := atMap["first_chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return nil, fmt.Errorf("api_timeouts.first_chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return nil, fmt.Errorf("validate first_chunk_timeout_sec: %w", err)
				}
				cfg.APITimeouts.FirstChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return nil, fmt.Errorf("api_timeouts.chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return nil, fmt.Errorf("validate chunk_timeout_sec: %w", err)
				}
				cfg.APITimeouts.ChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["overall_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return nil, fmt.Errorf("api_timeouts.overall_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return nil, fmt.Errorf("validate overall_timeout_sec: %w", err)
				}
				cfg.APITimeouts.OverallTimeoutSec = n
			}
			if v2, ok2 := atMap["commit_message_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return nil, fmt.Errorf("api_timeouts.commit_message_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return nil, fmt.Errorf("validate commit_message_timeout_sec: %w", err)
				}
				cfg.APITimeouts.CommitMessageTimeoutSec = n
			}
		}
	}

	// Provider models / provider_priority (simple maps & slices)
	if v, ok := patch["provider_models"]; ok {
		knownKeys["provider_models"] = true
		if m, ok := v.(map[string]interface{}); ok {
			pm := make(map[string]string, len(m))
			for k, val := range m {
				s, _ := val.(string)
				pm[truncateString(k, maxSettingNameLength)] = truncateString(s, maxSettingNameLength)
			}
			cfg.ProviderModels = pm
		}
	}
	if v, ok := patch["provider_priority"]; ok {
		knownKeys["provider_priority"] = true
		if arr, ok := v.([]interface{}); ok {
			pp := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					pp = append(pp, truncateString(s, maxSettingNameLength))
				}
			}
			cfg.ProviderPriority = pp
		}
	}

	// Version
	if v, ok := patch["version"]; ok {
		knownKeys["version"] = true
		s, _ := v.(string)
		cfg.Version = truncateString(s, maxSettingGenericLength)
	}

	// LastUsedProvider
	if v, ok := patch["last_used_provider"]; ok {
		knownKeys["last_used_provider"] = true
		s, _ := v.(string)
		cfg.LastUsedProvider = truncateString(s, maxSettingNameLength)
	}

	// MCP — complex struct, use JSON marshal/unmarshal
	if v, ok := patch["mcp"]; ok {
		knownKeys["mcp"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid mcp config: %w", err)
		}
		var mcpCfg mcp.MCPConfig
		if err := json.Unmarshal(raw, &mcpCfg); err != nil {
			return nil, fmt.Errorf("invalid mcp config: %w", err)
		}
		truncateMCPConfig(&mcpCfg)
		cfg.MCP = mcpCfg
	}

	// CustomProviders — map[string]CustomProviderConfig
	if v, ok := patch["custom_providers"]; ok {
		knownKeys["custom_providers"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid custom_providers config: %w", err)
		}
		var providers map[string]configuration.CustomProviderConfig
		if err := json.Unmarshal(raw, &providers); err != nil {
			return nil, fmt.Errorf("invalid custom_providers config: %w", err)
		}
		for i, p := range providers {
			providers[i] = truncateCustomProvider(p)
		}
		cfg.CustomProviders = providers
	}

	// EmbeddingIndex — *EmbeddingIndexConfig, use JSON marshal/unmarshal
	if v, ok := patch["embedding_index"]; ok {
		knownKeys["embedding_index"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid embedding_index config: %w", err)
		}
		var ei configuration.EmbeddingIndexConfig
		if err := json.Unmarshal(raw, &ei); err != nil {
			return nil, fmt.Errorf("invalid embedding_index config: %w", err)
		}
		for i, p := range ei.ExcludePaths {
			ei.ExcludePaths[i] = truncateString(p, maxSettingPathLength)
		}
		// Provider field removed — embedding provider is always the
		// bundled ONNX EmbeddingGemma-300M today.
		ei.IndexDir = truncateString(ei.IndexDir, maxSettingPathLength)
		cfg.EmbeddingIndex = &ei
	}

	// ComputerUse — *ComputerUseConfig, use JSON marshal/unmarshal (SP-063)
	if v, ok := patch["computer_use"]; ok {
		knownKeys["computer_use"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid computer_use config: %w", err)
		}
		var cu configuration.ComputerUseConfig
		if err := json.Unmarshal(raw, &cu); err != nil {
			return nil, fmt.Errorf("invalid computer_use config: %w", err)
		}
		cu.AuditLogDir = truncateString(cu.AuditLogDir, maxSettingPathLength)
		for i, p := range cu.WorkspaceAllowlist {
			cu.WorkspaceAllowlist[i] = truncateString(p, maxSettingPathLength)
		}
		cfg.ComputerUse = &cu
	}

	// LanguageServers — []LanguageServerOverride, use JSON marshal/unmarshal
	if v, ok := patch["language_servers"]; ok {
		knownKeys["language_servers"] = true
		if v == nil {
			cfg.LanguageServers = nil
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("invalid language_servers config: %w", err)
			}
			var servers []configuration.LanguageServerOverride
			if err := json.Unmarshal(raw, &servers); err != nil {
				return nil, fmt.Errorf("invalid language_servers config: %w", err)
			}
			for i := range servers {
				servers[i].ID = truncateString(servers[i].ID, maxSettingNameLength)
				servers[i].Binary = truncateString(servers[i].Binary, maxSettingPathLength)
				servers[i].InstallHint = truncateString(servers[i].InstallHint, maxSettingDescriptionLength)
				for j := range servers[i].Args {
					servers[i].Args[j] = truncateString(servers[i].Args[j], maxSettingPathLength)
				}
				for j := range servers[i].LanguageIDs {
					servers[i].LanguageIDs[j] = truncateString(servers[i].LanguageIDs[j], maxSettingNameLength)
				}
			}
			cfg.LanguageServers = servers
		}
	}

	// SecurityPolicy — *SecurityPolicy, JSON marshal/unmarshal
	if v, ok := patch["security_policy"]; ok {
		knownKeys["security_policy"] = true
		if v == nil {
			cfg.SecurityPolicy = nil
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("invalid security_policy config: %w", err)
			}
			var sp configuration.SecurityPolicy
			if err := json.Unmarshal(raw, &sp); err != nil {
				return nil, fmt.Errorf("invalid security_policy config: %w", err)
			}
			cfg.SecurityPolicy = &sp
		}
	}

	// PersistentContext — *PersistentContextConfig, JSON marshal/unmarshal
	if v, ok := patch["persistent_context"]; ok {
		knownKeys["persistent_context"] = true
		if v == nil {
			cfg.PersistentContext = nil
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("invalid persistent_context config: %w", err)
			}
			var pc configuration.PersistentContextConfig
			if err := json.Unmarshal(raw, &pc); err != nil {
				return nil, fmt.Errorf("invalid persistent_context config: %w", err)
			}
			cfg.PersistentContext = &pc
		}
	}

	// SubagentTypes — personas are catalog-fixed. Older clients (and
	// round-trip GET→PUT flows like "copy global to workspace") may still
	// include this field; we accept-and-ignore so existing payloads don't
	// 400. Use 'disabled_personas' to hide a persona or 'default_subagent_persona'
	// to redirect default spawns.
	if _, ok := patch["subagent_types"]; ok {
		knownKeys["subagent_types"] = true
		// Intentionally no-op: the catalog is the source of truth.
	}

	// Skills — map[string]Skill
	if v, ok := patch["skills"]; ok {
		knownKeys["skills"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid skills config: %w", err)
		}
		var skills map[string]configuration.Skill
		if err := json.Unmarshal(raw, &skills); err != nil {
			return nil, fmt.Errorf("invalid skills config: %w", err)
		}
		for name, s := range skills {
			skills[name] = truncateSkill(s)
		}
		cfg.Skills = skills
	}

	// Collect unknown keys
	var unknown []string
	for k := range patch {
		if !knownKeys[k] {
			unknown = append(unknown, k)
		}
	}
	return unknown, nil
}
