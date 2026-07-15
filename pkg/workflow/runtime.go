//go:build !js

package workflow

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func ApplyWorkflowRuntimeOverrides(chatAgent *agent.Agent, runtime AgentWorkflowRuntime, overrides *CLIOverrides) error {
	if chatAgent == nil {
		return errors.New("agent is required")
	}

	cfg := chatAgent.GetConfig()
	if cfg == nil {
		return errors.New("agent config is unavailable")
	}

	if runtime.SkipPrompt != nil || NormalizeReasoningEffort(runtime.ReasoningEffort) != "" {
		normalized := NormalizeReasoningEffort(runtime.ReasoningEffort)
		if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			if runtime.SkipPrompt != nil {
				cfg.SkipPrompt = *runtime.SkipPrompt
			}
			if normalized != "" {
				cfg.ReasoningEffort = normalized
				currentProvider := strings.TrimSpace(chatAgent.GetProvider())
				if currentProvider != "" && cfg.CustomProviders != nil {
					if providerCfg, ok := cfg.CustomProviders[currentProvider]; ok {
						providerCfg.ReasoningEffort = normalized
						cfg.CustomProviders[currentProvider] = providerCfg
					}
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to apply workflow runtime overrides: %w", err)
		}
	}
	if runtime.MaxIterations != nil {
		chatAgent.SetMaxIterations(*runtime.MaxIterations)
	}
	if runtime.Unsafe != nil {
		chatAgent.SetUnsafeMode(*runtime.Unsafe)
	}
	if runtime.NoStream != nil && overrides != nil && overrides.SetNoStream != nil {
		overrides.SetNoStream(*runtime.NoStream)
	}
	if runtime.DryRun != nil {
		if *runtime.DryRun {
			_ = configuration.SetEnv("DRY_RUN", "1")
		} else {
			configuration.UnsetEnv("DRY_RUN")
		}
	}
	if runtime.NoSubagents != nil {
		if *runtime.NoSubagents {
			_ = configuration.SetEnv("NO_SUBAGENTS", "1")
		} else {
			configuration.UnsetEnv("NO_SUBAGENTS")
		}
	}
	if runtime.ResourceDirectory != "" {
		_ = configuration.SetEnv("RESOURCE_DIRECTORY", runtime.ResourceDirectory)
	}

	// SP-058: per-step risk-profile override. Accepts a built-in
	// profile name OR a user-defined name from config.risk_profiles.
	// Empty string preserves the prior setting. Unrecognized names
	// (no built-in AND no override) are warned about.
	if runtime.RiskProfile != "" {
		_, hasUserOverride := func() (configuration.AutoApproveRules, bool) {
			if cfg == nil || cfg.RiskProfiles == nil {
				return configuration.AutoApproveRules{}, false
			}
			v, ok := cfg.RiskProfiles[runtime.RiskProfile]
			return v, ok
		}()
		if configuration.IsValidRiskProfile(runtime.RiskProfile) || hasUserOverride {
			chatAgent.SetRiskProfileOverride(configuration.RiskProfile(runtime.RiskProfile))
		} else {
			fmt.Fprintf(os.Stderr, "Warning: unknown workflow risk_profile %q. Built-in: readonly, cautious, default, permissive, unrestricted. Define custom profiles in config.risk_profiles.\n", runtime.RiskProfile)
		}
	}

	if runtime.Provider != "" {
		clientType, err := configuration.MapProviderStringToClientType(cfg, runtime.Provider)
		if err != nil {
			return fmt.Errorf("invalid provider %q: %w", runtime.Provider, err)
		}
		if err := chatAgent.SetProviderPersisted(api.ClientType(clientType)); err != nil {
			return fmt.Errorf("failed to set provider %q: %w", runtime.Provider, err)
		}
	}

	if runtime.Model != "" {
		if err := chatAgent.SetModelPersisted(runtime.Model); err != nil {
			return fmt.Errorf("failed to set model %q: %w", runtime.Model, err)
		}
	}

	systemPrompt, err := ResolveWorkflowTextOrFile(runtime.SystemPrompt, runtime.SystemPromptFile, "system_prompt")
	if err != nil {
		return fmt.Errorf("failed to resolve workflow system prompt: %w", err)
	}
	if systemPrompt != "" {
		chatAgent.SetSystemPrompt(systemPrompt)
		chatAgent.SetBaseSystemPrompt(chatAgent.GetSystemPrompt())
	}

	if runtime.Persona != "" {
		if err := chatAgent.ApplyPersona(runtime.Persona); err != nil {
			return fmt.Errorf("failed to apply persona %q: %w", runtime.Persona, err)
		}
	}

	if len(runtime.SubagentOverrides) > 0 {
		if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			if cfg.SubagentTypes == nil {
				return nil
			}
			ApplyWorkflowSubagentOverrides(cfg.SubagentTypes, runtime.SubagentOverrides)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to apply workflow runtime overrides: %w", err)
		}

		// Path B fix: if no global subagent defaults are set, seed them from the
		// overrides so no-persona run_subagent calls inside the workflow still
		// pick up a workflow-appropriate model instead of inheriting the
		// coordinator's primary provider/model.
		if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			if cfg.SubagentProvider != "" || cfg.SubagentModel != "" {
				return nil
			}
			pick := pickSubagentDefault(runtime.SubagentOverrides)
			if pick.Provider != "" {
				cfg.SubagentProvider = pick.Provider
			}
			if pick.Model != "" {
				cfg.SubagentModel = pick.Model
			}
			if pick.Provider != "" || pick.Model != "" {
				log.Printf("[workflow] global subagent defaults not set — seeding from overrides: provider=%s model=%s", pick.Provider, pick.Model)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to seed global subagent defaults: %w", err)
		}
	}

	return nil
}

func ApplyWorkflowInitialOverrides(chatAgent *agent.Agent, cfg *AgentWorkflowConfig, overrides *CLIOverrides) error {
	if cfg == nil || cfg.Initial == nil {
		return nil
	}
	return ApplyWorkflowRuntimeOverrides(chatAgent, cfg.Initial.AgentWorkflowRuntime, overrides)
}

// pickSubagentDefault selects a deterministic override entry to use as the
// global subagent default when no global SubagentProvider/SubagentModel is set.
// Preference order: orchestrator → coder → first entry alphabetically.
// Persona IDs are normalized (lowercase, hyphens→underscores) before matching
// to stay consistent with ApplyWorkflowSubagentOverrides / GetSubagentType.
func pickSubagentDefault(overrides WorkflowSubagentOverrides) WorkflowSubagentOverride {
	// Prefer orchestrator, then coder — match against normalized keys so
	// "Orchestrator", "ORCHESTRATOR", "repo_orchestrator" all resolve.
	wanted := []string{"orchestrator", "coder"}
	for _, target := range wanted {
		for key, v := range overrides {
			if NormalizeWorkflowPersonaID(key) != target {
				continue
			}
			if v.Provider != "" || v.Model != "" {
				return v
			}
		}
	}
	// Fall back to first entry alphabetically.
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := overrides[k]
		if v.Provider != "" || v.Model != "" {
			return v
		}
	}
	return WorkflowSubagentOverride{}
}

type workflowRuntimeSnapshot struct {
	Provider                string
	Model                   string
	Persona                 string
	ReasoningEffort         string
	SkipPrompt              bool
	Unsafe                  bool
	MaxIterations           int
	NoStream                bool
	DryRunEnv               string
	NoSubagentsEnv          string
	ResourceDirectoryEnv    string
	SystemPrompt            string
	CustomReasoningEfforts  map[string]string
	SubagentOverridesBackup map[string]struct {
		OriginalProvider string
		OriginalModel    string
		OriginalKey      string
	}
}

func PrepareWorkflowRuntimeRestorer(chatAgent *agent.Agent, cfg *AgentWorkflowConfig, overrides *CLIOverrides) (func() error, error) {
	if cfg == nil || cfg.ShouldPersistRuntimeOverrides() {
		return nil, nil
	}
	if chatAgent == nil {
		return nil, errors.New("agent is required")
	}

	agentCfg := chatAgent.GetConfig()
	if agentCfg == nil {
		return nil, errors.New("agent config is unavailable")
	}

	snapshot := workflowRuntimeSnapshot{
		Provider:               strings.TrimSpace(chatAgent.GetProvider()),
		Model:                  strings.TrimSpace(chatAgent.GetModel()),
		Persona:                strings.TrimSpace(chatAgent.GetActivePersona()),
		ReasoningEffort:        agentCfg.ReasoningEffort,
		SkipPrompt:             agentCfg.SkipPrompt,
		Unsafe:                 chatAgent.GetUnsafeMode(),
		MaxIterations:          chatAgent.GetMaxIterations(),
		DryRunEnv:              configuration.GetEnvSimple("DRY_RUN"),
		NoSubagentsEnv:         configuration.GetEnvSimple("NO_SUBAGENTS"),
		ResourceDirectoryEnv:   configuration.GetEnvSimple("RESOURCE_DIRECTORY"),
		SystemPrompt:           chatAgent.GetSystemPrompt(),
		CustomReasoningEfforts: map[string]string{},
	}
	if overrides != nil && overrides.GetNoStream != nil {
		snapshot.NoStream = overrides.GetNoStream()
	}
	for providerName, providerCfg := range agentCfg.CustomProviders {
		snapshot.CustomReasoningEfforts[providerName] = providerCfg.ReasoningEffort
	}

	// Snapshot SubagentTypes entries that will be overwritten by any step's subagent_overrides.
	snapshot.SubagentOverridesBackup = make(map[string]struct {
		OriginalProvider string
		OriginalModel    string
		OriginalKey      string
	})
	allOverrides := make(WorkflowSubagentOverrides)
	if cfg.Initial != nil {
		for k, v := range cfg.Initial.SubagentOverrides {
			allOverrides[k] = v
		}
	}
	for _, step := range cfg.Steps {
		for k, v := range step.SubagentOverrides {
			allOverrides[k] = v
		}
	}
	if agentCfg.SubagentTypes != nil {
		for personaID := range allOverrides {
			normalizedID := NormalizeWorkflowPersonaID(personaID)
			if normalizedID == "" {
				continue
			}
			mapKey, found := FindSubagentTypeMapKey(agentCfg.SubagentTypes, normalizedID)
			if !found {
				continue
			}
			st := agentCfg.SubagentTypes[mapKey]
			snapshot.SubagentOverridesBackup[normalizedID] = struct {
				OriginalProvider string
				OriginalModel    string
				OriginalKey      string
			}{
				OriginalProvider: st.Provider,
				OriginalModel:    st.Model,
				OriginalKey:      mapKey,
			}
		}
	}

	restore := func() error {
		var restoreErrors []error

		if snapshot.Provider != "" && !strings.EqualFold(strings.TrimSpace(chatAgent.GetProvider()), snapshot.Provider) {
			if err := ApplyWorkflowRuntimeOverrides(chatAgent, AgentWorkflowRuntime{Provider: snapshot.Provider}, overrides); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("failed to restore provider %q: %w", snapshot.Provider, err))
			}
		}
		if snapshot.Model != "" && strings.TrimSpace(chatAgent.GetModel()) != snapshot.Model {
			if err := chatAgent.SetModelPersisted(snapshot.Model); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("failed to restore model %q: %w", snapshot.Model, err))
			}
		}
		currentPersona := strings.TrimSpace(chatAgent.GetActivePersona())
		if snapshot.Persona == "" && currentPersona != "" {
			chatAgent.ClearActivePersona()
		} else if snapshot.Persona != "" && !strings.EqualFold(currentPersona, snapshot.Persona) {
			if err := chatAgent.ApplyPersona(snapshot.Persona); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("failed to restore persona %q: %w", snapshot.Persona, err))
			}
		}

		currentCfg := chatAgent.GetConfig()
		if currentCfg != nil {
			currentCfg.ReasoningEffort = snapshot.ReasoningEffort
			currentCfg.SkipPrompt = snapshot.SkipPrompt
			for providerName, providerCfg := range currentCfg.CustomProviders {
				originalReasoning, exists := snapshot.CustomReasoningEfforts[providerName]
				if !exists {
					continue
				}
				providerCfg.ReasoningEffort = originalReasoning
				currentCfg.CustomProviders[providerName] = providerCfg
			}
			// Restore subagent persona overrides to their original values.
			if currentCfg.SubagentTypes != nil {
				for _, backup := range snapshot.SubagentOverridesBackup {
					if _, exists := currentCfg.SubagentTypes[backup.OriginalKey]; !exists {
						continue
					}
					st := currentCfg.SubagentTypes[backup.OriginalKey]
					st.Provider = backup.OriginalProvider
					st.Model = backup.OriginalModel
					currentCfg.SubagentTypes[backup.OriginalKey] = st
				}
			}
		}

		if overrides != nil && overrides.SetNoStream != nil {
			overrides.SetNoStream(snapshot.NoStream)
		}
		chatAgent.SetUnsafeMode(snapshot.Unsafe)
		chatAgent.SetMaxIterations(snapshot.MaxIterations)
		chatAgent.SetSystemPrompt(snapshot.SystemPrompt)
		chatAgent.SetBaseSystemPrompt(snapshot.SystemPrompt)

		if snapshot.DryRunEnv != "" {
			_ = configuration.SetEnv("DRY_RUN", snapshot.DryRunEnv)
		} else {
			configuration.UnsetEnv("DRY_RUN")
		}
		if snapshot.NoSubagentsEnv != "" {
			_ = configuration.SetEnv("NO_SUBAGENTS", snapshot.NoSubagentsEnv)
		} else {
			configuration.UnsetEnv("NO_SUBAGENTS")
		}
		if snapshot.ResourceDirectoryEnv != "" {
			_ = configuration.SetEnv("RESOURCE_DIRECTORY", snapshot.ResourceDirectoryEnv)
		} else {
			configuration.UnsetEnv("RESOURCE_DIRECTORY")
		}

		if len(restoreErrors) > 0 {
			return fmt.Errorf("failed to restore runtime overrides: %w", errors.Join(restoreErrors...))
		}
		return nil
	}

	return restore, nil
}

func ShouldRunWorkflowStep(when string, hasError bool) bool {
	switch NormalizeWorkflowWhen(when) {
	case WorkflowWhenOnSuccess:
		return !hasError
	case WorkflowWhenOnError:
		return hasError
	default:
		return true
	}
}
