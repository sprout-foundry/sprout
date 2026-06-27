//go:build !js

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

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
// Log lines are emitted for every skip and every successful apply so that silent
// divergence between the workflow JSON and the actual SubagentTypes is visible.
func applyWorkflowSubagentOverrides(subagentTypes map[string]configuration.SubagentType, overrides WorkflowSubagentOverrides) {
	for personaID, override := range overrides {
		if override.Provider == "" && override.Model == "" {
			log.Printf("[workflow] subagent_overrides: empty override for %q — both provider and model are empty; nothing to apply", personaID)
			continue
		}
		normalizedID := normalizeWorkflowPersonaID(personaID)
		if normalizedID == "" {
			continue
		}
		mapKey, found := findSubagentTypeMapKey(subagentTypes, normalizedID)
		if !found {
			log.Printf("[workflow] subagent_overrides: unknown persona %q — no matching SubagentTypes entry or alias; override ignored (provider=%s model=%s)", personaID, override.Provider, override.Model)
			continue
		}
		st := subagentTypes[mapKey]
		if !st.Enabled {
			log.Printf("[workflow] subagent_overrides: disabled persona %q — enabled=false; override ignored (provider=%s model=%s)", personaID, override.Provider, override.Model)
			continue
		}
		if override.Provider != "" {
			st.Provider = override.Provider
		}
		if override.Model != "" {
			st.Model = override.Model
		}
		subagentTypes[mapKey] = st
		log.Printf("[workflow] subagent_overrides applied: persona %q → provider=%s model=%s", personaID, st.Provider, st.Model)
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
