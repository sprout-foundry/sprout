package core

import (
	"fmt"
	"sort"
	"strings"
)

// ToolThreadingViolation describes a single place in the message list where the
// tool-call threading invariants — enforced strictly by providers like MiniMax
// and DeepSeek — are broken. ValidateToolThreading produces a slice of these.
//
// The invariants are:
//
//   - Every tool result (role "tool") must have a tool_call_id that matches a
//     tool call ID in the most recent preceding assistant message containing
//     tool calls.
//   - Tool results must appear immediately after that assistant message, with
//     no interleaved user/system/assistant-without-tool-calls message between
//     the assistant turn and its results.
//   - When an assistant message has N tool calls, the following tool results
//     (up to the next non-tool message) must appear in the same order as the
//     tool calls. A result whose call ID is not in the assistant message is an
//     orphan and is reported separately.
//
// Providers that reject malformed threading return HTTP 400 (MiniMax error
// code 2013: "tool call result does not follow tool call"). This validator
// localizes exactly which messages break the contract so a diagnostic capture
// can be saved before the failing request is retried or surfaced.
type ToolThreadingViolation struct {
	// Kind categorizes the violation. Use the ToolThreadingViolation* constants.
	Kind string `json:"kind"`
	// Index is the position in the message list of the offending message.
	Index int `json:"index"`
	// ToolCallID is the tool_call_id implicated, when applicable.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Detail is a human-readable explanation of the violation.
	Detail string `json:"detail"`
}

// Violation kinds.
const (
	// ToolThreadingViolationOrphanResult: a tool result whose tool_call_id does
	// not match any tool call in the preceding assistant message (or there is no
	// preceding assistant message with tool calls at all).
	ToolThreadingViolationOrphanResult = "orphan_result"
	// ToolThreadingViolationMissingResult: an assistant message has a tool call
	// with no matching tool result in the immediately following run of tool
	// messages. Providers reject "tool call without result".
	ToolThreadingViolationMissingResult = "missing_result"
	// ToolThreadingViolationOutOfOrder: tool results following an assistant
	// message are present for all calls but appear in a different order than the
	// tool calls. This is the classic reorder case.
	ToolThreadingViolationOutOfOrder = "out_of_order"
	// ToolThreadingViolationGapBeforeResult: a non-tool message (user, system,
	// or assistant without tool calls) appears between an assistant tool-call
	// message and its results, breaking the "results immediately follow" rule.
	ToolThreadingViolationGapBeforeResult = "gap_before_result"
)

// ValidateToolThreading checks a message list against the tool-call threading
// invariants and returns a violation for each problem found. An empty (or nil)
// slice means the list is well-formed. It never mutates the input.
//
// The check is purely structural; it does not look at message content. It is
// cheap enough to run on every prepared request as a final safety net.
func ValidateToolThreading(messages []Message) []ToolThreadingViolation {
	var violations []ToolThreadingViolation

	// Map of every tool call ID declared by any assistant message, so we can
	// distinguish "orphan" (no call anywhere) from "missing result" (call
	// exists but no result follows it).
	allCallIDs := make(map[string]bool)
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				allCallIDs[tc.ID] = true
			}
		}
	}

	i := 0
	for i < len(messages) {
		msg := messages[i]

		// Flag tool results that appear with no preceding assistant tool-call
		// message at all (or whose id matches no known call).
		if msg.Role == "tool" {
			if !allCallIDs[msg.ToolCallID] {
				violations = append(violations, ToolThreadingViolation{
					Kind:       ToolThreadingViolationOrphanResult,
					Index:      i,
					ToolCallID: msg.ToolCallID,
					Detail:     "tool result has no matching tool call in any preceding assistant message",
				})
			}
			i++
			continue
		}

		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			i++
			continue
		}

		// Assistant message with tool calls. The results must start at i+1.
		assistantIdx := i
		callOrder := msg.ToolCalls
		expectedIDs := make(map[string]int, len(callOrder))
		for j, tc := range callOrder {
			if tc.ID != "" {
				expectedIDs[tc.ID] = j
			}
		}

		// Collect the contiguous run of tool results immediately following.
		j := assistantIdx + 1
		var results []Message
		for j < len(messages) && messages[j].Role == "tool" {
			results = append(results, messages[j])
			j++
		}
		resultEnd := j // exclusive

		// Detect a gap: a non-tool message between this assistant turn and any
		// later tool results that DO belong to it. If there are later tool
		// results whose IDs match this assistant's calls but appear after a
		// gap, that's a threading break.
		if len(results) < len(expectedIDs) {
			// Look ahead past a non-tool message to see if results for these
			// calls appear later (gap violation).
			for k := resultEnd; k < len(messages); k++ {
				if messages[k].Role != "tool" {
					// Only flag as a gap if there ARE stray results for this
					// assistant's calls after the gap. Otherwise it's just the
					// natural end of the turn (handled by missing_result).
					hasStrayForThis := false
					for kk := k + 1; kk < len(messages); kk++ {
						if messages[kk].Role == "tool" {
							if _, ok := expectedIDs[messages[kk].ToolCallID]; ok {
								hasStrayForThis = true
								break
							}
						}
					}
					if hasStrayForThis {
						violations = append(violations, ToolThreadingViolation{
							Kind:   ToolThreadingViolationGapBeforeResult,
							Index:  k,
							Detail: fmt.Sprintf("non-tool message separates assistant tool calls (at %d) from their results", assistantIdx),
						})
					}
					break
				}
			}
		}

		// Check for missing results: calls without a matching result.
		resultIDs := make(map[string]bool, len(results))
		for _, r := range results {
			resultIDs[r.ToolCallID] = true
		}
		for _, tc := range callOrder {
			if tc.ID == "" {
				continue
			}
			if !resultIDs[tc.ID] {
				violations = append(violations, ToolThreadingViolation{
					Kind:       ToolThreadingViolationMissingResult,
					Index:      assistantIdx,
					ToolCallID: tc.ID,
					Detail:     "assistant tool call has no matching tool result in the immediately following run",
				})
			}
		}

		// Check ordering: results must appear in the same order as the calls.
		if len(results) > 1 {
			// Build expected position for each result id.
			inOrder := true
			lastPos := -1
			for _, r := range results {
				pos, ok := expectedIDs[r.ToolCallID]
				if !ok {
					// orphan within this block — not an ordering issue
					continue
				}
				if pos <= lastPos {
					inOrder = false
					break
				}
				lastPos = pos
			}
			if !inOrder {
				// Summarize: actual order vs expected order.
				var actualOrder []string
				var expectedOrder []string
				for _, r := range results {
					if r.ToolCallID != "" {
						actualOrder = append(actualOrder, shortID(r.ToolCallID))
					}
				}
				for _, tc := range callOrder {
					if tc.ID != "" {
						expectedOrder = append(expectedOrder, shortID(tc.ID))
					}
				}
				violations = append(violations, ToolThreadingViolation{
					Kind:   ToolThreadingViolationOutOfOrder,
					Index:  assistantIdx + 1,
					Detail: fmt.Sprintf("tool results out of order: got [%s], expected [%s]", strings.Join(actualOrder, ", "), strings.Join(expectedOrder, ", ")),
				})
			}
		}

		i = resultEnd
	}

	return violations
}

// shortID returns a shortened tool call id for compact diagnostic output.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// sortViolations returns violations sorted by index then kind for stable
// diagnostic output.
func sortViolations(v []ToolThreadingViolation) {
	sort.SliceStable(v, func(i, j int) bool {
		if v[i].Index != v[j].Index {
			return v[i].Index < v[j].Index
		}
		return v[i].Kind < v[j].Kind
	})
}

// DiagnosticCapture is the payload delivered to Options.OnDiagnosticCapture
// when the agent detects a condition severe enough to warrant saving the full
// conversation transcript for offline analysis. The primary trigger today is a
// tool-call threading rejection (MiniMax error 2013) — a recurring, hard to
// reproduce failure where the prepared message list breaks a provider's
// tool-call/result ordering invariants.
//
// The capture freezes a point-in-time snapshot of exactly what the agent was
// about to send (or did send) so the violation can be reproduced and fixed.
type DiagnosticCapture struct {
	// Trigger explains why the capture was taken. Use the DiagnosticTrigger*
	// constants.
	Trigger string `json:"trigger"`
	// Provider is the model/provider the request was directed at.
	Provider string `json:"provider"`
	// Iteration is the conversation-loop iteration number (0-based) at capture.
	Iteration int `json:"iteration"`
	// Violations is the non-empty set of tool-threading problems found by
	// ValidateToolThreading. Empty when the capture was triggered purely by a
	// provider error before local validation ran.
	Violations []ToolThreadingViolation `json:"violations,omitempty"`
	// Messages is the exact message list that was (or would be) sent to the
	// provider. This is the authoritative artifact for reproduction.
	Messages []Message `json:"messages"`
	// Error is the provider error that triggered the capture, when reactive.
	// Empty for proactive captures.
	Error string `json:"error,omitempty"`
	// SystemPrompt is the active system prompt at capture time.
	SystemPrompt string `json:"system_prompt,omitempty"`
}

// Diagnostic trigger reasons.
const (
	// DiagnosticTriggerProviderRejection: the provider returned a tool-threading
	// rejection (e.g. MiniMax 2013) and the capture was taken reactively.
	DiagnosticTriggerProviderRejection = "provider_threading_rejection"
	// DiagnosticTriggerPreSendValidation: the pre-send validator detected
	// threading violations and the capture was taken proactively before the
	// request went out.
	DiagnosticTriggerPreSendValidation = "pre_send_validation"
)
