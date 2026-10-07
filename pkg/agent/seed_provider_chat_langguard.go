// seed_provider_chat_langguard.go — the sproutProvider chat engine's streaming
// language-guard repair: finalize/recheck of the streamed hold-back, the
// regeneration helper, and the mismatch metric. Split out of
// seed_provider_chat.go.

package agent

import (
	"context"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// finalizeStreamHoldback finalizes the streaming hold-back for one streamed
// response. It releases a stream that never reached the prose
// threshold (short or code-only — not judged, so it must not be held). If the
// stream was held (a reliable language mismatch at the START), it keeps the
// held content on the response message's Meta (langGuardOriginalMetaKey) for
// "view original" and — for the turn's FINAL (no-tool-call) response only —
// regenerates the reply once in the user's language and delivers the
// regenerated text through the hold-back's sink (the same client-facing
// delivery as released content), so the user sees the corrected language as
// normal streamed content; when the regeneration errors or still mismatches
// it delivers the localized notice instead. The delivered text —
// regenerated or notice — is also written back onto the response message
// (the message seed's chat loop records in state and returns as the turn
// result), so the client's buffer and the agent's state carry the SAME text
// and the final-message guard (applyLanguageGuard) sees the repaired reply,
// not the held original — no second regeneration, no display-vs-state
// split. A held mid-turn preamble (a reply carrying tool calls) stays
// suppressed and shows nothing: the user-facing repair belongs to the turn's
// final answer, handled by the final-message guard. Either held path is the
// terminal handling for the reply: it is NOT re-checked (a second event for
// the same reply would be a duplicate).
//
// A RELEASED stream (the start passed, or it was below the threshold) was
// already streamed to the client and cannot be un-streamed. If it switched
// language mid-stream, the completion re-check (recheckStreamedReply)
// re-judges the FULL content and, on a reliable mismatch, regenerates it and
// tells the client to replace the already-streamed message with a server
// event.
func (sp *sproutProvider) finalizeStreamHoldback(ctx context.Context, holdback *StreamHoldback, resp *api.ChatResponse) {
	holdback.Finish()
	if held := holdback.Held(); held != "" {
		// The stream is held (a reliable mismatch at the start): keep the held
		// content for "view original" regardless of what is shown.
		if resp != nil && len(resp.Choices) > 0 {
			resp.Choices[0].Message.SetMeta(langGuardOriginalMetaKey, held)
		}
		// A held mid-turn preamble (a reply carrying tool calls that continues
		// the turn) stays suppressed and shows nothing: the user-facing repair
		// belongs to the turn's final answer, handled by the final-message
		// guard. Only the final (no-tool-call) reply is repaired here.
		if streamResponseHasToolCalls(resp) {
			return
		}
		// A held FINAL (no-tool-call) reply: regenerate it once in the
		// user's language. When the regeneration passes the language check,
		// deliver the regenerated text through the hold-back's sink — the
		// same client-facing delivery as released content — so the CLI and
		// the web UI receive the corrected language as normal streamed content
		// instead of a notice. When the regeneration errors or still
		// mismatches, fall back to the localized notice.
		display := LanguageMismatchNotice(holdback.User())
		if regenerated, ok := sp.regenerateStreamedReply(ctx, held, holdback.User()); ok {
			display = regenerated
		}
		holdback.Deliver(display)
		// State consistency: the response message is what seed's chat loop
		// records in state and returns as the turn's result, so it must carry
		// the same text the client's buffer received, plus the repaired
		// marker — applyLanguageGuard then sees the repaired reply and skips
		// it: one regeneration, one displayed text, state and client in
		// agreement, one metric count.
		//
		// Marker and metric are recorded TOGETHER, only when there is a
		// message to mark: on a pathological successful stream that returns a
		// nil/empty-choices response there is nothing to mark, so the (still
		// wrong-language) reply would be re-judged by applyLanguageGuard
		// anyway — counting the mismatch here would double-count it.
		if resp != nil && len(resp.Choices) > 0 {
			resp.Choices[0].Message.Content = display
			resp.Choices[0].Message.SetMeta(langGuardRepairedMetaKey, "held_stream")
			// The model produced a mismatched user-facing reply;
			// record one check + one mismatch here (the reply was repaired during
			// the stream, so applyLanguageGuard will not judge it again — this is
			// the only count it gets).
			sp.recordLanguageGuardMetric()
		}
		return
	}
	// Released (or below threshold): the reply reached the client. Re-check
	// the full content for a mid-stream switch.
	sp.recheckStreamedReply(ctx, holdback, resp)
}

// recordLanguageGuardMetric records one check + one mismatch for the agent's
// (model, role) cell. The streaming repair sites call it for a
// reply they detected as a mismatch and repaired themselves — the final-message
// guard skips already-repaired replies (the meta marker), so this is the one
// count those replies get.
func (sp *sproutProvider) recordLanguageGuardMetric() {
	GlobalLanguageGuardMetrics().Record(sp.agent.GetModel(), sp.agent.GetRole(), true)
}

// streamResponseHasToolCalls reports whether a streamed response carries tool
// calls — i.e. it is a mid-turn reply (the turn continues with tool
// execution) rather than the turn's final (no-tool-call) answer. The
// language-guard repair applies to the final answer only.
func streamResponseHasToolCalls(resp *api.ChatResponse) bool {
	return resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0
}

// regenerateStreamedReply issues the single regeneration for a held or
// mid-stream-switched streamed reply: it regenerates content
// once in the user's language — one model call, grounded in the turn's user
// message (the streaming path has no query-run context, so it uses the turn's
// stored user message and the agent's client) — and reports whether the
// regenerated text passes the language check. It returns the regenerated text
// (for delivery / replacement) and ok=true when the check passes (not a
// mismatch); ok=false when the regeneration errors, is empty, or still
// mismatches (the caller then falls back to the notice). It records no
// metric: the caller that consumes its outcome records the mismatch exactly
// once.
func (sp *sproutProvider) regenerateStreamedReply(ctx context.Context, content string, user langguard.Language) (string, bool) {
	regenerated, err := sp.agent.regenerateInUserLanguageCore(ctx, sp.agent.storedTurnUserQuery(), content, user)
	if err != nil {
		if sp.agent.debug {
			sp.agent.Logger().Debug("[langguard] streaming regeneration failed: %v\n", err)
		}
		return "", false
	}
	if strings.TrimSpace(regenerated) == "" {
		return "", false
	}
	if langguard.CheckLanguage(regenerated, user) == langguard.VerdictMismatch {
		return "", false
	}
	return regenerated, true
}

// recheckStreamedReply re-checks a RELEASED (non-held) streamed reply at
// completion. The hold-back only judged the START
// of the stream; a reply whose start was fine but which switched language
// later is already streamed to the client and cannot be un-streamed. So it is
// re-judged here on its FULL content: when the full reply is a reliable
// mismatch (a mid-stream switch), it is regenerated once in the user's
// language and the client is told — via a language_guard_replacement event —
// to replace the already-streamed message. The replacement carries the
// REGENERATED text when the regeneration passes the language check (so the
// client shows the corrected reply, not just a notice), falling back to the
// localized notice when the regeneration errors or still
// mismatches; the original (full switched content) is always carried for
// "view original" and kept on the message Meta.
//
// No event is published when:
//   - the full content is not judgable (short or code-only — not
//     judged);
//   - the full content is not a reliable mismatch (the reply is in the user's
//     language, or detection is undetermined); or
//   - the reply carries tool calls (a mid-turn preamble — the user-facing
//     repair belongs to the turn's final answer, mirroring the held path).
//
// The hold-back is only non-nil (and thus only finalized) when it is active
// for the turn (guard on, non-subagent, determined user language), so the
// "guard off / undetermined / subagent" cases never reach this function.
func (sp *sproutProvider) recheckStreamedReply(ctx context.Context, holdback *StreamHoldback, resp *api.ChatResponse) {
	full := holdback.Full()
	if !langguard.Judgable(langguard.ExtractProse(full)) {
		// Below the prose threshold: not judged — no re-check.
		return
	}
	if langguard.CheckLanguage(full, holdback.User()) != langguard.VerdictMismatch {
		// No mid-stream switch: the full reply is the user's language (or
		// detection is undetermined) — nothing to replace.
		return
	}
	if streamResponseHasToolCalls(resp) {
		// A mid-turn preamble whose start passed the hold-back: the turn
		// continues with tool execution, so no user-facing repair here —
		// mirroring the held path (the final answer is what gets repaired).
		return
	}
	// A reliable mid-stream switch: regenerate the full reply once in the
	// user's language. The replacement carries the regenerated text when it
	// passes the language check (the corrected reply, not just a notice),
	// falling back to the localized notice when the regeneration
	// errors or still mismatches.
	replacement := LanguageMismatchNotice(holdback.User())
	if regenerated, ok := sp.regenerateStreamedReply(ctx, full, holdback.User()); ok {
		replacement = regenerated
	}
	sp.agent.publishEvent(
		events.EventTypeLanguageGuardReplacement,
		events.LanguageGuardReplacementEvent(
			sp.agent.GetChatID(),
			replacement,
			full,
			"mid_stream_switch",
		),
	)
	if sp.agent.debug {
		sp.agent.Logger().Debug("[langguard] streamed reply switched language mid-stream (%s): replacement event published\n", holdback.User())
	}
	// Keep the full switched content on the message Meta for "view original"
	// (mirrors the held path's language_guard_original) and write the
	// replacement into the message content — it is what seed's chat loop
	// records in state and returns as the turn's result, so state carries
	// what the replacement event told the client to show. The meta marker
	// marks the reply repaired, so applyLanguageGuard neither re-generates
	// nor re-publishes (no duplicate replacement event) and the metric counts
	// the mismatch here — the one count it gets.
	//
	// Marker and metric are recorded TOGETHER, only when there is a message
	// to mark: on a pathological successful stream that returns a
	// nil/empty-choices response there is nothing to mark, so the (still
	// wrong-language) reply would be re-judged by applyLanguageGuard anyway —
	// counting the mismatch here would double-count it.
	if resp != nil && len(resp.Choices) > 0 {
		resp.Choices[0].Message.SetMeta(langGuardOriginalMetaKey, full)
		resp.Choices[0].Message.SetMeta(langGuardRepairedMetaKey, "mid_stream_switch")
		resp.Choices[0].Message.Content = replacement
		sp.recordLanguageGuardMetric()
	}
}
