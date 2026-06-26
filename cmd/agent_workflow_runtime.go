//go:build !js

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func applyWorkflowRuntimeOverrides(chatAgent *agent.Agent, runtime AgentWorkflowRuntime) error {
	if chatAgent == nil {
		return errors.New("agent is required")
	}

	cfg := chatAgent.GetConfig()
	if cfg == nil {
		return errors.New("agent config is unavailable")
	}

	if runtime.SkipPrompt != nil || normalizeReasoningEffort(runtime.ReasoningEffort) != "" {
		normalized := normalizeReasoningEffort(runtime.ReasoningEffort)
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
	if runtime.NoStream != nil {
		agentNoStreaming = *runtime.NoStream
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

	systemPrompt, err := resolveWorkflowTextOrFile(runtime.SystemPrompt, runtime.SystemPromptFile, "system_prompt")
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
			applyWorkflowSubagentOverrides(cfg.SubagentTypes, runtime.SubagentOverrides)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to apply workflow runtime overrides: %w", err)
		}
	}

	return nil
}

func applyWorkflowInitialOverrides(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) error {
	if cfg == nil || cfg.Initial == nil {
		return nil
	}
	return applyWorkflowRuntimeOverrides(chatAgent, cfg.Initial.AgentWorkflowRuntime)
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

func prepareWorkflowRuntimeRestorer(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) (func() error, error) {
	if cfg == nil || cfg.shouldPersistRuntimeOverrides() {
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
		NoStream:               agentNoStreaming,
		DryRunEnv:              configuration.GetEnvSimple("DRY_RUN"),
		NoSubagentsEnv:         configuration.GetEnvSimple("NO_SUBAGENTS"),
		ResourceDirectoryEnv:   configuration.GetEnvSimple("RESOURCE_DIRECTORY"),
		SystemPrompt:           chatAgent.GetSystemPrompt(),
		CustomReasoningEfforts: map[string]string{},
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
			normalizedID := normalizeWorkflowPersonaID(personaID)
			if normalizedID == "" {
				continue
			}
			mapKey, found := findSubagentTypeMapKey(agentCfg.SubagentTypes, normalizedID)
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
			if err := applyWorkflowRuntimeOverrides(chatAgent, AgentWorkflowRuntime{Provider: snapshot.Provider}); err != nil {
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

		agentNoStreaming = snapshot.NoStream
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

func shouldRunWorkflowStep(when string, hasError bool) bool {
	switch normalizeWorkflowWhen(when) {
	case workflowWhenOnSuccess:
		return !hasError
	case workflowWhenOnError:
		return hasError
	default:
		return true
	}
}

func newWorkflowExecutionState() *workflowExecutionState {
	return &workflowExecutionState{
		Version:       1,
		NextStepIndex: 0,
	}
}

func loadWorkflowExecutionState(cfg *AgentWorkflowConfig) (*workflowExecutionState, error) {
	if !cfg.orchestrationEnabled() || !cfg.orchestrationResumeEnabled() {
		return newWorkflowExecutionState(), nil
	}

	path := cfg.Orchestration.StateFile
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newWorkflowExecutionState(), nil
		}
		return nil, fmt.Errorf("failed to read orchestration state %q: %w", path, err)
	}

	var state workflowExecutionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse orchestration state %q: %w", path, err)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.NextStepIndex < 0 {
		state.NextStepIndex = 0
	}
	if state.Complete {
		return newWorkflowExecutionState(), nil
	}
	return &state, nil
}

func persistWorkflowExecutionState(cfg *AgentWorkflowConfig, state *workflowExecutionState) error {
	if state == nil || !cfg.orchestrationEnabled() {
		return nil
	}
	path := cfg.Orchestration.StateFile
	if path == "" {
		return errors.New("orchestration state file path is empty")
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize orchestration state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create orchestration state directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write orchestration state %q: %w", path, err)
	}
	return nil
}

func shouldRestoreWorkflowConversationState(state *workflowExecutionState) bool {
	if state == nil {
		return false
	}
	return state.InitialCompleted || state.NextStepIndex > 0 || state.HasError || strings.TrimSpace(state.FirstError) != ""
}

func restoreWorkflowConversationState(chatAgent *agent.Agent, cfg *AgentWorkflowConfig, state *workflowExecutionState) error {
	if chatAgent == nil || cfg == nil || !cfg.orchestrationEnabled() || !cfg.orchestrationResumeEnabled() {
		return nil
	}
	if !shouldRestoreWorkflowConversationState(state) {
		return nil
	}
	sessionID := strings.TrimSpace(cfg.Orchestration.ConversationSessionID)
	if sessionID == "" {
		return nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to resolve current working directory for workflow restore: %w", err)
	}
	restoredState, err := chatAgent.LoadStateScoped(sessionID, workingDir)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to load orchestration conversation session %q: %w", sessionID, err)
	}
	chatAgent.ApplyState(restoredState)
	return nil
}

func persistWorkflowConversationState(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) error {
	if chatAgent == nil || cfg == nil || !cfg.orchestrationEnabled() {
		return nil
	}
	sessionID := strings.TrimSpace(cfg.Orchestration.ConversationSessionID)
	if sessionID == "" {
		return nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to resolve current working directory for workflow checkpoint: %w", err)
	}
	if err := chatAgent.SaveStateScoped(sessionID, workingDir); err != nil {
		return fmt.Errorf("failed to write orchestration conversation session %q: %w", sessionID, err)
	}
	return nil
}

func persistWorkflowCheckpoint(cfg *AgentWorkflowConfig, state *workflowExecutionState, chatAgent *agent.Agent) error {
	if err := persistWorkflowExecutionState(cfg, state); err != nil {
		return fmt.Errorf("failed to persist workflow checkpoint: %w", err)
	}
	return persistWorkflowConversationState(chatAgent, cfg)
}

func emitWorkflowOrchestrationEvent(cfg *AgentWorkflowConfig, eventType string, payload map[string]interface{}) error {
	if !cfg.orchestrationEnabled() {
		return nil
	}
	path := cfg.Orchestration.EventsFile
	if path == "" {
		return errors.New("orchestration events file path is empty")
	}

	record := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"type":      strings.TrimSpace(eventType),
	}
	for k, v := range payload {
		record[k] = v
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to serialize orchestration event: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create orchestration events directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open orchestration events file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("failed to append orchestration event to %q: %w", path, err)
	}
	return nil
}

func workflowEffectiveStepProvider(chatAgent *agent.Agent, step AgentWorkflowStep) string {
	if strings.TrimSpace(step.Provider) != "" {
		return strings.TrimSpace(step.Provider)
	}
	return strings.TrimSpace(chatAgent.GetProvider())
}

func shouldYieldBeforeWorkflowStep(cfg *AgentWorkflowConfig, state *workflowExecutionState, nextStep AgentWorkflowStep, chatAgent *agent.Agent) bool {
	if !cfg.orchestrationEnabled() || !cfg.orchestrationYieldOnProviderHandoff() {
		return false
	}
	lastProvider := strings.TrimSpace(state.LastProvider)
	if lastProvider == "" {
		return false
	}
	nextProvider := workflowEffectiveStepProvider(chatAgent, nextStep)
	if nextProvider == "" {
		return false
	}
	return !strings.EqualFold(lastProvider, nextProvider)
}
