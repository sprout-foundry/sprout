# SP-051: Depth-Aware Subagent UI — Visible Nesting in the CLI

**Status:** 📋 Proposed
**Date:** 2026-05-22
**Depends on:** SP-026 (Executive Assistant — introduced `subagentDepth`), SP-048 (CLI Delight — added the terminal tool-event subscriber), SP-050 (Orchestrator Persona Collapse — keeps the depth model honest about which persona owns which level)
**Priority:** Medium-High (the EA → orchestrator → specialist chain is currently invisible in the CLI; users can't tell when work has been delegated or what's running underneath)
**Effort Estimate:** ~1 day, two phases — phase 1 is the structural plumbing (event decoration) and phase 2 is the rendering layer

## Problem

The `sprout agent` CLI gained a per-tool spinner / timeline in SP-048-1c
(see `cmd/agent_modes.go:1055-1122` `startTerminalToolSubscriber`),
which renders each tool call as:

```
  read_file (path: pkg/agent/persona.go)
  [OK] read_file (path: pkg/agent/persona.go) · 0.1s
```

That timeline is great when the primary agent is the only one running.
The moment a subagent enters the picture, it fails three ways:

### Gap 1: Subagent tool calls are visually indistinguishable from the parent's

The event bus is **shared** across all nesting levels
(`subagent_runner.go:404-409` reuses `r.shared.EventBus` when creating
the subagent's `OutputRouter`). When the orchestrator spawns a coder
subagent and the coder runs `read_file`, the `ToolStart` event fires
from inside the coder's tool executor and lands on the same event bus
the CLI is subscribed to. The CLI just sees another `read_file` line
with no marker indicating "this came from two levels down."

Result: when EA → orchestrator → coder is running, the user sees a
flat wall of `[OK] read_file`, `[OK] edit_file`, `[OK] shell_command`
lines and has no way to tell which agent did which thing.

### Gap 2: Provider/model visibility ends at the spawn point

`formatRunSubagentPreview` at `cmd/agent_modes.go:1147-1165` shows
`(coder · anthropic/claude-haiku-4-5)` when `run_subagent` is called,
which is exactly right at the spawn site. But once the subagent is
running, that information is gone — there is no live "currently
running coder@haiku-4-5" indicator anywhere. The status footer
(`pkg/console/status_footer.go`) shows the *parent's* model only.

This matters because subagents commonly use cheaper / faster models
(haiku for coder, gpt-5-mini for researcher) and one of the practical
reasons to delegate is cost — but if the user can't see which model
ran the work, they can't validate the cost story.

### Gap 3: Subagent stream prefix exists but doesn't carry to tool lines

`buildSubagentPrefix` at `subagent_runner.go:125-133` prepends
`[coder]` to subagent *stream output* lines (lines 397-400). That
prefix is rendered by the dim-gray output path *inside* the subagent's
output router. Tool-call lines do **not** go through that path —
they're rendered by the CLI subscriber on the parent side. So you see
`[coder]` in front of streamed text from the model, but the
subsequent tool lines drop the prefix.

The result is jarring:

```
[coder] I'll start by reading the file you mentioned.
[coder] ...
  read_file (path: pkg/agent/persona.go)         ← parent-style chrome
  [OK] read_file (path: pkg/agent/persona.go) · 0.1s
[coder] Now I'll edit the file.
  edit_file (path: pkg/agent/persona.go)
```

### Gap 4: No "what subagents are currently running" view

If two subagents are running in parallel (`run_parallel_subagents`),
the only way to tell is to count `[persona]`-prefixed stream lines as
they fly past. The status footer doesn't show subagent count, and
there's no `/subagents` command to list active ones.

## Current State

### What Works (do not regress)

| Mechanism | File:Line | Notes |
|---|---|---|
| `subagentDepth int` on Agent | `pkg/agent/agent.go` (added SP-026 Phase A) | 0=EA, 1=orchestrator, 2=specialist; `MaxSubagentDepth=2` |
| Depth propagation on spawn | `pkg/agent/subagent_runner.go:680` | Child = parent depth + 1 |
| Shared event bus | `pkg/agent/subagent_runner.go:404-409` | Child publishes to parent's bus |
| Event metadata decoration | `pkg/agent/agent_events.go:12-53` | `decorateEventPayload` already merges per-agent `EventMetadata` into every published event |
| Stream prefix | `pkg/agent/subagent_runner.go:125-133,400` | `[coder]` / `[coder:taskID]` on stream-output lines |
| Tool timeline | `cmd/agent_modes.go:1055-1122` | Spinner + `[OK]`/`[FAIL]` per tool call |
| Spawn-time provider/model | `cmd/agent_modes.go:1147-1165` | `(coder · anthropic/claude-haiku-4-5)` only on `run_subagent` line |
| Status footer | `pkg/console/status_footer.go` | Shows parent's model + cost + ctx |
| Persona alias canonicalization | `pkg/agent/persona.go:38-50` (added SP-050) | `repo_orchestrator` resolves to `orchestrator` so depth-1 always reads as one name |

### What's Actually Missing

| Gap | Impact | Addressed by |
|---|---|---|
| Tool events not tagged with depth/persona | High (timeline is unreadable when subagents run) | Phase 1 |
| Tool lines don't carry the `[persona]` prefix that stream lines do | High (visual inconsistency, can't tell who did what) | Phase 2 |
| Live subagent model isn't shown anywhere | Medium (cost validation needs it) | Phase 2 |
| No active-subagent count in footer | Low (nice-to-have during parallel) | Phase 2 |

## Proposed Solution

Two phases. Phase 1 is structural and lands first; it's a low-risk
change (one new `SetEventMetadata` call at subagent creation) that
enables everything in phase 2.

### Phase 1: Tag every event with depth + persona

**Scope:** one file changed (`pkg/agent/subagent_runner.go`), zero new
packages, zero CLI changes. After this phase, every event a subagent
publishes carries `subagent_depth` and `active_persona` in its
payload — including events the parent and Web UI subscribers see
today.

**1a. Decorate the subagent's event metadata at creation.** In the
subagent creation path (`subagent_runner.go` around `:680` where
`subagentDepth` is set), call:

```go
subAgent.SetEventMetadata(map[string]interface{}{
    "subagent_depth":   subAgent.subagentDepth,
    "active_persona":   opts.Persona,
    "subagent_task_id": taskID, // only set when non-empty (parallel)
})
```

`SetEventMetadata` already merges metadata into every published event
via `decorateEventPayload` (`pkg/agent/agent_events.go:19-53`). No new
plumbing is required — the metadata reaches every `ToolStart`,
`ToolEnd`, `StreamChunk`, `AgentMessage`, etc.

Care: `SetEventMetadata` *replaces* metadata wholesale. If the parent
already set client/chat/user IDs and we want them to propagate
through subagents, the call needs to be additive. Look at how
`r.shared.EventBus` propagation works today — if subagents inherit
the parent's metadata via some other mechanism, simply layer the
depth/persona fields on top. Worst case: read existing metadata, merge,
write back. (Spec lets phase-1 implementer decide based on what they
find; the test below pins the contract either way.)

**1b. Decorate the EA / primary agent the same way.** For
`subagentDepth == 0`, the primary agent should publish events with
`subagent_depth: 0` and its active persona. This keeps the
subscriber's branching logic uniform — every event has the field, even
at depth 0. Done at agent creation (or via a hook in `ApplyPersona`
that updates metadata whenever the active persona changes).

**1c. Tests:** in `pkg/agent/subagent_runner_test.go`, assert that a
`ToolStart` event published from a spawned subagent carries
`subagent_depth: 1` (or 2 for grandchildren) and the expected
persona ID. Use a test event bus and inspect the published payload.

### Phase 2: Render the depth in the CLI tool timeline

**Scope:** `cmd/agent_modes.go` (the tool subscriber rendering code),
optionally a small helper in `pkg/console` for the persona color
lookup. No protocol changes; the Web UI already receives the same
metadata and can render it independently if desired (out of scope for
this spec).

**2a. Indent by depth.** In `startTerminalToolSubscriber`
(`cmd/agent_modes.go:1055`), read `subagent_depth` from the event
data on each `ToolStart` / `ToolEnd`. Prepend an indent string of
`strings.Repeat("  ", depth)` to the existing spinner / result line.
The current 2-space `"  "` prefix in the format strings at `:1087`
and `:1107` becomes `indent + "  "`.

Result for an EA-spawned orchestrator that runs `read_file`:

```
  read_file (path: AGENTS.md)                  ← depth 0 (EA), unchanged
  [OK] read_file (path: AGENTS.md) · 0.1s
    read_file (path: pkg/agent/persona.go)      ← depth 1 (orchestrator), indented 2 spaces
    [OK] read_file (path: pkg/agent/persona.go) · 0.1s
      shell_command (cmd: go test ./...)         ← depth 2 (coder), indented 4 spaces
      [OK] shell_command (cmd: go test ./...) · 12.3s
```

**2b. Persona color + badge.** Add a deterministic
persona-ID → ANSI-color map (e.g., `coder=cyan`, `tester=green`,
`debugger=yellow`, `researcher=magenta`, `code_reviewer=blue`,
`orchestrator=white`). Prepend a `[persona]` badge to each tool line,
colored by the map; depth-0 events have no badge (it's just the user's
session). Compact form for narrow terminals: use a single-letter glyph
in brackets (`[C]` for coder, `[T]` for tester) when the line would
otherwise wrap.

Result:

```
  read_file (path: AGENTS.md) · 0.1s
    [orchestrator] read_file (path: pkg/agent/persona.go) · 0.1s
      [coder] shell_command (cmd: go test ./...) · 12.3s
```

The color comes from the persona map regardless of depth, so a coder
spawned at depth 1 and a coder spawned at depth 2 read as the same
agent type.

**2c. Subagent model footer line.** When a non-empty `subagent_depth`
event arrives, also emit a one-shot "Spawned" line that includes the
resolved provider/model. This re-uses the existing
`formatRunSubagentPreview` logic but fires from the subscriber when
*the first event from a new depth+persona pair* arrives:

```
    ↳ orchestrator spawned (openrouter · openai/gpt-5)
      ↳ coder spawned (anthropic · claude-haiku-4-5)
```

Tracked via a `map[depth]persona` in the subscriber struct; cleared
when the subagent's terminal event (currently inferred from `ToolEnd`
status, more reliable: a new event type
`EventTypeSubagentLifecycle` — out of scope, defer to phase 3 if it
becomes load-bearing).

**2d. Status-footer subagent count.** Extend `ContentSource` in
`pkg/console/status_footer.go` with an optional
`ActiveSubagents() int` method (interface assertion, so existing
sources don't break). When non-zero, append ` · N sub` to the footer
line. The count source: a new atomic counter on `Agent` that
increments on subagent start and decrements on subagent finish, read
via the new method.

**2e. Tests:** in `cmd/agent_modes_test.go`, add cases that exercise
the depth-indented format strings. Pure unit tests against a small
helper extracted from `startTerminalToolSubscriber` — the goroutine
itself is hard to test, but the format builder is straightforward.

## Out of Scope

Deferred or explicitly rejected for this round:

- **Tree-rendering with box-drawing characters.** Indent-by-depth
  reads clearly enough; box-drawing characters (`├─`, `└─`) add
  complexity around when to render the corner vs. the cross and
  don't survive resize cleanly. Revisit if users ask.
- **Subagent persona shown inline in the status footer.** The footer
  is already crowded (model + ctx + cost + cwd + branch); adding a
  per-subagent breakdown would overflow. The "Spawned" lines + the
  per-tool `[persona]` badges give the user the same info without
  fighting for footer real estate.
- **Web UI changes.** The metadata now flows to every event, so the
  Web UI client *could* render the same nesting — but the Web UI's
  current chat-bubble layout already has subagent-collapse semantics
  for its own reasons. Leaving the Web UI alone for now; future spec.
- **Persisting active-subagent state across reconnects.** The footer
  count is best-effort live state; if a reconnect drops it, the next
  subagent event re-establishes the count.
- **`/subagents` slash command listing active subagents.** Useful but
  small; can land in a follow-up CLI-affordance spec.
- **`EventTypeSubagentLifecycle` event type.** Today subagent start /
  end is implicit (you infer it from the first/last event carrying
  a given depth+persona). A first-class lifecycle event would be
  cleaner but is its own design conversation.

## Success Criteria

- **Every event published from a subagent carries `subagent_depth`
  and `active_persona` in its payload.** Tested via a unit test that
  spawns a subagent against a captured event bus and asserts both
  fields on a `ToolStart` event.
- **The CLI tool timeline indents by depth.** Test: a depth-2 event
  produces output prefixed with 4 spaces of indent (above the
  baseline 2-space prefix the current implementation uses).
- **Each non-zero-depth tool line shows a `[persona]` badge,
  color-coded per persona.** Test: format-builder output contains the
  expected ANSI color sequence for the given persona ID. Respects
  `NO_COLOR` (no ANSI when set), consistent with SP-048-4a.
- **On first arrival of a new (depth, persona) pair, the subscriber
  emits a `↳ persona spawned (provider · model)` line.** Test:
  feeding two consecutive events from the same (depth, persona) emits
  exactly one spawn line.
- **Status footer shows ` · N sub` when subagents are active and
  hides the suffix when N==0.** Test: a `ContentSource` stub with
  `ActiveSubagents() = 2` produces a footer line containing
  `· 2 sub`; with 0, the suffix is absent.
- **The orchestrator persona at depth 1 reads as `orchestrator`, not
  `repo_orchestrator`.** Reinforces SP-050's canonicalization — alias
  resolution at persona-apply time means the depth-1 badge is stable.
- **No regression in existing CLI tests.** `cmd/agent_modes_test.go`
  passes, plus the per-tool subscriber's existing format strings
  remain byte-identical when `subagent_depth == 0` (depth-0 events
  produce no badge and no extra indent).
- **`make build-all` succeeds.**
