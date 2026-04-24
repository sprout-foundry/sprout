package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	agentpkg "github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Method router — General settings
// ---------------------------------------------------------------------------

// handleAPISettings dispatches GET and PUT /api/settings.
func (ws *ReactWebServer) handleAPISettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsGet(w http.ResponseWriter, r *http.Request) {
	layer := strings.TrimSpace(r.URL.Query().Get("layer"))

	switch layer {
	case "global":
		ws.handleGetGlobalSettings(w, r)
		return
	case "workspace":
		ws.handleGetWorkspaceSettings(w, r)
		return
	case "session":
		ws.handleGetSessionSettings(w, r)
		return
	}

	// Default: return effective merged config (current behavior)
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, sanitizedConfig(cfg))
}

// handleGetGlobalSettings returns the global config file contents.
func (ws *ReactWebServer) handleGetGlobalSettings(w http.ResponseWriter, r *http.Request) {
	configPath, err := configuration.GetConfigPath()
	if err != nil {
		writeJSON(w, http.StatusOK, sanitizedConfig(configuration.NewConfig()))
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		writeJSON(w, http.StatusOK, sanitizedConfig(configuration.NewConfig()))
		return
	}
	var cfg configuration.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		writeJSON(w, http.StatusOK, sanitizedConfig(configuration.NewConfig()))
		return
	}
	writeJSON(w, http.StatusOK, sanitizedConfig(&cfg))
}

// handleGetWorkspaceSettings returns the workspace config if it exists, else empty.
func (ws *ReactWebServer) handleGetWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	workspacePath := configuration.GetWorkspaceConfigPath(workspaceRoot)
	data, err := os.ReadFile(workspacePath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	var cfg configuration.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, sanitizedConfig(&cfg))
}

// handleGetSessionSettings returns the current session's config overrides.
func (ws *ReactWebServer) handleGetSessionSettings(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if ctx == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	activeChatID := ctx.getActiveChatID()
	cs := ctx.getChatSession(activeChatID)
	if cs == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	cs.mu.Lock()
	overrides := cs.ConfigOverrides
	if overrides == nil {
		overrides = make(map[string]interface{})
	}
	cs.mu.Unlock()
	writeJSON(w, http.StatusOK, overrides)
}

// ---------------------------------------------------------------------------
// PUT /api/settings
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsPut(w http.ResponseWriter, r *http.Request) {
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

	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		return applyPartialSettings(cfg, incoming)
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
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"config":  sanitizedConfig(updated),
	})
}

// applyPartialSettings applies a partial JSON patch to the config struct.
// Only whitelisted top-level keys are accepted to prevent accidental
// overwrite of internal bookkeeping fields.
func applyPartialSettings(cfg *configuration.Config, patch map[string]interface{}) error {
	// Simple scalar fields
	if v, ok := patch["reasoning_effort"]; ok {
		s, _ := v.(string)
		if err := validateReasoningEffort(s); err != nil {
			return fmt.Errorf("validate reasoning_effort: %w", err)
		}
		cfg.ReasoningEffort = s
	}
	if v, ok := patch["system_prompt_text"]; ok {
		s, _ := v.(string)
		cfg.SystemPromptText = s
	}
	if v, ok := patch["skip_prompt"]; ok {
		cfg.SkipPrompt, _ = v.(bool)
	}
	if v, ok := patch["enable_pre_write_validation"]; ok {
		cfg.EnablePreWriteValidation, _ = v.(bool)
	}
	if v, ok := patch["allow_orchestrator_git_write"]; ok {
		cfg.AllowOrchestratorGitWrite, _ = v.(bool)
	}
	if v, ok := patch["resource_directory"]; ok {
		s, _ := v.(string)
		cfg.ResourceDirectory = s
	}
	if v, ok := patch["history_scope"]; ok {
		s, _ := v.(string)
		if err := validateHistoryScope(s); err != nil {
			return fmt.Errorf("validate history_scope: %w", err)
		}
		cfg.HistoryScope = s
	}
	if v, ok := patch["self_review_gate_mode"]; ok {
		s, _ := v.(string)
		if err := validateSelfReviewGateMode(s); err != nil {
			return fmt.Errorf("validate self_review_gate_mode: %w", err)
		}
		cfg.SelfReviewGateMode = s
	}
	if v, ok := patch["subagent_provider"]; ok {
		s, _ := v.(string)
		cfg.SubagentProvider = s
	}
	if v, ok := patch["subagent_model"]; ok {
		s, _ := v.(string)
		cfg.SubagentModel = s
	}
	if v, ok := patch["subagent_max_parallel"]; ok {
		n, ok2 := asInt(v)
		if ok2 && n >= 0 {
			cfg.SubagentMaxParallel = n
		}
	}
	if v, ok := patch["subagent_parallel_enabled"]; ok {
		b, ok2 := v.(bool)
		if ok2 {
			cfg.SubagentParallelEnabled = &b
		}
	}
	if v, ok := patch["commit_provider"]; ok {
		s, _ := v.(string)
		cfg.CommitProvider = s
	}
	if v, ok := patch["commit_model"]; ok {
		s, _ := v.(string)
		cfg.CommitModel = s
	}
	if v, ok := patch["review_provider"]; ok {
		s, _ := v.(string)
		cfg.ReviewProvider = s
	}
	if v, ok := patch["review_model"]; ok {
		s, _ := v.(string)
		cfg.ReviewModel = s
	}
	if v, ok := patch["pdf_ocr_enabled"]; ok {
		cfg.PDFOCREnabled, _ = v.(bool)
	}
	if v, ok := patch["pdf_ocr_provider"]; ok {
		s, _ := v.(string)
		cfg.PDFOCRProvider = s
	}
	if v, ok := patch["pdf_ocr_model"]; ok {
		s, _ := v.(string)
		cfg.PDFOCRModel = s
	}
	if v, ok := patch["enable_zsh_command_detection"]; ok {
		cfg.EnableZshCommandDetection, _ = v.(bool)
	}
	if v, ok := patch["auto_execute_detected_commands"]; ok {
		cfg.AutoExecuteDetectedCommands, _ = v.(bool)
	}
	if v, ok := patch["disable_thinking"]; ok {
		cfg.DisableThinking, _ = v.(bool)
	}

	// APITimeouts
	if at, ok := patch["api_timeouts"]; ok {
		if atMap, ok := at.(map[string]interface{}); ok {
			if existing := cfg.APITimeouts; existing == nil {
				cfg.APITimeouts = &configuration.APITimeoutConfig{}
			}
			if v2, ok2 := atMap["connection_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.connection_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate connection_timeout_sec: %w", err)
				}
				cfg.APITimeouts.ConnectionTimeoutSec = n
			}
			if v2, ok2 := atMap["first_chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.first_chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate first_chunk_timeout_sec: %w", err)
				}
				cfg.APITimeouts.FirstChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate chunk_timeout_sec: %w", err)
				}
				cfg.APITimeouts.ChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["overall_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.overall_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate overall_timeout_sec: %w", err)
				}
				cfg.APITimeouts.OverallTimeoutSec = n
			}
		}
	}

	// Provider models / provider_priority (simple maps & slices)
	if v, ok := patch["provider_models"]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			pm := make(map[string]string, len(m))
			for k, val := range m {
				pm[k], _ = val.(string)
			}
			cfg.ProviderModels = pm
		}
	}
	if v, ok := patch["provider_priority"]; ok {
		if arr, ok := v.([]interface{}); ok {
			pp := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					pp = append(pp, s)
				}
			}
			cfg.ProviderPriority = pp
		}
	}

	return nil
}

// applySystemPromptToLiveAgents distributes a system prompt update to all
// live agents that are currently using the base/orchestrator persona.
func (ws *ReactWebServer) applySystemPromptToLiveAgents(systemPrompt string) {
	if strings.TrimSpace(systemPrompt) == "" {
		return
	}

	ws.mutex.RLock()
	agents := make([]*agentpkg.Agent, 0, len(ws.clientContexts))
	seen := make(map[*agentpkg.Agent]struct{})
	for _, ctx := range ws.clientContexts {
		if ctx == nil || ctx.Agent == nil {
			continue
		}
		if _, exists := seen[ctx.Agent]; exists {
			continue
		}
		agents = append(agents, ctx.Agent)
		seen[ctx.Agent] = struct{}{}
	}
	ws.mutex.RUnlock()

	for _, agentInst := range agents {
		agentInst.SetBaseSystemPrompt(systemPrompt)
		activePersona := strings.TrimSpace(agentInst.GetActivePersona())
		if activePersona == "" || activePersona == "orchestrator" {
			agentInst.SetSystemPrompt(systemPrompt)
		}
	}
}
