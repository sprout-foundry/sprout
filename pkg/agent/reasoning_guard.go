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
// (provider, model) cell and logs exactly one line.
func (sp *sproutProvider) noteReasoningLoop() {
	if sp.agent == nil {
		return
	}
	provider := sp.agent.GetProvider()
	model := sp.agent.GetModel()
	GlobalRepetitionMetrics().RecordReasoning(provider, model)
	sp.agent.Logger().Warn(
		"[repetition] degenerate reasoning loop detected for %s/%s; scrubbing history and steering the turn\n",
		provider, model)
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
// The guard calls this on every reasoning chunk once it has flagged, so the
// whole response is gated on the turn's steered marker: the scrub walks the
// entire history and must not run per chunk. A second loop later in the same
// turn is already covered by the armed nudge.
func (sp *sproutProvider) steerReasoningLoop() {
	if sp.agent == nil {
		return
	}
	if !sp.agent.markReasoningLoopSteered() {
		return
	}
	sp.agent.scrubDegenerateReasoning(sp.reasoningGuardConfigFromAgent())
	sp.agent.armReasoningLoopNudge(reasoningLoopNudge)
	sp.noteReasoningLoop()
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
// reasoning_content of the conversation's assistant messages. It runs over
// the agent's state (the history seed replays), so the model stops reading
// its own loop back on the next iteration and on every later turn.
//
// Only lines that are themselves part of a degenerate run are removed, and
// one copy of each survives, so the model keeps the thought without the
// stutter. Visible content, tool calls, and message identity are untouched.
func (a *Agent) scrubDegenerateReasoning(cfg RepetitionGuardConfig) {
	if a == nil || a.state == nil {
		return
	}
	messages := a.state.GetMessages()
	if len(messages) == 0 {
		return
	}
	changed := false
	out := make([]api.Message, len(messages))
	copy(out, messages)
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
	if changed {
		a.state.SetMessages(out)
	}
}

// collapseDegenerateLineRuns drops repeated occurrences of a short line once
// the run reaches the reasoning threshold, keeping the first occurrence. It
// returns the collapsed text and how many lines it removed.
//
// The run is measured per contiguous block of identical normalized lines, so
// a loop that spans several blocks (a tool call interrupting the thinking)
// is collapsed in each block. Lines longer than the reasoning maximum are
// never collapsed — long repeated prose is legitimate reasoning.
func collapseDegenerateLineRuns(text string, cfg RepetitionGuardConfig) (string, int) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) < 2 {
		return text, 0
	}
	var b strings.Builder
	b.Grow(len(text))

	removed := 0
	runLine := ""
	runCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
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
