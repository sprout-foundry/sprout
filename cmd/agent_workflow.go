//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	workflowWhenAlways    = "always"
	workflowWhenOnSuccess = "on_success"
	workflowWhenOnError   = "on_error"

	defaultWorkflowOrchestrationStateFile  = ".sprout/workflow_state.json"
	defaultWorkflowOrchestrationEventsFile = ".sprout/workflow_events.jsonl"
	defaultWorkflowConversationSessionID   = "workflow"
)

// AgentWorkflowConfig defines non-interactive workflow orchestration.
type AgentWorkflowConfig struct {
	Description              string                            `json:"description,omitempty"`
	Initial                  *AgentWorkflowInitial             `json:"initial,omitempty"`
	Steps                    []AgentWorkflowStep               `json:"steps"`
	ContinueOnError          bool                              `json:"continue_on_error,omitempty"`
	PersistRuntimeOverrides  *bool                             `json:"persist_runtime_overrides,omitempty"`
	Orchestration            *AgentWorkflowOrchestrationConfig `json:"orchestration,omitempty"`
	NoWebUI                  *bool                             `json:"no_web_ui,omitempty"`
	WebPort                  *int                              `json:"web_port,omitempty"`
	Daemon                   *bool                             `json:"daemon,omitempty"`
	Budget                   *AgentWorkflowBudgetConfig        `json:"budget,omitempty"`
	Progress                 *AgentWorkflowProgressConfig      `json:"progress,omitempty"`

	// RequiresApproval controls whether the run_automate agent tool must
	// surface an intent-confirmation prompt to the user before launching
	// this workflow. Pointer so we can distinguish "unset" (default: true)
	// from explicit false. Set to false for workflows that exist
	// specifically so an agent can invoke them mid-task — e.g. a
	// validation workflow referenced from AGENTS.md that the model must
	// run before considering work done. Anyone with workflow-file access
	// can flip this, so the security implication should be obvious to a
	// reader of the JSON.
	//
	// Only affects the agent tool path. The CLI (`sprout automate run`)
	// always prompts unless --yes is passed, because a human at the
	// keyboard might still fat-finger the wrong workflow.
	RequiresApproval *bool `json:"requires_approval,omitempty"`
}

// IsApprovalRequired reports whether the run_automate tool path should
// surface an intent-confirmation prompt before launching this workflow.
// Defaults to true when unset.
func (c *AgentWorkflowConfig) IsApprovalRequired() bool {
	if c == nil || c.RequiresApproval == nil {
		return true
	}
	return *c.RequiresApproval
}

// AgentWorkflowBudgetConfig caps the total USD spend of a workflow run
// (primary agent + every subagent it spawns share the same budget).
//
// USD-denominated rather than tokens because mixed-provider workflows
// route different personas to different price tiers — a token cap that
// covers an Opus orchestrator would let a DeepSeek coder consume 50×
// the work for the same budget, defeating the cap.
type AgentWorkflowBudgetConfig struct {
	// USD is the hard cap on cumulative cost across the workflow.
	// <= 0 means no cap.
	USD float64 `json:"usd,omitempty"`
	// WarnAt is a list of fractional thresholds (0.0–1.0). When the
	// cumulative spend first crosses each threshold, a single warning
	// is emitted to stdout and (when wired) the event bus.
	// Empty defaults to [0.50, 0.80].
	WarnAt []float64 `json:"warn_at,omitempty"`
	// OnExceed controls what happens when USD is reached.
	// "truncate" (default) sets the truncation flag so the run finishes
	// the current LLM response and stops gracefully. "stop" is reserved
	// for future hard-kill behavior; today it's treated like truncate.
	OnExceed string `json:"on_exceed,omitempty"`
}

// AgentWorkflowProgressConfig controls runtime visibility of the workflow.
type AgentWorkflowProgressConfig struct {
	// HeartbeatSeconds is the interval at which the workflow prints a
	// progress line ([budget] $X of $Y · iter N · elapsed Tm).
	// <= 0 disables the heartbeat. Default 600 (10 min) when Budget is set.
	HeartbeatSeconds int `json:"heartbeat_seconds,omitempty"`
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
	// RiskProfile selects a named shell-command risk cascade preset
	// for this step / initial run (SP-058). One of: readonly,
	// cautious, default, permissive, unrestricted. Per-step values
	// override the workflow-level initial setting and the global
	// config. Unknown values fall through to the agent's default
	// resolution chain (override > config > "default").
	RiskProfile string `json:"risk_profile,omitempty"`
}

// AgentWorkflowInitial is the first run definition (can replace CLI prompt).
type AgentWorkflowInitial struct {
	Prompt     string `json:"prompt,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	AgentWorkflowRuntime
}

// AgentWorkflowStep is a single step executed after the initial query.
//
// A step is either an agent step (Prompt or PromptFile) or a shell step
// (Command or CommandFile). The two kinds are mutually exclusive — validation
// fails if both are set or neither is set.
//
// Shell steps run the command via the user's $SHELL (or /bin/sh) with the
// workflow's working directory and inherit stdout/stderr. They do NOT trigger
// model inference; they are useful for cheap, deterministic steps like
// `make build`, `git status`, or invoking a custom script that prepares
// state for the next agent step.
type AgentWorkflowStep struct {
	Name          string   `json:"name,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	PromptFile    string   `json:"prompt_file,omitempty"`
	Command       string   `json:"command,omitempty"`
	CommandFile   string   `json:"command_file,omitempty"`
	When          string   `json:"when,omitempty"`
	FileExists    []string `json:"file_exists,omitempty"`
	FileNotExists []string `json:"file_not_exists,omitempty"`
	AgentWorkflowRuntime
}

// IsShellStep reports whether the step is configured to run a shell command
// instead of triggering model inference.
func (s AgentWorkflowStep) IsShellStep() bool {
	return strings.TrimSpace(s.Command) != "" || strings.TrimSpace(s.CommandFile) != ""
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

	if c.Budget != nil {
		if c.Budget.USD < 0 {
			return errors.New("budget.usd must be >= 0")
		}
		for i, t := range c.Budget.WarnAt {
			if t <= 0 || t > 1 {
				return fmt.Errorf("budget.warn_at[%d] must be in (0, 1]; got %v", i, t)
			}
		}
		switch strings.TrimSpace(strings.ToLower(c.Budget.OnExceed)) {
		case "", "truncate", "stop":
			c.Budget.OnExceed = strings.TrimSpace(strings.ToLower(c.Budget.OnExceed))
			if c.Budget.OnExceed == "" {
				c.Budget.OnExceed = "truncate"
			}
		default:
			return fmt.Errorf("budget.on_exceed must be one of: truncate, stop; got %q", c.Budget.OnExceed)
		}
		if len(c.Budget.WarnAt) == 0 {
			c.Budget.WarnAt = []float64{0.50, 0.80}
		}
		// Keep warn thresholds sorted ascending so the runtime can scan
		// them in order without re-sorting on every response.
		sort.Float64s(c.Budget.WarnAt)
	}

	if c.Progress != nil && c.Progress.HeartbeatSeconds < 0 {
		return errors.New("progress.heartbeat_seconds must be >= 0")
	}

	if c.Initial != nil {
		c.Initial.Prompt = strings.TrimSpace(c.Initial.Prompt)
		c.Initial.PromptFile = strings.TrimSpace(c.Initial.PromptFile)
		if err := c.Initial.AgentWorkflowRuntime.validate("initial"); err != nil {
			return fmt.Errorf("validating initial step: %w", err)
		}
		if c.Initial.Prompt != "" && c.Initial.PromptFile != "" {
			return errors.New("initial.prompt and initial.prompt_file are mutually exclusive")
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
		step.Command = strings.TrimSpace(step.Command)
		step.CommandFile = strings.TrimSpace(step.CommandFile)
		step.When = normalizeWorkflowWhen(step.When)
		step.FileExists = normalizeWorkflowPaths(step.FileExists)
		step.FileNotExists = normalizeWorkflowPaths(step.FileNotExists)

		hasPrompt := step.Prompt != "" || step.PromptFile != ""
		hasCommand := step.Command != "" || step.CommandFile != ""
		if hasPrompt && hasCommand {
			return fmt.Errorf("steps[%d] cannot mix prompt/prompt_file with command/command_file", i)
		}
		if !hasPrompt && !hasCommand {
			return fmt.Errorf("steps[%d] requires one of prompt, prompt_file, command, command_file", i)
		}
		if step.Prompt != "" && step.PromptFile != "" {
			return fmt.Errorf("steps[%d].prompt and steps[%d].prompt_file are mutually exclusive", i, i)
		}
		if step.Command != "" && step.CommandFile != "" {
			return fmt.Errorf("steps[%d].command and steps[%d].command_file are mutually exclusive", i, i)
		}
		if !isValidWorkflowWhen(step.When) {
			return fmt.Errorf("steps[%d].when must be one of: %s, %s, %s", i, workflowWhenAlways, workflowWhenOnSuccess, workflowWhenOnError)
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

	// CLI → workflow JSON overrides for budget + heartbeat.
	// Only positive values override — 0 means "inherit JSON".
	if agentBudgetUSD > 0 {
		if cfg.Budget == nil {
			cfg.Budget = &AgentWorkflowBudgetConfig{}
		}
		cfg.Budget.USD = agentBudgetUSD
	}
	if strings.TrimSpace(agentBudgetWarn) != "" {
		thresholds, err := parseBudgetWarnList(agentBudgetWarn)
		if err == nil && len(thresholds) > 0 {
			if cfg.Budget == nil {
				cfg.Budget = &AgentWorkflowBudgetConfig{}
			}
			cfg.Budget.WarnAt = thresholds
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: ignoring invalid --budget-warn %q: %v\n", agentBudgetWarn, err)
		}
	}
	if agentHeartbeatSeconds > 0 {
		if cfg.Progress == nil {
			cfg.Progress = &AgentWorkflowProgressConfig{}
		}
		cfg.Progress.HeartbeatSeconds = agentHeartbeatSeconds
	}
}

// parseBudgetWarnList parses a comma-separated list of fractional thresholds
// (e.g. "0.5,0.8") into a sorted []float64. Each value must be in (0, 1].
func parseBudgetWarnList(s string) ([]float64, error) {
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(p, "%f", &v); err != nil {
			return nil, fmt.Errorf("invalid threshold %q: %w", p, err)
		}
		if v <= 0 || v > 1 {
			return nil, fmt.Errorf("threshold %v must be in (0, 1]", v)
		}
		out = append(out, v)
	}
	sort.Float64s(out)
	return out, nil
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

		fmt.Println()
		console.GlyphAction.Printf("Workflow step %d/%d (%s)", i+1, len(cfg.Steps), stepName)
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

		if step.IsShellStep() {
			shellErr := runWorkflowShellStep(ctx, step)
			if shellErr != nil {
				hasError = true
				if firstErr == nil {
					firstErr = fmt.Errorf("workflow step %q failed: %w", stepName, shellErr)
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
					"kind":       "shell",
					"error":      shellErr.Error(),
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
				"kind":       "shell",
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow step completed event")
			}
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

// attachWorkflowBudget wires the workflow's USD budget and progress
// heartbeat onto the agent. Returns a stop function the caller MUST
// invoke before the agent shuts down — it unregisters callbacks and
// stops the heartbeat goroutine. If no budget is configured the
// returned stop is a no-op and no goroutines are started.
//
// Heartbeat semantics:
//   - Default cadence: 600s when a budget is configured, off otherwise.
//   - cfg.Progress.HeartbeatSeconds > 0 overrides the cadence.
//   - The heartbeat prints to stdout in a single line so it composes with
//     existing console output without clobbering it.
func attachWorkflowBudget(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) (stop func()) {
	if chatAgent == nil || cfg == nil || cfg.Budget == nil || cfg.Budget.USD <= 0 {
		// Heartbeat without a budget is still meaningful, but only if
		// explicitly requested. Most workflows want budget+heartbeat as
		// a pair, so skip the goroutine when neither is set.
		if chatAgent != nil && cfg != nil && cfg.Progress != nil && cfg.Progress.HeartbeatSeconds > 0 {
			return startWorkflowHeartbeat(chatAgent, time.Duration(cfg.Progress.HeartbeatSeconds)*time.Second)
		}
		return func() {}
	}

	budget := agent.NewFleetUsdBudget(cfg.Budget.USD, cfg.Budget.WarnAt)
	chatAgent.SetFleetUsdBudget(budget)

	chatAgent.SetBudgetWarningCallback(func(threshold, spent, limit float64) {
		fmt.Printf("\n[budget] WARNING — crossed %.0f%% threshold: $%.2f of $%.2f spent\n",
			threshold*100, spent, limit)
	})
	chatAgent.SetBudgetExceededCallback(func(spent, limit float64) {
		fmt.Printf("\n[budget] CAP HIT — $%.2f of $%.2f spent; workflow will truncate after the current LLM response.\n",
			spent, limit)
	})

	heartbeatSeconds := 600
	if cfg.Progress != nil && cfg.Progress.HeartbeatSeconds > 0 {
		heartbeatSeconds = cfg.Progress.HeartbeatSeconds
	}
	stopHeartbeat := startWorkflowHeartbeat(chatAgent, time.Duration(heartbeatSeconds)*time.Second)

	return func() {
		stopHeartbeat()
		chatAgent.SetBudgetWarningCallback(nil)
		chatAgent.SetBudgetExceededCallback(nil)
	}
}

// startWorkflowHeartbeat starts a goroutine that prints a one-line budget
// progress message to stdout on the given interval, until the returned
// stop function is called. Safe to call with a nil agent (returns a noop).
func startWorkflowHeartbeat(chatAgent *agent.Agent, interval time.Duration) func() {
	if chatAgent == nil || interval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	started := time.Now()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				spent, limit := 0.0, 0.0
				if b := chatAgent.GetFleetUsdBudget(); b != nil {
					spent, limit = b.Snapshot()
				} else {
					spent = chatAgent.GetTotalCost()
				}
				iter := chatAgent.GetCurrentIteration()
				elapsed := time.Since(started).Round(time.Second)
				if limit > 0 {
					fmt.Printf("\n[budget] $%.2f of $%.2f · iter %d · elapsed %s\n",
						spent, limit, iter, elapsed)
				} else {
					fmt.Printf("\n[budget] $%.2f (no cap) · iter %d · elapsed %s\n",
						spent, iter, elapsed)
				}
			}
		}
	}()
	return func() { close(stop) }
}

// runWorkflowShellStep executes a shell command step. Stdout and stderr are
// inherited from the workflow's terminal so progress is visible in real time.
// A non-zero exit code becomes a step failure.
//
// command_file is interpreted as a script path passed to the shell, not as a
// raw command line — this avoids quoting headaches and lets users keep
// multi-line scripts in version control.
func runWorkflowShellStep(ctx context.Context, step AgentWorkflowStep) error {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}

	command := strings.TrimSpace(step.Command)
	commandFile := strings.TrimSpace(step.CommandFile)

	var cmd *exec.Cmd
	switch {
	case command != "":
		fmt.Printf("$ %s\n", singleLinePreview(command))
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	case commandFile != "":
		if _, err := os.Stat(commandFile); err != nil {
			return fmt.Errorf("command_file %q not accessible: %w", commandFile, err)
		}
		fmt.Printf("$ %s %s\n", shell, commandFile)
		cmd = exec.CommandContext(ctx, shell, commandFile)
	default:
		return errors.New("shell step has neither command nor command_file")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// singleLinePreview collapses a multi-line command to a single display line.
func singleLinePreview(s string) string {
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		return strings.TrimSpace(s[:idx]) + " …"
	}
	return s
}
