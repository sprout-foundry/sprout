package agent

import (
	"strings"
	"sync"

	core "github.com/sprout-foundry/seed/core"
)

// specialTokenGuardState tracks the guard's per-streak budget. Reset on
// any assistant message that does NOT end in a special token (a clean
// response proves the model escaped the loop).
type specialTokenGuardState struct {
	mu sync.Mutex
	// fired counts hints appended in the current streak (capped).
	fired int
	// lastHinted is the assistant content the last hint was appended for.
	// Provider calls retry internally on transient errors; a retry presents
	// the identical message list, so deduping on content prevents one
	// logical call from burning the whole budget.
	lastHinted string
}

// specialTokenSuffixes are the control-token literals whose emission as
// an actual token ID terminates generation on tokenizer-misconfigured
// serving stacks (observed: vLLM + Qwen int4, finish_reason reported as
// "stop" while prose is cut mid-sentence exactly at the token position).
// The model often *tries* to write these literals as text — the ASCII
// bytes survive the round-trip — so an assistant message ending in one
// is the observable signature of the failure.
var specialTokenSuffixes = []string{
	"<|im_start|>",
	"<|im_end|>",
	"<|endoftext|>",
	"<|tool_call|>",
	"<|eot_id|>",
}

// specialTokenHintCap bounds how many corrective hints the provider can
// append per streak. The failure repeats across iterations when the
// model keeps emitting the literal; after two corrections we stop
// adding hints and let seed's own continuation budget end the turn.
const specialTokenHintCap = 2

// hintPrefix is the stable prefix of specialTokenHint used to detect an
// already-appended hint without rescanning for the full text.
const hintPrefix = "IMPORTANT: Do not emit the literal byte sequence"

// specialTokenHint is appended as a trailing user-role instruction when
// the guard fires. It deliberately contains no raw special-token bytes:
// the hint itself must not re-prime the exact failure it corrects.
const specialTokenHint = hintPrefix + " of any chat template " +
	"control token (the angle-bracket pipe sequences like im_start/im_end markers). " +
	"When you need to mention one, describe it in words (e.g. \"the im_end marker\") " +
	"or use look-alike brackets. Your last response was cut off because a control " +
	"token terminated the generation."

// endsWithSpecialToken reports whether content ends (after trimming
// trailing whitespace) with one of the known special-token literals.
// Exact-suffix match only: a token literal followed by a closing
// backtick or quote is legitimately-quoted text, not the failure
// signature.
func endsWithSpecialToken(content string) bool {
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed == "" {
		return false
	}
	for _, tok := range specialTokenSuffixes {
		if strings.HasSuffix(trimmed, tok) {
			return true
		}
	}
	return false
}

// observeAndHint inspects the outgoing message list. When the last
// assistant message ends in a special-token literal and no corrective
// hint is already present, it appends the hint as a final user message.
// The hint is additive to any seed continuation nudge already in the
// list — it names the cause seed's validators cannot see — and must be
// last so the model attends to it.
func (sp *sproutProvider) observeAndHint(messages []core.Message) []core.Message {
	var lastAssistant string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistant = messages[i].Content
			break
		}
	}
	if lastAssistant == "" {
		return messages
	}

	if !endsWithSpecialToken(lastAssistant) {
		sp.resetSpecialTokenGuard()
		return messages
	}

	sp.specialTokenGuard.mu.Lock()
	defer sp.specialTokenGuard.mu.Unlock()

	// Retry dedupe: the provider retries transient errors with the same
	// message list; only a NEW truncation (different content) consumes budget.
	if sp.specialTokenGuard.lastHinted == lastAssistant {
		return messages
	}
	if sp.specialTokenGuard.fired >= specialTokenHintCap {
		return messages
	}
	if hintPresent(messages) {
		return messages
	}

	sp.specialTokenGuard.fired++
	sp.specialTokenGuard.lastHinted = lastAssistant
	out := make([]core.Message, len(messages), len(messages)+1)
	copy(out, messages)
	out = append(out, core.Message{Role: "user", Content: specialTokenHint})
	return out
}

// resetSpecialTokenGuard clears the hint budget after a clean response.
// A separate method (rather than inline reset in observeAndHint) so the
// reset path never holds specialTokenGuard.mu.
func (sp *sproutProvider) resetSpecialTokenGuard() {
	sp.specialTokenGuard.mu.Lock()
	sp.specialTokenGuard.fired = 0
	sp.specialTokenGuard.lastHinted = ""
	sp.specialTokenGuard.mu.Unlock()
}

// hintPresent reports whether any user message in the list already
// carries the corrective hint.
func hintPresent(messages []core.Message) bool {
	for _, m := range messages {
		if m.Role == "user" && strings.HasPrefix(m.Content, hintPrefix) {
			return true
		}
	}
	return false
}

// recordContinuationNudges observes seed continuation messages flowing
// through the provider. Seed's transient "Please continue …" nudges
// never enter conversation state, so transcripts show unexplained
// consecutive assistant messages. We count them here so transcript
// snapshots can annotate the gap.
func (sp *sproutProvider) recordContinuationNudges(messages []core.Message) {
	if sp.agent == nil {
		return
	}
	n := 0
	for _, m := range messages {
		if m.Role == "user" && isSeedContinuationNudge(m.Content) {
			n++
		}
	}
	if n == 0 {
		return
	}
	sp.agent.state.RecordContinuationNudges(n)
}

// isSeedContinuationNudge matches seed's hardcoded transient messages
// (core/conversation.go). These exact strings are a version-pinned
// dependency on seed; update them when seed changes the texts.
func isSeedContinuationNudge(content string) bool {
	switch strings.TrimSpace(content) {
	case "Please continue your response from where you left off.",
		"Please continue.",
		"Your previous response was filtered. Please rephrase your response.",
		"Your previous response appears incomplete. Please provide your final answer.",
		"Your previous response had reasoning but no visible text. Call the next tool or state the final answer explicitly — do not end with only thinking.":
		return true
	}
	return false
}
