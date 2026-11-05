// reasoning_guard.go — the reasoning-channel repetition guard.
//
// Some model families degenerate inside their thinking rather than their
// visible reply: DeepSeek V4 (Flash and Pro) and Kimi emit a short action
// announcement ("Let me run it.", "Run.", "Emitting now.") hundreds of times
// in `reasoning_content`, then still produce a coherent answer — so the reply
// looks fine while the turn burns its budget on the loop. The failure is
// self-reinforcing: these providers require the full reasoning_content of
// every prior assistant turn to be replayed when the request carries tools,
// so the model reads its own loop as the immediate prefix and continues it.
// Session state, not the transport, drives it: a fresh session on the same
// model does not loop.
//
// The detection is the same line-run analysis the visible-text guard uses
// (RepetitionGuard with Reasoning set); what differs is the response. A
// degenerate visible reply is worthless, so it is cut and retried. A
// degenerate reasoning block is followed by a usable reply, and the loop is
// already in the context the model will read next — so the turn is steered:
// the degenerated lines are scrubbed from the turn's reasoning in history and
// a short system nudge tells the model to stop re-deriving.
//
// Scrubbing is confined to reasoning_content. The visible content and the
// tool call of the same assistant message are untouched, and the message
// itself is kept, so the provider contract (an assistant message with
// tool_calls must carry reasoning_content) still holds.
package agent

import (
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// reasoningLoopNudge is the system message appended for the rest of the turn
// once a reasoning loop is confirmed. It names the behaviour, not the task:
// the model already has the task in context.
const reasoningLoopNudge = "Your reasoning started repeating itself without making progress on the task. Do not re-derive conclusions you have already reached. Take the next concrete action — emit the tool call or give the final answer — and keep any further reasoning brief."

// reasoningGuardConfigFromAgent reads the reasoning-channel thresholds from
// the agent's config. A nil agent or config resolves to the enabled defaults.
func (sp *sproutProvider) reasoningGuardConfigFromAgent() RepetitionGuardConfig {
	cfg := RepetitionGuardConfig{Enabled: true, Reasoning: true}
	if sp.agent == nil {
		return cfg
	}
	conf := sp.agent.GetConfig()
	if conf == nil {
		return cfg
	}
	cfg.Enabled = conf.RepetitionGuardReasoningEnabled()
	cfg.MinRepetitions = conf.RepetitionGuardReasoningMinRepetitions()
	cfg.MaxLineChars = conf.RepetitionGuardReasoningMaxLineChars()
	return cfg
}

// noteReasoningLoop records one cut reasoning loop for the agent's
// (provider, model) cell and logs exactly one line, naming the line that
// completed the degenerate run so the detection can be judged from the log.
func (sp *sproutProvider) noteReasoningLoop(evidence string) {
	if sp.agent == nil {
		return
	}
	provider := sp.agent.GetProvider()
	model := sp.agent.GetModel()
	GlobalRepetitionMetrics().RecordReasoning(provider, model)
	sp.agent.Logger().Warn(
		"[repetition] degenerate reasoning loop detected for %s/%s (repeated line %q); scrubbing history and steering the turn\n",
		provider, model, truncateLoopEvidence(evidence))
}

// truncateLoopEvidence bounds the logged evidence line so a long line cannot
// flood the log.
func truncateLoopEvidence(evidence string) string {
	const max = 80
	trimmed := strings.TrimSpace(evidence)
	if len([]rune(trimmed)) <= max {
		return trimmed
	}
	return string([]rune(trimmed)[:max]) + "…"
}

// scrubResponseReasoning collapses degenerate line runs in the response's own
// reasoning_content before seed records it in the conversation state. It is
// the second half of the reasoning-loop response: the mid-stream scrub can
// only fix history that already exists, so the reply that triggered the
// steering is cleaned here, on its way into history.
func (sp *sproutProvider) scrubResponseReasoning(resp *api.ChatResponse) {
	if resp == nil || len(resp.Choices) == 0 {
		return
	}
	msg := &resp.Choices[0].Message
	if msg.ReasoningContent == "" {
		return
	}
	cfg := sp.reasoningGuardConfigFromAgent()
	scrubbed, removed := collapseDegenerateLineRuns(msg.ReasoningContent, resolveRepetitionGuardConfig(cfg))
	if removed == 0 {
		return
	}
	msg.ReasoningContent = scrubbed
}

// steerReasoningLoop applies the reasoning-loop response: scrub the
// degenerated lines out of the turn's reasoning in the conversation state and
// arm the nudge for the rest of the turn. The nudge is appended to the system
// prompt of every later request this turn (see withReasoningLoopNudge), so it
// reaches the model on the very next iteration.
//
// The scrub must reach seed's LIVE per-query state: that is the history the
// provider replays on the next iteration, and the durable store is synced
// from it at query end (the recovery-compaction path writes through the same
// handle for the same reason). Scrubbing only sprout's durable copy left the
// loop in the prefix the model reads, so the "steering" did nothing and the
// model kept re-reading its own loop.
//
// The guard calls this on every reasoning chunk once it has flagged, so the
// whole response is gated on the turn's steered marker: the scrub walks the
// entire history and must not run per chunk. A second loop later in the same
// turn is already covered by the armed nudge.
func (sp *sproutProvider) steerReasoningLoop(evidence string) {
	if sp.agent == nil {
		return
	}
	if !sp.agent.markReasoningLoopSteered() {
		return
	}
	sp.scrubReasoningHistory()
	sp.agent.armReasoningLoopNudge(reasoningLoopNudge)
	sp.noteReasoningLoop(evidence)
}

// scrubReasoningHistory de-loops the degenerate reasoning in the history the
// turn will replay: seed's live per-query state when the provider has the
// handle (the effective copy), plus sprout's durable copy so the persisted
// view is consistent before the query-end sync.
func (sp *sproutProvider) scrubReasoningHistory() {
	if sp == nil || sp.agent == nil {
		return
	}
	cfg := resolveRepetitionGuardConfig(sp.reasoningGuardConfigFromAgent())
	if st := sp.currentRecoveryState(); st != nil {
		if scrubbed, changed := collapseDegenerateReasoningInMessages(st.Messages(), cfg); changed {
			st.SetMessages(scrubbed)
		}
	}
	sp.agent.scrubDegenerateReasoning(cfg)
}

// collapseDegenerateReasoningInMessages returns a copy of msgs with the
// degenerate line runs collapsed out of every assistant message's
// reasoning_content. It is index-stable: the slice keeps its length and
// message identity, so no checkpoint rebase is needed. Visible content, tool
// calls, and non-assistant messages are untouched.
func collapseDegenerateReasoningInMessages(msgs []api.Message, cfg RepetitionGuardConfig) ([]api.Message, bool) {
	if len(msgs) == 0 {
		return msgs, false
	}
	out := make([]api.Message, len(msgs))
	copy(out, msgs)
	changed := false
	for i := range out {
		if out[i].Role != "assistant" || out[i].ReasoningContent == "" {
			continue
		}
		scrubbed, removed := collapseDegenerateLineRuns(out[i].ReasoningContent, cfg)
		if removed == 0 {
			continue
		}
		out[i].ReasoningContent = scrubbed
		changed = true
	}
	if !changed {
		return msgs, false
	}
	return out, true
}

// markReasoningLoopSteered reports whether this turn has not yet been steered
// for a reasoning loop, and records that it now has. A turn is steered once:
// the nudge is a system message and the metric counts a turn, not a chunk.
func (a *Agent) markReasoningLoopSteered() bool {
	if a == nil {
		return false
	}
	a.turnLangMu.Lock()
	defer a.turnLangMu.Unlock()
	if a.reasoningLoopSteered {
		return false
	}
	a.reasoningLoopSteered = true
	return true
}

// armReasoningLoopNudge stores the nudge for the rest of the turn. It is
// idempotent: a turn that loops repeatedly is nudged once, not once per
// detection (the nudge is a system message and would otherwise accumulate).
func (a *Agent) armReasoningLoopNudge(nudge string) {
	if a == nil || strings.TrimSpace(nudge) == "" {
		return
	}
	a.turnLangMu.Lock()
	a.reasoningLoopNudge = nudge
	a.turnLangMu.Unlock()
}

// reasoningLoopNudgeForTurn returns the nudge armed for this turn (empty when
// none). It is not consumed: every request of the steered turn carries it, so
// an iteration that follows a tool call is steered as well as the one right
// after the detection. prepareQueryRun clears it at the next turn's start.
func (a *Agent) reasoningLoopNudgeForTurn() string {
	if a == nil {
		return ""
	}
	a.turnLangMu.RLock()
	defer a.turnLangMu.RUnlock()
	return a.reasoningLoopNudge
}

// clearReasoningLoopNudge drops any armed nudge. It runs at the turn's start
// so a previous turn's steering never leaks into this one.
func (a *Agent) clearReasoningLoopNudge() {
	if a == nil {
		return
	}
	a.turnLangMu.Lock()
	a.reasoningLoopNudge = ""
	a.reasoningLoopSteered = false
	a.turnLangMu.Unlock()
}

// scrubDegenerateReasoning collapses degenerate line runs inside the
// reasoning_content of the conversation's assistant messages in sprout's
// durable copy. The live seed state is scrubbed by the provider through
// scrubReasoningHistory (see reasoning_guard.go); this keeps the persisted
// view consistent until the query-end sync.
//
// Only lines that are themselves part of a degenerate run are removed, and
// one copy of each survives, so the model keeps the thought without the
// stutter. Visible content, tool calls, and message identity are untouched.
func (a *Agent) scrubDegenerateReasoning(cfg RepetitionGuardConfig) {
	if a == nil || a.state == nil {
		return
	}
	scrubbed, changed := collapseDegenerateReasoningInMessages(a.state.GetMessages(), resolveRepetitionGuardConfig(cfg))
	if changed {
		a.state.SetMessages(scrubbed)
	}
}

// collapseDegenerateLineRuns drops the degenerate text the guard flags: the
// repeated occurrences of an identical short line (keeping the first
// occurrence) and, when a window of short lines is cycling a few phrases, the
// duplicate occurrences inside that window. It returns the collapsed text and
// how many lines it removed.
//
// The identical-run pass is measured per contiguous block, so a loop that
// spans several blocks (a tool call interrupting the thinking) is collapsed in
// each block. The cycling pass is what the window signal flags: a model that
// alternates "Let me write." / "Let me go." / "Let me emit." never repeats one
// line past the run threshold, so a run-only scrub would remove nothing from
// the exact loop the guard just confirmed.
func collapseDegenerateLineRuns(text string, cfg RepetitionGuardConfig) (string, int) {
	collapsed, removed := collapseIdenticalRuns(text, cfg)
	cycled, cycledRemoved := collapseCyclingLines(collapsed, cfg)
	if cycledRemoved == 0 {
		return collapsed, removed
	}
	return cycled, removed + cycledRemoved
}

// collapseIdenticalRuns drops repeated occurrences of a short line once the
// run reaches the threshold, keeping the first occurrence. It returns the
// collapsed text and how many lines it removed.
//
// The run is measured per contiguous block of identical normalized lines, so
// a loop that spans several blocks (a tool call interrupting the thinking)
// is collapsed in each block. Lines longer than the reasoning maximum are
// never collapsed — long repeated prose is legitimate reasoning.
func collapseIdenticalRuns(text string, cfg RepetitionGuardConfig) (string, int) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) < 2 {
		return text, 0
	}
	var b strings.Builder
	b.Grow(len(text))

	removed := 0
	runLine := ""
	runCount := 0
	inFence := false
	fence := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Fenced blocks are skipped exactly as the guard skips them:
		// repeated lines inside code are legitimate.
		if marker, ok := fenceMarker(trimmed); ok {
			if !inFence {
				inFence, fence = true, marker
			} else if marker == fence {
				inFence, fence = false, ""
			}
			runLine, runCount = "", 0
			b.WriteString(line)
			continue
		}
		if inFence {
			b.WriteString(line)
			continue
		}
		normalized := normalizeLoopLine(trimmed)
		if trimmed == "" || len([]rune(trimmed)) > cfg.MaxLineChars || normalized == "" {
			// Blank and long lines are neutral: they end the current run.
			runLine, runCount = "", 0
			b.WriteString(line)
			continue
		}
		if normalized == runLine {
			runCount++
			if runCount >= cfg.MinRepetitions {
				removed++
				continue
			}
			b.WriteString(line)
			continue
		}
		runLine, runCount = normalized, 1
		b.WriteString(line)
	}
	if removed == 0 {
		return text, 0
	}
	return b.String(), removed
}

// collapseCyclingLines drops the duplicate occurrences inside a degenerate
// cycling window — the second signal the guard flags on. A window is the last
// loopWindowLines short lines (blank lines are neutral, a long line ends the
// window); once at most half of them are distinct, the region is cycling, and
// every later short line that repeats a phrase already seen in the region is
// dropped until a long line ends it.
//
// Carrying the phrase set forward (rather than only looking back one window)
// is what makes the scrub effective: a 20-line cycle whose phrases recur more
// than a window apart would otherwise survive pass after pass. A long cycling
// loop collapses to the handful of distinct phrases it was cycling. Text that
// never fills a degenerate window is untouched, and a genuinely new short line
// in a degenerate region is kept.
func collapseCyclingLines(text string, cfg RepetitionGuardConfig) (string, int) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) < loopWindowLines {
		return text, 0
	}
	var b strings.Builder
	b.Grow(len(text))

	window := make([]string, 0, loopWindowLines)
	var seen map[string]struct{}
	removed := 0
	inFence := false
	fence := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if marker, ok := fenceMarker(trimmed); ok {
			if !inFence {
				inFence, fence = true, marker
			} else if marker == fence {
				inFence, fence = false, ""
			}
			window = window[:0]
			seen = nil
			b.WriteString(line)
			continue
		}
		if inFence {
			b.WriteString(line)
			continue
		}
		if trimmed == "" {
			// Blank lines are neutral in the guard's window too.
			b.WriteString(line)
			continue
		}
		if len([]rune(trimmed)) > cfg.MaxLineChars {
			window = window[:0]
			seen = nil
			b.WriteString(line)
			continue
		}
		normalized := normalizeLoopLine(trimmed)
		if seen == nil {
			window = append(window, normalized)
			if len(window) > loopWindowLines {
				window = window[1:]
			}
			if len(window) == loopWindowLines && degenerateLoopWindow(window) {
				seen = make(map[string]struct{}, len(window))
				for _, phrase := range window {
					seen[phrase] = struct{}{}
				}
			}
			b.WriteString(line)
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			removed++
			continue
		}
		seen[normalized] = struct{}{}
		b.WriteString(line)
	}
	if removed == 0 {
		return text, 0
	}
	return b.String(), removed
}

// degenerateLoopWindow reports whether a full window is cycling: at most half
// its lines are distinct.
func degenerateLoopWindow(window []string) bool {
	distinct := make(map[string]struct{}, len(window))
	for _, line := range window {
		distinct[line] = struct{}{}
	}
	return len(distinct)*2 <= len(window)
}
