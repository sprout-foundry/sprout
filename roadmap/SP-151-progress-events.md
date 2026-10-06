# SP-151 — Progress Events

> **Status (2026-10-03):** Proposed.
> Depends on SP-148 (plan scope IDs) and SP-149 (verification results).
> Summaries use the `summarizer` role from SP-150 when configured.

## Problem

Sprout's event stream (`pkg/events/events_types.go`) describes mechanics:
`tool_start`, `tool_end`, `file_changed`, `todo_update`,
`ask_user_request`. That is the right granularity for the live tool view,
but it leaves three gaps:

- Long runs have no milestone-level signal. Someone checking in after
  twenty minutes has to scroll a tool log to learn where things stand.
- Integrations (notifications, CI annotations, chat bridges, embedding
  UIs) have to reverse-engineer progress from tool events.
- There is no single structured "this run finished, here is the
  verified outcome" event to hang a notification or a summary on.

## Design

### 151a. Event types

New structured events, emitted by the runtime rather than parsed from
model text:

- `progress_milestone`: a plan scope item (SP-148) started or finished,
  with files-touched count and elapsed time.
- `progress_question`: the agent needs a decision; carries the question,
  options if any, and why it matters. Complements `ask_user_request`
  with plan context.
- `progress_verification`: the SP-149 result: checks, pass/fail,
  evidence references.
- `progress_complete`: the run finished, with the final verification
  result (or "not verified" when SP-149 is disabled).

Each event carries stable IDs (run, plan revision, scope item) so
consumers can de-duplicate and correlate.

### 151b. Cadence

Milestones are coalesced on the stream write path
(`pkg/webui/stream_coalesce.go`) so consumers get a readable rate on long
runs; question, verification and completion events are never coalesced.

### 151c. Rendered summaries

- Default: deterministic templates over event fields ("Finished: sign-up
  form (4 files). Checks: 6/6 passed."). No model call, no invented
  detail.
- Optional: when the `summarizer` role is configured (SP-150), a short
  model-written summary built only from event fields, with the template
  as fallback. A summary never states success unless a passing
  verification event exists.
- CLI: summaries print in the status footer area
  (`pkg/cliui/terminal_subscriber_events.go`); web UI: a compact progress
  strip in the chat.

### 151d. Consumers

The events are part of the public event schema: generated into the
`@sprout/events` TypeScript union (`packages/events`) and available to
webhooks and any embedding UI over the existing WebSocket stream.

## Acceptance criteria

- [ ] Event types in `events_types.go` and the generated TS union.
- [ ] A scripted run with a fixture plan emits milestone, verification
      and complete events in order with correlating IDs.
- [ ] Template summaries rendered in CLI and web UI.
- [ ] Optional summarizer falls back to the template on error; a failing
      verification never yields a success summary (test).
- [ ] Coalescing applies to milestones only (test).

## Non-goals

- No change to existing tool-level events.
- No notification delivery (email, push); consumers handle that.

## Open questions

- Whether `progress_question` replaces or wraps `ask_user_request` in
  the long run.
- Default milestone cadence for very long runs.
