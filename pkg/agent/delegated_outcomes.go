package agent

import (
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Delegated-task outcome tracking.
//
// Every delegated (subagent) task is recorded once, at the point it
// completes, as a DelegatedTaskOutcome: the model that produced its final
// result, the task type, the verification outcome, and the tokens/cost. The
// records live in the agent's bounded per-session store (the session stats)
// and are also published as a delegated_task_outcome event a host can
// aggregate. Together they answer "did the cheaper worker model hold up, and
// what did it save."
//
// Coverage: the run_subagent (blocking and background) and
// run_parallel_subagents paths, which are the two delegation entry points a
// host can direct. The internal ReviewChanges mechanism (runReviewTasks) is
// deliberately not instrumented here: it resolves the reviewer persona to the
// reviewer/coder role — never the worker role — so its tasks are not
// worker-model work and excluding them cannot distort the worker share or
// credits-saved figures.

// Task-type categories a delegated task is classified into. The order is the
// one classifyTaskType checks a matching word against, so a word that names
// several kinds of work takes the earlier category.
const (
	TaskTypeReadSearch = "read/search"
	TaskTypeSummarise  = "summarise"
	TaskTypeEdit       = "edit"
	TaskTypeTest       = "test"
	TaskTypeOther      = "other"
)

// Verification outcomes a delegated task can finish with. "verified" means the
// worker's result passed the pre-reliance checks (or the task is a
// non-worker task that completed normally); "escalated" means the task was
// retried once on the coder model and that retry held; "failed" means it did
// not hold up.
const (
	OutcomeVerified  = "verified"
	OutcomeEscalated = "escalated"
	OutcomeFailed    = "failed"
)

// DelegatedTaskOutcome is the record of one completed delegated task. Model
// and Provider name the model that produced the FINAL result (the coder model
// when a worker task was escalated), Role the role that model was resolved
// through, TaskType the prompt's work kind, Outcome the verification result,
// and Tokens/Cost what the final result consumed. No prompt text is carried.
type DelegatedTaskOutcome struct {
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	Role             string  `json:"role"`
	TaskType         string  `json:"task_type"`
	Outcome          string  `json:"outcome"`
	Tokens           int     `json:"tokens"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
	// At is the record time (RFC3339 UTC).
	At string `json:"at"`
}

// DelegationSummary aggregates a session's delegated-task outcomes: the share
// of the delegated work the worker model actually completed, and the credits
// saved versus running that work on the coder model.
type DelegationSummary struct {
	// TaskCount is the number of recorded delegated tasks.
	TaskCount int `json:"task_count"`
	// WorkerTaskCount is the number of tasks whose final result the worker
	// model produced (verified without escalation).
	WorkerTaskCount int `json:"worker_task_count"`
	// TotalTokens / WorkerTokens are the delegated token totals, overall and
	// for the worker-model-completed tasks.
	TotalTokens  int `json:"total_tokens"`
	WorkerTokens int `json:"worker_tokens"`
	// WorkerTokenShare is WorkerTokens/TotalTokens (0 when there is no
	// delegated work): the share of delegated work the worker model handled.
	WorkerTokenShare float64 `json:"worker_token_share"`
	// CoderProvider/CoderModel and WorkerProvider/WorkerModel are the two
	// models the savings comparison is made between.
	CoderProvider  string `json:"coder_provider"`
	CoderModel     string `json:"coder_model"`
	WorkerProvider string `json:"worker_provider"`
	WorkerModel    string `json:"worker_model"`
	// CreditsSaved is the USD saved by running the worker-model tasks on the
	// worker model instead of the coder model.
	CreditsSaved float64 `json:"credits_saved"`
	// CreditsSavedKnown is false when the savings could not be determined
	// (the coder model's rate is unknown) — render "unknown", not $0.
	CreditsSavedKnown bool `json:"credits_saved_known"`
}

// wordSet builds a whole-word keyword set.
func wordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// taskTypeKeywords maps each task-type category to the whole-word cues that
// mark a prompt as that kind of work. The "edit" set is the same mutation
// verbs promptRequestsMutation uses (see subagent_worker_verify.go), so a
// prompt's work kind agrees with whether the edit check applies.
var taskTypeKeywords = map[string]map[string]bool{
	TaskTypeReadSearch: wordSet("read", "search", "searching", "find", "locate", "grep", "inspect", "examine", "scan", "trace", "explore"),
	TaskTypeSummarise:  wordSet("summarise", "summarize", "summary", "summarization", "summarisation", "condense", "digest", "outline", "brief"),
	TaskTypeEdit:       wordSet("edit", "modify", "refactor", "rename", "implement", "insert", "append", "replace", "patch", "write", "delete", "remove", "add", "fix", "update", "create"),
	TaskTypeTest:       wordSet("test", "tests", "spec", "specs", "assert", "assertion"),
}

// taskTypePriority is the category order a matching word is checked against,
// so a word naming several kinds of work takes the earlier category.
var taskTypePriority = []string{TaskTypeReadSearch, TaskTypeSummarise, TaskTypeEdit, TaskTypeTest}

// classifyTaskType categorises a delegated task's prompt as read/search,
// summarise, edit, test, or other, by whole-word keyword match. The leading
// matching word in the prompt decides, so the task's primary action classifies
// it. The word-boundary split mirrors promptRequestsMutation, so "address"
// never matches "add" and "prefix" never matches "fix". Pure and
// side-effect free.
func classifyTaskType(prompt string) string {
	for _, word := range strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if cat, ok := taskTypeCategory(word); ok {
			return cat
		}
	}
	return TaskTypeOther
}

// taskTypeCategory maps a single whole word to the task-type category whose
// keywords include it ("" when the word matches no category).
func taskTypeCategory(word string) (string, bool) {
	for _, cat := range taskTypePriority {
		if taskTypeKeywords[cat][word] {
			return cat, true
		}
	}
	return "", false
}

// isFailedSubagentRun reports whether a subagent result is a failed or
// aborted run, the same aborted-run check verifyWorkerResult applies.
func isFailedSubagentRun(r *SubagentResult) bool {
	if r == nil {
		return true
	}
	return r.Error != nil || r.Cancelled || r.BudgetExceeded || r.Truncated
}

// coderModelForDelegation resolves the coder role's (provider, model), the
// model a worker task is escalated to. ("","") when there is no config.
func (a *Agent) coderModelForDelegation() (string, string) {
	if cfg := a.GetConfig(); cfg != nil {
		return cfg.ResolveRole(configuration.RoleCoder)
	}
	return "", ""
}

// delegatedOutcomeCap bounds the per-session delegated-task outcome store.
// Beyond it the oldest records are dropped (head-trimmed) so a long session
// stays bounded.
const delegatedOutcomeCap = 512

// storeDelegatedOutcome appends one record to the agent's bounded outcome
// store, head-trimming past the cap.
func (a *Agent) storeDelegatedOutcome(rec DelegatedTaskOutcome) {
	a.delegatedOutcomesMu.Lock()
	defer a.delegatedOutcomesMu.Unlock()
	a.delegatedOutcomes = append(a.delegatedOutcomes, rec)
	if overflow := len(a.delegatedOutcomes) - delegatedOutcomeCap; overflow > 0 {
		a.delegatedOutcomes = a.delegatedOutcomes[overflow:]
	}
}

// GetDelegatedTaskOutcomes returns a copy of the session's bounded
// delegated-task outcome records, most recent last. Nil-safe for a nil
// agent.
func (a *Agent) GetDelegatedTaskOutcomes() []DelegatedTaskOutcome {
	if a == nil {
		return nil
	}
	a.delegatedOutcomesMu.Lock()
	defer a.delegatedOutcomesMu.Unlock()
	out := make([]DelegatedTaskOutcome, len(a.delegatedOutcomes))
	copy(out, a.delegatedOutcomes)
	return out
}

// recordDelegatedTaskOutcome records one delegated task's completion in the
// agent's bounded outcome store and publishes a delegated_task_outcome event
// a host can aggregate. model/provider/role name the model that produced the
// FINAL result (the coder model when a worker task was escalated); no prompt
// text is carried in the record or the event.
func (a *Agent) recordDelegatedTaskOutcome(prompt string, result *SubagentResult, model, provider, role, outcome string) {
	if a == nil {
		return
	}
	rec := DelegatedTaskOutcome{
		Model:    model,
		Provider: provider,
		Role:     role,
		TaskType: classifyTaskType(prompt),
		Outcome:  outcome,
		At:       time.Now().UTC().Format(time.RFC3339),
	}
	if result != nil {
		rec.Tokens = result.TokensUsed
		rec.PromptTokens = result.PromptTokens
		rec.CompletionTokens = result.CompletionTokens
		rec.Cost = result.Cost
	}
	a.storeDelegatedOutcome(rec)
	a.publishEvent(events.EventTypeDelegatedTaskOutcome, events.DelegatedTaskOutcomeEvent(
		rec.Model, rec.Provider, rec.Role, rec.TaskType, rec.Outcome,
		rec.Tokens, rec.PromptTokens, rec.CompletionTokens, rec.Cost,
	))
}

// recordDelegatedOutcome records the outcome for a task on the single
// (blocking or background) path, where the verification/escalation has just
// completed. final is the result the primary sees; workerBefore is the
// result before the escalation (nil for a non-worker task or a task that
// verified on the first pass). When the escalation ran, final differs from
// workerBefore and the coder model is attributed; otherwise the worker model
// is.
func (a *Agent) recordDelegatedOutcome(spec *subagentLaunchSpec, final, workerBefore *SubagentResult, verification workerVerifyResult) {
	if a == nil || final == nil {
		return
	}
	if isWorkerRoleTask(spec.role) {
		// The escalation produced the final result when it differs from the
		// worker's first result. Comparing pointers (not guarding on
		// workerBefore being non-nil) is load-bearing: when the worker's first
		// run returned nil and the escalation ran, workerBefore is nil but
		// final is the escalated coder result, so the two differ and the coder
		// model is correctly attributed. When the escalation could not run,
		// escalateWorkerRun returns the original failed pointer, so final ==
		// workerBefore and the worker's own result stands.
		escalationRan := final != workerBefore
		if escalationRan {
			// The coder model produced the final result (the retry ran). When
			// the retry held, the task is "escalated"; when the retry also
			// failed verification, the task is "failed" (the spec ties
			// "failed" to "verification, including escalation, failed").
			provider, model := a.coderModelForDelegation()
			outcome := OutcomeEscalated
			if !verification.Verified {
				outcome = OutcomeFailed
			}
			a.recordDelegatedTaskOutcome(spec.prompt, final, model, provider, configuration.RoleCoder, outcome)
			return
		}
		outcome := OutcomeVerified
		if !verification.Verified {
			outcome = OutcomeFailed
		}
		a.recordDelegatedTaskOutcome(spec.prompt, final, spec.model, spec.provider, spec.role, outcome)
		return
	}
	// A non-worker task is not subject to worker verification: it completes
	// normally (or fails), recorded with the model that ran it. These are
	// recorded (not skipped) so the worker model's share of the delegated
	// work is measured against ALL delegated work, not just the worker's.
	outcome := OutcomeVerified
	if isFailedSubagentRun(final) {
		outcome = OutcomeFailed
	}
	a.recordDelegatedTaskOutcome(spec.prompt, final, spec.model, spec.provider, spec.role, outcome)
}

// GetDelegationSummary computes the session's delegation summary from the
// recorded outcomes: the share of the delegated work the worker model
// completed and the credits saved versus running it on the coder model. The
// savings are determined from the coder model's catalog rate; when that rate
// is unknown the savings are reported as unknown rather than a fabricated
// $0 (mirroring the cache-savings handling).
func (a *Agent) GetDelegationSummary() DelegationSummary {
	summary := DelegationSummary{}
	outcomes := a.GetDelegatedTaskOutcomes()
	summary.TaskCount = len(outcomes)

	var coderProvider, coderModel, workerProvider, workerModel string
	if cfg := a.GetConfig(); cfg != nil {
		coderProvider, coderModel = cfg.ResolveRole(configuration.RoleCoder)
		// ResolveWorkerRole falls back to the coder role's model when the
		// worker role is unset — the same model a worker task actually runs
		// on — so the displayed worker model matches what was executed.
		workerProvider, workerModel = cfg.ResolveWorkerRole()
	}
	summary.CoderProvider, summary.CoderModel = coderProvider, coderModel
	summary.WorkerProvider, summary.WorkerModel = workerProvider, workerModel

	var coderInput, coderOutput float64
	coderRateKnown := false
	if coderProvider != "" && coderModel != "" {
		coderInput, coderOutput, _, coderRateKnown = api.ResolveModelPricing(coderProvider, coderModel)
	}

	var totalTokens, workerTokens int
	var creditsSaved float64
	hasWorkerWork := false
	for _, o := range outcomes {
		totalTokens += o.Tokens
		// Only a task the worker model actually completed (verified, not
		// escalated) is work the worker model "held up" on — the share and
		// the savings are computed over those.
		if o.Role != configuration.RoleWorker || o.Outcome != OutcomeVerified {
			continue
		}
		hasWorkerWork = true
		summary.WorkerTaskCount++
		workerTokens += o.Tokens
		// Credits this task saved by running on the worker model instead of
		// the coder model: what the same tokens would have cost at the
		// coder's input/output rates, minus what it actually cost. A
		// negative delta (the worker model is more expensive) is clamped to
		// 0 — there is no saving to claim.
		if coderRateKnown {
			coderCost := float64(o.PromptTokens)*coderInput/1e6 + float64(o.CompletionTokens)*coderOutput/1e6
			if saved := coderCost - o.Cost; saved > 0 {
				creditsSaved += saved
			}
		}
	}
	summary.TotalTokens = totalTokens
	summary.WorkerTokens = workerTokens
	if totalTokens > 0 {
		summary.WorkerTokenShare = float64(workerTokens) / float64(totalTokens)
	}
	summary.CreditsSaved = creditsSaved
	// The savings are unknown only when there is worker-model work but the
	// coder rate could not be resolved; with no worker work the saving is
	// trivially $0 and known.
	summary.CreditsSavedKnown = !hasWorkerWork || coderRateKnown
	return summary
}
