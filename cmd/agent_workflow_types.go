//go:build !js

package cmd

import (
	"strings"
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
	Description             string                            `json:"description,omitempty"`
	Initial                 *AgentWorkflowInitial             `json:"initial,omitempty"`
	Steps                   []AgentWorkflowStep               `json:"steps"`
	ContinueOnError         bool                              `json:"continue_on_error,omitempty"`
	PersistRuntimeOverrides *bool                             `json:"persist_runtime_overrides,omitempty"`
	Orchestration           *AgentWorkflowOrchestrationConfig `json:"orchestration,omitempty"`
	NoWebUI                 *bool                             `json:"no_web_ui,omitempty"`
	WebPort                 *int                              `json:"web_port,omitempty"`
	Daemon                  *bool                             `json:"daemon,omitempty"`
	Budget                  *AgentWorkflowBudgetConfig        `json:"budget,omitempty"`
	Progress                *AgentWorkflowProgressConfig      `json:"progress,omitempty"`

	// SubagentTimeoutSeconds overrides the per-run_subagent tool timeout
	// (default 1800 = 30 minutes). Set higher for very large refactors.
	SubagentTimeoutSeconds  *int                              `json:"subagent_timeout_seconds,omitempty"`

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

	// Loop configures the workflow to iterate over a TODO file, processing
	// each unchecked item independently with a fresh agent context.
	// When Loop is set, Steps are ignored — the loop IS the execution plan.
	Loop *AgentWorkflowLoopConfig `json:"loop,omitempty"`
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
	Version            int    `json:"version"`
	InitialCompleted   bool   `json:"initial_completed"`
	NextStepIndex      int    `json:"next_step_index"`
	CurrentTodoLineNum int    `json:"current_todo_line_num,omitempty"`
	HasError           bool   `json:"has_error"`
	FirstError         string `json:"first_error,omitempty"`
	LastProvider       string `json:"last_provider,omitempty"`
	Complete           bool   `json:"complete"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

// AgentWorkflowRuntime contains runtime options aligned with agent CLI flags.
type AgentWorkflowRuntime struct {
	SkipPrompt        *bool                     `json:"skip_prompt,omitempty"`
	Provider          string                    `json:"provider,omitempty"`
	Model             string                    `json:"model,omitempty"`
	Persona           string                    `json:"persona,omitempty"`
	DryRun            *bool                     `json:"dry_run,omitempty"`
	MaxIterations     *int                      `json:"max_iterations,omitempty"`
	NoStream          *bool                     `json:"no_stream,omitempty"`
	SystemPrompt      string                    `json:"system_prompt,omitempty"`
	SystemPromptFile  string                    `json:"system_prompt_file,omitempty"`
	Unsafe            *bool                     `json:"unsafe,omitempty"`
	NoSubagents       *bool                     `json:"no_subagents,omitempty"`
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

// AgentWorkflowLoopConfig configures the workflow to iterate over a TODO file,
// processing each unchecked item independently with a fresh agent context.
// When Loop is set, Steps are ignored — the loop IS the execution plan.
type AgentWorkflowLoopConfig struct {
	// TodoFile is the markdown file to scan for [ ] items. Default: TODO.md.
	TodoFile string `json:"todo_file,omitempty"`
	// GatePromptFile is the system prompt for the gate LLM call that
	// parses each TODO section into a structured delegation prompt.
	// Required. The gate call uses the agent's existing client.
	GatePromptFile string `json:"gate_prompt_file,omitempty"`
	// MaxRetries is the number of retry attempts on build failure
	// before skipping an item. Default: 2.
	MaxRetries int `json:"max_retries,omitempty"`
	// MaxIterations caps the agent iterations per item. Default: 50.
	MaxIterations int `json:"max_iterations,omitempty"`
	// BuildCommand is run after each item to verify. Default: "go build ./...".
	BuildCommand string `json:"build_command,omitempty"`
}

// IsShellStep reports whether the step is configured to run a shell command
// instead of triggering model inference.
func (s AgentWorkflowStep) IsShellStep() bool {
	return strings.TrimSpace(s.Command) != "" || strings.TrimSpace(s.CommandFile) != ""
}
