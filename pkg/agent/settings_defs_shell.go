package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// settings_defs_shell.go — the zsh, shell-command-allowlist, risk-profile,
// max-context, and tool-invocation-display sections of the settingDefs
// registry. Split out of settings_defs.go.
var settingDefsShell = []settingDef{
	// --- Zsh ---
	{
		Key:         "enable_zsh_command_detection",
		Description: "Enable zsh-aware command detection",
		ValidValues: "true, false",
		GetValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.EnableZshCommandDetection) },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				cfg.EnableZshCommandDetection = true
				return nil
			case "false":
				cfg.EnableZshCommandDetection = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("enable_zsh_command_detection must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
	// --- Shell Command Allowlists ---
	{
		Key:         "approved_shell_commands",
		Description: "Always-approved shell commands (exact match)",
		ValidValues: "comma-separated list of shell command strings",
		GetValue: func(cfg *configuration.Config) string {
			return strings.Join(cfg.ApprovedShellCommands, ",")
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			if value == "" {
				cfg.ApprovedShellCommands = nil
				return nil
			}
			var cmds []string
			for _, raw := range strings.Split(value, ",") {
				trimmed := strings.TrimSpace(raw)
				if trimmed == "" {
					continue
				}
				cmds = append(cmds, trimmed)
			}
			cfg.ApprovedShellCommands = cmds
			return nil
		},
		ListType: true,
	},
	{
		Key:         "approved_shell_command_patterns",
		Description: "Always-approved shell command glob patterns",
		ValidValues: "comma-separated list of glob patterns",
		GetValue: func(cfg *configuration.Config) string {
			return strings.Join(cfg.ApprovedShellCommandPatterns, ",")
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			if value == "" {
				cfg.ApprovedShellCommandPatterns = nil
				return nil
			}
			var patterns []string
			for _, raw := range strings.Split(value, ",") {
				trimmed := strings.TrimSpace(raw)
				if trimmed == "" {
					continue
				}
				patterns = append(patterns, trimmed)
			}
			cfg.ApprovedShellCommandPatterns = patterns
			return nil
		},
		ListType: true,
	},
	// --- Risk Profile ---
	{
		Key:         "risk_profile",
		Description: "Shell-command risk cascade profile",
		ValidValues: "readonly, cautious, default, permissive, unrestricted, or any user-defined name in risk_profiles",
		GetValue:    func(cfg *configuration.Config) string { return cfg.RiskProfile },
		SetValue: func(cfg *configuration.Config, value string) error {
			v := strings.ToLower(value)
			// Accept the five built-in names plus any user-defined
			// profile present in cfg.RiskProfiles (checked via the
			// config-aware predicate so custom profiles resolve).
			if configuration.IsValidRiskProfileWithConfig(v, cfg) {
				cfg.RiskProfile = v
				return nil
			}
			return agenterrors.NewValidation(fmt.Sprintf("risk_profile must be readonly, cautious, default, permissive, unrestricted, or a user-defined name in risk_profiles, got %q", value), nil)
		},
		EnumValues: []string{"readonly", "cautious", "default", "permissive", "unrestricted"},
	},
	// --- Max Context ---
	{
		Key:         "max_context_tokens",
		Description: "Max context token cap for cost control (0 = no cap)",
		ValidValues: "0 or integer >= 1024",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.MaxContextTokens != nil {
				return strconv.Itoa(*cfg.MaxContextTokens)
			}
			return "0"
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("max_context_tokens must be an integer, got %q", value), nil)
			}
			if val < 0 {
				return agenterrors.NewValidation(fmt.Sprintf("max_context_tokens must be >= 0, got %d", val), nil)
			}
			if val > 0 && val < 1024 {
				return agenterrors.NewValidation(fmt.Sprintf("max_context_tokens must be at least 1024 when setting a cap, got %d", val), nil)
			}
			if val == 0 {
				cfg.MaxContextTokens = nil
			} else {
				cfg.MaxContextTokens = &val
			}
			return nil
		},
	},
	// --- Tool Invocation Display ---
	{
		Key:         "show_tool_invocations",
		Description: "Show/hide per-tool invocation details in the UI",
		ValidValues: "true, false",
		GetValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.ShowToolInvocations) },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				cfg.ShowToolInvocations = true
				return nil
			case "false":
				cfg.ShowToolInvocations = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("show_tool_invocations must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
}
