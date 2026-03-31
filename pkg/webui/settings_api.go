package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	agentpkg "github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// MAX SETTINGS REQUEST BODY SIZE
const maxSettingsBodyBytes = 2 << 20 // 2 MiB

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sanitizedConfig returns a JSON-serializable map of the current config with
// sensitive fields stripped so they are never sent to the browser.
func sanitizedConfig(cfg *configuration.Config) map[string]interface{} {
	out := map[string]interface{}{
		"version":                        cfg.Version,
		"last_used_provider":             cfg.LastUsedProvider,
		"provider_models":                cfg.ProviderModels,
		"provider_priority":              cfg.ProviderPriority,
		"mcp":                            cfg.MCP,
		"enable_pre_write_validation":    cfg.EnablePreWriteValidation,
		"resource_directory":             cfg.ResourceDirectory,
		"reasoning_effort":               cfg.ReasoningEffort,
		"system_prompt_text":             cfg.SystemPromptText,
		"skip_prompt":                    cfg.SkipPrompt,
		"api_timeouts":                   cfg.APITimeouts,
		"custom_providers":               sanitizedCustomProviders(cfg.CustomProviders),
		"history_scope":                  cfg.HistoryScope,
		"self_review_gate_mode":          cfg.SelfReviewGateMode,
		"subagent_provider":              cfg.SubagentProvider,
		"subagent_model":                 cfg.SubagentModel,
		"subagent_types":                 cfg.SubagentTypes,
		"pdf_ocr_enabled":                cfg.PDFOCREnabled,
		"pdf_ocr_provider":               cfg.PDFOCRProvider,
		"pdf_ocr_model":                  cfg.PDFOCRModel,
		"skills":                         cfg.Skills,
		"enable_zsh_command_detection":   cfg.EnableZshCommandDetection,
		"auto_execute_detected_commands": cfg.AutoExecuteDetectedCommands,
	}
	return out
}

// sanitizedCustomProviders strips sensitive api_key fields from each provider.
func sanitizedCustomProviders(providers map[string]configuration.CustomProviderConfig) map[string]configuration.CustomProviderConfig {
	if providers == nil {
		return nil
	}
	clean := make(map[string]configuration.CustomProviderConfig, len(providers))
	for k, v := range providers {
		cp := v
		cp.APIKey = "" // never send stored API key to browser
		clean[k] = cp
	}
	return clean
}

// writeJSON is a convenience helper that sets Content-Type and encodes the payload.
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeJSONError writes a JSON-formatted error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

// writeJSONErr writes a JSON error message with both a code string and a message.
func writeJSONErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": message,
		"code":  code,
	})
}

// getConfigManager safely resolves the config manager from the request client's
// live agent, or sends a 503 and returns nil.
func (ws *ReactWebServer) getConfigManager(r *http.Request, w http.ResponseWriter) *configuration.Manager {
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil || agentInst.GetConfigManager() == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "config_unavailable", "Configuration manager is not available")
		return nil
	}
	return agentInst.GetConfigManager()
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

var validReasoningEfforts = map[string]bool{
	"":       true,
	"low":    true,
	"medium": true,
	"high":   true,
}

var validSelfReviewGateModes = map[string]bool{
	configuration.SelfReviewGateModeOff:    true,
	configuration.SelfReviewGateModeCode:   true,
	configuration.SelfReviewGateModeAlways: true,
}

var validHistoryScopes = map[string]bool{
	"project": true,
	"global":  true,
}

func validateReasoningEffort(v string) error {
	if !validReasoningEfforts[v] {
		return fmt.Errorf("invalid reasoning_effort %q (allowed: \"\", low, medium, high)", v)
	}
	return nil
}

func validateSelfReviewGateMode(v string) error {
	if !validSelfReviewGateModes[v] {
		return fmt.Errorf("invalid self_review_gate_mode %q (allowed: off, code, always)", v)
	}
	return nil
}

func validateHistoryScope(v string) error {
	if !validHistoryScopes[v] {
		return fmt.Errorf("invalid history_scope %q (allowed: project, global)", v)
	}
	return nil
}

func validateAPITimeout(t int) error {
	if t <= 0 {
		return fmt.Errorf("api_timeout values must be positive integers (seconds)")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Method routers — Go's ServeMux doesn't dispatch on HTTP method,
// so each route funnels into one of these routers.
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

// handleAPISettingsMCP dispatches GET and PUT /api/settings/mcp.
func (ws *ReactWebServer) handleAPISettingsMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsMCPGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsMCPPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPISettingsMCPServers dispatches POST/PUT/DELETE /api/settings/mcp/servers/{name}.
func (ws *ReactWebServer) handleAPISettingsMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// POST without a name in URL means list-level create
		// The name is taken from the body.
		ws.handleAPISettingsMCPServersPost(w, r)
	case http.MethodPut:
		ws.handleAPISettingsMCPServersPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsMCPServersDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPISettingsProviders dispatches all methods for /api/settings/providers and /api/settings/providers/{name}.
// Exact path (/api/settings/providers) maps here for GET/POST; trailing-slash (/api/settings/providers/) maps here for PUT/DELETE.
func (ws *ReactWebServer) handleAPISettingsProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsProvidersGet(w, r)
	case http.MethodPost:
		ws.handleAPISettingsProvidersPost(w, r)
	case http.MethodPut:
		ws.handleAPISettingsProvidersPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsProvidersDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPISettingsSkills dispatches GET and PUT /api/settings/skills.
func (ws *ReactWebServer) handleAPISettingsSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsSkillsGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsSkillsPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPISettingsSubagentTypes dispatches GET/PUT on collection, PUT/DELETE on individual persona.
// Exact path (/api/settings/subagent-types) maps here for GET; trailing-slash
// (/api/settings/subagent-types/) maps here for PUT/DELETE individual persona.
func (ws *ReactWebServer) handleAPISettingsSubagentTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsSubagentTypesGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsSubagentTypesPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsSubagentTypesDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsGet(w http.ResponseWriter, r *http.Request) {
	// Guard removed — the router already selected GET.

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

// ---------------------------------------------------------------------------
// GET /api/settings/mcp
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, cfg.MCP)
}

// ---------------------------------------------------------------------------
// PUT /api/settings/mcp
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPPut(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var incoming struct {
		Enabled      *bool  `json:"enabled"`
		AutoStart    *bool  `json:"auto_start"`
		AutoDiscover *bool  `json:"auto_discover"`
		Timeout      string `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if incoming.Enabled != nil {
			cfg.MCP.Enabled = *incoming.Enabled
		}
		if incoming.AutoStart != nil {
			cfg.MCP.AutoStart = *incoming.AutoStart
		}
		if incoming.AutoDiscover != nil {
			cfg.MCP.AutoDiscover = *incoming.AutoDiscover
		}
		if incoming.Timeout != "" {
			d, err := time.ParseDuration(incoming.Timeout)
			if err != nil {
				return fmt.Errorf("invalid timeout duration: %w", err)
			}
			cfg.MCP.Timeout = d
		}
		return nil
	}); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"mcp":     updated.MCP,
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/mcp/servers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPServersPost(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var server mcp.MCPServerConfig
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if server.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required")
		return
	}

	// Validate server config
	if err := validateMCPServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		if _, exists := cfg.MCP.Servers[server.Name]; exists {
			return fmt.Errorf("MCP server %q already exists (use PUT to update)", server.Name)
		}
		cfg.MCP.Servers[server.Name] = server
		cfg.MCP.Enabled = true
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"server":  server,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/mcp/servers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPServersPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/mcp/servers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var server mcp.MCPServerConfig
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Ensure the name in the body matches the URL (and always have Name populated)
	server.Name = name

	if err := validateMCPServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return fmt.Errorf("MCP server %q not found (use POST to create)", name)
		}
		cfg.MCP.Servers[name] = server
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"server":  server,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/mcp/servers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPServersDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/mcp/servers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			return fmt.Errorf("MCP server %q not found", name)
		}
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}
		delete(cfg.MCP.Servers, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}

// ---------------------------------------------------------------------------
// GET /api/settings/providers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"custom_providers": sanitizedCustomProviders(cfg.CustomProviders),
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/providers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersPost(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if provider.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	// Strip any incoming API key — never accept from the browser.
	provider.APIKey = ""

	if err := validateCustomProvider(provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := provider.Name
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[key]; exists {
			return fmt.Errorf("custom provider %q already exists (use PUT to update)", key)
		}
		cfg.CustomProviders[key] = provider
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/providers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/providers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Ensure name in body matches URL
	provider.Name = name
	// Strip incoming API key
	provider.APIKey = ""

	if err := validateCustomProvider(provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found (use POST to create)", name)
		}
		// Preserve the existing API key if stored
		existing := cfg.CustomProviders[name]
		provider.APIKey = existing.APIKey
		cfg.CustomProviders[name] = provider
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/providers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/providers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			return fmt.Errorf("custom provider %q not found", name)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found", name)
		}
		delete(cfg.CustomProviders, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}

// ---------------------------------------------------------------------------
// GET /api/settings/skills
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSkillsGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"skills": cfg.Skills,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/skills
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSkillsPut(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Accept either:
	//   { "skills": { "id": { "enabled": true/false, ... }, ... } }
	// or a flat list of toggles:
	//   { "toggles": { "id": true/false, ... } }
	var incoming struct {
		Skills  map[string]configuration.Skill `json:"skills"`
		Toggles map[string]bool                `json:"toggles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.Skills == nil {
			cfg.Skills = make(map[string]configuration.Skill)
		}

		// Apply full skill entries
		for id, skill := range incoming.Skills {
			existing, exists := cfg.Skills[id]
			if exists {
				// Preserve existing metadata that wasn't provided
				skill.Path = existing.Path
				if skill.Metadata == nil {
					skill.Metadata = existing.Metadata
				}
				if skill.AllowedTools == "" {
					skill.AllowedTools = existing.AllowedTools
				}
				if skill.Description == "" {
					skill.Description = existing.Description
				}
				if skill.Name == "" {
					skill.Name = existing.Name
				}
			}
			skill.ID = id
			cfg.Skills[id] = skill
		}

		// Apply simple enable/disable toggles
		for id, enabled := range incoming.Toggles {
			if existing, exists := cfg.Skills[id]; exists {
				existing.Enabled = enabled
				cfg.Skills[id] = existing
			}
		}

		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"skills":  updated.Skills,
	})
}

// ---------------------------------------------------------------------------
// GET /api/settings/subagent-types
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSubagentTypesGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()

	// Get available providers (same format as /api/providers)
	providers := ws.listProviders(ws.resolveClientID(r))

	// Get current subagent provider and model from config
	currentProvider := cfg.GetSubagentProvider()
	currentModel := cfg.GetSubagentModel()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"subagent_types":      cfg.SubagentTypes,
		"available_providers": providers,
		"current_provider":    currentProvider,
		"current_model":       currentModel,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/subagent-types/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSubagentTypesPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/subagent-types/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "subagent type name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Accept only provider and model updates.
	// Use raw JSON to distinguish "field present (possibly empty string)" from "field absent".
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			return fmt.Errorf("subagent type %q not found", name)
		}
		if _, exists := cfg.SubagentTypes[name]; !exists {
			return fmt.Errorf("subagent type %q not found (use GET to list available types)", name)
		}

		existing := cfg.SubagentTypes[name]
		if v, ok := raw["provider"]; ok {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("provider must be a string")
			}
			// Empty string means "inherit from default subagent settings"
			existing.Provider = strings.TrimSpace(s)
		}
		if v, ok := raw["model"]; ok {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("model must be a string")
			}
			// Empty string means "inherit from default subagent settings"
			existing.Model = strings.TrimSpace(s)
		}
		cfg.SubagentTypes[name] = existing
		return nil
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "must be a string") {
			writeJSONError(w, http.StatusBadRequest, errMsg)
		} else {
			writeJSONError(w, http.StatusNotFound, errMsg)
		}
		return
	}

	updated := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"type":    updated.SubagentTypes[name],
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/subagent-types/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsSubagentTypesDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/subagent-types/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "subagent type name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			return fmt.Errorf("subagent type %q not found", name)
		}
		if _, exists := cfg.SubagentTypes[name]; !exists {
			return fmt.Errorf("subagent type %q not found", name)
		}
		delete(cfg.SubagentTypes, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateMCPServer(s mcp.MCPServerConfig) error {
	serverType := strings.TrimSpace(strings.ToLower(s.Type))
	if serverType == "http" {
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("URL is required for HTTP servers")
		}
	} else {
		// stdio (default)
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("command is required for stdio servers")
		}
	}
	if s.MaxRestarts < 0 {
		return fmt.Errorf("max_restarts must be non-negative")
	}
	if s.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	return nil
}

func validateCustomProvider(p configuration.CustomProviderConfig) error {
	if p.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	if p.Endpoint == "" {
		return fmt.Errorf("provider endpoint is required")
	}
	if p.ContextSize < 0 {
		return fmt.Errorf("context_size must be non-negative")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// extractPathSegment returns the trailing path segment after the given prefix.
// For example, extractPathSegment("/api/settings/mcp/servers/myserver", "/api/settings/mcp/servers/")
// returns "myserver".
func extractPathSegment(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	segment := path[len(prefix):]
	// Trim trailing slash if present
	segment = strings.TrimRight(segment, "/")
	return segment
}

// asInt attempts to convert a JSON-decoded value (float64 from json.Decoder) to an int.
func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
