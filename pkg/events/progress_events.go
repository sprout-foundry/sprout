// Package events provides event system for sprout UI architecture
package events

// SP-151 §151a progress event payloads (item 151.1).
//
// These are the wire payloads for the four progress event types declared in
// events_types.go. They are declared in this package (events is a leaf
// package) rather than importing pkg/plancontract or pkg/verify: the fields
// mirror those shapes (the plan scope item, the verification result) so the
// public event schema (SP-151 §151d) stays decoupled from the runtime
// packages that produce them. The JSON field names are the public contract
// — the @sprout/events TypeScript union (packages/events/src/types.ts) and
// the webui mirror (webui/src/types/generated.ts) mirror them 1:1 in
// snake_case.

// Milestone phase values for ProgressMilestoneData.Phase (SP-151 §151a:
// "a plan scope item started or finished").
const (
	// MilestonePhaseStarted marks a scope item that just began.
	MilestonePhaseStarted = "started"
	// MilestonePhaseFinished marks a scope item that just completed.
	MilestonePhaseFinished = "finished"
)

// ProgressMilestoneData is the payload for progress_milestone (SP-151
// §151a): a plan scope item (SP-148) started or finished, with the
// files-touched count and the scope item's elapsed wall time.
type ProgressMilestoneData struct {
	// RunID is the stable run identifier that correlates every event of
	// one run (SP-151 §151a).
	RunID string `json:"run_id"`
	// PlanRevision is the revision of the SP-148 plan the scope item
	// belongs to (0 when the run has no active plan).
	PlanRevision int `json:"plan_revision"`
	// ScopeID is the id of the scope item this milestone reports
	// (plancontract.ScopeItem.ID); empty when there is no plan scope.
	ScopeID string `json:"scope_id,omitempty"`
	// ScopeTitle is the scope item's title, for display.
	ScopeTitle string `json:"scope_title,omitempty"`
	// Phase is the milestone phase: MilestonePhaseStarted or
	// MilestonePhaseFinished.
	Phase string `json:"phase"`
	// FilesTouched is how many files the scope item changed.
	FilesTouched int `json:"files_touched,omitempty"`
	// ElapsedMs is the scope item's elapsed wall time in milliseconds
	// (wire-friendly; consumers render it as a duration).
	ElapsedMs int64 `json:"elapsed_ms"`
}

// ProgressQuestionData is the payload for progress_question (SP-151
// §151a): the agent needs a decision. It carries the question, the options
// if any, and why the decision matters, complementing ask_user_request
// with plan context (which scope item and plan revision it belongs to).
type ProgressQuestionData struct {
	RunID        string `json:"run_id"`
	PlanRevision int    `json:"plan_revision"`
	// ScopeID is the scope item the question belongs to (empty when the
	// question is not scoped to one).
	ScopeID string `json:"scope_id,omitempty"`
	// Question is the decision being requested.
	Question string `json:"question"`
	// Header is a short categorizing label rendered above the question.
	Header string `json:"header,omitempty"`
	// Options are the selectable choices, reusing the ask_user option
	// shape; nil for freeform questions.
	Options []AskUserRequestOption `json:"options,omitempty"`
	// WhyItMatters explains why the decision matters.
	WhyItMatters string `json:"why_it_matters,omitempty"`
}

// ProgressVerificationCheck is the compact evidence a single verification
// check carries (SP-149). It mirrors the consumer-facing fields of
// verify.Check and deliberately drops its execution metadata (routes,
// screenshots, steps, duration) to keep the event small: the full result
// stays server-side, the event is for webhooks and embedding UIs.
type ProgressVerificationCheck struct {
	// Kind is the plancontract check kind ("build", "test", ...).
	Kind string `json:"kind"`
	// Items are the ids of the plan acceptance items this check covers.
	Items []string `json:"items,omitempty"`
	// Command is the trusted command that ran ("" when skipped).
	Command string `json:"command,omitempty"`
	// Skipped marks a check that did not run (no trusted command, or the
	// run was cancelled). Skipped checks are listed, never hidden.
	Skipped bool `json:"skipped,omitempty"`
	// Passed reports whether the check passed (false for a skipped check).
	Passed bool `json:"passed,omitempty"`
	// Reason explains a skipped or abnormal check (timeout, cancellation).
	// Empty for a clean pass or a normal non-zero exit (the excerpt is the
	// evidence there).
	Reason string `json:"reason,omitempty"`
	// Excerpt is a bounded excerpt of the check's output (evidence).
	Excerpt string `json:"excerpt,omitempty"`
}

// ProgressVerificationData is the payload for progress_verification
// (SP-151 §151a): the SP-149 verification result — the checks, pass/fail,
// and evidence references.
type ProgressVerificationData struct {
	RunID        string `json:"run_id"`
	PlanRevision int    `json:"plan_revision"`
	// Baseline is true when the run happened without an active SP-148
	// plan (SP-149 §149a baseline mode).
	Baseline bool `json:"baseline,omitempty"`
	// Passed reports whether the run passed: nothing failed and at least
	// one check actually ran (SP-149 §149d).
	Passed bool `json:"passed,omitempty"`
	// Checks are the individual check outcomes, in run order.
	Checks []ProgressVerificationCheck `json:"checks"`
	// Errors are run-level findings that prevented some commands from
	// resolving; they are reported, never swallowed.
	Errors []string `json:"errors,omitempty"`
}

// ProgressCompleteData is the payload for progress_complete (SP-151
// §151a): the run finished. A run is "verified" only when a passing
// verification result exists (SP-149 §149d / SP-151 §151c).
// NotVerifiedReason is set only when verification is enabled and the turn
// was not verified (e.g. "no code changes this turn"); when SP-149 is
// disabled (the CLI default) both Verification and NotVerifiedReason are
// absent and the payload carries just run_id, so the default UI is
// unchanged.
type ProgressCompleteData struct {
	RunID        string `json:"run_id"`
	PlanRevision int    `json:"plan_revision"`
	// Verified is true only when a passing verification result exists.
	Verified bool `json:"verified,omitempty"`
	// Verification is the final verification result; nil when SP-149 is
	// disabled or was not run.
	Verification *ProgressVerificationData `json:"verification,omitempty"`
	// NotVerifiedReason explains why the run is not verified (empty when
	// verification is disabled).
	NotVerifiedReason string `json:"not_verified_reason,omitempty"`
}
