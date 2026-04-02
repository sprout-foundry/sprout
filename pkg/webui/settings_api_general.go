package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	agentpkg "github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
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
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, sanitizedConfig(cfg))
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
			return err
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
			return err
		}
		cfg.HistoryScope = s
	}
	if v, ok := patch["self_review_gate_mode"]; ok {
		s, _ := v.(string)
		if err := validateSelfReviewGateMode(s); err != nil {
			return err
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
					return err
				}
				cfg.APITimeouts.ConnectionTimeoutSec = n
			}
			if v2, ok2 := atMap["first_chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.first_chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return err
				}
				cfg.APITimeouts.FirstChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return err
				}
				cfg.APITimeouts.ChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["overall_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.overall_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return err
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
