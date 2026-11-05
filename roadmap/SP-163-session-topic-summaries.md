# SP-163 — Session Topic Summaries

Status: Implemented (2026-10-19). All sections landed with 26 tests across
the agent, CLI-command and WebUI layers. `pkg/agent`, `pkg/agent_commands`
and `pkg/webui` suites pass; the WebUI switcher vitest passes; `make vet` /
`make fmt-check` are clean. Split from a 17-item user nit list. Depends on
SP-150 (Model Roles — the `summarizer` role) and SP-151 (Progress Events —
summary templates and the "never claim success without a verification"
rule). Touches session persistence (`pkg/agent/persistence_session.go`),
the summary producer (`pkg/agent/session_summary.go`), the CLI session
picker (`pkg/agent_commands/sessions.go`), and the WebUI history switcher
(`webui/src/components/chat/ChatHistorySwitcher.tsx`).

## Problem

There is no conversation topic anywhere in the product. The user's report:

> session select truncates too much of the prompt and without a claude-like
> summary of the conversation topic, this makes /session problematic

What exists today:

- **CLI `/sessions` picker**: each row's *label* is
  `GetSessionPreviewScoped` — the first user message, hard-capped at **50
  runes** (`pkg/agent/persistence_session.go`). The only other field is a
  timestamp. There is no second line.
- **WebUI history switcher**: each row shows `name` (or the raw session ID)
  plus `"${message_count} messages"` as the preview
  (`ChatHistorySwitcher.tsx`). The *search* path returns a real excerpt
  (`pkg/search/session_search.go`); the *list* path returns a count.
- **`generateSessionName`** (`pkg/agent/state.go`) derives a name from the
  first user message, capped at 60 runes, or returns `"Unnamed session"`.

So both surfaces identify a conversation by its opening line. A session that
starts "hi" and becomes a two-hour refactor is indistinguishable from a
session that was actually about saying hi.

This is a feature, not a nit: it needs a summary producer, a storage field,
a backfill path for existing sessions, and a rendering change in two
surfaces.

## Design

### 163a. What a summary is

One short line (target ≤ 120 chars) describing what the conversation was
*about* — the topic and the outcome, not the opening message. Claude-style:
a topic phrase, not a transcript excerpt.

Distinct from, and complementary to, the existing name:

- **name**: user-set or first-message-derived; the label the user recognizes.
- **summary**: derived; the description that disambiguates two sessions with
  similar names.

The summary never replaces the name. Both render.

### 163b. Where it comes from

Three sources, in priority order:

1. **Model-written** via the `summarizer` role (SP-150), when configured.
   Built from the conversation's own messages — the same posture as SP-151's
   optional summaries: the model summarizes what happened, it does not
   invent.
2. **Deterministic template** when no summarizer is configured — derived
   from the session's own signals: files touched, tools used, the plan's
   scope if one exists. No model call, no invented detail. This is the
   default, and it must be genuinely useful, not a placeholder.
3. **Fallback**: the existing first-message preview, so a session with no
   summary still renders something.

The summary is produced at a natural boundary — turn end, session save, or
on demand — not on every keystroke. Producing it at save time keeps it
current without a background job.

### 163c. Storage

A new field on `ConversationState` (`pkg/agent/persistence_session.go`),
alongside `Name`. Additive and optional: an absent field is the normal state
for existing sessions, and every reader degrades to 163b's fallback.

`SessionInfo` gains the same field so list paths carry it without loading
full state.

### 163d. Backfill

Existing sessions have no summary. Options, to be decided (see Open
questions): summarize lazily on first display, or leave them on the
fallback until they are next saved. Either way, no migration that rewrites
every session file — the field is optional and absence is handled.

### 163e. Rendering

- **CLI picker**: the summary becomes the row's second line (the `Detail`
  field `SelectList` already supports), with the name as the label and the
  timestamp alongside. The 50-rune cap on the label is raised or removed
  once the summary carries the disambiguating information.
- **WebUI switcher**: the summary replaces `"${message_count} messages"` as
  the preview. Message count moves to secondary metadata.

Both surfaces render the same string, so the two agree.

### 163f. Reuse, don't duplicate

The summary machinery belongs in one place (`pkg/agent`), consumed by both
the CLI and the WebUI API. The WebUI's list endpoint
(`pkg/webui/sessions_api.go`) carries the field through; it does not compute
its own.

## Acceptance criteria

- [x] A session's summary describes its topic, not its first message (test:
      a session starting "hi" that then edits files summarizes as the work,
      not as "hi").
      Test: `TestTopicSummaryDescribesTopicNotFirstMessage`.
- [x] With no summarizer role configured, the deterministic template produces
      a useful summary from session signals (test over a fixture session).
      Tests: `TestTopicSummaryTemplateFromSessionSignals`,
      `TestTopicSummaryPlanGoalSignal`, `TestTemplateToolsOnlyFallback`,
      `TestTopicSummaryNewClientNilWithoutSummarizerRole`.
- [x] With a summarizer configured, a model failure falls back to the
      template (test).
      Tests: `TestTopicSummaryModelFallbackToTemplateOnModelError`,
      `TestTopicSummaryModelEmptyReplyFallsBackToTemplate`,
      `TestWriteTopicSummaryTimeoutFallsBack`,
      `TestTopicSummaryModelWrittenWhenSummarizerAnswers`.
- [x] `ConversationState` with no summary field loads and renders the
      fallback (test — existing-session compatibility).
      Tests: `TestResolveSessionTopicSummaryFallsBackToPreviewForLegacySession`,
      `TestSessionTopicSummaryForDisplayLoadsTemplateForOldSessions`.
- [x] CLI picker shows the summary as a second line; the label is no longer
      truncated at 50 runes (test).
      Tests: `TestBuildSessionPickerItemsSummaryAsDetail`,
      `TestBuildSessionPickerItemsFallsBackToPreviewWhenNoSummary`,
      `TestGetSessionLabelScopedUncappedPreview`,
      `TestGetSessionPreviewScopedStillCapsAt50`.
- [x] WebUI switcher shows the summary instead of the message count (vitest).
      Test: `ChatHistorySwitcher.test.tsx` (7 tests).
- [x] CLI and WebUI render the same summary string for the same session.
      → both call `agent.SessionTopicSummaryForDisplay`; the WebUI API
      (`buildSessionList`) carries the resolved field through rather than
      computing its own.
      Tests: `TestSessionInfoCarriesTopicSummary`,
      `TestBuildSessionListCarriesStoredTopicSummary`,
      `TestBuildSessionListLazilyResolvesTemplateForOldSessions`.

## Decisions (Open Questions)

- **Backfill**: lazy-on-display via the deterministic template, at zero
  model cost. `SessionTopicSummaryForDisplay` resolves a stored summary when
  present and otherwise computes the template from the session's own file.
  No migration rewrites existing session files; the field stays optional.
- **Refresh cadence**: computed at save time. The template path is cheap and
  the model path is bounded by a 5s timeout, so recomputing on each save is
  simpler than tracking growth.
- **Length target**: 120 runes (`topicSummaryMaxRunes`), truncated at render
  if the surface is narrower.

## Non-goals

- No change to `generateSessionName`'s first-message behavior for sessions
  the user has not renamed.
- No semantic search change — `pkg/search` already produces excerpts and is
  unaffected.
- No summary of *individual turns* (that is SP-162's outcome line).

## Open questions

- **Backfill strategy**: lazy-on-display (a model call the first time an old
  session is listed — surprising cost) vs. never (old sessions keep the
  fallback forever). Leaning: deterministic template applied lazily at zero
  cost, model-written only for sessions that are saved again.
- **Summary refresh cadence**: recompute every save, or only when the
  conversation has grown materially? Every save is simplest and cheap for
  the template path.
- **Length target**: 120 chars fits the CLI second line at 80 cols; a wider
  terminal could take more. One target, truncated at render.
