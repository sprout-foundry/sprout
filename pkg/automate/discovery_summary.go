package automate

// discovery_summary.go — the workflow-summary data types filled in by
// Summarize (discovery_summarize.go): Summary, the allowed-path summary
// helpers, the budget/initial/subagent/step summaries, and the
// IsApprovalRequired check. Split out of discovery.go.
// Summary describes the structure of a workflow file at a glance. It is
// produced by Summarize and used by the CLI to render a human-readable
// overview before kicking off the workflow. JSON tags use snake_case to
// match the rest of the WebUI API surface; nil pointers are serialized as
// `null` (no omitempty) so `requires_approval` and `subagent_timeout_seconds`
// are always visible to the frontend — the original 3-state semantics
// (unset = default, true, false) must round-trip through the wire without
// collapsing to "absent."
type Summary struct {
	Description     string          `json:"description,omitempty"`
	ContinueOnError bool            `json:"continue_on_error,omitempty"`
	NoWebUI         bool            `json:"no_web_ui,omitempty"`
	Initial         *InitialSummary `json:"initial,omitempty"`
	Steps           []StepSummary   `json:"steps,omitempty"`
	Budget          *BudgetSummary  `json:"budget,omitempty"`
	// RequiresApproval reports whether the run_automate tool path should
	// prompt the user before launching this workflow. nil means the field
	// was unset in JSON (defaults to true). Explicit false marks the
	// workflow as agent-runnable without user confirmation. Serialized
	// as `null` when nil — the field is intentionally NOT omitempty so
	// the WebUI can distinguish "unset (defaults to required)" from
	// "absent (treated as not_required)".
	RequiresApproval *bool `json:"requires_approval"`
	// SubagentTimeoutSeconds overrides the per-run_subagent tool timeout
	// (default 1800 = 30 minutes). nil means use the default. Same nil-
	// semantics as RequiresApproval — serialized as `null` when unset.
	SubagentTimeoutSeconds *int `json:"subagent_timeout_seconds"`
	// AllowedPaths is the display-only view of the workflow's
	// declared allowed_paths entries. Populated by Summarize after
	// the same Validate() that the loader runs, so a malformed entry
	// surfaces as a parse error rather than silently dropping the
	// whole field. Entries are sorted by path for stable display.
	AllowedPaths []AllowedPathSummary `json:"allowed_paths,omitempty"`
	// Warnings collects advisory messages produced while building the
	// summary — currently the system-prefix warning when an
	// allowed_path falls under /etc, /usr, /var, etc. The CLI and
	// WebUI render these alongside the allowed_paths block so the
	// user sees the "this workflow touches platform infrastructure"
	// heads-up even when the path itself is well-formed.
	Warnings []string `json:"warnings,omitempty"`
}

// AllowedPathSummary is the display-only mirror of workflow.AllowedPath.
// It deliberately does NOT carry a Validate method — the parser runs
// workflow.AllowedPath.Validate() once during Summarize, so the summary
// only contains entries that already passed validation.
type AllowedPathSummary struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Reason string `json:"reason,omitempty"`
}

// allowedPathRaw is the raw parsed form of an allowed_path entry before
// validation. Defined at package level so it can be used by the helper
// functions parseSummaryAllowedPaths and extractSystemPathWarnings.
type allowedPathRaw struct {
	Path   string `json:"path,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// IsApprovalRequired returns true unless the workflow JSON explicitly
// declared requires_approval: false. Used by the agent tool path to
// decide whether to surface the intent-confirmation prompt.
func (s *Summary) IsApprovalRequired() bool {
	if s == nil || s.RequiresApproval == nil {
		return true
	}
	return *s.RequiresApproval
}

// BudgetSummary mirrors the cmd-level budget config in a package that has no
// cmd dependency, so the overview renderer can display it.
type BudgetSummary struct {
	USD    float64   `json:"usd"`
	WarnAt []float64 `json:"warn_at,omitempty"`
}

// InitialSummary describes the initial run.
type InitialSummary struct {
	Persona           string                    `json:"persona,omitempty"`
	Provider          string                    `json:"provider,omitempty"`
	Model             string                    `json:"model,omitempty"`
	MaxIterations     int                       `json:"max_iterations"`
	RiskProfile       string                    `json:"risk_profile,omitempty"`
	HasPrompt         bool                      `json:"has_prompt"`
	SubagentOverrides []SubagentOverrideSummary `json:"subagent_overrides,omitempty"`
	AllowedPaths      []AllowedPathSummary      `json:"allowed_paths,omitempty"`
}

// SubagentOverrideSummary describes one entry of subagent_overrides for display.
type SubagentOverrideSummary struct {
	Persona  string `json:"persona"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// StepSummary describes a single workflow step.
//
// Kind is one of "agent" (LLM inference) or "shell" (raw command). For shell
// steps, CommandPreview holds a single-line excerpt of the command for display.
type StepSummary struct {
	Name           string               `json:"name,omitempty"`
	Kind           string               `json:"kind"`
	Persona        string               `json:"persona,omitempty"`
	Provider       string               `json:"provider,omitempty"`
	Model          string               `json:"model,omitempty"`
	When           string               `json:"when,omitempty"`
	CommandPreview string               `json:"command_preview,omitempty"`
	AllowedPaths   []AllowedPathSummary `json:"allowed_paths,omitempty"`
}
