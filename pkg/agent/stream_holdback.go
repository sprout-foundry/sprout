// stream_holdback.go — the streaming language-guard hold-back (SP-152
// §152c). A reply streams token by token, so a wrong-language reply would be
// visible to the client before a whole-message check could run. The guard
// holds back the start of each streamed reply until it has enough prose to
// judge (a few dozen words, code excluded via langguard.ExtractProse), checks
// it against the user's language, and then either releases the buffer and
// streams the rest live (pass / undetermined) or keeps the whole reply held
// (a reliable mismatch), so the wrong-language content never reaches the
// client.
//
// A reply that never reaches the prose threshold — a short or code-only
// stream, which §152a says is not judged — is released on Finish: it is
// never held. Only a judged, reliably-mismatched stream is held.
//
// A reply that switches language mid-stream is re-checked at completion
// (SP-152 §152c, item 152.7): the hold-back only judges the START of the
// stream (and releases it once the start passes), so a later switch is
// already streamed to the client and cannot be un-streamed. The hold-back
// therefore also accumulates the FULL content of the reply (Full), and the
// agent re-checks it at completion — replacing the already-streamed message
// with a server event when it fails.
//
// The component is pure and fully unit-testable: it wraps a sink (the
// client-facing delivery) and exposes Write / Finish / State / Held. The
// agent wiring (seed_provider_chat.go) gates assistant-text delivery
// through it and, when a stream is held, delivers the localized §152b notice
// instead of the wrong-language stream.
package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// HoldbackState is the lifecycle of a StreamHoldback over one streamed
// reply.
type HoldbackState int

// The hold-back states.
const (
	// HoldbackBuffering: chunks are accumulating; the prose has not yet
	// reached the judgable threshold (or the user language is still being
	// judged). Nothing has been delivered to the sink.
	HoldbackBuffering HoldbackState = iota
	// HoldbackLive: the buffer has been released (or the hold-back is
	// inactive); subsequent chunks pass straight through to the sink.
	HoldbackLive
	// HoldbackHeld: the accumulated prose is a reliable mismatch; the whole
	// reply is held and never delivered to the sink.
	HoldbackHeld
)

// StreamHoldback holds back the start of a streamed reply until enough prose
// has accumulated to judge its language (§152c), then releases the buffer and
// streams the rest live (pass/undetermined) or keeps the whole reply held
// (a reliable mismatch). It wraps a sink that receives released content.
type StreamHoldback struct {
	// user is the turn's resolved user language the reply is judged against.
	user langguard.Language
	// sink receives released content (the client-facing delivery). It is
	// called once with the whole buffered content when the buffer is
	// released, and once per chunk thereafter (live).
	sink func(content string)
	// state is the current lifecycle stage.
	state HoldbackState
	// buf accumulates the streamed content: the pending buffer while
	// buffering, and the held (wrong-language) content once held.
	buf strings.Builder
	// full accumulates EVERY chunk written for this response, regardless of
	// state — including chunks delivered live after the buffer is released.
	// The completion re-check (SP-152 §152c, item 152.7) needs the whole
	// reply, not just the held/buffered prefix, so it is tracked separately
	// from buf (which is reset on release).
	full strings.Builder
}

// NewStreamHoldback constructs a hold-back that judges a streamed reply
// against user's language and delivers released content to sink. An
// undetermined user language (Code == "") makes the hold-back a no-op
// passthrough (constructed inactive): it never holds and every chunk
// reaches sink directly.
func NewStreamHoldback(user langguard.Language, sink func(content string)) *StreamHoldback {
	h := &StreamHoldback{user: user, sink: sink}
	if user.Code == "" {
		// Undetermined user language: never judge, never hold.
		h.state = HoldbackLive
		return h
	}
	h.state = HoldbackBuffering
	return h
}

// Write feeds one chunk of the streamed reply. While buffering, chunks
// accumulate; once the extracted prose reaches the judgable threshold the
// language check runs and the hold-back either releases the buffer (pass or
// undetermined) and goes live, or holds the reply (a reliable mismatch).
// Live chunks pass straight through; held chunks are captured (not
// delivered). Every chunk is also recorded into full (the completion
// re-check's input, item 152.7).
func (h *StreamHoldback) Write(chunk string) {
	// Accumulate the whole reply for the completion re-check BEFORE any
	// state-specific handling (release resets buf, so it cannot be the
	// source of the full content).
	h.full.WriteString(chunk)
	switch h.state {
	case HoldbackLive:
		h.deliver(chunk)
		return
	case HoldbackHeld:
		// The reply is already judged wrong: capture the rest so Held()
		// returns the whole held content, but never deliver it.
		h.buf.WriteString(chunk)
		return
	}

	// Buffering: accumulate, then judge once the prose is judgable.
	h.buf.WriteString(chunk)
	if utf8.RuneCountInString(h.buf.String()) < langguard.MinJudgedProseRunes {
		// Raw buffer below the threshold: the extracted prose (which is
		// never longer than the raw text) cannot be judgable yet.
		return
	}
	if !langguard.Judgable(langguard.ExtractProse(h.buf.String())) {
		// Code-heavy: the extracted prose is still below the threshold.
		// Keep buffering; the stream is released on Finish if it stays
		// below (§152a: code is not judged).
		return
	}
	// Prose is judgable: run the check. A reliable mismatch holds the
	// reply; a pass or an undetermined detection releases it.
	if langguard.CheckLanguage(h.buf.String(), h.user) == langguard.VerdictMismatch {
		h.state = HoldbackHeld
		return
	}
	h.deliver(h.buf.String())
	h.buf.Reset()
	h.state = HoldbackLive
}

// Finish is called at the end of the stream. A reply that never reached the
// prose threshold (short or code-only — not judged, §152a) is released here.
// A held stream stays held (the agent replaces it with the §152b notice).
func (h *StreamHoldback) Finish() {
	if h.state != HoldbackBuffering {
		return
	}
	if h.buf.Len() > 0 {
		h.deliver(h.buf.String())
		h.buf.Reset()
	}
	h.state = HoldbackLive
}

// Reset returns the hold-back to its initial state for a fresh stream (a
// provider retry). Any buffered, held, or fully-accumulated content from the
// discarded attempt is dropped so it never leaks into the next attempt.
func (h *StreamHoldback) Reset() {
	h.buf.Reset()
	h.full.Reset()
	if h.user.Code == "" {
		h.state = HoldbackLive
	} else {
		h.state = HoldbackBuffering
	}
}

// State returns the current lifecycle stage.
func (h *StreamHoldback) State() HoldbackState { return h.state }

// RawLen reports the number of bytes buffered so far (raw assistant-text,
// before the prose threshold is evaluated). It lets the reasoning-model
// fallback (which runs when no assistant-text was delivered) distinguish
// "the hold-back already holds this response's content" (RawLen > 0 — the
// finalize releases or holds it) from "the model streamed it as reasoning and
// the hold-back holds nothing" (RawLen == 0 — route the content through the
// hold-back so it is language-gated). A released hold-back has an empty buffer
// too, so callers must also confirm the streaming buffer is empty.
func (h *StreamHoldback) RawLen() int { return h.buf.Len() }

// Deliver writes content through the hold-back's sink — the same
// client-facing delivery as released content — so the user sees the
// regenerated text instead of the held wrong-language stream. It bypasses
// the hold-back state: the reply is already judged and held, and the
// regenerated text must reach the client regardless of which delivery
// mechanisms the sink wires up.
func (h *StreamHoldback) Deliver(content string) {
	h.deliver(content)
}

// DeliverNotice writes notice through the hold-back's sink — the same
// client-facing delivery as released content — so the user sees the §152b
// notice instead of the held wrong-language stream. It is a thin wrapper over
// Deliver for the templated-notice case.
func (h *StreamHoldback) DeliverNotice(notice string) {
	h.Deliver(notice)
}

// Held returns the held (wrong-language) content — the whole reply,
// including chunks that arrived after the mismatch was detected — or ""
// when the stream is not held.
func (h *StreamHoldback) Held() string {
	if h.state == HoldbackHeld {
		return h.buf.String()
	}
	return ""
}

// Full returns the entire content written for this response — every chunk,
// including those delivered live after the buffer was released, not just the
// held/buffered prefix. It is the completion re-check's input (SP-152 §152c,
// item 152.7): a reply whose start passed the hold-back but which switched
// language later in the stream is re-judged on its full content. Empty when
// nothing was written.
func (h *StreamHoldback) Full() string { return h.full.String() }

// User returns the user language the hold-back judges against.
func (h *StreamHoldback) User() langguard.Language { return h.user }

// deliver writes content to the sink (a no-op for a nil sink).
func (h *StreamHoldback) deliver(content string) {
	if h.sink != nil {
		h.sink(content)
	}
}

// ---------------------------------------------------------------------------
// Agent wiring
// ---------------------------------------------------------------------------

// setTurnLanguageGuard stores the turn's resolved user language, whether the
// streaming hold-back applies to it, and the turn's user message. Called once
// per turn from prepareQueryRun (via resolveTurnLanguageGuard), before the
// seed conversation loop runs, so the streaming provider path can read them
// during the turn.
func (a *Agent) setTurnLanguageGuard(user langguard.Language, active bool, userQuery string) {
	a.turnLangMu.Lock()
	a.turnUserLanguage = user
	a.streamHoldbackActive = active
	a.turnUserQuery = userQuery
	a.turnLangMu.Unlock()
}

// turnUserLanguageGuard returns the turn's resolved user language and whether
// the streaming hold-back applies to it. The streaming provider path reads it
// to decide whether to gate assistant-text delivery: it applies only when the
// guard is enabled, the agent is not a subagent, and the user language was
// determined.
func (a *Agent) turnUserLanguageGuard() (langguard.Language, bool) {
	a.turnLangMu.RLock()
	defer a.turnLangMu.RUnlock()
	return a.turnUserLanguage, a.streamHoldbackActive
}

// turnUserQuery returns the current turn's user message (the user's last
// message), stored by setTurnLanguageGuard. The streaming regeneration prompt
// carries it so the regeneration is grounded in the user's own request, the
// way the final-message guard's regeneration is grounded in the run's query.
func (a *Agent) storedTurnUserQuery() string {
	a.turnLangMu.RLock()
	defer a.turnLangMu.RUnlock()
	return a.turnUserQuery
}

// resolveTurnLanguageGuard resolves the turn's user language once (§152a)
// and stores it on the agent for the streaming hold-back. It reuses the same
// resolution as the final-message guard (152.5): the user's recent messages
// plus the current query, with the configured language as the fallback. The
// hold-back is inactive (no hold-back, byte-identical streaming) when the
// agent is a subagent (its output is not user-facing prose), when the guard is
// disabled (§152f), or when the user language is undetermined.
func (a *Agent) resolveTurnLanguageGuard(currentQuery string) {
	if a.IsSubagent() {
		// Subagent output goes to the orchestrator, not the end user; the
		// user-facing hold-back never applies to it.
		a.setTurnLanguageGuard(langguard.Language{}, false, currentQuery)
		return
	}
	cfg := a.GetConfig()
	// LanguageGuardEnabled is nil-safe: a nil config resolves to the
	// default (enabled, §152f).
	if !cfg.LanguageGuardEnabled() {
		a.setTurnLanguageGuard(langguard.Language{}, false, currentQuery)
		return
	}
	configured := langguard.Language{}
	if cfg != nil {
		configured = langguard.ParseLanguage(cfg.Language)
	}
	recent := a.recentUserMessages(a.state.GetMessages())
	if q := StripUserMessageTimestamp(currentQuery); strings.TrimSpace(q) != "" {
		// The current query is the most recent user signal; it is not in
		// state yet (it enters during the seed loop), so add it explicitly.
		recent = append(recent, q)
	}
	user, _ := langguard.ResolveUserLanguage(recent, configured)
	a.setTurnLanguageGuard(user, user.Code != "", currentQuery)
}
