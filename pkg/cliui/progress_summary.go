//go:build !js

package cliui

// progress_summary.go — deterministic template summaries for the SP-151
// progress events (SP-151 §151c, item 151.6). Each template is a pure
// function of the event payload fields: no model call, no invented
// detail. The optional summarizer-role summary (item 151.8) layers on
// top of these and falls back to them; the web UI progress strip (item
// 151.7) renders the same text.

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ProgressMilestoneSummary renders a progress_milestone event (SP-151
// §151a) as a single line. A coalesced batch (SP-151 §151b — the payload
// carries a "milestones" array of flat payloads) renders as a count
// only; a flat milestone renders the scope item's phase. Returns "" when
// there is nothing to say.
func ProgressMilestoneSummary(data map[string]interface{}) string {
	if batch, ok := data["milestones"].([]interface{}); ok {
		// A coalesced run of milestones (n>=2 by construction of
		// 151.5) stays one line — do not expand the batch.
		return fmt.Sprintf("Milestones: %d", len(batch))
	}
	phase, _ := data["phase"].(string)
	title, _ := data["scope_title"].(string)
	if title == "" {
		title, _ = data["scope_id"].(string)
	}
	switch phase {
	case events.MilestonePhaseFinished:
		files := ReadEventInt(data, "files_touched")
		if title == "" {
			title = "run"
		}
		if files > 0 {
			return fmt.Sprintf("Finished: %s (%d files)", title, files)
		}
		return "Finished: " + title
	default:
		// "started" or an unrecognized phase. An empty or unknown
		// phase with no title has nothing to say.
		if title == "" {
			return ""
		}
		return "Started: " + title
	}
}

// ProgressVerificationSummary renders a progress_verification event
// (SP-151 §151a) as a single "Checks: <passed>/<total> passed" line. A
// skipped check is listed, never counted as passed. Returns "" when
// nothing ran.
func ProgressVerificationSummary(data map[string]interface{}) string {
	checks, _ := data["checks"].([]interface{})
	return verificationChecksSummary(checks)
}

// verificationChecksSummary is the shared passed/total logic over a
// checks list (SP-151 §151c): "Checks: <passed>/<total> passed", or ""
// when the list is empty. A check counts as passed only when its
// "passed" field is true — a skipped check is never passed.
func verificationChecksSummary(checks []interface{}) string {
	if len(checks) == 0 {
		return ""
	}
	passed := 0
	for _, c := range checks {
		check, _ := c.(map[string]interface{})
		if p, _ := check["passed"].(bool); p {
			passed++
		}
	}
	return fmt.Sprintf("Checks: %d/%d passed", passed, len(checks))
}

// ProgressCompleteSummary renders a progress_complete event (SP-151
// §151a) as a single line: a verified run carries the final check count
// when the nested verification has checks, an unverified run carries
// its reason when one is present. A run that is not verified and carries
// no reason (verification disabled, the default) renders nothing — the
// CLI's empty-summary suppression then prints no line, so the default
// user sees no per-turn "not verified" notice.
func ProgressCompleteSummary(data map[string]interface{}) string {
	if verified, _ := data["verified"].(bool); verified {
		if nested, ok := data["verification"].(map[string]interface{}); ok {
			checks, _ := nested["checks"].([]interface{})
			if summary := verificationChecksSummary(checks); summary != "" {
				return "Run complete — verified (" + summary + ")"
			}
		}
		return "Run complete — verified"
	}
	if reason, _ := data["not_verified_reason"].(string); reason != "" {
		return "Run complete — not verified (" + reason + ")"
	}
	return ""
}

// ProgressQuestionSummary renders a progress_question event (SP-151
// §151a) as a single line. The terminal does not print this one — the
// interactive ask_user prompt already shows the decision (see
// HandleProgressEvent); the template exists for the web UI and webhooks
// (SP-151 §151c / §151d). Returns "" when the question is empty.
func ProgressQuestionSummary(data map[string]interface{}) string {
	question, _ := data["question"].(string)
	if question == "" {
		return ""
	}
	return "Needs a decision: " + question
}

// ProgressEventSummary dispatches a progress event to its deterministic
// template summary (SP-151 §151c). Returns "" for non-progress event
// types or when the template has nothing to say.
func ProgressEventSummary(eventType string, data map[string]interface{}) string {
	switch eventType {
	case events.EventTypeProgressMilestone:
		return ProgressMilestoneSummary(data)
	case events.EventTypeProgressVerification:
		return ProgressVerificationSummary(data)
	case events.EventTypeProgressComplete:
		return ProgressCompleteSummary(data)
	case events.EventTypeProgressQuestion:
		return ProgressQuestionSummary(data)
	default:
		return ""
	}
}
