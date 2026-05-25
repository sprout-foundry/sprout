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
		if ctx != nil {
			activeChatID := ctx.getActiveChatID()
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

		// Apply to the live agent in-memory if one exists
		if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
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

	// Apply to live agent in-memory
	providerOrModelChanged := false
	if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
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
		if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
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
	if v, ok := patch["allow_orchestrator_git_write"]; ok {
		knownKeys["allow_orchestrator_git_write"] = true
		cfg.AllowOrchestratorGitWrite, _ = v.(bool)
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
	if v, ok := patch["self_review_gate_mode"]; ok {
		knownKeys["self_review_gate_mode"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateSelfReviewGateMode(s); err != nil {
			return nil, fmt.Errorf("validate self_review_gate_mode: %w", err)
		}
		cfg.SelfReviewGateMode = s
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
		ei.Provider = truncateString(ei.Provider, maxSettingNameLength)
		ei.IndexDir = truncateString(ei.IndexDir, maxSettingPathLength)
		cfg.EmbeddingIndex = &ei
	}

	// SubagentTypes — map[string]SubagentType
	if v, ok := patch["subagent_types"]; ok {
		knownKeys["subagent_types"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid subagent_types config: %w", err)
		}
		var types map[string]configuration.SubagentType
		if err := json.Unmarshal(raw, &types); err != nil {
			return nil, fmt.Errorf("invalid subagent_types config: %w", err)
		}
		for name, st := range types {
			st.Provider = truncateString(st.Provider, maxSettingNameLength)
			st.Model = truncateString(st.Model, maxSettingNameLength)
			st.SystemPrompt = truncateString(st.SystemPrompt, maxSettingPathLength)
			st.SystemPromptText = truncateString(st.SystemPromptText, maxSettingPromptLength)
			st.SystemPromptAppend = truncateString(st.SystemPromptAppend, maxSettingPromptLength)
			st.Name = truncateString(st.Name, maxSettingNameLength)
			st.Description = truncateString(st.Description, maxSettingDescriptionLength)
			for i, a := range st.AllowedTools {
				st.AllowedTools[i] = truncateString(a, maxSettingNameLength)
			}
			for i, a := range st.Aliases {
				st.Aliases[i] = truncateString(a, maxSettingNameLength)
			}
			types[name] = st
		}
		cfg.SubagentTypes = types
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
