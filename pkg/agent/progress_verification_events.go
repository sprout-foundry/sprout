// progress_verification_events.go — emit the run's
// progress_verification and progress_complete events at turn completion,
// from the turn-end verification result.
//
// The turn-end verification hook stores the final *verify.Result on the
// agent's per-turn state (reset each turn). handleQueryResult calls
// publishTurnProgressComplete on the success path — after the verification
// reply has been attached, and before the commit/finalize/streaming
// early-returns — so both the streaming and the non-streaming success
// outcomes emit the pair. When a verification result exists the two events
// arrive in a fixed order: progress_verification (the evidence) first, then
// progress_complete (the verdict). Both carry the same correlation ids (the
// run id, the session id, and the plan revision). When the hook never ran
// for the turn (verification disabled, no code change, a subagent turn, or a
// runner setup error) only progress_complete is emitted: verified is omitted
// (false). When verification is enabled, not_verified_reason states why
// there is no result; when it is disabled (the CLI default) the event
// carries only run_id — no not-verified content — so the default user sees
// no per-turn notice (default UI unchanged).
//
// The payloads are built as map[string]interface{} with the exact snake_case
// wire names (mirroring the events.ProgressVerificationData /
// events.ProgressCompleteData structs) rather than the Go
// structs, so decorateEventPayload can merge the event metadata (chat_id)
// into them and the field names are the public contract.

package agent

import (
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// publishTurnProgressComplete emits the turn-completion progress
// events from the turn-end verification result the hook
// stored for this turn. It is the single call site: handleQueryResult invokes
// it on the success path, after the verification reply attachment.
//
// When a verification result exists it emits progress_verification — the
// evidence: the checks, pass/fail, and run-level errors — and then
// progress_complete with the same result nested under verification. That
// ordering is the contract (the verdict follows the evidence), and both
// events carry the same run_id (the session id) and plan_revision.
//
// When the hook never ran for the turn (nil stored result) it emits only
// progress_complete: verified is omitted (false), and not_verified_reason
// states why there is no result — except when verification is disabled
// (the default), in which case the payload carries only run_id and the
// renderers show nothing. No progress_verification is emitted either way —
// there was no result to report.
//
// publishEvent drops the event when no event bus is wired, so this is a no-op
// on a bare agent (the bus-nil guard lives in publishEvent).
//
// Subagent turns are part of the parent's run, not independent runs: a
// subagent shares the parent's event bus but carries a fresh (empty) session
// id, so its progress events would arrive with an empty run_id and read to a
// consumer as extra "run finished" signals for one run. Subagent turns are
// therefore silent for progress events (matching the verification hook's subagent
// guard, verification_hook.go).
func (a *Agent) publishTurnProgressComplete() {
	if a.subagentDepth > 0 {
		return
	}
	tv := a.currentTurnVerification()
	res := tv.result
	runID := a.GetSessionID()

	if res != nil {
		a.publishEvent(events.EventTypeProgressVerification, progressVerificationPayload(res, runID))
		a.publishEvent(events.EventTypeProgressComplete, progressCompletePayload(res, runID, ""))
		return
	}
	a.publishEvent(events.EventTypeProgressComplete, progressCompletePayload(nil, runID, a.notVerifiedReason()))
}

// progressVerificationData builds the map shared by the progress_verification
// event and the nested verification field of progress_complete:
// the identical shape in both places, so the two can never drift. The
// keys are exactly the snake_case wire names of events.ProgressVerificationData;
// zero/empty optional keys are omitted (mirroring that struct's omitempty):
// plan_revision when 0, baseline when false, passed when false, and errors
// when empty. checks is always present — a result with no checks emits an
// empty slice ([]), never null. The verification runner's execution metadata
// (routes, screenshots, steps, duration) is deliberately dropped to keep the
// event compact; the full result stays server-side.
func progressVerificationData(res *verify.Result, runID string) map[string]interface{} {
	checks := make([]map[string]interface{}, 0, len(res.Checks))
	for _, c := range res.Checks {
		check := map[string]interface{}{
			"kind": string(c.Kind),
		}
		if len(c.Items) > 0 {
			check["items"] = c.Items
		}
		if c.Command != "" {
			check["command"] = c.Command
		}
		if c.Skipped {
			check["skipped"] = true
		}
		if c.Passed {
			check["passed"] = true
		}
		if c.Reason != "" {
			check["reason"] = c.Reason
		}
		if c.Excerpt != "" {
			check["excerpt"] = c.Excerpt
		}
		checks = append(checks, check)
	}

	payload := map[string]interface{}{
		"run_id": runID,
		"checks": checks,
	}
	if res.PlanRevision > 0 {
		payload["plan_revision"] = res.PlanRevision
	}
	if res.Baseline {
		payload["baseline"] = true
	}
	if res.Passed() {
		payload["passed"] = true
	}
	if len(res.Errors) > 0 {
		payload["errors"] = res.Errors
	}
	return payload
}

// progressVerificationPayload is the progress_verification event payload:
// the verification map itself.
func progressVerificationPayload(res *verify.Result, runID string) map[string]interface{} {
	return progressVerificationData(res, runID)
}

// progressCompletePayload is the progress_complete event payload.
// When res is non-nil it carries run_id, plan_revision (omitted when
// 0), verified (res.Passed(), omitted when false), and verification — the same
// map the standalone progress_verification event carries (shared builder, so
// the two cannot drift). When res is nil it carries run_id and
// not_verified_reason (omitted when empty): verified is omitted (false) and
// there is no verification key, because there was no result to attach. The
// disabled-verification path passes an empty reason, so the event is the
// run-completion signal only (run_id) with no not-verified content.
func progressCompletePayload(res *verify.Result, runID string, notVerifiedReason string) map[string]interface{} {
	payload := map[string]interface{}{
		"run_id": runID,
	}
	if res != nil {
		if res.PlanRevision > 0 {
			payload["plan_revision"] = res.PlanRevision
		}
		if res.Passed() {
			payload["verified"] = true
		}
		payload["verification"] = progressVerificationData(res, runID)
		return payload
	}
	if notVerifiedReason != "" {
		payload["not_verified_reason"] = notVerifiedReason
	}
	return payload
}

// notVerifiedReason states why a completed turn has no verification result
// (progress_complete.not_verified_reason), mirroring the
// turn-end hook's guard conditions. It is cheap — a config read plus the
// turn's application-code paths, no I/O:
//
//   - "" — verification is disabled (no configuration manager, or the
//     configuration does not enable verification, the CLI default).
//     A disabled turn carries no not-verified content, so the
//     default user sees no per-turn notice (default UI unchanged);
//   - "no code changes this turn" — verification is enabled but the turn
//     changed no application code (the hook gates on the turn's own
//     application-code changes — a docs-only or .sprout-bookkeeping
//     turn reads as no code changes);
//   - "verification did not run this turn" — verification is enabled and the
//     turn changed application code, but the hook still did not run (a
//     subagent turn, or a runner setup error).
func (a *Agent) notVerifiedReason() string {
	if a == nil || a.configManager == nil {
		return ""
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || !cfg.VerificationEnabled() {
		return ""
	}
	if len(a.TurnChangedApplicationPaths()) == 0 {
		return "no code changes this turn"
	}
	return "verification did not run this turn"
}
