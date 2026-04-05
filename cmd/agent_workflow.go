package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	workflowWhenAlways    = "always"
	workflowWhenOnSuccess = "on_success"
	workflowWhenOnError   = "on_error"

	defaultWorkflowOrchestrationStateFile  = ".ledit/workflow_state.json"
	defaultWorkflowOrchestrationEventsFile = ".ledit/workflow_events.jsonl"
	defaultWorkflowConversationSessionID   = "workflow"
)

// AgentWorkflowConfig defines non-interactive workflow orchestration.
type AgentWorkflowConfig struct {
	Initial                 *AgentWorkflowInitial             `json:"initial,omitempty"`
	Steps                   []AgentWorkflowStep               `json:"steps"`
	ContinueOnError         bool                              `json:"continue_on_error,omitempty"`
	PersistRuntimeOverrides *bool                             `json:"persist_runtime_overrides,omitempty"`
	Orchestration           *AgentWorkflowOrchestrationConfig `json:"orchestration,omitempty"`
	NoWebUI                 *bool                             `json:"no_web_ui,omitempty"`
	WebPort                 *int                              `json:"web_port,omitempty"`
	Daemon                  *bool                             `json:"daemon,omitempty"`
}

// AgentWorkflowOrchestrationConfig enables external orchestration integration.
type AgentWorkflowOrchestrationConfig struct {
	Enabled                bool   `json:"enabled,omitempty"`
	Resume                 *bool  `json:"resume,omitempty"`
	YieldOnProviderHandoff *bool  `json:"yield_on_provider_handoff,omitempty"`
	StateFile              string `json:"state_file,omitempty"`
	EventsFile             string `json:"events_file,omitempty"`
	ConversationSessionID  string `json:"conversation_session_id,omitempty"`
}

// workflowSubagentOverride defines per-persona subagent provider/model overrides.
type workflowSubagentOverride struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// WorkflowSubagentOverrides maps persona IDs to their subagent routing overrides.
// Keys are normalized persona IDs (lowercase, hyphens→underscores).
// Values override provider/model for subagents with that persona.
type WorkflowSubagentOverrides map[string]workflowSubagentOverride

type workflowExecutionState struct {
	Version          int    `json:"version"`
	InitialCompleted bool   `json:"initial_completed"`
	NextStepIndex    int    `json:"next_step_index"`
	HasError         bool   `json:"has_error"`
	FirstError       string `json:"first_error,omitempty"`
	LastProvider     string `json:"last_provider,omitempty"`
	Complete         bool   `json:"complete"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

// AgentWorkflowRuntime contains runtime options aligned with agent CLI flags.
type AgentWorkflowRuntime struct {
	SkipPrompt        *bool  `json:"skip_prompt,omitempty"`
	Provider          string `json:"provider,omitempty"`
	Model             string `json:"model,omitempty"`
	Persona           string `json:"persona,omitempty"`
	DryRun            *bool  `json:"dry_run,omitempty"`
	MaxIterations     *int   `json:"max_iterations,omitempty"`
	NoStream          *bool  `json:"no_stream,omitempty"`
	SystemPrompt      string `json:"system_prompt,omitempty"`
	SystemPromptFile  string `json:"system_prompt_file,omitempty"`
	Unsafe            *bool  `json:"unsafe,omitempty"`
	NoSubagents       *bool  `json:"no_subagents,omitempty"`
	ResourceDirectory string                    `json:"resource_directory,omitempty"`
	ReasoningEffort   string                    `json:"reasoning_effort,omitempty"`
	SubagentOverrides WorkflowSubagentOverrides `json:"subagent_overrides,omitempty"`
}

// AgentWorkflowInitial is the first run definition (can replace CLI prompt).
type AgentWorkflowInitial struct {
	Prompt     string `json:"prompt,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	AgentWorkflowRuntime
}

// AgentWorkflowStep is a single prompt step executed after the initial query.
type AgentWorkflowStep struct {
	Name          string   `json:"name,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	PromptFile    string   `json:"prompt_file,omitempty"`
	When          string   `json:"when,omitempty"`
	FileExists    []string `json:"file_exists,omitempty"`
	FileNotExists []string `json:"file_not_exists,omitempty"`
	AgentWorkflowRuntime
}

func loadAgentWorkflowConfig(path string) (*AgentWorkflowConfig, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, nil
	}

	data, err := os.ReadFile(trimmed)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow config %q: %w", trimmed, err)
	}

	var cfg AgentWorkflowConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse workflow config %q: %w", trimmed, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid workflow config %q: %w", trimmed, err)
	}

	return &cfg, nil
}

func (c *AgentWorkflowConfig) validate() error {
	if c == nil {
		return nil
	}
	if c.Orchestration != nil {
		c.Orchestration.StateFile = strings.TrimSpace(c.Orchestration.StateFile)
		c.Orchestration.EventsFile = strings.TrimSpace(c.Orchestration.EventsFile)
		c.Orchestration.ConversationSessionID = strings.TrimSpace(c.Orchestration.ConversationSessionID)
		if c.Orchestration.StateFile == "" {
			c.Orchestration.StateFile = defaultWorkflowOrchestrationStateFile
		}
		if c.Orchestration.EventsFile == "" {
			c.Orchestration.EventsFile = defaultWorkflowOrchestrationEventsFile
		}
		if c.Orchestration.ConversationSessionID == "" {
			c.Orchestration.ConversationSessionID = defaultWorkflowConversationSessionID
		}
		if c.Orchestration.Enabled {
			if c.Orchestration.StateFile == "" {
				return errors.New("orchestration.state_file is required when orchestration.enabled=true")
			}
			if c.Orchestration.EventsFile == "" {
				return errors.New("orchestration.events_file is required when orchestration.enabled=true")
			}
			if c.Orchestration.ConversationSessionID == "" {
				return errors.New("orchestration.conversation_session_id is required when orchestration.enabled=true")
			}
		}
	}
	if c.WebPort != nil && *c.WebPort < 0 {
		return errors.New("web_port must be >= 0")
	}

	if c.Initial != nil {
		c.Initial.Prompt = strings.TrimSpace(c.Initial.Prompt)
		c.Initial.PromptFile = strings.TrimSpace(c.Initial.PromptFile)
		if err := c.Initial.AgentWorkflowRuntime.validate("initial"); err != nil {
			return fmt.Errorf("validating initial step: %w", err)
		}
		if c.Initial.Prompt != "" && c.Initial.PromptFile != "" {
			return fmt.Errorf("initial.prompt and initial.prompt_file are mutually exclusive")
		}
	}

	if len(c.Steps) == 0 {
		hasInitialPrompt := c.Initial != nil && (c.Initial.Prompt != "" || c.Initial.PromptFile != "")
		if !hasInitialPrompt {
			return errors.New("workflow requires at least one step or an initial prompt/prompt_file")
		}
	}

	for i := range c.Steps {
		step := &c.Steps[i]
		step.Name = strings.TrimSpace(step.Name)
		step.Prompt = strings.TrimSpace(step.Prompt)
		step.PromptFile = strings.TrimSpace(step.PromptFile)
		step.When = normalizeWorkflowWhen(step.When)
		step.FileExists = normalizeWorkflowPaths(step.FileExists)
		step.FileNotExists = normalizeWorkflowPaths(step.FileNotExists)

		if step.Prompt == "" && step.PromptFile == "" {
			return errors.New(fmt.Sprintf("steps[%d] requires prompt or prompt_file", i))
		}
		if step.Prompt != "" && step.PromptFile != "" {
			return errors.New(fmt.Sprintf("steps[%d].prompt and steps[%d].prompt_file are mutually exclusive", i, i))
		}
		if !isValidWorkflowWhen(step.When) {
			return errors.New(fmt.Sprintf("steps[%d].when must be one of: %s, %s, %s", i, workflowWhenAlways, workflowWhenOnSuccess, workflowWhenOnError))
		}
		prefix := fmt.Sprintf("steps[%d]", i)
		if err := step.AgentWorkflowRuntime.validate(prefix); err != nil {
			return fmt.Errorf("validating step %s: %w", prefix, err)
		}
	}

	return nil
}

func applyWorkflowCommandOverrides(cfg *AgentWorkflowConfig) {
	if cfg == nil {
		return
	}
	if cfg.NoWebUI != nil {
		disableWebUI = *cfg.NoWebUI
	}
	if cfg.WebPort != nil {
		webPort = *cfg.WebPort
	}
	if cfg.Daemon != nil {
		daemonMode = *cfg.Daemon
	}
}

func (r *AgentWorkflowRuntime) validate(prefix string) error {
	if r == nil {
		return nil
	}
	r.Provider = strings.TrimSpace(r.Provider)
	r.Model = strings.TrimSpace(r.Model)
	r.Persona = strings.TrimSpace(r.Persona)
	r.SystemPrompt = strings.TrimSpace(r.SystemPrompt)
	r.SystemPromptFile = strings.TrimSpace(r.SystemPromptFile)
	r.ResourceDirectory = strings.TrimSpace(r.ResourceDirectory)
	rawReasoning := r.ReasoningEffort
	r.ReasoningEffort = normalizeReasoningEffort(r.ReasoningEffort)

	if r.SystemPrompt != "" && r.SystemPromptFile != "" {
		return fmt.Errorf("%s.system_prompt and %s.system_prompt_file are mutually exclusive", prefix, prefix)
	}
	if r.ReasoningEffort == "" && strings.TrimSpace(rawReasoning) != "" {
		return fmt.Errorf("%s.reasoning_effort must be one of: low, medium, high", prefix)
	}
	if r.MaxIterations != nil && *r.MaxIterations < 0 {
		return fmt.Errorf("%s.max_iterations must be >= 0", prefix)
	}
	for personaID, override := range r.SubagentOverrides {
		normalized := normalizeWorkflowPersonaID(personaID)
		if normalized == "" {
			return fmt.Errorf("%s.subagent_overrides has an empty persona key", prefix)
		}
		if override.Provider == "" && override.Model == "" {
			return fmt.Errorf("%s.subagent_overrides[%q] must have at least one of provider or model", prefix, normalized)
		}
	}

	return nil
}

func (c *AgentWorkflowConfig) shouldPersistRuntimeOverrides() bool {
	if c == nil || c.PersistRuntimeOverrides == nil {
		return true
	}
	return *c.PersistRuntimeOverrides
}

func (c *AgentWorkflowConfig) orchestrationEnabled() bool {
	return c != nil && c.Orchestration != nil && c.Orchestration.Enabled
}

func (c *AgentWorkflowConfig) orchestrationResumeEnabled() bool {
	if !c.orchestrationEnabled() || c.Orchestration.Resume == nil {
		return true
	}
	return *c.Orchestration.Resume
}

func (c *AgentWorkflowConfig) orchestrationYieldOnProviderHandoff() bool {
	if !c.orchestrationEnabled() || c.Orchestration.YieldOnProviderHandoff == nil {
		return true
	}
	return *c.Orchestration.YieldOnProviderHandoff
}

func normalizeReasoningEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return ""
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	default:
		return ""
	}
}

func normalizeWorkflowWhen(v string) string {
	trimmed := strings.TrimSpace(strings.ToLower(v))
	if trimmed == "" {
		return workflowWhenAlways
	}
	return trimmed
}

func normalizeWorkflowPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

// normalizeWorkflowPersonaID normalizes a persona ID the same way config.go does.
func normalizeWorkflowPersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// findSubagentTypeMapKey finds the original map key in SubagentTypes matching the
// given normalized persona ID. It mirrors the lookup logic in config.go GetSubagentType.
func findSubagentTypeMapKey(subagentTypes map[string]configuration.SubagentType, normalizedID string) (string, bool) {
	for key, st := range subagentTypes {
		if normalizeWorkflowPersonaID(key) == normalizedID {
			return key, true
		}
		for _, alias := range st.Aliases {
			if normalizeWorkflowPersonaID(alias) == normalizedID {
				return key, true
			}
		}
	}
	return "", false
}

// applyWorkflowSubagentOverrides patches the SubagentTypes map entries matching
// the given overrides. No error is returned for unknown personas — they are skipped.
func applyWorkflowSubagentOverrides(subagentTypes map[string]configuration.SubagentType, overrides WorkflowSubagentOverrides) {
	for personaID, override := range overrides {
		if override.Provider == "" && override.Model == "" {
			continue
		}
		normalizedID := normalizeWorkflowPersonaID(personaID)
		if normalizedID == "" {
			continue
		}
		mapKey, found := findSubagentTypeMapKey(subagentTypes, normalizedID)
		if !found {
			continue
		}
		st := subagentTypes[mapKey]
		if !st.Enabled {
			continue
		}
		if override.Provider != "" {
			st.Provider = override.Provider
		}
		if override.Model != "" {
			st.Model = override.Model
		}
		subagentTypes[mapKey] = st
	}
}

func isValidWorkflowWhen(v string) bool {
	switch v {
	case workflowWhenAlways, workflowWhenOnSuccess, workflowWhenOnError:
		return true
	default:
		return false
	}
}

func stepFileTriggersSatisfied(step AgentWorkflowStep) (bool, error) {
	for _, path := range step.FileExists {
		_, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to check file_exists path %q: %w", path, err)
		}
	}

	for _, path := range step.FileNotExists {
		_, err := os.Stat(path)
		if err == nil {
			return false, nil
		}
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("failed to check file_not_exists path %q: %w", path, err)
		}
	}

	return true, nil
}

func resolveWorkflowTextOrFile(text, filePath, label string) (string, error) {
	text = strings.TrimSpace(text)
	filePath = strings.TrimSpace(filePath)
	if text != "" && filePath != "" {
		return "", fmt.Errorf("%s and %s_file are mutually exclusive", label, label)
	}
	if filePath == "" {
		return text, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s_file %q: %w", label, filePath, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func resolveWorkflowInitialPrompt(cliQuery string, cfg *AgentWorkflowConfig) (string, error) {
	query := strings.TrimSpace(cliQuery)
	if query != "" {
		return query, nil
	}
	if cfg == nil || cfg.Initial == nil {
		return "", nil
	}
	return resolveWorkflowTextOrFile(cfg.Initial.Prompt, cfg.Initial.PromptFile, "prompt")
}

func resolveStepPrompt(step AgentWorkflowStep) (string, error) {
	return resolveWorkflowTextOrFile(step.Prompt, step.PromptFile, "prompt")
}

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
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		} else {
			_ = os.Unsetenv("LEDIT_DRY_RUN")
		}
	}
	if runtime.NoSubagents != nil {
		if *runtime.NoSubagents {
			_ = os.Setenv("LEDIT_NO_SUBAGENTS", "1")
		} else {
			_ = os.Unsetenv("LEDIT_NO_SUBAGENTS")
		}
	}
	if runtime.ResourceDirectory != "" {
		_ = os.Setenv("LEDIT_RESOURCE_DIRECTORY", runtime.ResourceDirectory)
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
	Provider               string
	Model                  string
	Persona                string
	ReasoningEffort        string
	SkipPrompt             bool
	Unsafe                 bool
	MaxIterations          int
	NoStream               bool
	DryRunEnv              string
	NoSubagentsEnv         string
	ResourceDirectoryEnv   string
	SystemPrompt           string
	CustomReasoningEfforts map[string]string
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
		return nil, fmt.Errorf("agent is required")
	}

	agentCfg := chatAgent.GetConfig()
	if agentCfg == nil {
		return nil, fmt.Errorf("agent config is unavailable")
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
		DryRunEnv:              os.Getenv("LEDIT_DRY_RUN"),
		NoSubagentsEnv:         os.Getenv("LEDIT_NO_SUBAGENTS"),
		ResourceDirectoryEnv:   os.Getenv("LEDIT_RESOURCE_DIRECTORY"),
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
		var restoreErrors []string

		if snapshot.Provider != "" && !strings.EqualFold(strings.TrimSpace(chatAgent.GetProvider()), snapshot.Provider) {
			if err := applyWorkflowRuntimeOverrides(chatAgent, AgentWorkflowRuntime{Provider: snapshot.Provider}); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("failed to restore provider %q: %s", snapshot.Provider, err.Error()))
			}
		}
		if snapshot.Model != "" && strings.TrimSpace(chatAgent.GetModel()) != snapshot.Model {
			if err := chatAgent.SetModelPersisted(snapshot.Model); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("failed to restore model %q: %s", snapshot.Model, err.Error()))
			}
		}
		currentPersona := strings.TrimSpace(chatAgent.GetActivePersona())
		if snapshot.Persona == "" && currentPersona != "" {
			chatAgent.ClearActivePersona()
		} else if snapshot.Persona != "" && !strings.EqualFold(currentPersona, snapshot.Persona) {
			if err := chatAgent.ApplyPersona(snapshot.Persona); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("failed to restore persona %q: %s", snapshot.Persona, err.Error()))
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
			_ = os.Setenv("LEDIT_DRY_RUN", snapshot.DryRunEnv)
		} else {
			_ = os.Unsetenv("LEDIT_DRY_RUN")
		}
		if snapshot.NoSubagentsEnv != "" {
			_ = os.Setenv("LEDIT_NO_SUBAGENTS", snapshot.NoSubagentsEnv)
		} else {
			_ = os.Unsetenv("LEDIT_NO_SUBAGENTS")
		}
		if snapshot.ResourceDirectoryEnv != "" {
			_ = os.Setenv("LEDIT_RESOURCE_DIRECTORY", snapshot.ResourceDirectoryEnv)
		} else {
			_ = os.Unsetenv("LEDIT_RESOURCE_DIRECTORY")
		}

		if len(restoreErrors) > 0 {
			return fmt.Errorf("failed to restore runtime overrides: %s", strings.Join(restoreErrors, "; "))
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

func runAgentWorkflow(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, cfg *AgentWorkflowConfig, state *workflowExecutionState) (bool, error) {
	if cfg == nil || len(cfg.Steps) == 0 {
		return false, nil
	}
	if state == nil {
		state = newWorkflowExecutionState()
	}
	if state.NextStepIndex >= len(cfg.Steps) {
		state.Complete = true
		return false, nil
	}

	hasError := state.HasError
	var firstErr error
	if strings.TrimSpace(state.FirstError) != "" {
		firstErr = fmt.Errorf("workflow error: %s", state.FirstError)
	}

	for i := state.NextStepIndex; i < len(cfg.Steps); i++ {
		step := cfg.Steps[i]
		stepName := step.Name
		if stepName == "" {
			stepName = fmt.Sprintf("step-%d", i+1)
		}

		if shouldYieldBeforeWorkflowStep(cfg, state, step, chatAgent) {
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_yielded", map[string]interface{}{
				"reason":          "provider_handoff",
				"next_step_index": i,
				"next_step_name":  stepName,
				"from_provider":   strings.TrimSpace(state.LastProvider),
				"to_provider":     workflowEffectiveStepProvider(chatAgent, step),
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow yield event")
			}
			state.NextStepIndex = i
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			fmt.Printf("\n[||] Workflow yielded for orchestration before step %s\n", stepName)
			return true, nil
		}

		if !shouldRunWorkflowStep(step.When, hasError) {
			state.NextStepIndex = i + 1
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			continue
		}

		fmt.Printf("\n[~] Workflow step %d/%d (%s)\n", i+1, len(cfg.Steps), stepName)
		if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_started", map[string]interface{}{
			"step_index": i,
			"step_name":  stepName,
			"provider":   workflowEffectiveStepProvider(chatAgent, step),
		}); err != nil {
			return false, utils.WrapError(err, "emit workflow step started event")
		}

		triggersSatisfied, triggerErr := stepFileTriggersSatisfied(step)
		if triggerErr != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q trigger evaluation failed: %w", stepName, triggerErr)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}
		if !triggersSatisfied {
			fmt.Printf("\n[>|] Skipping workflow step %s: file trigger conditions not met\n", stepName)
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_skipped", map[string]interface{}{
				"step_index": i,
				"step_name":  stepName,
				"reason":     "file_triggers_not_satisfied",
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow step skipped event")
			}
			state.NextStepIndex = i + 1
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			continue
		}

		if err := applyWorkflowRuntimeOverrides(chatAgent, step.AgentWorkflowRuntime); err != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q runtime setup failed: %w", stepName, err)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}

		stepPrompt, err := resolveStepPrompt(step)
		if err != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q prompt resolution failed: %w", stepName, err)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}
		if stepPrompt == "" {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q resolved an empty prompt", stepName)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}

		err = ProcessQuery(ctx, chatAgent, eventBus, stepPrompt)
		if err != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q failed: %w", stepName, err)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_failed", map[string]interface{}{
				"step_index": i,
				"step_name":  stepName,
				"provider":   state.LastProvider,
				"error":      err.Error(),
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow step failed event")
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}

		hasError = false
		state.NextStepIndex = i + 1
		state.HasError = false
		state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
		if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_completed", map[string]interface{}{
			"step_index": i,
			"step_name":  stepName,
			"provider":   state.LastProvider,
		}); err != nil {
			return false, utils.WrapError(err, "emit workflow step completed event")
		}
		if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
			return false, utils.WrapError(err, "persist workflow checkpoint")
		}
	}

	state.Complete = true
	if firstErr != nil {
		state.FirstError = firstErr.Error()
		state.HasError = true
	}
	if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
		return false, utils.WrapError(err, "persist workflow checkpoint")
	}
	if err := emitWorkflowOrchestrationEvent(cfg, "workflow_completed", map[string]interface{}{
		"has_error": state.HasError,
	}); err != nil {
		return false, utils.WrapError(err, "emit workflow completed event")
	}

	return false, firstErr
}
