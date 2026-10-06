// verification_hook.go — the SP-149 §149c / 149.5 turn-end verification
// hook: after a turn that changed application code, and before the final
// reply, the runtime (not the model) runs the project's verification
// checks. A failing gated check is fed back to the model as a structured
// verification report and the turn continues; after N repair attempts on
// the same failing check the loop stops and the failure stands (SP-149
// §149d). The last verification result is stored on the agent for the
// final-reply contract (149.6) and the SP-151 verification event. The
// gate is Agent.TurnChangedApplicationPaths (turn_application_code.go):
// documentation and .sprout bookkeeping changes never open it.

package agent

import (
	"fmt"
	"strings"

	core "github.com/sprout-foundry/seed/core"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// Explicit envelope of the structured verification report the hook feeds
// back to the model (SP-149 §149c).
const (
	verificationReportOpenTag  = "<verification-report>"
	verificationReportCloseTag = "</verification-report>"
)

// turnVerification is the per-turn verification state the final-reply
// contract (SP-149 §149c/§149d, item 149.6) and the consumers outside
// pkg/agent (the SP-154 benchmark metrics, 154.3) read in one access:
// the turn's last verification run, the per-check repair attempts that
// run consumed, the configured repair limit N, and the number of repair
// rounds the hook ran for the turn. The turn-end hook stores a fresh
// state on every verification run (pass, fail, stop-rule);
// prepareQueryRun resets it at each turn start so a previous turn's
// result never attaches to a later turn's reply. A nil result means the
// hook never ran for the turn (verification disabled, no code change, a
// subagent turn, or a runner setup error) — the reply then stands
// untouched.
type turnVerification struct {
	result   *verify.Result
	attempts map[string]int
	limit    int
	// rounds is how many repair rounds the hook has run for the turn
	// (one report feed = one round): a stored snapshot carries the
	// rounds completed before its verification run, so a turn that
	// failed and repaired once stores rounds=1 on its last run.
	rounds int
}

// snapshotVerificationAttempts copies the repair loop's per-check counters
// so a stored state never aliases the map the loop keeps mutating across
// repair rounds: each stored run keeps the counts that run saw.
func snapshotVerificationAttempts(attempts map[string]int) map[string]int {
	if len(attempts) == 0 {
		return nil
	}
	snapshot := make(map[string]int, len(attempts))
	for key, used := range attempts {
		snapshot[key] = used
	}
	return snapshot
}

// runTurnEndVerification implements the SP-149 §149c gate and repair loop
// (item 149.5). processQueryWithSeed invokes it on the success path,
// between the turn's first seedAgent.Run and handleQueryResult:
// finalResult is the turn's answer, and this method either returns it
// unchanged or continues the turn through repair rounds.
//
// No-ops (returns finalResult untouched, no stored result) when:
//   - the agent has no configuration, or verification is disabled — the
//     default (SP-149 §149e: enabling it changes no other behavior);
//   - the turn changed no application code (§149a: the run gates on the
//     turn's own changes, Agent.TurnChangedApplicationPaths — a turn
//     that touched only documentation or .sprout bookkeeping skips the
//     run);
//   - the agent is a subagent (subagent turns run through subagent_runner
//     and never own the final reply; belt-and-braces since that path does
//     not reach this hook).
//
// A runner setup error (no executor, empty root) is logged and returns the
// turn's answer: a verification setup problem never gates the turn. A
// non-nil error from a repair round (interrupted/provider error) is
// returned so handleQueryResult classifies it exactly as a first-run
// error.
func (a *Agent) runTurnEndVerification(qc *queryRunContext, finalResult string) (string, error) {
	if a.configManager == nil {
		return finalResult, nil
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || !cfg.VerificationEnabled() {
		return finalResult, nil
	}
	if len(a.TurnChangedApplicationPaths()) == 0 {
		return finalResult, nil
	}
	if a.subagentDepth > 0 {
		return finalResult, nil
	}

	// The runner resolves commands only from the starter manifest and the
	// explicit project configuration (SP-149 §149b) — never from model
	// output.
	runner := verify.New()
	runner.ConfigCommands = verify.ConfigurationCommands(cfg)

	// Every verification run of the turn executes against the turn's frozen
	// snapshot (SP-149 §149b): the manifest's commands and the plan's
	// acceptance captured at the turn's start. The snapshot is taken in
	// prepareQueryRun; if it is missing for some reason (verification was
	// enabled after the turn started), take one now at hook entry. Either
	// way there is exactly one snapshot per turn, taken no later than the
	// hook's first verify run, so a model that edits .sprout/starter.json
	// or .sprout/plan.json during a repair round cannot change what
	// "passing" means.
	snap := a.getTurnVerifySnapshot()
	if snap == nil {
		snap = runner.Snapshot(a.GetWorkspaceRoot())
		a.setTurnVerifySnapshot(snap)
	}

	limit := cfg.VerificationRepairAttempts()
	totalCap := cfg.VerificationRepairTotalRounds()
	attempts := make(map[string]int)
	rounds := 0

	for {
		// A stop that lands in the window between the turn's answer and a
		// verify run (or between repair rounds) must report as an
		// interrupt, not a completed turn: the error reaches
		// handleQueryResult, whose runCtx check classifies it.
		if qc.runCtx.Err() != nil {
			return finalResult, fmt.Errorf("%w: %w", core.ErrInterrupted, qc.runCtx.Err())
		}
		res, err := runner.RunSnapshot(qc.runCtx, a.GetWorkspaceRoot(), snap)
		if err != nil {
			a.Logger().Debug("turn-end verification setup error: %v\n", err)
			return finalResult, nil
		}
		// Stored on every run (pass, fail, stop-rule): the result, the
		// per-check repair attempts consumed so far, the configured
		// limits, and the repair rounds the hook has run — the state
		// 149.6 attaches to the final reply, SP-151 records, and the
		// SP-154 benchmark reads (154.3). A snapshot of the counters:
		// the loop keeps counting into its own map across repair
		// rounds, so the stored state never mutates after it is stored.
		a.setTurnVerification(turnVerification{
			result:   res,
			attempts: snapshotVerificationAttempts(attempts),
			limit:    limit,
			rounds:   rounds,
		})
		if !res.Failed() {
			// Passing, or all-skipped where nothing failed — both stand.
			return finalResult, nil
		}
		if verificationLoopShouldStop(res, attempts, limit, rounds, totalCap) {
			// The stopping rule fired — every failing check has used its
			// repair attempts (the per-check rule), or the turn's total
			// repair rounds have reached the cap. The loop stops and the
			// last result stands (149d: the final reply states what
			// failed).
			return finalResult, nil
		}
		for _, c := range res.Checks {
			if c.Skipped || c.Passed {
				continue
			}
			attempts[checkAttemptKey(c)]++
		}

		// Continue the turn: the report lands as a user-role message in
		// the transcript (the same mechanism steer messages use) and the
		// repair round's answer becomes the final result. One report
		// feed = one repair round: the count grows now, so the next
		// stored snapshot (after this round's verification run) carries
		// it.
		report := buildVerificationReport(res, attempts, limit)
		rounds++
		repairResult, repairErr := qc.seedAgent.Run(qc.runCtx, report)
		if repairErr != nil {
			return repairResult, repairErr
		}
		finalResult = repairResult
	}
}

// verificationLoopShouldStop reports whether the SP-149 §149c stopping
// rule has fired for one failing verification run. Either condition ends
// the loop, whichever fires first:
//
//   - the per-check rule: every failing check has used its repair-attempt
//     limit. A run whose failure comes only from run-level errors (no
//     failing check) fires immediately — there is nothing to repair per
//     check.
//   - the total-rounds cap: the loop has already run totalCap repair
//     rounds. The per-check counters cannot see failure patterns that
//     rotate between rounds — two checks alternating failures so neither
//     key's counter ever reaches the limit, or interaction checks whose
//     item id (and therefore counter key) is new every round — so the
//     total bound is what guarantees the loop always terminates.
func verificationLoopShouldStop(res *verify.Result, attempts map[string]int, limit, rounds, totalCap int) bool {
	if rounds >= totalCap {
		return true
	}
	return verificationStopRuleFired(res, attempts, limit)
}

// verificationStopRuleFired reports whether the SP-149 §149c stopping rule
// has fired: every failing check has used its repair-attempt limit. A run
// whose failure comes only from run-level errors (no failing check) fires
// immediately — there is nothing to repair per check.
func verificationStopRuleFired(res *verify.Result, attempts map[string]int, limit int) bool {
	for _, c := range res.Checks {
		if c.Skipped || c.Passed {
			continue
		}
		if attempts[checkAttemptKey(c)] < limit {
			return false
		}
	}
	return true
}

// checkAttemptKey is the per-check counter key for the repair loop
// (SP-149 §149c): the kind string, plus the interaction item id for
// interaction checks (each scripted flow is its own check and counts its
// own attempts, so two flows never share a counter). Manual checks never
// fail — they are pre-filled skipped — so they never need a key.
func checkAttemptKey(c verify.Check) string {
	if c.Kind == plancontract.KindInteraction && len(c.Items) > 0 {
		return string(c.Kind) + ":" + c.Items[0]
	}
	return string(c.Kind)
}

// buildVerificationReport renders the structured verification report
// (SP-149 §149c) the hook feeds back to continue the turn. It is
// deterministic over (result, attempts, limit): an explicit
// <verification-report> envelope carrying
//
//  1. a one-line framing (failing checks found; fix the repairable ones),
//  2. the run summary (res.Summary),
//  3. one bullet per still-repairable failing check: key, attempts used
//     (n/limit), the failure reason, and the check's bounded output
//     excerpt, indented,
//  4. when any check is exhausted, an explicit section telling the model
//     those checks are DONE after the repair limit: stop trying to fix
//     them and state the remaining failure plainly in the final reply
//     (SP-149 §149d).
//
// The report is the query of the continued seedAgent.Run call, so it is
// sent to the model as a user-role message.
func buildVerificationReport(res *verify.Result, attempts map[string]int, limit int) string {
	var b strings.Builder
	b.WriteString(verificationReportOpenTag + "\n")
	b.WriteString("Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.\n")
	b.WriteString("Summary: " + res.Summary() + "\n")

	var repairable, exhausted []verify.Check
	for _, c := range res.Checks {
		if c.Skipped || c.Passed {
			continue
		}
		if attempts[checkAttemptKey(c)] >= limit {
			exhausted = append(exhausted, c)
		} else {
			repairable = append(repairable, c)
		}
	}

	if len(repairable) > 0 {
		b.WriteString("\nFailing checks:\n")
		for _, c := range repairable {
			key := checkAttemptKey(c)
			fmt.Fprintf(&b, "- %s %d/%d — %s\n", key, attempts[key], limit, checkFailureReason(c))
			b.WriteString(indentVerificationExcerpt(c.Excerpt))
		}
	}

	if len(exhausted) > 0 {
		b.WriteString("\nChecks that are DONE after the repair limit — stop trying to fix them and state the remaining failure plainly in your final reply:\n")
		for _, c := range exhausted {
			key := checkAttemptKey(c)
			fmt.Fprintf(&b, "- %s %d/%d\n", key, attempts[key], limit)
		}
	}

	b.WriteString("\n" + verificationReportCloseTag + "\n")
	return b.String()
}

// checkFailureReason is the one-line reason for a failing check's report
// bullet: the check's own reason where one exists (an executor error,
// timeout, or cancellation), otherwise a plain "command failed" — the
// excerpt below the bullet carries the output evidence there.
func checkFailureReason(c verify.Check) string {
	if c.Reason != "" {
		return c.Reason
	}
	return "command failed"
}

// indentVerificationExcerpt indents a check's bounded output excerpt so it
// reads as a block under its report bullet. An empty excerpt renders
// nothing (a failure whose output was empty still carries its reason).
func indentVerificationExcerpt(excerpt string) string {
	excerpt = strings.TrimRight(excerpt, "\n")
	if excerpt == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(excerpt, "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// LastVerificationResult returns the last turn-end verification result
// (SP-149 §149c), stored on every verification run (pass, fail, or
// stop-rule), or nil when the turn-end hook never ran for this agent
// (verification disabled, the turn changed no code, a subagent turn, or a
// runner setup error). The final-reply contract (149.6) and the SP-151
// verification event report from it.
func (a *Agent) LastVerificationResult() *verify.Result {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	if a.turnVerification.result == nil {
		return nil
	}
	return a.turnVerification.result
}

// setTurnVerification stores the per-turn verification state (the hook's
// per-run store: result + repair attempts + limit + repair rounds,
// SP-149 §149d).
func (a *Agent) setTurnVerification(tv turnVerification) {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	a.turnVerification = tv
}

// resetTurnVerification clears the per-turn verification state at the
// turn's start (prepareQueryRun), so a previous turn's stored result,
// attempts, and limit never attach to this turn's reply: a turn's final
// reply may only carry that turn's verification outcome. It also clears
// the turn's frozen verification snapshot (SP-149 §149b) so a previous
// turn's snapshot never feeds a later turn's verification runs.
func (a *Agent) resetTurnVerification() {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	a.turnVerification = turnVerification{}
	a.turnVerifySnapshot = nil
}

// setTurnVerifySnapshot stores the turn's frozen verification input
// (SP-149 §149b): the starter manifest's commands and the plan's acceptance
// captured once at the turn's start. It is guarded by turnVerificationMu and
// cleared at each turn's start (resetTurnVerification), so a turn's
// verification runs against the inputs the turn began with.
func (a *Agent) setTurnVerifySnapshot(snap *verify.Snapshot) {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	a.turnVerifySnapshot = snap
}

// getTurnVerifySnapshot returns the turn's frozen verification input
// (SP-149 §149b), or nil when none was captured for the turn (verification
// was not enabled at the turn's start). The turn-end hook reads it to run
// every repair round against the turn-start inputs instead of re-reading
// the files.
func (a *Agent) getTurnVerifySnapshot() *verify.Snapshot {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	return a.turnVerifySnapshot
}

// currentTurnVerification returns the stored per-turn verification state
// (SP-149 §149d): the single access the final-reply contract reads. The
// struct copy is all that is needed — the hook stores snapshots, so a
// stored attempts map is never mutated after it is stored.
func (a *Agent) currentTurnVerification() turnVerification {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	return a.turnVerification
}

// lastTurnVerificationSnapshot returns a defensive copy of the stored
// per-turn verification state for consumers outside pkg/agent: the
// Attempts map is copied (callers may mutate their copy without touching
// the agent's stored state). The struct value is returned by value.
func (a *Agent) lastTurnVerificationSnapshot() turnVerification {
	a.turnVerificationMu.Lock()
	defer a.turnVerificationMu.Unlock()
	return turnVerification{
		result:   a.turnVerification.result,
		attempts: snapshotVerificationAttempts(a.turnVerification.attempts),
		limit:    a.turnVerification.limit,
		rounds:   a.turnVerification.rounds,
	}
}
