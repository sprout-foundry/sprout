// progress_question_events.go — emit progress_question alongside
// ask_user_request whenever the agent asks the
// user a decision, carrying the plan context (the active scope item and plan
// revision) so the question is correlated to the plan.
//
// progress_question complements the interactive ask_user_request event: it is
// the progress-stream record that a decision is pending, so a webhook or
// embedding UI can surface "the agent needs a decision on scope X" without
// parsing the interactive event. It is emitted at the same ask_user entry
// points as ask_user_request (the native handler and the AskUserService), and
// BEFORE the ask channel is engaged, so for one question it precedes the
// ask_user_request event. The two entry points are mutually exclusive (a
// question is dispatched through exactly one), so emitting at both never
// double-emits for a single question.
//
// The payload is built as map[string]interface{} with the exact snake_case
// wire names of events.ProgressQuestionData, not the Go struct,
// so decorateEventPayload can merge the event metadata (chat_id). Zero/empty
// optional keys are omitted (plan_revision when 0, scope_id when empty,
// header when empty, options when empty, and each option's value/description
// when empty). run_id (the session id) and question are always present.
// why_it_matters is deliberately omitted: tools.AskUserRequest carries no
// source field for it, so it stays out of the payload.

package agent

import (
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// publishProgressQuestion emits the progress_question event for an ask_user
// decision, correlated to the active plan scope item
// and plan revision (planQuestionContext). It is called at the ask_user entry
// points (the native handler and the AskUserService) before the ask channel is
// engaged, so the progress_question precedes the ask_user_request for the same
// question.
//
// A nil receiver is a harmless no-op; with no event bus wired, publishEvent
// drops the event (the bus-nil guard lives in publishEvent), so this is a
// no-op on a bare agent.
func (a *Agent) publishProgressQuestion(req tools.AskUserRequest) {
	if a == nil {
		return
	}
	scopeID, planRev := a.planQuestionContext()
	runID := a.GetSessionID()

	payload := map[string]interface{}{
		"run_id":   runID,
		"question": req.Question,
	}
	if planRev > 0 {
		payload["plan_revision"] = planRev
	}
	if scopeID != "" {
		payload["scope_id"] = scopeID
	}
	if req.Header != "" {
		payload["header"] = req.Header
	}
	if len(req.Options) > 0 {
		payload["options"] = progressQuestionOptions(req.Options)
	}
	a.publishEvent(events.EventTypeProgressQuestion, payload)
}

// progressQuestionOptions maps the ask_user options to the progress_question
// option shape: the same keys as events.AskUserRequestOption (label, value,
// description), with value and description omitted when empty (mirroring that
// struct's omitempty). Each option always carries its label.
func progressQuestionOptions(options []tools.AskUserOption) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(options))
	for _, opt := range options {
		entry := map[string]interface{}{
			"label": opt.Label,
		}
		if opt.Value != "" {
			entry["value"] = opt.Value
		}
		if opt.Description != "" {
			entry["description"] = opt.Description
		}
		out = append(out, entry)
	}
	return out
}
