//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const maxSettingsBodyBytes = 2 << 20 // 2 MiB

// ---------------------------------------------------------------------------
// Auto-truncation limits for settings string values
// ---------------------------------------------------------------------------
//
// When the frontend (or any API client) sends a settings value that exceeds
// the corresponding limit below, the server silently truncates it rather than
// rejecting the request.  This prevents accidentally saving megabytes of text
// into the config file (e.g. pasting a whole file into the system-prompt
// textarea) while keeping the UX smooth — the user's edit is accepted; only
// the tail is dropped.

const (
	// maxSettingEnumLength applies to fields that hold a small set of
	// predefined values (reasoning_effort, history_scope, etc.).
	maxSettingEnumLength = 64

	// maxSettingNameLength applies to identifiers and short labels
	// (provider name, model id, skill id/name).
	maxSettingNameLength = 256

	// maxSettingPathLength applies to filesystem paths.
	maxSettingPathLength = 4096

	// maxSettingPromptLength applies to large free-text fields such as
	// system_prompt_text.
	maxSettingPromptLength = 100_000

	// maxSettingDescriptionLength applies to medium free-text fields
	// (skill description, persona description).
	maxSettingDescriptionLength = 10_000

	// maxSettingURLLength applies to URLs and endpoint strings.
	maxSettingURLLength = 4096

	// maxSettingCommandLength applies to shell command strings.
	maxSettingCommandLength = 4096

	// maxSettingArgLength applies to individual CLI arguments.
	maxSettingArgLength = 4096

	// maxSettingGenericLength is the fallback for any string field that
	// doesn't have a more specific limit.
	maxSettingGenericLength = 1000

	// truncationEllipsis is appended when a value is truncated so that
	// the user (and logs) can see the cut happened.
	truncationEllipsis = "\n...[truncated]"
)

// truncateString clips s to maxLen characters.  If truncation occurs the
// returned string ends with truncationEllipsis (which is counted toward
// maxLen so the result is always ≤ maxLen runes).
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Work in runes to avoid breaking multi-byte characters.
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	ell := []rune(truncationEllipsis)
	if maxLen <= len(ell) {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-len(ell)]) + truncationEllipsis
}

// ---------------------------------------------------------------------------
// Validation sets
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

// ---------------------------------------------------------------------------
// Sanitization helpers
// ---------------------------------------------------------------------------

// sanitizedConfig returns a JSON-serializable map of the current config with
// sensitive fields stripped so they are never sent to the browser.
func sanitizedConfig(cfg *configuration.Config) map[string]interface{} {
	out := map[string]interface{}{
		"version":                        cfg.Version,
		"last_used_provider":             cfg.LastUsedProvider,
		"provider_models":                cfg.ProviderModels,
		"provider_priority":              cfg.ProviderPriority,
		"mcp":                            mcp.RedactMCPConfig(cfg.MCP),
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
		// SubagentTypes is catalog-derived and never persisted — exposed via
		// GET /api/settings/subagent-types for the persona list view. Keeping
		// it out of the generic settings payload prevents round-trip PUTs
		// from accidentally writing catalog state back through user config.
		"default_subagent_persona":       cfg.DefaultSubagentPersona,
		"disabled_personas":              cfg.DisabledPersonas,
		"subagent_max_parallel":          cfg.GetSubagentMaxParallel(),
		"subagent_parallel_enabled":      cfg.GetSubagentParallelEnabled(),
		"commit_provider":                cfg.CommitProvider,
		"commit_model":                   cfg.CommitModel,
		"review_provider":                cfg.ReviewProvider,
		"review_model":                   cfg.ReviewModel,
		"pdf_ocr_enabled":                cfg.PDFOCREnabled,
		"pdf_ocr_provider":               cfg.PDFOCRProvider,
		"pdf_ocr_model":                  cfg.PDFOCRModel,
		"skills":                         cfg.Skills,
		"disable_thinking":              cfg.DisableThinking,
		"enable_zsh_command_detection":   cfg.EnableZshCommandDetection,
		"auto_execute_detected_commands": cfg.AutoExecuteDetectedCommands,
		"embedding_index":                cfg.EmbeddingIndex,
		"ea_mode":                        cfg.EAMode,
		"subagent_max_depth":             cfg.SubagentMaxDepth,
		"approved_shell_commands":        cfg.ApprovedShellCommands,
		"language_servers":               cfg.LanguageServers,
		"security_policy":                cfg.SecurityPolicy,
		"persistent_context":             cfg.PersistentContext,
		// SP-058: risk profile + per-profile overrides. The single-
		// value selector is editable via the settings UI; the
		// per-profile map is read-only here (advanced; edit
		// config.json directly for now).
		"risk_profile":  cfg.RiskProfile,
		"risk_profiles": cfg.RiskProfiles,
	}
	return out
}

// sanitizedCustomProviders returns a defensive copy of the providers map.
// The CustomProviderConfig struct no longer contains sensitive fields (API keys are
// stored in the credential store), but the copy prevents accidental mutation of
// the in-memory config.
func sanitizedCustomProviders(providers map[string]configuration.CustomProviderConfig) map[string]configuration.CustomProviderConfig {
	if providers == nil {
		return nil
	}
	clean := make(map[string]configuration.CustomProviderConfig, len(providers))
	for k, v := range providers {
		clean[k] = v
	}
	return clean
}

// ---------------------------------------------------------------------------
// JSON response helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Config manager resolver
// ---------------------------------------------------------------------------

// getConfigManager safely resolves the config manager from the request client's
// live agent, or sends a 503 and returns nil.
func (ws *ReactWebServer) getConfigManager(r *http.Request, w http.ResponseWriter) *configuration.Manager {
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err == nil && agentInst != nil && agentInst.GetConfigManager() != nil {
		return agentInst.GetConfigManager()
	}
	// Fallback: create a config manager using layered approach.
	// This ensures per-client isolation even when no agent exists yet.
	// Settings writes will go to the session layer (most specific).
	//
	// This covers:
	//   • ErrNoProviderConfigured (no provider set up yet)
	//   • Provider config errors (key missing, editor mode, etc.)
	//   • Agent creation failures that don't match the provider-error patterns
	//     (e.g. "create agent in workspace: ...", "failed to initialize configuration: ...")
	//   • Agent exists but GetConfigManager() is nil (err is nil in that case)

	// Get base directory (global config location)
	configBase, configErr := configuration.GetConfigDir()
	if configErr != nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "config_unavailable",
			"Configuration directory not available")
		return nil
	}

	// Get workspace directory if workspace is available
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	var workspaceDir string
	if workspaceRoot != "" {
		workspaceDir = filepath.Join(workspaceRoot, configuration.ConfigDirName)
	}

	cm, createErr := configuration.NewManagerWithLayers(configBase, workspaceDir)
	if createErr != nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "config_unavailable",
			"Configuration manager is not available")
		return nil
	}
	return cm
}

// resolveConfigManagerQuietly is like getConfigManager but does NOT write any
// response to the http.ResponseWriter. It returns nil when the config manager
// cannot be resolved (e.g. no agent, config dir unavailable).
//
// This is useful for read-only checks where writing an error response would
// corrupt the response stream before the caller has a chance to send its own
// response.
func (ws *ReactWebServer) resolveConfigManagerQuietly(r *http.Request) *configuration.Manager {
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err == nil && agentInst != nil && agentInst.GetConfigManager() != nil {
		return agentInst.GetConfigManager()
	}

	// Fallback: create a config manager using layered approach.
	configBase, configErr := configuration.GetConfigDir()
	if configErr != nil {
		return nil
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	var workspaceDir string
	if workspaceRoot != "" {
		workspaceDir = filepath.Join(workspaceRoot, configuration.ConfigDirName)
	}

	cm, createErr := configuration.NewManagerWithLayers(configBase, workspaceDir)
	if createErr != nil {
		return nil
	}
	return cm
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Auto-truncation for config structs
// ---------------------------------------------------------------------------

// truncateConfigStrings applies per-field truncation limits to every string
// field on cfg that could originate from user input.  The function mutates
// cfg in place and returns it for convenience.
func truncateConfigStrings(cfg *configuration.Config) *configuration.Config {
	// --- Top-level scalar strings ---
	cfg.ReasoningEffort = truncateString(cfg.ReasoningEffort, maxSettingEnumLength)
	cfg.HistoryScope = truncateString(cfg.HistoryScope, maxSettingEnumLength)
	cfg.SelfReviewGateMode = truncateString(cfg.SelfReviewGateMode, maxSettingEnumLength)
	cfg.ResourceDirectory = truncateString(cfg.ResourceDirectory, maxSettingPathLength)
	cfg.SystemPromptText = truncateString(cfg.SystemPromptText, maxSettingPromptLength)
	cfg.LastUsedProvider = truncateString(cfg.LastUsedProvider, maxSettingNameLength)
	cfg.SubagentProvider = truncateString(cfg.SubagentProvider, maxSettingNameLength)
	cfg.SubagentModel = truncateString(cfg.SubagentModel, maxSettingNameLength)
	cfg.CommitProvider = truncateString(cfg.CommitProvider, maxSettingNameLength)
	cfg.CommitModel = truncateString(cfg.CommitModel, maxSettingNameLength)

	// --- Custom providers ---
	for i, p := range cfg.CustomProviders {
		cfg.CustomProviders[i] = truncateCustomProvider(p)
	}

	// --- Subagent types ---
	for name, st := range cfg.SubagentTypes {
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
		cfg.SubagentTypes[name] = st
	}

	// --- MCP ---
	truncateMCPConfig(&cfg.MCP)

	return cfg
}

// truncateCustomProvider truncates string fields of a CustomProviderConfig.
func truncateCustomProvider(p configuration.CustomProviderConfig) configuration.CustomProviderConfig {
	p.Name = truncateString(p.Name, maxSettingNameLength)
	p.Endpoint = truncateString(p.Endpoint, maxSettingURLLength)
	p.EnvVar = truncateString(p.EnvVar, maxSettingNameLength)
	p.ModelName = truncateString(p.ModelName, maxSettingNameLength)
	p.ReasoningEffort = truncateString(p.ReasoningEffort, maxSettingEnumLength)
	p.VisionModel = truncateString(p.VisionModel, maxSettingNameLength)
	p.VisionFallbackProvider = truncateString(p.VisionFallbackProvider, maxSettingNameLength)
	p.VisionFallbackModel = truncateString(p.VisionFallbackModel, maxSettingNameLength)
	for i, m := range p.ToolCalls {
		p.ToolCalls[i] = truncateString(m, maxSettingNameLength)
	}
	return p
}

// truncateMCPConfig truncates string fields of MCP server configs.
func truncateMCPConfig(mc *mcp.MCPConfig) {
	for name, srv := range mc.Servers {
		srv.Name = truncateString(srv.Name, maxSettingNameLength)
		srv.Command = truncateString(srv.Command, maxSettingCommandLength)
		srv.URL = truncateString(srv.URL, maxSettingURLLength)
		srv.WorkingDir = truncateString(srv.WorkingDir, maxSettingPathLength)
		for i, a := range srv.Args {
			srv.Args[i] = truncateString(a, maxSettingArgLength)
		}
		for k, v := range srv.Env {
			srv.Env[k] = truncateString(v, maxSettingGenericLength)
		}
		mc.Servers[name] = srv
	}
}

// truncateSkill truncates string fields of a Skill.
func truncateSkill(s configuration.Skill) configuration.Skill {
	s.ID = truncateString(s.ID, maxSettingNameLength)
	s.Name = truncateString(s.Name, maxSettingNameLength)
	s.Description = truncateString(s.Description, maxSettingDescriptionLength)
	s.Path = truncateString(s.Path, maxSettingPathLength)
	s.AllowedTools = truncateString(s.AllowedTools, maxSettingGenericLength)
	for k, v := range s.Metadata {
		s.Metadata[k] = truncateString(v, maxSettingGenericLength)
	}
	return s
}
