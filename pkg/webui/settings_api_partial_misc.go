//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// settings_api_partial_misc.go — the "misc" partial-settings helpers
// (toggles/routing fields without a dedicated domain tab), split out of the
// original settings_api_partial_settings.go. Pure move; same signature
// contract as the rest of the set.

// ---------------------------------------------------------------------------
// Misc toggles and routing fields without a dedicated domain tab
// ---------------------------------------------------------------------------

func applyMiscAgentSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["context_mode"]; ok {
		knownKeys["context_mode"] = true
		s, _ := v.(string)
		switch configuration.ContextMode(s) {
		case "", configuration.ContextModeFull, configuration.ContextModeLowContext:
			cfg.ContextMode = configuration.ContextMode(s)
		default:
			return fmt.Errorf("invalid context_mode %q: must be \"\", \"full\", or \"low_context\"", s)
		}
	}
	if v, ok := patch["show_tool_invocations"]; ok {
		knownKeys["show_tool_invocations"] = true
		cfg.ShowToolInvocations, _ = v.(bool)
	}
	if v, ok := patch["disable_update_check"]; ok {
		knownKeys["disable_update_check"] = true
		cfg.DisableUpdateCheck, _ = v.(bool)
	}
	if v, ok := patch["refresh_system_prompt_on_model_change"]; ok {
		knownKeys["refresh_system_prompt_on_model_change"] = true
		cfg.RefreshSystemPromptOnModelChange, _ = v.(bool)
	}
	if v, ok := patch["allow_git_history_rewrite"]; ok {
		knownKeys["allow_git_history_rewrite"] = true
		cfg.AllowGitHistoryRewrite, _ = v.(bool)
	}
	if v, ok := patch["unified_risk_resolver"]; ok {
		knownKeys["unified_risk_resolver"] = true
		cfg.UnifiedRiskResolver, _ = v.(bool)
	}
	if v, ok := patch["daemon_multi_session"]; ok {
		knownKeys["daemon_multi_session"] = true
		cfg.DaemonMultiSession, _ = v.(bool)
	}
	if v, ok := patch["coordinator_auto_activate"]; ok {
		knownKeys["coordinator_auto_activate"] = true
		cfg.CoordinatorAutoActivate, _ = v.(bool)
	}
	if v, ok := patch["disable_coordinator_auto_activate"]; ok {
		knownKeys["disable_coordinator_auto_activate"] = true
		cfg.DisableCoordinatorAutoActivate, _ = v.(bool)
	}
	if v, ok := patch["completion_provider"]; ok {
		knownKeys["completion_provider"] = true
		s, _ := v.(string)
		cfg.CompletionProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["completion_model"]; ok {
		knownKeys["completion_model"] = true
		s, _ := v.(string)
		cfg.CompletionModel = truncateString(s, maxSettingNameLength)
	}
	return nil
}

// applyShellAllowlistSettings mirrors approved_shell_commands (already
// handled by the risk/safety applier) for the wildcard-pattern twin.
func applyShellAllowlistSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["approved_shell_command_patterns"]; ok {
		knownKeys["approved_shell_command_patterns"] = true
		if arr, ok := v.([]interface{}); ok {
			patterns := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					trimmed := strings.TrimSpace(truncateString(s, maxSettingNameLength))
					if trimmed != "" {
						patterns = append(patterns, trimmed)
					}
				}
			}
			cfg.ApprovedShellCommandPatterns = patterns
		}
	}
	return nil
}

func applyVisionPipelineSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["vision"]; ok {
		knownKeys["vision"] = true
		if v == nil {
			cfg.Vision = nil
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid vision: %w", err)
		}
		var vc configuration.VisionConfig
		if err := json.Unmarshal(raw, &vc); err != nil {
			return fmt.Errorf("invalid vision: %w", err)
		}
		cfg.Vision = &vc
	}
	return nil
}

func applyNotificationsAgentSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	// Notifications is agent-side (turn-completion bell/OS notify); the
	// WebUI's own NotificationsSettingsTab is browser-local and unrelated.
	if v, ok := patch["notifications"]; ok {
		knownKeys["notifications"] = true
		if v == nil {
			cfg.Notifications = nil
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid notifications: %w", err)
		}
		var nc configuration.NotificationsConfig
		if err := json.Unmarshal(raw, &nc); err != nil {
			return fmt.Errorf("invalid notifications: %w", err)
		}
		cfg.Notifications = &nc
	}
	return nil
}

func applyEditApprovalSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["edit_approval"]; ok {
		knownKeys["edit_approval"] = true
		if v == nil {
			cfg.EditApproval = nil
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid edit_approval: %w", err)
		}
		var ec configuration.EditApprovalConfig
		if err := json.Unmarshal(raw, &ec); err != nil {
			return fmt.Errorf("invalid edit_approval: %w", err)
		}
		cfg.EditApproval = &ec
	}
	return nil
}

func applyChangeTrackingSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["change_tracking"]; ok {
		knownKeys["change_tracking"] = true
		if v == nil {
			cfg.ChangeTracking = nil
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid change_tracking: %w", err)
		}
		var tc configuration.ChangeTrackingConfig
		if err := json.Unmarshal(raw, &tc); err != nil {
			return fmt.Errorf("invalid change_tracking: %w", err)
		}
		cfg.ChangeTracking = &tc
	}
	return nil
}

func applyTrainingSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["training"]; ok {
		knownKeys["training"] = true
		if v == nil {
			cfg.Training = configuration.TrainingConfig{}
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid training: %w", err)
		}
		var tc configuration.TrainingConfig
		if err := json.Unmarshal(raw, &tc); err != nil {
			return fmt.Errorf("invalid training: %w", err)
		}
		cfg.Training = tc
	}
	return nil
}

// partialSettingsApplier is the ordered list of per-domain helpers invoked by
// applyPartialSettings. Order is not significant (each helper owns a disjoint
// set of keys) but reading it top-to-bottom roughly tracks the settings UI.
