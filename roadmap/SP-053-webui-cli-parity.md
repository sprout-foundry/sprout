# SP-053: WebUI CLI Parity — Persona/Depth, Live Tools, Cost Footer

**Status:** 📋 Proposed
**Date:** 2026-05-22
**Depends on:** SP-051 (Depth-Aware Subagent UI — backend now publishes `subagent_depth` + `active_persona` on every event; this spec consumes them in the WebUI), SP-048 (CLI Delight — the model + ToolCard parity targets), SP-050 (Persona Collapse — keeps "orchestrator" stable as the depth-1 ID)
**Priority:** Medium-High (closes the visible feature gap between the polished CLI and the still-flat WebUI; users on each surface see meaningfully different fidelity today)
**Effort Estimate:** ~half-day, three phases — each independently shippable

## Problem

The CLI got three rounds of polish (SP-048 spinner+footer+timeline,
SP-050 persona collapse, SP-051 depth-aware subagent UI, SP-052
multi-line error formatter). The WebUI didn't move during any of
those. The result is concrete fidelity gaps:

### Gap 1: Chat messages don't show persona or depth

`webui/src/components/chat/MessageItem.tsx:14-63` renders every
assistant message identically — same bubble color, no badge, no
indent — regardless of whether it came from the primary orchestrator
or a depth-2 coder subagent. Compare with the CLI, where a coder's
tool calls render `    [coder] read_file (foo.go) · 0.1s` (4-space
depth indent + colored badge) and the same exchange in the WebUI
reads as one flat conversation.

The data is already on the wire: SP-051's `decorateEventPayload` at
`pkg/agent/agent_events.go:19-53` puts `subagent_depth` and
`active_persona` on every published event, so the WebUI's event
handler can read them — it just doesn't.

### Gap 2: Tool calls only show up after they complete

`webui/src/components/contextPanel/ToolCard.tsx` renders completed
tools in a sidebar; while a tool is running, the only feedback is a
generic `Processing...` skeleton in `ChatFooter.tsx:54-63`. Long
shell commands or web fetches feel like the agent is wedged.

The CLI got per-tool spinners in SP-048-1c: `read_file (foo.go)` →
`[OK] read_file (foo.go) · 0.1s`, one line per call, replaced in
place. The WebUI has the same `ToolStart` / `ToolEnd` events
available — `ToolExecution.status` cycles through
`'started' → 'running' → 'completed' | 'error'` — but nothing
renders them in real time.

### Gap 3: No cost / model / context visibility in the status bar

`webui/src/components/StatusBar.tsx:50-122` wraps the shared
`@sprout/ui` `StatusBar` and shows only file metadata (git branch,
language, cursor position, encoding, line ending, indentation). No
provider, no model, no cost, no token count.

The CLI's status footer shows
`claude-haiku-4-5 · 14.2k/200.0k ctx · $0.42 · sprout/ (main)`
on every refresh (`pkg/console/status_footer.go:281-303`). On the
WebUI, the user has to open a settings panel or wait for a
per-turn cost line to see what they're spending — and they only see
it *after* the turn ends.

## Current State

### What Works (do not regress)

| Mechanism | File:Line | Notes |
|---|---|---|
| Per-event `subagent_depth` + `active_persona` | `pkg/agent/agent_events.go:19-53` | Already on the wire as of SP-051; WebUI just needs to read |
| ToolExecution model includes `persona` | `packages/ui/src/types/chat.ts:39` | Populated for spawner tools; would need extending if we want it on every tool's events |
| Persona color map | `webui/src/components/chat/SubagentActivityFeed.tsx:11-24` | Already exists but lives in chat-feed scope; needs hoisting into `@sprout/ui` for reuse |
| Shared `Message` type | `packages/ui/src/types/chat.ts:20-27` | Owns the fields we extend; sprout-foundry consumes via `@sprout/ui` |
| Shared `MessageBubble` | `packages/ui/src/components/MessageBubble.tsx` | Adding optional props is backwards-compatible |
| Shared `StatusBar` | `packages/ui/src/components/StatusBar.tsx` | Already supports custom `leftItems`/`rightItems` slots — no schema change needed |
| `ChatProps.stats` already passed through | `packages/ui/src/types/chat.ts:153` | Cost/model/token data is already plumbed; just needs surfacing |

### What's Actually Missing

| Gap | Impact | Addressed by |
|---|---|---|
| Chat bubbles don't show persona/depth | High (delegation chain is invisible) | Phase 1 |
| No real-time tool feedback (live spinner per tool) | High (long tools feel wedged) | Phase 2 |
| No cost/model/context in status bar | Medium-High (cost surprise) | Phase 3 |
| `PERSONA_COLORS` lives only in SubagentActivityFeed | Low (blocks reuse) | Phase 1 prereq |

## Proposed Solution

Three phases. Phase 1 sets up the shared persona color helper and
extends the Message type — both needed by everything that follows.
Phase 2 builds on phase 1's color map. Phase 3 is independent and
could ship in parallel.

### Phase 1: Persona badge + depth indent in chat messages

**Scope:** extend the shared `Message` type; lift the persona color
map into `@sprout/ui`; render a colored persona badge and a
depth-indented bubble in `MessageItem.tsx`.

**1a. Lift the persona color map into `@sprout/ui`.** Create
`packages/ui/src/utils/personaColors.ts` with the existing map from
`SubagentActivityFeed.tsx:11-24` plus a `getPersonaColor(persona?:
string): string` export. Re-export from the package barrel. Update
`SubagentActivityFeed.tsx` to import from `@sprout/ui` instead of
defining its own copy — single source of truth, matching the CLI's
`pkg/console/persona_style.go` from SP-051-2b.

**1b. Extend the `Message` type.** Add two optional fields to
`packages/ui/src/types/chat.ts:20-27`:

```ts
export interface Message {
  // ... existing fields ...
  /** SP-053: persona ID (e.g. "coder") when this message came from a
   *  subagent. Drives the colored persona badge in MessageBubble. */
  persona?: string;
  /** SP-053: nesting depth (0=primary, 1=orchestrator, 2=specialist).
   *  Drives the left-margin indent. Absent or 0 means primary agent. */
  subagentDepth?: number;
}
```

Both optional — existing message-construction sites stay valid.

**1c. Populate persona/depth from incoming events.** In
`webui/src/hooks/useEventHandler.ts` (or wherever assistant messages
are constructed from `StreamChunk` / `AgentMessage` events — the
exact site falls out of grepping for `type: 'assistant'`), read
`subagent_depth` and `active_persona` from the event payload and
attach them to the constructed `Message`. The fields land on every
event thanks to SP-051's `decorateEventPayload`, so this is a one-
or two-line lift per construction site.

**1d. Render the badge + indent in `MessageBubble`.** Add optional
`persona?: string` and `depth?: number` to
`packages/ui/src/components/MessageBubble.tsx`. When `depth > 0`,
apply `marginLeft: depth * 12px` to the outer `.message` container;
when `persona` is non-empty, render a colored badge to the left of
the existing copy button. Default styling preserves today's look for
messages without these fields (depth 0 + no persona).

Visual target, mirroring the CLI's `    [coder] read_file …` line:

```
[user message bubble, no indent]
  [assistant message bubble — primary agent, no badge, no indent]
    [• coder] [assistant message bubble — indented 12px, colored badge]
      [• tester] [assistant message bubble — indented 24px, colored badge]
```

**1e. Tests.** Add `MessageBubble.test.tsx` cases: persona+depth
absent (snapshot matches today), persona present (badge rendered
with expected color via `getPersonaColor`), depth > 0 (correct
indent applied), depth 0 with persona (badge but no indent — primary
agent could have a non-default persona). One test pinning
backwards-compat: `<MessageBubble type="assistant" ariaLabel="…">…`
without the new props renders identically.

### Phase 2: Live tool timeline in chat footer

**Scope:** new `ToolTimelineBar` component in
`webui/src/components/chat/`; wire it into `ChatFooter.tsx` to render
above the existing `SubagentActivityFeed` / `queryProgress` /
processing-indicator stack.

**2a. New `ToolTimelineBar` component.** Renders up to 4 most-recent
in-flight or just-completed tool executions horizontally. Each card
shows:

- A small status icon (spinner for `started`/`running`, green check
  for `completed`, red X for `error` — reuse `lucide-react` icons,
  same set as `ToolCard`).
- The tool name in monospace.
- A compact arg preview (e.g. `(foo.go)`, `(go test ./…)`) — reuse
  the existing arg-preview logic from `ToolCard.tsx` or extract a
  shared `formatToolPreview(tool, args)` helper into `@sprout/ui`.
- The persona badge (when `ToolExecution.persona` is set), colored
  by `getPersonaColor` from phase 1.
- Elapsed time for in-flight tools (live-ticking); final duration
  for completed.

Completed tools fade out after 3 seconds (CSS transition) to keep
the bar uncluttered; errors stick until the next tool starts. The
component is purely view — it accepts `toolExecutions:
ToolExecution[]` and `maxVisible: number` as props.

**2b. Wire `ToolTimelineBar` into `ChatFooter`.** Insert above the
existing elements at `ChatFooter.tsx:28` when
`filteredToolExecutions.length > 0`. Replace the "generic
Processing..." path at `:54-63` with the timeline — if any tool is
visible, the skeleton suppresses (no need for two redundant signals
of "the agent is doing something"). When no tools are visible AND
`isProcessing`, fall back to today's skeleton.

**2c. Tests.** `ToolTimelineBar.test.tsx`: zero tools (renders
nothing), one running tool (shows spinner + tool name), one
completed tool (shows check + duration), mix of running and
completed (correct ordering — running first), error sticks past the
3s window, persona badge appears with expected color.

### Phase 3: Provider/model/cost in status bar

**Scope:** add chat-context props to the shared `StatusBar` (or
compose them via `rightItems`); plumb them from `AppContent`'s
existing `stats` object.

**3a. New `ChatStatusBarItems` component.** Lives in
`webui/src/components/chat/ChatStatusBarItems.tsx`. Renders the
right-aligned cluster:

```
<provider-icon> claude-haiku-4-5 · 14.2k/200.0k ctx · $0.42
```

Reads from `stats` (already in `ChatProps`) — fields
`provider`, `model`, `tokensUsed`, `tokensLimit`, `cost`. Uses the
same color-threshold logic for cost as the CLI's
`pkg/console/status_footer.go:310-319` (yellow >$1, red >$5);
respect `NO_COLOR` and the existing CSS theme variables.

Provider icon: a small lucide glyph keyed off provider name
(`Cloud` for cloud providers, `Server` for local, fallback to `Cpu`).
Defer fancier per-provider icons (Anthropic / OpenAI / etc.) to a
follow-up — generic icon + name is enough.

**3b. Render `ChatStatusBarItems` via `rightItems` slot.** The
shared `@sprout/ui` `StatusBar` already takes a `rightItems?:
ReactNode` prop (`packages/ui/src/components/StatusBar.tsx:25`).
The WebUI wrapper at `webui/src/components/StatusBar.tsx` composes
it: when a chat is active (i.e. `stats` is non-empty), pass
`<ChatStatusBarItems stats={stats} />` as `rightItems`; otherwise
keep today's editor metadata. No schema change to the shared
component.

**3c. Live update on stats events.** `stats` already flows into the
ChatPanel as a prop; phase 3 just consumes it. If `stats` updates
are throttled too aggressively (>1s between updates), bump the
event source's cadence to match the CLI footer's behavior
(refreshes on every `ToolEnd`) — but check first; the existing
pipeline may already be live enough.

**3d. Tests.** `ChatStatusBarItems.test.tsx`: renders with full
stats (all segments present), missing fields (segment omitted, no
empty `·` separator), cost threshold styling (below warn = no
color; above warn = yellow class; above alert = red class). Test
the WebUI `StatusBar.tsx` composition: when `stats` is empty, the
right section shows editor metadata; when `stats` is non-empty, it
shows the chat items.

## Out of Scope

Deferred or rejected for this round:

- **Mobile / responsive layout.** The survey flagged this as broken
  but it's its own significant effort — separate spec.
- **WebSocket reconnect UX polish.** Real concern (silent on drop)
  but unrelated to CLI parity.
- **Keyboard shortcuts** (Ctrl-R history search, command palette,
  etc.). Same: its own spec.
- **Dark/light mode toggle.** `ThemeContext` exists but isn't
  exposed; UX decision needed about where the toggle lives.
- **In-stream tool detail expansion** (clicking a tool line in the
  timeline opens a detail card inline). Worth doing eventually; for
  now `ToolsTab` remains the deep-dive surface and the timeline is
  glanceable status only.
- **Per-provider brand icons.** Generic provider type icon is
  sufficient for this round.
- **Tracking `filesModified`** (`App.tsx:238` TODO). Unrelated.

## Success Criteria

- **Chat bubbles from a subagent render indented and badged.** Test:
  a Message with `persona: "coder"`, `subagentDepth: 2` renders with
  24px left margin and a "coder"-colored badge. A Message without
  these fields renders byte-identical to today.
- **`PERSONA_COLORS` lives in `@sprout/ui` and `SubagentActivityFeed`
  imports from there.** Test: deleting the local copy in
  `SubagentActivityFeed.tsx:11-24` and re-running the suite still
  passes.
- **`ToolTimelineBar` shows a spinner for an in-flight tool and a
  check + duration for a completed one.** Test:
  `<ToolTimelineBar toolExecutions={[…running, …completed]} />`
  renders both states with the expected icons; the running tool's
  elapsed time updates on a ticker.
- **`ChatFooter` shows `ToolTimelineBar` when tools are active and
  suppresses the generic "Processing..." skeleton.** Test: with one
  running tool, the skeleton is absent; with no tools and
  `isProcessing=true`, the skeleton is present.
- **Status bar shows model + cost + tokens when chat is active.**
  Test: with `stats={provider: "anthropic", model:
  "claude-haiku-4-5", tokensUsed: 14200, tokensLimit: 200000, cost:
  0.42}`, the right section contains "claude-haiku-4-5",
  "14.2k/200.0k", "$0.42".
- **Cost color thresholds apply.** Test: cost 0.5 = no color class;
  cost 2.0 = yellow class; cost 10.0 = red class.
- **`npm run build` in `webui/` succeeds AND `make build-all` (Go
  binary + embedded UI) succeeds.** Both gates required since the
  shared `@sprout/ui` package is consumed by both `webui/` and
  `sprout-foundry`.
- **Existing `MessageBubble` consumers (sprout-foundry) keep
  compiling.** New props are optional; default render is unchanged
  for callers that don't pass them.
