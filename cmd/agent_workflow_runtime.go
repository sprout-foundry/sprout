//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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

func applyWorkflowInitialOverrides(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) error {
	if cfg == nil || cfg.Initial == nil {
		return nil
	}
	return applyWorkflowRuntimeOverrides(chatAgent, cfg.Initial.AgentWorkflowRuntime)
}

// pickSubagentDefault selects a deterministic override entry to use as the
// global subagent default when no global SubagentProvider/SubagentModel is set.
// Preference order: orchestrator → coder → first entry alphabetically.
// Persona IDs are normalized (lowercase, hyphens→underscores) before matching
// to stay consistent with applyWorkflowSubagentOverrides / GetSubagentType.
func pickSubagentDefault(overrides WorkflowSubagentOverrides) workflowSubagentOverride {
	// Prefer orchestrator, then coder — match against normalized keys so
	// "Orchestrator", "ORCHESTRATOR", "repo_orchestrator" all resolve.
	wanted := []string{"orchestrator", "coder"}
	for _, target := range wanted {
		for key, v := range overrides {
			if normalizeWorkflowPersonaID(key) != target {
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
	return workflowSubagentOverride{}
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

	// Gracefully handle empty or whitespace-only files.
	if len(bytes.TrimSpace(data)) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: workflow state file is empty — starting fresh")
		return newWorkflowExecutionState(), nil
	}

	var state workflowExecutionState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt JSON — log a warning and start fresh rather than failing
		// the entire workflow.
		fmt.Fprintf(os.Stderr, "Warning: workflow state file %q is corrupt (%v) — starting fresh\n", path, err)
		return newWorkflowExecutionState(), nil
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

// writeFileAtomic writes data to path atomically by writing to a temp file
// in the same directory and then renaming. This prevents partial/corrupt
// state files if the process crashes mid-write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp_*.write")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Clean up temp file on any failure.
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file %q: %w", tmpName, err)
	}
	// Sync to ensure data is on disk before rename.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %q → %q: %w", tmpName, path, err)
	}
	// After rename, the file at tmpName is gone, so don't try to remove it.
	cleanup = false
	return nil
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
	if err := writeFileAtomic(path, data, 0600); err != nil {
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

// loopCheckpointFilePath returns the path to the lightweight fallback
// checkpoint file that stores just the TODO line number.
func loopCheckpointFilePath(workDir string) string {
	return filepath.Join(workDir, ".sprout", "todo_loop_checkpoint.txt")
}

// persistLoopCheckpoint writes just the line number to the fallback
// checkpoint file using an atomic write (temp file + rename).
func persistLoopCheckpoint(workDir string, lineNum int) error {
	path := loopCheckpointFilePath(workDir)
	data := []byte(fmt.Sprintf("%d\n", lineNum))
	if err := writeFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("failed to persist loop checkpoint: %w", err)
	}
	return nil
}

// removeLoopCheckpoint deletes the fallback checkpoint file, ignoring
// not-found errors.
func removeLoopCheckpoint(workDir string) {
	path := loopCheckpointFilePath(workDir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove loop checkpoint %q: %v\n", path, err)
	}
}

// loadLoopCheckpoint reads the fallback checkpoint file and returns
// the line number. Returns (0, nil) if the file doesn't exist.
func loadLoopCheckpoint(workDir string) (int, error) {
	path := loopCheckpointFilePath(workDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read loop checkpoint %q: %w", path, err)
	}

	lineStr := strings.TrimSpace(string(data))
	if lineStr == "" {
		return 0, nil
	}
	var lineNum int
	if _, err := fmt.Sscanf(lineStr, "%d", &lineNum); err != nil {
		// Corrupt file — log warning and treat as missing.
		fmt.Fprintf(os.Stderr, "Warning: loop checkpoint %q has invalid content %q — ignoring\n", path, lineStr)
		return 0, nil
	}
	if lineNum <= 0 {
		return 0, nil
	}
	return lineNum, nil
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
