//go:build !js

package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// PathModeReadOnly declares the declared external folder is read-only —
// writes against any path under it must be refused by the filesystem gate.
const PathModeReadOnly = "read_only"

// PathModeReadWrite declares the declared external folder is fully
// readable and writable for the duration of the workflow run.
const PathModeReadWrite = "read_write"

const (
	WorkflowWhenAlways    = "always"
	WorkflowWhenOnSuccess = "on_success"
	WorkflowWhenOnError   = "on_error"

	DefaultWorkflowOrchestrationStateFile  = ".sprout/workflow_state.json"
	DefaultWorkflowOrchestrationEventsFile = ".sprout/workflow_events.jsonl"
	DefaultWorkflowConversationSessionID   = "workflow"
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
	SubagentTimeoutSeconds *int `json:"subagent_timeout_seconds,omitempty"`

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

	// AllowedPaths declares the external directories the workflow needs
	// to read or write outside the workspace. Each entry is validated by
	// AllowedPath.Validate and pre-seeded into the running agent's
	// session allowlist (with mode) at run start so the workflow
	// doesn't re-prompt for every external access. See
	// `roadmap/SP-128-workflow-allowed-paths.md` for the full design.
	AllowedPaths []AllowedPath `json:"allowed_paths,omitempty"`
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

// WorkflowSubagentOverride defines per-persona subagent provider/model overrides.
type WorkflowSubagentOverride struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// WorkflowSubagentOverrides maps persona IDs to their subagent routing overrides.
// Keys are normalized persona IDs (lowercase, hyphens→underscores).
// Values override provider/model for subagents with that persona.
type WorkflowSubagentOverrides map[string]WorkflowSubagentOverride

// WorkflowExecutionState tracks workflow execution progress for
// checkpoint/resume support.
type WorkflowExecutionState struct {
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
	Prompt       string       `json:"prompt,omitempty"`
	PromptFile   string       `json:"prompt_file,omitempty"`
	AllowedPaths []AllowedPath `json:"allowed_paths,omitempty"`
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
	Name          string         `json:"name,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	PromptFile    string         `json:"prompt_file,omitempty"`
	Command       string         `json:"command,omitempty"`
	CommandFile   string         `json:"command_file,omitempty"`
	When          string         `json:"when,omitempty"`
	FileExists    []string       `json:"file_exists,omitempty"`
	FileNotExists []string       `json:"file_not_exists,omitempty"`
	AllowedPaths []AllowedPath  `json:"allowed_paths,omitempty"`
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

// AllowedPath is a single directory the workflow needs access to outside
// the workspace. Declared at the workflow level so the user can review
// every external path in one place at launch time, and the runtime can
// pre-seed the session allowlist so no per-tool approval dialogs fire
// mid-run.
//
// Rules enforced by Validate:
//   - Path must be absolute (filepath.IsAbs).
//   - Path must not contain `..` segments after filepath.Clean.
//   - Path must not start with `~` (we refuse to expand — the workflow
//     author must supply a canonical absolute path).
//   - Mode must be one of {read_only, read_write} (case-sensitive).
//   - Reason is optional; loader does not require it.
//
// Paths that fall under a known system prefix (see isSystemPathPrefix)
// trigger a warning via log.Printf — the loader surfaces that warning
// so the user is aware the workflow touches platform infrastructure,
// but the path itself is allowed. Sensitive-path blocking is enforced
// elsewhere (filesystem tier classification / path_tier.go).
type AllowedPath struct {
	// Path is the absolute path to the directory. Must be absolute,
	// free of `..` traversal after Clean(), and must not start with
	// `~` (refuse to expand; user must provide the canonical absolute
	// path).
	Path string `json:"path"`

	// Mode is one of {read_only, read_write}. The filesystem layer
	// enforces this: a write tool called against a read_only entry
	// gets a security error explaining the declared mode blocked it.
	Mode string `json:"mode"`

	// Reason is surfaced verbatim in the launch confirmation dialog
	// so the user understands why the workflow needs this path.
	// Strongly recommended; loader does not require it.
	Reason string `json:"reason,omitempty"`
}

// Validate enforces the schema rules documented on AllowedPath. Returns
// nil for a valid entry; a descriptive error otherwise. The error
// message identifies the offending field so the loader can attribute
// the failure to the right entry index.
func (a *AllowedPath) Validate() error {
	if a == nil {
		return nil
	}
	path := strings.TrimSpace(a.Path)
	if path == "" {
		return errors.New("path is required")
	}
	if strings.HasPrefix(path, "~") {
		return errors.New("path must not start with `~`; provide an absolute path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute; got %q", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned != path {
		// filepath.Clean strips trailing slashes and collapses `./` and
		// `foo/../bar`. We reject any input that needs cleaning because
		// the user almost certainly meant to write a different path —
		// silent canonicalization would let `/foo/../etc` slip past as
		// a sibling of `/etc`.
		return fmt.Errorf("path must already be cleaned (no `./`, `..`, or trailing separators); got %q", path)
	}
	if strings.Contains(cleaned, "..") {
		// Defense in depth: filepath.Clean can leave `..` intact in
		// some edge cases (a leading `..` for example); refuse those
		// outright.
		return fmt.Errorf("path must not contain `..` segments; got %q", path)
	}
	mode := strings.TrimSpace(a.Mode)
	switch mode {
	case PathModeReadOnly, PathModeReadWrite:
		// ok
	default:
		return fmt.Errorf("mode must be %q or %q; got %q", PathModeReadOnly, PathModeReadWrite, a.Mode)
	}
	return nil
}

// IsSystemPathPrefix reports whether the (already-cleaned, absolute) path
// falls under one of the OS system directories. Used by the loader to
// emit a warning when a workflow declares an allowed_path inside a
// system prefix — the user is touching platform infrastructure and
// deserves the louder heads-up, even though the workflow is allowed to
// proceed.
//
// The list mirrors pkg/agent/path_tier.go::systemPathPrefixes; both
// should grow together if a new OS system directory is added. We keep
// the list local to the workflow package rather than importing the
// agent package (workflow is a leaf dependency — agent imports
// workflow, not the other way around).
//
// Exported (capitalized) so pkg/automate/discovery.go::Summarize can
// reuse the same prefix list when rendering the WebUI/CLI summary.
func IsSystemPathPrefix(p string) bool {
	if p == "" {
		return false
	}
	for _, prefix := range systemPathPrefixList() {
		if p == prefix {
			return true
		}
		if strings.HasPrefix(p, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// systemPathPrefixList returns the list of system-directory prefixes
// matched by isSystemPathPrefix. Kept in a function (not a package
// var) so tests can override it and so the match logic in
// isSystemPathPrefix stays next to its data.
func systemPathPrefixList() []string {
	return []string{
		"/etc",
		"/usr",
		"/var",
		"/bin",
		"/sbin",
		"/boot",
		"/proc",
		"/sys",
		"/dev",
		"/lib",
		"/lib64",
		"/opt",
		"/root",
		"/System",
		"/Library",
		"/private/etc",
		"/private/var",
		"/Applications",
	}
}
