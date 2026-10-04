// runner.go — the SP-154 §154b runner: headless non-interactive runs of a
// benchmark task through the existing agent path, one fresh copy of the
// task's starter per run, 3 runs per model by default (so one lucky or
// unlucky run does not decide the result), with the run's pass/fail taken
// only from the SP-149 verification result of the turn (never from the
// model's own reply — SP-154 §154a).
//
// The turn-end verification hook (SP-149 §149c / 149.5) is the verifier:
// the run's configuration enables verification, the hook runs the
// project's checks after the turn, and the runner reads the result the
// hook stored on the agent (Agent.LastVerificationResult). The runner
// never runs verify itself and never scores the reply text.
//
// Per-run isolation: every run gets its own fresh directory (a fresh
// instantiation of the embedded starter + the task's frozen plan written
// via planstore), so a run can never observe another run's changes. The
// copy is removed when the run finishes, success or failure.
//
// Record-and-continue: a benchmark reports every run. A run whose setup
// fails (unknown starter, plan write, copy shaping), whose agent build
// fails, or whose turn errors is recorded with Run.Err and the loop moves
// on; none of those failures is returned from RunTask.
package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/planstore"
	"github.com/sprout-foundry/sprout/pkg/starters"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// defaultRunsPerTask is the SP-154 §154b runs-per-model default: 3, so one
// lucky or unlucky run does not decide the result.
const defaultRunsPerTask = 3

// ModelSpec selects the model (and optionally the provider) one set of
// benchmark runs uses. SP-154 §154b calls for a configurable model list
// that defaults to the provider catalog's recommended entries — this item
// ships the per-model unit that 154.4 feeds; there is no default model
// list here yet.
type ModelSpec struct {
	// Model is the model id to request. Empty → the run's configuration
	// default for the provider.
	Model string
	// Provider is the provider name to run against (e.g. "anthropic").
	// Empty → the run's configuration default provider.
	Provider string
}

// AgentFactory builds the headless agent for one run. It receives the
// run's fresh copy directory (starter instantiated, frozen plan written,
// ShapeCopy applied) and must return an agent whose workspace root is
// that directory. The default factory (a real provider client from
// pkg/factory plus agent.NewAgentWithClient, the production SDK path)
// is used when Runner.AgentFactory is nil; tests and custom harnesses
// supply their own (e.g. a scripted model).
type AgentFactory func(runDir string, spec ModelSpec) (*agent.Agent, error)

// Run is one benchmark run: one headless agent turn for one task with one
// model in one fresh copy of the task's starter (SP-154 §154b).
//
// Passed is derived ONLY from the SP-149 verification result (Result) —
// never from the model's own reply (SP-154 §154a/§154b): a run whose
// reply claims success but whose verification failed, or never produced a
// passing result, records Passed = false.
//
// Strictness pin: verify.Result.Passed() is false for an all-skipped run
// (a run that verified nothing is neither a pass nor a failure), so
// Passed = Result != nil && Result.Passed() is exactly "a passing
// verification result". An all-skipped run, a run whose turn changed no
// code (the hook's change gate never opened), and a run whose turn or
// setup ended in an error all record as fail: no passing result, no pass.
//
// The per-task metrics (SP-154 §154b, 154.3) are the fields below the
// wall-time anchors: the repair data the turn-end hook stored on the
// agent, the runner's turn count, the run's token and cost totals (the
// agent's existing conversation cost tracking — the run's agent is
// fresh, so the conversation total is the run's usage), and the run's
// share of the process-wide language-guard metric (SP-152 §152e). The
// spec's "per role via SP-150" is a future qualifier: the per-role split
// lands with SP-150's usage ledger (150.5) and is not tracked here yet.
// A setup-error run (Err set before the agent was built) leaves every
// metric zero; a run whose agent was built but whose turn errored still
// records its metrics (the turn was issued and may have consumed usage).
type Run struct {
	// TaskID is the benchmark task this run executed (Task.ID).
	TaskID string
	// Starter is the task's starter id (Task.Starter).
	Starter string
	// Model is the requested model (ModelSpec.Model, "" when the run's
	// configuration default applies).
	Model string
	// RunNumber is the 1-based run number within this task+model.
	RunNumber int
	// Passed is the run's acceptance outcome, derived only from Result.
	Passed bool
	// Result is the turn's SP-149 verification result, stored by the
	// turn-end hook, or nil when the hook never ran (verification
	// disabled, the turn changed no code, a setup error before the turn,
	// or a verify runner setup error).
	Result *verify.Result
	// Err is a run-level failure (fresh-copy setup, plan write, agent
	// build, or the agent turn itself). Non-nil means the run did not
	// complete cleanly; such a run is never Passed.
	Err error
	// StartedAt is when the run started (fresh-copy creation).
	StartedAt time.Time
	// FinishedAt is when the run finished (the copy is removed).
	FinishedAt time.Time
	// Turns is the number of top-level agent turns the runner issued for
	// this run (1 for the current single-request runner; the field grows
	// naturally when multi-request tasks land).
	Turns int
	// RepairRounds is the number of turn-end verification repair rounds
	// the hook ran within the turn (0 when verification never ran).
	RepairRounds int
	// RepairAttempts is the per-check repair attempts the hook consumed
	// against the stopping rule, keyed like the hook's per-check counters
	// (empty when verification never ran).
	RepairAttempts map[string]int
	// RepairLimit is the stopping-rule limit that was in effect for the
	// turn (0 when verification never ran).
	RepairLimit int
	// Tokens is the run's total token usage: the agent's conversation
	// total — the run's agent is fresh, so the conversation total is the
	// run's usage (existing cost tracking, SP-154 §154b).
	Tokens int
	// Cost is the run's total cost (the agent's conversation total cost).
	Cost float64
	// LangChecks is how many final messages the SP-152 language guard
	// judged for the run's model during the run (a delta of the
	// process-wide per-model metric).
	LangChecks int64
	// LangMismatches is how many of those judged messages were a
	// reliable language mismatch (a delta).
	LangMismatches int64
}

// Runner runs a benchmark task's headless runs (SP-154 §154b).
//
// A zero-value Runner is usable with a custom AgentFactory (tests and
// custom harnesses); the default factory additionally needs ConfigManager
// to target the run's model/provider and to enable verification for the
// run.
type Runner struct {
	// ConfigManager owns the per-run configuration. Before each agent
	// build the runner writes the run's requirements into it with
	// UpdateConfigNoSave — verification forced on (the benchmark's
	// pass/fail source must run; the manager's other verification
	// settings are preserved) and the spec's model/provider where set —
	// and a benchmark run never persists a run's model choice into the
	// user's configuration. The writes are narrow, never clearing: an
	// empty spec field leaves the manager's current value, and writes
	// persist across RunTask calls on the same runner — a caller that
	// wants isolated defaults per model supplies a fresh manager.
	// nil is allowed with a custom AgentFactory that manages its own
	// configuration.
	ConfigManager *configuration.Manager
	// AgentFactory builds the headless agent for one run (the default
	// factory, above, is used when nil).
	AgentFactory AgentFactory
	// ShapeCopy is an optional per-copy hook (tests / custom harnesses)
	// run after instantiation + plan write and before agent creation —
	// e.g. to override the copy's .sprout/starter.json commands. nil by
	// default: production runs use the starter's own manifest.
	ShapeCopy func(runDir string, task *Task) error
	// RunsPerTask is the runs-per-model count (SP-154 §154b). 0 (or
	// negative) → defaultRunsPerTask (3).
	RunsPerTask int
	// WorkDir is the parent directory for the fresh copies. "" →
	// os.TempDir(). It is created (with parents) if missing.
	WorkDir string
}

// RunTask runs task's request with one model (spec) RunsPerTask times —
// 3 by default, so one lucky or unlucky run does not decide the result
// (SP-154 §154b) — and returns one Run record per run, in run order.
//
// Each run is headless (the existing non-interactive agent path: one
// ProcessQuery turn with the task's request) and isolated: a fresh copy
// of the task's starter, one per run, with the task's frozen plan written
// into it. The run's pass/fail comes only from that turn's SP-149
// verification result — the turn-end hook runs the checks against the
// copy, and the model's own reply is never read for scoring (SP-154
// §154a/§154b).
//
// Return contract:
//   - (runs, nil): every scheduled run was attempted. A run's own failure
//     (setup, agent build, provider error) is carried on Run.Err — a
//     benchmark reports every run, and one bad run never aborts the task.
//   - (runs, err): ctx was cancelled between runs (the completed runs are
//     returned alongside the wrapped context error), the task is invalid
//     (nil task or empty request), the runner is nil, or WorkDir cannot
//     be created.
//
// Cancellation is honored between runs. A cancellation that lands
// mid-turn does not stop the in-flight turn: the agent's turn entry point
// offers no synchronous mid-turn cancel, and TriggerInterrupt from here
// would race the turn's own unwinding (a future CLI entry point, 154.5,
// can wire it with the CLI's interrupt dance). The loop stops before the
// next run.
func (r *Runner) RunTask(ctx context.Context, task *Task, spec ModelSpec) ([]Run, error) {
	if r == nil {
		return nil, errors.New("benchmark: nil runner")
	}
	if task == nil {
		return nil, errors.New("benchmark: task is required")
	}
	if strings.TrimSpace(task.Request) == "" {
		return nil, errors.New("benchmark: task request is required (the plain-language request the agent receives)")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	workDir := r.workDir()
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("benchmark: prepare work dir %s: %w", workDir, err)
	}

	total := r.runsPerTask()
	runs := make([]Run, 0, total)
	for n := 1; n <= total; n++ {
		if err := ctx.Err(); err != nil {
			return runs, fmt.Errorf("benchmark: task %s cancelled before run %d of %d: %w", task.ID, n, total, err)
		}
		runs = append(runs, r.runOnce(task, spec, n))
	}
	return runs, nil
}

// runOnce executes one run of task with one model and returns its record.
// Every step failure is recorded on the record (Run.Err, never returned)
// so the benchmark reports every run: one bad setup does not abort the
// suite. The run's fresh copy is always removed, success or failure.
func (r *Runner) runOnce(task *Task, spec ModelSpec, runNumber int) Run {
	run := Run{
		TaskID:    task.ID,
		Starter:   task.Starter,
		Model:     spec.Model,
		RunNumber: runNumber,
		StartedAt: time.Now(),
	}
	fail := func(err error) Run {
		run.Err = err
		run.FinishedAt = time.Now()
		return run
	}

	dir, err := os.MkdirTemp(r.workDir(), runDirPrefix(task.ID, runNumber))
	if err != nil {
		return fail(fmt.Errorf("benchmark: create fresh copy for task %s run %d: %w", task.ID, runNumber, err))
	}
	// Best-effort cleanup: a failed removal must not mask the run's own
	// outcome (the copy is debug evidence, not the result).
	defer func() { _ = os.RemoveAll(dir) }()

	if err := starters.Instantiate(task.Starter, dir); err != nil {
		return fail(fmt.Errorf("benchmark: instantiate starter %q for task %s run %d: %w", task.Starter, task.ID, runNumber, err))
	}

	// The frozen plan lands in the copy: planstore.Save validates it and
	// works on a copy (the caller's task is never mutated). The
	// verification run in the turn-end hook reads it for the task's
	// acceptance criteria (SP-154 §154a).
	if _, err := planstore.New().Save(dir, &task.Plan); err != nil {
		return fail(fmt.Errorf("benchmark: write frozen plan for task %s run %d: %w", task.ID, runNumber, err))
	}

	if r.ShapeCopy != nil {
		if err := r.ShapeCopy(dir, task); err != nil {
			return fail(fmt.Errorf("benchmark: shape copy for task %s run %d: %w", task.ID, runNumber, err))
		}
	}

	if err := r.configureRun(spec); err != nil {
		return fail(fmt.Errorf("benchmark: configure run for task %s run %d: %w", task.ID, runNumber, err))
	}
	agFactory := r.AgentFactory
	if agFactory == nil {
		agFactory = r.defaultAgentFactory
	}
	ag, err := agFactory(dir, spec)
	if err != nil {
		return fail(fmt.Errorf("benchmark: build agent for task %s run %d: %w", task.ID, runNumber, err))
	}
	defer ag.Shutdown()

	// The language-guard delta is a diff of the process-wide per-model
	// metric (SP-152 §152e): snapshot the run's model's stat before the
	// turn and diff it after. The guard records under the agent's model
	// id (an empty model id bucketed under "unknown" by the recorder),
	// so the lookup mirrors that bucketing to find the guard's entries.
	langModel := ag.GetModel()
	if langModel == "" {
		langModel = "unknown"
	}
	langBefore := langGuardStat(agent.GlobalLanguageGuardMetrics().Snapshot(), langModel)

	// The headless turn (the same entry point the non-interactive CLI
	// uses). The reply is never read for scoring — pass/fail comes only
	// from the verification result the turn-end hook stored on the agent
	// (SP-154 §154a/§154b).
	_, turnErr := ag.ProcessQueryWithContinuityAs(agent.QuerySourceCLI, task.Request)
	res := ag.LastVerificationResult()
	run.Result = res
	run.Passed = res != nil && res.Passed()
	if turnErr != nil {
		// An errored run is never Passed, even if a result was stored
		// (kept on the record as evidence).
		run.Err = fmt.Errorf("benchmark: agent turn for task %s run %d: %w", task.ID, runNumber, turnErr)
		run.Passed = false
	}

	// Per-task metrics (SP-154 §154b, 154.3), captured after the turn
	// while the run's agent still owns its conversation totals: the
	// repair data from the turn-end hook's stored state (nil when the
	// hook never ran — the fields stay zero), the runner's own count of
	// the turns it issued, the agent's existing token and cost totals
	// (fresh agent → the conversation total is the run's usage), and
	// the run's share of the process-wide language-guard metric.
	tv := ag.LastTurnVerification()
	run.Turns = 1
	run.Tokens = ag.GetTotalTokens()
	run.Cost = ag.GetTotalCost()
	if tv != nil {
		run.RepairRounds = tv.Rounds
		run.RepairAttempts = tv.Attempts
		run.RepairLimit = tv.Limit
	}
	langAfter := langGuardStat(agent.GlobalLanguageGuardMetrics().Snapshot(), langModel)
	run.LangChecks = langAfter.Checks - langBefore.Checks
	run.LangMismatches = langAfter.Mismatches - langBefore.Mismatches

	run.FinishedAt = time.Now()
	return run
}

// configureRun applies the run's requirements to the runner's
// ConfigManager (SP-154 §154b): verification forced on — the
// benchmark's pass/fail source must run for every run — and the spec's
// model/provider where set. The manager's other verification settings
// (the repair-attempt limit, the explicit build/test commands of SP-149
// §149b) are preserved, and an empty spec field leaves the manager's
// current value: the spec narrows, it never clears. A nil ConfigManager
// skips the step (a custom AgentFactory owns the run's configuration
// entirely).
func (r *Runner) configureRun(spec ModelSpec) error {
	if r.ConfigManager == nil {
		return nil
	}
	return r.ConfigManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.Verification == nil {
			cfg.Verification = &configuration.VerificationConfig{}
		}
		cfg.Verification.Enabled = true
		if spec.Provider != "" {
			cfg.LastUsedProvider = spec.Provider
		}
		if spec.Model != "" {
			provider := spec.Provider
			if provider == "" {
				provider = cfg.LastUsedProvider
			}
			if provider != "" {
				cfg.SetModelForProvider(provider, spec.Model)
			}
		}
		return nil
	})
}

// defaultAgentFactory builds the headless agent for a run from the
// runner's ConfigManager (the run is already configured: verification
// enabled, the spec's model/provider in place). It mirrors the production
// SDK path (cmd/wasm): a real provider client from pkg/factory plus
// agent.NewAgentWithClient with the runner's config manager — so the
// agent sees exactly the run's configuration — with the workspace root
// pinned to the run's fresh copy.
func (r *Runner) defaultAgentFactory(runDir string, spec ModelSpec) (*agent.Agent, error) {
	if r.ConfigManager == nil {
		return nil, errors.New("benchmark: the default agent factory needs a ConfigManager to target the run's model and provider (set Runner.ConfigManager or supply an AgentFactory)")
	}
	provider, err := r.ConfigManager.GetProvider()
	if err != nil {
		return nil, fmt.Errorf("benchmark: resolve the run's provider: %w (set ModelSpec.Provider or select a provider in the run's configuration)", err)
	}
	model := spec.Model
	if model == "" {
		model = r.ConfigManager.GetModelForProvider(provider)
	}
	client, err := factory.CreateProviderClient(provider, model)
	if err != nil {
		return nil, fmt.Errorf("benchmark: create client for provider %q model %q: %w", provider, model, err)
	}
	ag, err := agent.NewAgentWithClient(client, provider, r.ConfigManager)
	if err != nil {
		return nil, fmt.Errorf("benchmark: build the headless agent: %w", err)
	}
	ag.SetWorkspaceRoot(runDir)
	return ag, nil
}

// runsPerTask is the effective runs-per-model count (SP-154 §154b):
// RunsPerTask where positive, defaultRunsPerTask (3) otherwise.
func (r *Runner) runsPerTask() int {
	if r != nil && r.RunsPerTask > 0 {
		return r.RunsPerTask
	}
	return defaultRunsPerTask
}

// langGuardStat returns one model's language-guard stat from a snapshot
// (the zero stat when the model has no recorded checks yet). The
// snapshot is small (one entry per model ever judged, sorted by model
// id), so a linear scan is the whole job.
func langGuardStat(snapshot []agent.LanguageGuardModelStat, modelID string) agent.LanguageGuardModelStat {
	for _, s := range snapshot {
		if s.ModelID == modelID {
			return s
		}
	}
	return agent.LanguageGuardModelStat{ModelID: modelID}
}

// workDir is the parent for the fresh copies: WorkDir where set,
// os.TempDir() otherwise.
func (r *Runner) workDir() string {
	if r != nil && r.WorkDir != "" {
		return r.WorkDir
	}
	return os.TempDir()
}

// runDirPrefix builds the MkdirTemp prefix for one run's fresh copy:
// deterministic-ish (task id + run number) so a listing of the work dir
// says which run a directory was. Path separators and spaces in the id
// (an in-memory task need not be loader-validated) are replaced, never
// trusted.
func runDirPrefix(taskID string, runNumber int) string {
	safe := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(taskID)
	return fmt.Sprintf("sp154-%s-r%d-", safe, runNumber)
}
