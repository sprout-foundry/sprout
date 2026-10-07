// verification_report.go — the verification hook's report builder and the
// shared repair-loop stopping rule. Everything here is pure and
// deterministic so the report the model sees, the per-check counter keys,
// and the loop's termination decision are asserted byte-for-byte (and
// termination-pattern-wise) in unit tests without a workspace or a shell.

package agent

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

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

// repairLoopShouldStop is the shared stopping rule for a turn-end repair loop
// over a set of checks: the total-rounds cap fires first (when the loop has
// already run totalCap repair rounds), then the per-check rule (every failing
// check has used its repair-attempt limit). Both the verification loop and
// the quality loop read it, so the two feed a failing check back to the model
// through exactly one mechanism. A check set whose failure comes only from
// run-level findings (no failing check) fires the per-check rule immediately —
// there is nothing to repair per check.
func repairLoopShouldStop(checks []verify.Check, attempts map[string]int, limit, rounds, totalCap int) bool {
	if rounds >= totalCap {
		return true
	}
	return repairStopRuleFired(checks, attempts, limit)
}

// verificationStopRuleFired reports whether the SP-149 §149c stopping rule
// has fired: every failing check has used its repair-attempt limit. A run
// whose failure comes only from run-level errors (no failing check) fires
// immediately — there is nothing to repair per check.
func verificationStopRuleFired(res *verify.Result, attempts map[string]int, limit int) bool {
	if res == nil {
		return true
	}
	return repairStopRuleFired(res.Checks, attempts, limit)
}

// repairStopRuleFired is the per-check half of the stopping rule: every
// failing check has used its repair-attempt limit. A check set with no failing
// check fires immediately.
func repairStopRuleFired(checks []verify.Check, attempts map[string]int, limit int) bool {
	for _, c := range checks {
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
//     (n/limit), the failure reason, an optional "Classification:" line
//     naming the failure kind when the check's output matches a known
//     build/runtime error shape, and the check's bounded output excerpt,
//     indented,
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
			if annotation := errclassAnnotation(c.Excerpt); annotation != "" {
				b.WriteString("  Classification: " + annotation + "\n")
			}
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
