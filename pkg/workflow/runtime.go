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

// ApplyWorkflowRuntimeAllowedPaths adds each path in paths (derived from
// AllowedPath entries in the step's runtime config) to the agent's session
// allowlist and records the declared mode. This lets the cd-target gate
// (Phase 2.1) and the filesystem gate (Phase 2.2) see the paths as approved
// for the duration of the step.
//
// Snapshot semantics: the snapshot captures the state BEFORE any paths are
// added, so restoreWorkflowRuntimeAllowedPaths can undo only the paths this
// step added — not paths contributed by earlier steps. This means consecutive
// steps that declare overlapping paths have the documented "last restore wins"
// behavior: step 2's snapshot contains step 1's contribution; step 2's restore
// removes both step 2's new paths AND step 1's old paths. Declare overlapping
// paths in each step that needs them, or promote them to the workflow-level
// AllowedPaths, if you need per-step isolation.
//
// Idempotent: adding a path that's already on the allowlist is a no-op;
// it is NOT included in addedPaths, so it will NOT be removed on restore.
func ApplyWorkflowRuntimeAllowedPaths(chatAgent *agent.Agent, paths []AllowedPath) (snapshotPaths []string, snapshotModes map[string]string, addedPaths []string, err error) {
	if chatAgent == nil {
		return nil, nil, nil, errors.New("agent is required")
	}
	if len(paths) == 0 {
		// No paths to add: snapshot captures current state (empty for an
		// agent with no prior allowlist), nothing added, nothing to restore.
		snapshotPaths = chatAgent.SnapshotSessionAllowedFolders()
		snapshotModes = chatAgent.SnapshotSessionAllowedFolderModes()
		return snapshotPaths, snapshotModes, nil, nil
	}

	// Snapshot BEFORE mutating so we can restore exactly to the pre-step state.
	snapshotPaths = chatAgent.SnapshotSessionAllowedFolders()
	snapshotModes = chatAgent.SnapshotSessionAllowedFolderModes()

	// Track what we actually added so the restore path can remove only the
	// net-new entries without disturbing pre-existing ones.
	currentSet := make(map[string]bool)
	for _, f := range snapshotPaths {
		currentSet[f] = true
	}

	addedPaths = nil
	for _, ap := range paths {
		normalized := ap.Path
		if normalized == "" {
			continue
		}
		if currentSet[normalized] {
			// Already on the allowlist: nothing to do. Not added to
			// addedPaths so restore won't touch it.
			continue
		}
		chatAgent.AddSessionAllowedFolder(normalized)
		mode := strings.TrimSpace(ap.Mode)
		if mode == "" {
			mode = PathModeReadWrite // default
		}
		chatAgent.SetSessionAllowedFolderMode(normalized, mode)
		currentSet[normalized] = true // prevent dup in same step
		addedPaths = append(addedPaths, normalized)
	}

	return snapshotPaths, snapshotModes, addedPaths, nil
}

// restoreWorkflowRuntimeAllowedPaths undoes the paths added by the matching
// ApplyWorkflowRuntimeAllowedPaths call for this step. It is called at every
// step exit point (success, failure, skip, shell) to ensure no step's
// allowed_paths leak into the next step.
//
// Restore semantics:
//   - For each path in addedPaths: if it was NOT in snapshotPaths, remove it
//     from the allowlist (it was added by this step).
//   - For each path in snapshotPaths: restore its mode from snapshotModes
//     (may be different from what a prior step left behind).
//
// This means if step 1 adds /a and step 2 adds /b then /a:
//   - After step 1: allowlist = [/a]
//   - Step 2 snapshot: [/a]
//   - Step 2 addedPaths: [/b] (/a was already there)
//   - Step 2 restore removes [/b], leaves [/a]
//   - After step 2: allowlist = [/a]
// This is the documented "steps don't inherit paths from prior steps" behavior.
func RestoreWorkflowRuntimeAllowedPaths(chatAgent *agent.Agent, snapshotPaths []string, snapshotModes map[string]string, addedPaths []string) error {
	if chatAgent == nil {
		return errors.New("agent is required")
	}

	// Build a set from snapshotPaths for O(1) membership checks.
	snapshotSet := make(map[string]bool)
	for _, f := range snapshotPaths {
		snapshotSet[f] = true
	}

	// Remove net-new paths: any addedPath not in the snapshot.
	for _, ap := range addedPaths {
		if ap == "" {
			continue
		}
		if snapshotSet[ap] {
			// This path existed before the step started — leave it alone.
			continue
		}
		// This path was added by this step — remove it.
		if err := chatAgent.RemoveSessionAllowedFolder(ap); err != nil {
			return fmt.Errorf("failed to remove allowed folder %q: %w", ap, err)
		}
	}

	// Restore modes for all paths that were present in the snapshot.
	// This overwrites whatever mode the current step left behind.
	for _, f := range snapshotPaths {
		if f == "" {
			continue
		}
		mode := ""
		if snapshotModes != nil {
			mode = snapshotModes[f]
		}
		// SetSessionAllowedFolderMode handles the no-op case when the
		// folder isn't on the allowlist (shouldn't happen here since
		// we checked snapshotPaths is the current state).
		chatAgent.SetSessionAllowedFolderMode(f, mode)
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
