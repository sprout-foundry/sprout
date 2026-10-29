// seed_provider_chat_repetition.go — the sproutProvider streaming
// repetition-guard integration: it wires the pure RepetitionGuard into the
// streaming request, cuts the attempt when a degenerate run is confirmed,
// and retries the request once with a short system nudge. Split out of
// seed_provider_chat_stream.go to keep the streaming entry points readable.

package agent

import (
	"context"
	"errors"
	"fmt"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// errStreamRepetitionLoop is the internal sentinel a streamed attempt returns
// when the repetition guard cut it. It never reaches the client: the driver
// either retries once with a nudge or, when the retry loops too, surfaces it
// as a normal turn error.
var errStreamRepetitionLoop = errors.New("streamed reply degenerated into a repetition loop")

// repetitionNudge is the short system message appended for the single retry.
// It tells the model, in one line, what went wrong — act or answer — without
// re-explaining the task.
const repetitionNudge = "Your previous reply repeated itself without acting. Stop repeating and either emit the tool call that makes progress or give the final answer directly."

// repetitionGuardConfigFromAgent reads the guard's thresholds from the agent's
// config, falling back to the enabled defaults (a nil config resolves to
// enabled with sane thresholds).
func (sp *sproutProvider) repetitionGuardConfigFromAgent() RepetitionGuardConfig {
	if sp.agent == nil {
		return RepetitionGuardConfig{}
	}
	cfg := sp.agent.GetConfig()
	return RepetitionGuardConfig{
		Enabled:        cfg.RepetitionGuardEnabled(),
		MinRepetitions: cfg.RepetitionGuardMinRepetitions(),
		MaxLineChars:   cfg.RepetitionGuardMaxLineChars(),
	}
}

// noteRepetitionLoop records one cut loop for the agent's (provider, model)
// cell and logs exactly one line. It is the single detection-recording site,
// so the metric and the log can never diverge.
func (sp *sproutProvider) noteRepetitionLoop() {
	if sp.agent == nil {
		return
	}
	provider := sp.agent.GetProvider()
	model := sp.agent.GetModel()
	GlobalRepetitionMetrics().Record(provider, model)
	sp.agent.Logger().Info(
		"[repetition] degenerate repetition loop cut for %s/%s; retrying once with a nudge\n",
		provider, model)
}

// repetitionAttempt runs one streamed attempt under the guard: it creates a
// cancelable child context so the guard can stop the stream the moment a
// degenerate run is confirmed, and it returns the sentinel when the guard (not
// the transport) ended the attempt. The hold-back and the guard are reset at
// the start of each attempt so a cut attempt leaves no state behind.
//
// attemptFn is the caller's transport layer: the ChatStream entry passes its
// retry/backoff layer (doChatWithRetryStreaming), while the buffer-only path
// passes a single SendChatRequestStream — each path keeps the exact transport
// behavior it had before the guard existed.
func (sp *sproutProvider) repetitionAttempt(
	ctx context.Context,
	messages []api.Message,
	callback api.StreamCallback,
	holdback *StreamHoldback,
	guard *RepetitionGuard,
	attemptFn func(ctx context.Context, messages []api.Message, callback api.StreamCallback) (*api.ChatResponse, error),
) (*api.ChatResponse, error) {
	if holdback != nil {
		holdback.Reset()
	}
	if guard != nil {
		guard.Reset()
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Wrap the callback so a confirmed loop cancels the in-flight stream: the
	// provider's SSE reader returns promptly on the canceled context, and the
	// degenerate copies the guard held back are dropped.
	wrapped := func(content string, contentType string) {
		callback(content, contentType)
		if guard != nil && guard.Looping() {
			cancel()
		}
	}
	resp, err := attemptFn(attemptCtx, messages, wrapped)
	// A reply that carries a tool call acts rather than degenerating: if the
	// attempt completed with tool calls, keep it (the turn continues) instead
	// of reporting the cut. This is the reachable completion-time check — the
	// streaming callback cannot see tool-call deltas mid-stream. A reply whose
	// tool call would arrive in a later chunk than the flagging one has
	// already had its stream cut by the cancel above, so it is retried once;
	// the nudge asks for exactly that tool call, which the retry then emits.
	if resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
		if guard != nil {
			guard.NoteToolCall()
		}
		return resp, err
	}
	if guard != nil && guard.Looping() {
		return nil, errStreamRepetitionLoop
	}
	return resp, err
}

// streamWithRepetitionGuard runs the streamed request with the repetition
// guard active, escalating in stages when a degenerate run is cut:
//
//  1. The first loop is cut and retried ONCE with a short system nudge.
//  2. If the nudge retry ALSO loops, the recovery-compaction escalation runs
//     (when enabled): a careful re-seed — a durable de-loop of the
//     degenerate runs out of the state, plus (best-effort) an LLM summary of
//     the middle — and ONE final attempt. A model that degenerates
//     re-reads its own loop back as the immediate prefix on every later
//     request of the turn, so a local nudge often leaves the poisoned state
//     in place; removing the loop text and shortening the context re-seeds
//     the model from a clean entry point.
//  3. If the final attempt loops too (a third loop), the turn fails: a
//     persistently degenerating model fails the turn rather than looping
//     forever. When recovery is disabled, a second loop fails the turn
//     immediately (the pre-recovery behavior).
//
// When the guard is disabled (or the loop is not detected) the behavior is
// the existing streaming request, byte-for-byte.
//
// attemptFn is the caller's transport layer for the normal and nudge-retry
// attempts (see repetitionAttempt). recoveryAttemptFn is the transport for
// the recovery FINAL attempt — typically the same plumbing with a capped
// reasoning effort (see cappedRecoveryReasoning), since the failure is
// effort-dose dependent. When recoveryAttemptFn is nil it falls back to
// attemptFn, so a caller that does not want the effort cap passes the same
// function for both.
//
// The guard is composed OUTSIDE the language hold-back: assistant text flows
// guard → hold-back → client sink. The guard evaluates the model's raw stream
// (its degenerate-run detection is about the model's output, not the
// language-gated delivery), while the hold-back keeps its own gate on what the
// language guard has judged. Neither wraps the other's state; a held
// wrong-language reply and a cut repetition loop are independent outcomes.
func (sp *sproutProvider) streamWithRepetitionGuard(
	ctx context.Context,
	messages []api.Message,
	callback api.StreamCallback,
	holdback *StreamHoldback,
	guard *RepetitionGuard,
	attemptFn func(ctx context.Context, messages []api.Message, callback api.StreamCallback) (*api.ChatResponse, error),
	recoveryAttemptFn func(ctx context.Context, messages []api.Message, callback api.StreamCallback) (*api.ChatResponse, error),
) (*api.ChatResponse, error) {
	attempt := func(msgs []api.Message) (*api.ChatResponse, error) {
		return sp.repetitionAttempt(ctx, msgs, callback, holdback, guard, attemptFn)
	}
	// The recovery final attempt uses its own transport when one was supplied
	// (typically a capped-effort variant); otherwise it reuses the normal
	// transport so a nil recovery transport degrades cleanly.
	recoverTransport := recoveryAttemptFn
	if recoverTransport == nil {
		recoverTransport = attemptFn
	}
	recover := func(msgs []api.Message) (*api.ChatResponse, error) {
		return sp.repetitionAttempt(ctx, msgs, callback, holdback, guard, recoverTransport)
	}

	resp, err := attempt(messages)
	if !errors.Is(err, errStreamRepetitionLoop) {
		// The attempt finished without a loop: flush the guard's trailing
		// partial line so the whole reply reaches the hold-back / client sink.
		if guard != nil {
			guard.Finish()
		}
		return resp, err
	}

	// First detection: record it, log one line, and retry once with the
	// nudge appended as a system message.
	sp.noteRepetitionLoop()
	retryMessages := append(append([]api.Message(nil), messages...),
		api.Message{Role: "system", Content: repetitionNudge})

	resp, err = attempt(retryMessages)
	if !errors.Is(err, errStreamRepetitionLoop) {
		if guard != nil {
			guard.Finish()
		}
		return resp, err
	}

	// Second loop: the nudge did not recover it. Escalate to the
	// recovery-compaction re-seed and make ONE final attempt. When recovery is
	// disabled the turn fails here (the pre-recovery behavior).
	provider, model := sp.repetitionIdentity()
	if sp.recoveryEnabled() {
		finalMessages, detail := sp.performRecoveryCompaction(ctx, messages)
		if finalMessages != nil {
			GlobalRepetitionMetrics().RecordRecoveryEscalation(provider, model)
			sp.agent.Logger().Info(
				"[repetition] second loop for %s/%s; recovery compaction (%s) and final attempt\n",
				provider, model, detail)
			resp, err = recover(finalMessages)
			if !errors.Is(err, errStreamRepetitionLoop) {
				if guard != nil {
					guard.Finish()
				}
				if err == nil {
					GlobalRepetitionMetrics().RecordRecoverySuccess(provider, model)
				}
				return resp, err
			}
			// The final attempt looped too (a third loop): fall through to the
			// terminal failure below.
		}
	}

	// Terminal failure: recovery disabled, the recovery could not build a
	// re-seed, or the final attempt looped too. A persistently degenerating
	// model fails the turn instead of looping — this is the terminal failure
	// the retries existed to prevent.
	if sp.agent != nil {
		sp.agent.Logger().Warn(
			"[repetition] repetition loop persisted for %s/%s; failing the turn\n",
			provider, model)
	}
	return nil, &core.ClientError{Provider: provider, Wrapped: errStreamRepetitionLoop}
}

// recoveryEnabled reports whether the recovery-compaction escalation is
// active for this provider. It requires a non-nil agent (the escalation reads
// the config, the client, and the live-state handle from it) and the
// configured recovery gate. When the gate is disabled, a second loop fails
// the turn as before.
func (sp *sproutProvider) recoveryEnabled() bool {
	if sp == nil || sp.agent == nil {
		return false
	}
	return sp.agent.GetConfig().RepetitionGuardRecoveryEnabled()
}

// repetitionIdentity returns the (provider, model) pair for the repetition
// metric. Empty when the agent is nil; the metric buckets empties under
// "unknown".
func (sp *sproutProvider) repetitionIdentity() (provider, model string) {
	if sp.agent == nil {
		return "", ""
	}
	return sp.agent.GetProvider(), sp.agent.GetModel()
}

// recoveryDetail describes what the recovery compaction actually did, so the
// log can distinguish the full LLM-summary compaction from the de-loop-only
// path and report whether the durable re-seed touched live state.
type recoveryDetail struct {
	// SummaryApplied reports whether the LLM middle-summary ran (the "full"
	// compaction). When false, the re-seed is the in-place de-loop only.
	SummaryApplied bool
	// Summarized is how many messages the middle-summary compressed into the
	// summary (0 when the summary did not run).
	Summarized int
	// DurableChanged reports whether the durable de-loop rewrote seed's live
	// state (true only when degenerate runs were actually present and
	// collapsed there).
	DurableChanged bool
}

func (d recoveryDetail) String() string {
	mode := "de-loop only"
	if d.SummaryApplied {
		mode = fmt.Sprintf("full (LLM summary of %d messages)", d.Summarized)
	}
	durable := "no durable change"
	if d.DurableChanged {
		durable = "durable re-seed applied"
	}
	return mode + ", " + durable
}

// performRecoveryCompaction builds the re-seed for the final attempt after a
// second repetition loop, and persists its durable half into seed's live
// state. It returns the final wire view to send (nil when it cannot run),
// built as: the leading system prompt(s) + the de-looped and (best-effort)
// LLM-summarized history + the nudge, plus a detail describing what ran.
//
// Durable half: the de-loop is written back to seed's live per-query state so
// the CURRENT query's later iterations no longer read the model's own
// accumulated loop (the self-reinforcing attractor). It is index-stable —
// only repeated degenerate text is removed, one copy of each run survives —
// so no checkpoint rebase is needed. syncSeedStateToSprout carries it into
// the durable store at query end.
//
// Immediate half: the LLM summary of the middle re-seeds ONLY the final
// attempt from a shorter context. It is not written back to state (the lossy
// splice would need a checkpoint rebase the provider cannot safely compute).
// A failed or empty summary falls back to the de-loop-only re-seed, so the
// escalation never fails on a best-effort step.
func (sp *sproutProvider) performRecoveryCompaction(ctx context.Context, messages []api.Message) (finalMessages []api.Message, detail recoveryDetail) {
	if sp == nil || sp.agent == nil {
		return nil, recoveryDetail{}
	}
	cfg := sp.agent.GetConfig()

	// Resolve both channel configs once. The visible threshold (the looser
	// prose run) and the reasoning threshold (the tighter, self-reinforcing
	// thinking run) de-loop their respective channels.
	visibleCfg := resolveRepetitionGuardConfig(sp.repetitionGuardConfigFromAgent())
	reasoningCfg := resolveRepetitionGuardConfig(sp.reasoningGuardConfigFromAgent())

	// Durable de-loop: clean the accumulated degenerate runs out of seed's
	// live state (visible + reasoning) so later iterations of THIS query no
	// longer read the model's own loop. Index-stable, so safe to write back.
	if st := sp.currentRecoveryState(); st != nil {
		if deLooped, changed := collapseDegenerateRunsInMessages(st.Messages(), visibleCfg, reasoningCfg); changed {
			st.SetMessages(deLooped)
			detail.DurableChanged = true
		}
	}

	// Immediate re-seed: build the compacted wire view from the leading
	// system prompt(s) + the non-system history. De-loop the history; the
	// system prompt never degenerates.
	sysEnd := 0
	for sysEnd < len(messages) && messages[sysEnd].Role == "system" {
		sysEnd++
	}
	history := messages[sysEnd:]
	compact, _ := collapseDegenerateRunsInMessages(history, visibleCfg, reasoningCfg)

	// Best-effort LLM summary of the middle (the "full" compaction). Only for
	// the final attempt; see the doc comment for why it is not persisted.
	if cfg.RepetitionGuardRecoverySummary() {
		summarizer := buildRecoverySummarizer(sp.currentClient(), sp.agent.GetProvider(), cfg.RepetitionGuardRecoverySummarizerEffort())
		if summarized, ok, summarizedCount := summarizeMiddle(ctx, compact, summarizer); ok {
			compact = summarized
			detail.SummaryApplied = true
			detail.Summarized = summarizedCount
		}
	}

	finalMessages = make([]api.Message, 0, sysEnd+len(compact)+1)
	finalMessages = append(finalMessages, messages[:sysEnd]...)
	finalMessages = append(finalMessages, compact...)
	finalMessages = append(finalMessages, api.Message{Role: "system", Content: repetitionNudge})
	return finalMessages, detail
}
