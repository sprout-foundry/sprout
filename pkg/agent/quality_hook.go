// quality_hook.go — the quality-after-edits turn-end hook: after a turn that
// changed application code, and before the final reply, the runtime (not the
// model) runs the project's formatter and then its linter and repairs the
// findings in the same turn. It is a sibling of the verification hook and
// shares its two gates: the same "the turn changed application code" signal
// (Agent.TurnChangedApplicationPaths) and its own opt-in flag (default off).
//
// The runtime executes the formatter, whose in-place rewrite is a repair the
// runtime performs itself; a formatter failure never crashes the turn. The
// linter's findings (and any formatter failure) are fed back to the model as
// a structured quality report and the turn continues, reusing the same
// repair-loop stopping rule the verification hook uses — there is one
// feedback mechanism, not two. After the repair limit the loop stops and the
// last result stands.

package agent

import (
	"fmt"
	"strings"

	core "github.com/sprout-foundry/seed/core"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// Explicit envelope of the structured quality report the hook feeds back to
// the model.
const (
	qualityReportOpenTag  = "<quality-report>"
	qualityReportCloseTag = "</quality-report>"
)

// runTurnEndQuality implements the quality-after-edits gate and repair loop.
// processQueryWithSeed invokes it on the success path, after the turn's first
// seedAgent.Run and before (or alongside) the verification hook: finalResult
// is the turn's answer, and this method either returns it unchanged or
// continues the turn through repair rounds.
//
// No-ops (returns finalResult untouched, no stored result) when:
//   - the agent has no configuration, or quality is disabled — the default;
//   - the turn changed no application code (the run gates on the turn's own
//     changes, Agent.TurnChangedApplicationPaths);
//   - the agent is a subagent (subagent turns never own the final reply).
//
// A runner setup error (no executor, empty root) is logged and returns the
// turn's answer: a quality setup problem never gates the turn. A non-nil
// error from a repair round (interrupted/provider error) is returned so
// handleQueryResult classifies it exactly as a first-run error.
func (a *Agent) runTurnEndQuality(qc *queryRunContext, finalResult string) (string, error) {
	if a.configManager == nil {
		return finalResult, nil
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || !cfg.QualityEnabled() {
		return finalResult, nil
	}
	if len(a.TurnChangedApplicationPaths()) == 0 {
		return finalResult, nil
	}
	if a.subagentDepth > 0 {
		return finalResult, nil
	}

	// The runner resolves commands only from the starter manifest and the
	// explicit project configuration — never from model output. The commands
	// are frozen once at hook entry (a snapshot): a model that edits
	// .sprout/starter.json during a repair round cannot change what "clean"
	// means for the turn, mirroring the verification hook's snapshot.
	runner := verify.NewQualityRunner()
	runner.ConfigCommands = verify.QualityConfigurationCommands(cfg)
	snap := runner.Snapshot(a.GetWorkspaceRoot())

	// The repair bounds are the configuration's repair limits — the same
	// limits the verification loop uses, so there is one repair-budget
	// setting, not two.
	limit := cfg.VerificationRepairAttempts()
	totalCap := cfg.VerificationRepairTotalRounds()
	attempts := make(map[string]int)
	rounds := 0

	for {
		// A stop in the window between the turn's answer and a quality run
		// (or between repair rounds) reports as an interrupt, not a
		// completed turn.
		if qc.runCtx.Err() != nil {
			return finalResult, fmt.Errorf("%w: %w", core.ErrInterrupted, qc.runCtx.Err())
		}
		res, err := runner.RunQualitySnapshot(qc.runCtx, a.GetWorkspaceRoot(), snap)
		if err != nil {
			a.Logger().Debug("turn-end quality setup error: %v\n", err)
			return finalResult, nil
		}
		a.setTurnQuality(res)

		checks := qualityChecksAsVerifyChecks(res)
		if !res.Failed() {
			// Passing, or all-skipped where nothing failed — both stand.
			return finalResult, nil
		}
		if repairLoopShouldStop(checks, attempts, limit, rounds, totalCap) {
			// The stopping rule fired — every failing step has used its
			// repair attempts, or the turn's total rounds reached the cap.
			return finalResult, nil
		}
		for _, c := range checks {
			if c.Skipped || c.Passed {
				continue
			}
			attempts[checkAttemptKey(c)]++
		}

		// Continue the turn: the report lands as a user-role message in the
		// transcript (the mechanism steer messages and the verification
		// repair loop both use) and the repair round's answer becomes the
		// final result.
		report := buildQualityReport(res, attempts, limit)
		rounds++
		repairResult, repairErr := qc.seedAgent.Run(qc.runCtx, report)
		if repairErr != nil {
			return repairResult, repairErr
		}
		finalResult = repairResult
	}
}

// qualityChecksAsVerifyChecks adapts a quality result's checks to the shared
// repair-loop check shape, so the quality loop reuses the verification loop's
// stopping rule and per-check counter keys rather than a second mechanism.
// The quality kinds (format, lint) are their own counter keys; only the
// fields the stop rule reads are populated.
func qualityChecksAsVerifyChecks(res *verify.QualityResult) []verify.Check {
	if res == nil {
		return nil
	}
	checks := make([]verify.Check, 0, len(res.Checks))
	for _, c := range res.Checks {
		checks = append(checks, verify.Check{
			Kind:    plancontract.Kind(c.Kind),
			Command: c.Command,
			Skipped: c.Skipped,
			Passed:  c.Passed,
			Reason:  c.Reason,
			Excerpt: c.Excerpt,
		})
	}
	return checks
}

// buildQualityReport renders the structured quality report the hook feeds
// back to continue the turn. It is deterministic over (result, attempts,
// limit): an explicit <quality-report> envelope carrying a framing line, the
// run summary, one bullet per still-repairable failing step (key, attempts
// used n/limit, reason, an optional "Classification:" line naming the failure
// kind when the step's output matches a known error shape, and the step's
// bounded output excerpt indented), and — when any step is exhausted — an
// explicit section telling the model those steps are DONE after the repair
// limit.
func buildQualityReport(res *verify.QualityResult, attempts map[string]int, limit int) string {
	var b strings.Builder
	b.WriteString(qualityReportOpenTag + "\n")
	b.WriteString("Turn-end quality checks found findings. Fix the repairable ones, then finish the turn.\n")
	b.WriteString("Summary: " + res.Summary() + "\n")

	var repairable, exhausted []verify.Check
	for _, c := range qualityChecksAsVerifyChecks(res) {
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
		b.WriteString("\nFindings:\n")
		for _, c := range repairable {
			key := checkAttemptKey(c)
			fmt.Fprintf(&b, "- %s %d/%d — %s\n", key, attempts[key], limit, checkFailureReason(c))
			if annotation := errclassAnnotation(c.Excerpt); annotation != "" {
				b.WriteString("  Classification: " + annotation + "\n")
			}
			b.WriteString(indentVerificationExcerpt(c.Excerpt))
		}
	}

	if len(exhausted) > 0 {
		b.WriteString("\nSteps that are DONE after the repair limit — stop trying to fix them and state the remaining findings plainly in your final reply:\n")
		for _, c := range exhausted {
			key := checkAttemptKey(c)
			fmt.Fprintf(&b, "- %s %d/%d\n", key, attempts[key], limit)
		}
	}

	b.WriteString("\n" + qualityReportCloseTag + "\n")
	return b.String()
}

// setTurnQuality stores the turn's last quality run result.
func (a *Agent) setTurnQuality(res *verify.QualityResult) {
	a.turnQualityMu.Lock()
	defer a.turnQualityMu.Unlock()
	a.turnQuality = res
}

// resetTurnQuality clears the per-turn quality state at the turn's start, so a
// previous turn's quality result never attaches to this turn.
func (a *Agent) resetTurnQuality() {
	a.turnQualityMu.Lock()
	defer a.turnQualityMu.Unlock()
	a.turnQuality = nil
}

// LastQualityResult returns the turn's last quality-after-edits run, or nil
// when the hook never ran for the turn (quality disabled, no code change, a
// subagent turn, or a runner setup error).
func (a *Agent) LastQualityResult() *verify.QualityResult {
	a.turnQualityMu.Lock()
	defer a.turnQualityMu.Unlock()
	return a.turnQuality
}
