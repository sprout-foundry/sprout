package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const maxSettingsBodyBytes = 2 << 20 // 2 MiB

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
		"mcp":                            cfg.MCP,
		"enable_pre_write_validation":    cfg.EnablePreWriteValidation,
		"allow_orchestrator_git_write":   cfg.AllowOrchestratorGitWrite,
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

// sanitizedCustomProviders returns a copy of the providers map.
// Sensitive fields (if any) are excluded from the CustomProviderConfig JSON tags.
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
	if err != nil || agentInst == nil || agentInst.GetConfigManager() == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "config_unavailable", "Configuration manager is not available")
		return nil
	}
	return agentInst.GetConfigManager()
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
