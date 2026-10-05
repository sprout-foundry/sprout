// verification_reply.go — the SP-149 §149c/§149d final-reply contract
// (item 149.6): a final reply may report success only with a passing
// verification result attached, and when the stopping rule fires the reply
// states plainly what passes, what fails, and what was tried. The
// attachment is a deterministic block rendered from the stored per-turn
// verification state (turnVerification) — no NLP rewrites of the model's
// own prose: the failing attachment itself states the failure, which is
// what §149d requires. It is delivered in handleQueryResult, after the
// language guard, through deliverVerificationResult: appended to the final
// reply, written into the last assistant message in state, and — when the
// reply string is suppressed by streaming — emitted as a stream chunk.

package agent

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// attachVerificationReply appends the turn's verification attachment to the
// final reply (SP-149 §149c/§149d / 149.6): a passing result carries its
// summary, a failing result carries the §149d report (what passes, what
// fails, what was tried), and an all-skipped run states that no passing
// result exists — success is never reported without a passing result. It
// returns the reply byte-identical when the turn-end hook never ran for
// the turn (the stored state's result is nil): verification disabled, no
// code change, a subagent turn, or a runner setup error.
func (a *Agent) attachVerificationReply(reply string) string {
	attachment := verificationReplyAttachment(a.currentTurnVerification())
	if attachment == "" {
		return reply
	}
	return strings.TrimRight(reply, "\n") + "\n\n" + attachment
}

// deliverVerificationResult delivers the turn's verification result to the
// user on every path (fix.2): it appends the attachment to the final reply
// (the same formatting attachVerificationReply uses), writes the attachment
// into the last assistant message in state (what the query_completed event's
// response and later turns read), and — exactly when handleQueryResult's
// streaming early-return will suppress the returned result string — emits the
// attachment as a stream chunk (what the streaming CLI's terminal write and
// the Web UI's live stream consume). A turn with no stored verification
// result (verification disabled, no code change, a subagent turn, or a runner
// setup error) is a no-op: byte-identical reply, no state write, no chunk.
func (a *Agent) deliverVerificationResult(reply string) string {
	attachment := verificationReplyAttachment(a.currentTurnVerification())
	if attachment == "" {
		return reply
	}
	a.appendVerificationAttachmentToLastAssistantMessage(attachment)
	if a.suppressVerificationResult() {
		a.PublishStreamChunk("\n\n"+attachment, "assistant_text")
	}
	return a.attachVerificationReply(reply)
}

// suppressVerificationResult reports whether handleQueryResult's streaming
// early-return will suppress the returned result string (the content already
// reached the client via streaming). Exactly that condition: a primary agent
// with streaming enabled and a non-empty streaming buffer. The verification
// attachment is emitted as a stream chunk in this case — and only in this
// case — so the user sees the block once, whether the reply rides in the
// returned string (streaming off, or nothing streamed) or in the chunk.
func (a *Agent) suppressVerificationResult() bool {
	return !a.IsSubagent() && a.output.IsStreamingEnabled() && a.output.GetStreamingBuffer().Len() > 0
}

// appendVerificationAttachmentToLastAssistantMessage writes the verification
// attachment into the last assistant message (with non-empty content) in the
// agent's conversation state, using the copy-slice + SetMessages pattern
// applyLanguageGuard uses. It is what carries the block to the Web UI's
// completion path: the streaming CLI suppresses the result string, so only
// state feeds the query_completed response that the Web UI renders. When no
// assistant message with content exists it is a no-op — the attachment then
// rides only in the returned result string.
func (a *Agent) appendVerificationAttachmentToLastAssistantMessage(attachment string) {
	if a.state == nil {
		return
	}
	messages := a.state.GetMessages()
	index := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].Content != "" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	updated := make([]api.Message, len(messages))
	copy(updated, messages)
	updated[index].Content += "\n\n" + attachment
	a.state.SetMessages(updated)
}

// verificationReplyAttachment renders the SP-149 §149d final-reply
// attachment for one stored turn-verification state. Pure and
// deterministic over the state, so the contract is asserted
// byte-for-byte in the tests. It renders:
//
//   - a passing result: the one-line "passed" block with the run summary;
//   - a failing result (the stopping rule fired, or the turn ended on a
//     failing run): the §149d report — what passes, what fails, and what
//     was tried;
//   - an all-skipped run (nothing failed, nothing ran): the "no passing
//     result" block — success is not corroborated;
//   - an empty state (the hook never ran for the turn): nothing.
func verificationReplyAttachment(tv turnVerification) string {
	if tv.result == nil {
		return ""
	}
	switch {
	case tv.result.Passed():
		return "Verification: passed — " + tv.result.Summary()
	case tv.result.Failed():
		return verificationFailedAttachment(tv)
	default:
		return verificationAllSkippedAttachment(tv.result)
	}
}

// verificationFailedAttachment renders the §149d failure report for a
// failing run: what passes, what fails, and what was tried. The header
// carries the configured repair limit N (the stopping rule's parameter);
// the Passed and Failed sections carry one line per check (keyed like the
// repair loop's per-check counters) plus one line per run-level error;
// the Tried lines list the per-check repair attempts consumed against N.
// A run whose failure comes only from run-level errors tried nothing —
// there was no check to repair.
func verificationFailedAttachment(tv turnVerification) string {
	res := tv.result
	var b strings.Builder
	fmt.Fprintf(&b, "Verification: FAILED after the stopping rule (%d repair attempts)\n", tv.limit)

	passed, failed := verificationCheckKeys(res)
	if len(passed) == 0 {
		b.WriteString("Passed: none\n")
	}
	for _, key := range passed {
		fmt.Fprintf(&b, "Passed: %s\n", key)
	}
	for _, c := range res.Checks {
		if c.Skipped || c.Passed {
			continue
		}
		fmt.Fprintf(&b, "Failed: %s — %s\n", checkAttemptKey(c), checkFailureReason(c))
	}
	for _, runErr := range res.Errors {
		fmt.Fprintf(&b, "Failed: run — %s\n", runErr)
	}
	if len(failed) > 0 {
		for _, key := range failed {
			fmt.Fprintf(&b, "Tried: %s: %d/%d repair attempts\n", key, tv.attempts[key], tv.limit)
		}
	} else {
		b.WriteString("Tried: none (run-level failure; nothing was repaired)")
	}
	return strings.TrimRight(b.String(), "\n")
}

// verificationAllSkippedAttachment renders the attachment for a run that
// verified nothing: every check was skipped and no run-level error was
// recorded. Success is NOT corroborated, so the block states plainly that
// no passing result exists, and lists each skipped check with its reason
// so the reply says what could not be verified (SP-149 §149d).
func verificationAllSkippedAttachment(res *verify.Result) string {
	var b strings.Builder
	b.WriteString("Verification: ran, but no checks applied (all skipped) — no passing result\n")
	for _, c := range res.Checks {
		if !c.Skipped {
			continue
		}
		fmt.Fprintf(&b, "Skipped: %s — %s\n", c.Kind, checkSkipReason(c))
	}
	return strings.TrimRight(b.String(), "\n")
}

// verificationCheckKeys splits a run's checks into the attachment's Passed
// and Failed line keys, in run order, using the repair loop's per-check
// counter keys so the sections align with the report's bullets.
func verificationCheckKeys(res *verify.Result) (passed, failed []string) {
	for _, c := range res.Checks {
		switch {
		case c.Skipped:
		case c.Passed:
			passed = append(passed, checkAttemptKey(c))
		default:
			failed = append(failed, checkAttemptKey(c))
		}
	}
	return passed, failed
}

// checkSkipReason is the one-line reason for a skipped check's "Skipped:"
// line: the check's own reason where one exists (no command, cancellation,
// the manual "verified by a human" note), otherwise a plain fallback.
func checkSkipReason(c verify.Check) string {
	if c.Reason != "" {
		return c.Reason
	}
	return "not executed"
}
