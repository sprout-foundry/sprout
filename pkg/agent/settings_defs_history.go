package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// settings_defs_history.go — the paths, history & EA, output, commit, and
// review sections of the settingDefs registry. Split out of settings_defs.go.
var settingDefsHistory = []settingDef{
	// --- Paths & Directories ---
	{
		Key:         "resource_directory",
		Description: "Directory for captured web/vision resources",
		ValidValues: "any valid file path",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ResourceDirectory },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.ResourceDirectory = value
			return nil
		},
	},
	// --- History & EA ---
	{
		Key:         "history_scope",
		Description: "Change history scope",
		ValidValues: "project, global",
		GetValue:    func(cfg *configuration.Config) string { return cfg.HistoryScope },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "project", "global", "":
				cfg.HistoryScope = strings.ToLower(value)
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("history_scope must be project or global, got %q", value), nil)
			}
		},
		EnumValues: []string{"project", "global"},
	},
	{
		Key:         "subagent_provider",
		Description: "Provider used for subagents",
		ValidValues: "provider name or empty to inherit from provider",
		GetValue:    func(cfg *configuration.Config) string { return cfg.SubagentProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.SubagentProvider = value
			return nil
		},
	},
	{
		Key:         "subagent_model",
		Description: "Model used for subagents",
		ValidValues: "provider-specific model name or empty to use provider default",
		GetValue:    func(cfg *configuration.Config) string { return cfg.SubagentModel },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.SubagentModel = value
			return nil
		},
	},
	{
		Key:         "default_subagent_persona",
		Description: "Persona used when run_subagent is invoked without a persona argument",
		ValidValues: "persona ID (e.g. general, coder, reviewer) or empty to fall back to 'general'",
		GetValue:    func(cfg *configuration.Config) string { return cfg.DefaultSubagentPersona },
		SetValue: func(cfg *configuration.Config, value string) error {
			v := strings.TrimSpace(value)
			if v != "" && cfg.GetSubagentType(v) == nil {
				return agenterrors.NewValidation(fmt.Sprintf("default_subagent_persona %q is not a known persona ID or alias", v), nil)
			}
			cfg.DefaultSubagentPersona = v
			return nil
		},
	},
	{
		Key:         "disabled_personas",
		Description: "Comma-separated persona IDs hidden from /persona list and subagent spawning",
		ValidValues: "comma-separated persona IDs (e.g. researcher,coder) or empty to enable all",
		GetValue: func(cfg *configuration.Config) string {
			return strings.Join(cfg.DisabledPersonas, ",")
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			var ids []string
			for _, raw := range strings.Split(value, ",") {
				trimmed := strings.TrimSpace(raw)
				if trimmed == "" {
					continue
				}
				if cfg.GetSubagentType(trimmed) == nil && !cfg.IsPersonaDisabled(trimmed) {
					return agenterrors.NewValidation(fmt.Sprintf("disabled_personas: %q is not a known persona ID or alias", trimmed), nil)
				}
				ids = append(ids, trimmed)
			}
			cfg.DisabledPersonas = ids
			return nil
		},
	},
	{
		Key:         "subagent_max_parallel",
		Description: "Maximum number of parallel subagents",
		ValidValues: "1-8",
		GetValue:    func(cfg *configuration.Config) string { return strconv.Itoa(cfg.SubagentMaxParallel) },
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_parallel must be an integer, got %q", value), nil)
			}
			if val < 1 || val > 8 {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_parallel must be between 1 and 8, got %d", val), nil)
			}
			cfg.SubagentMaxParallel = val
			return nil
		},
	},
	{
		Key:         "subagent_parallel_enabled",
		Description: "Enable parallel subagent execution",
		ValidValues: "true, false",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.SubagentParallelEnabled != nil {
				return fmt.Sprintf("%v", *cfg.SubagentParallelEnabled)
			}
			return "false"
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				t := true
				cfg.SubagentParallelEnabled = &t
				return nil
			case "false":
				f := false
				cfg.SubagentParallelEnabled = &f
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("subagent_parallel_enabled must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
	{
		Key:         "subagent_max_depth",
		Description: "Maximum subagent nesting depth",
		ValidValues: "1-4",
		GetValue:    func(cfg *configuration.Config) string { return strconv.Itoa(cfg.SubagentMaxDepth) },
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_depth must be an integer, got %q", value), nil)
			}
			if val < 1 || val > 4 {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_depth must be between 1 and 4, got %d", val), nil)
			}
			cfg.SubagentMaxDepth = val
			return nil
		},
	},
	// --- Output ---
	{
		Key:         "output_verbosity",
		Description: "How much inter-tool-call narration the UI shows",
		ValidValues: "compact, default, verbose",
		GetValue:    func(cfg *configuration.Config) string { return cfg.OutputVerbosity },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "compact", "default", "verbose", "":
				cfg.OutputVerbosity = strings.ToLower(value)
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("output_verbosity must be compact, default, or verbose, got %q", value), nil)
			}
		},
		EnumValues: []string{"compact", "default", "verbose"},
	},
	// --- Commit ---
	{
		Key:         "commit_provider",
		Description: "Provider for commit message generation",
		ValidValues: "provider name or empty to inherit from provider",
		GetValue:    func(cfg *configuration.Config) string { return cfg.CommitProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.CommitProvider = value
			return nil
		},
	},
	{
		Key:         "commit_model",
		Description: "Model for commit message generation",
		ValidValues: "provider-specific model name or empty to use provider default",
		GetValue:    func(cfg *configuration.Config) string { return cfg.CommitModel },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.CommitModel = value
			return nil
		},
	},
	// --- Review ---
	{
		Key:         "review_provider",
		Description: "Provider for code review commands",
		ValidValues: "provider name or empty to inherit from provider",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ReviewProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.ReviewProvider = value
			return nil
		},
	},
	{
		Key:         "review_model",
		Description: "Model for code review commands",
		ValidValues: "provider-specific model name or empty to use provider default",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ReviewModel },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.ReviewModel = value
			return nil
		},
	},
}
