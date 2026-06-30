# SP-056: CLI Reasoning Fold — Collapsed Thinking Indicator

**Status:** ✅ Implemented (2026-06-30)
**Date:** 2026-05-24
**Depends on:** `pkg/agent/output_router.go` reasoning routing (already in place — binary on/off via `--show-reasoning`), CLI ergonomics Phase 1/2/3 (status footer + glyph vocabulary + tool collapse, all shipped).
**Priority:** Medium — current behavior is "all or nothing": reasoning is either invisible or streams the full chain-of-thought (often hundreds of dim-styled tokens) into the scroll region. A fold mode would let users *see that the model is thinking* without the noise.
**Effort Estimate:** One contained pass — single file in `pkg/console/`, two new methods on `OutputRouter`, ~5 wiring lines in `agent_modes.go`. Plus a flag.

## Problem

Today, reasoning content (from models that emit a separate chain-of-thought stream — Anthropic extended thinking, OpenAI o1/o3, DeepSeek-R1, GLM, etc.) has exactly two display modes:

1. **Hidden** (default) — `reasoningTerminalEnabled = false`. The reasoning still flows through the event bus to the WebUI, but the CLI scroll region shows nothing until the assistant content begins streaming. From the user's seat: silence, then suddenly the answer. With slow models this can feel like sprout is stuck. SP-055's ttft segment (this session) helps, but doesn't tell the user *what* the model is doing during the wait.

2. **Full stream** (`--show-reasoning`) — every reasoning token streams to stderr in dim text. Quickly fills the scroll region for a reasoning-heavy turn (Claude Opus thinking can easily hit 5–10k tokens of dim text). The actual answer scrolls past too fast to skim.

Neither mode communicates "the model is thinking, here's how much" in a low-density way. The webui handles this naturally because reasoning has its own UI affordance (collapsed by default with a token count). The CLI needs an equivalent.

## Current State

### What works

- The output router (`pkg/agent/output_router.go:37–61`) cleanly separates reasoning chunks from assistant text. Reasoning chunks arrive with `contentType: "reasoning"` on the streaming callback.
- The `--show-reasoning` flag controls terminal rendering of reasoning content via `SetReasoningTerminalEnabled`. The CLI wires this in `SetupAgentEvents` at `cmd/agent_modes.go:594`.
- The activity indicator (`pkg/console/activity_indicator.go`) already supports `Update(msg)` for spinner text and `ReplaceLastN` (added SP-056-prep in this session for tool-collapse) for in-place line updates.

### What's missing

- A **third mode** between hidden and full-stream: a single pinned line that updates with token count as reasoning streams.
- A flag (`--reasoning-fold` or rename `--show-reasoning` to take a value: `hidden|fold|full`).
- Per-turn reset so each new reasoning burst starts a fresh counter.
- Tasteful handoff: when assistant text starts, the fold line should *resolve* to a final summary (`⋯ thought for 1.2k tokens · 3.4s`) instead of just vanishing.

## Proposed Solution

### Display modes

Replace the binary `--show-reasoning` flag with `--reasoning=<mode>`:

| Mode | Behavior |
|---|---|
| `hidden` (default) | As today. Reasoning silent in terminal, still on event bus. |
| `fold` (new) | Pinned `⋯ thinking … 1.2k tokens` line that updates in place every ~100ms; resolves to `⋯ thought for 1.2k tokens · 3.4s` when assistant text begins, then assistant streams below. |
| `full` (was `--show-reasoning=true`) | As today. Every reasoning token streams in dim. |

Keep `--show-reasoning` as a back-compat alias for `--reasoning=full`.

### Mechanism

New helper in `pkg/console/`:

```go
// ReasoningFold renders a single pinned "⋯ thinking · N tokens · T elapsed"
// line that updates in place as reasoning chunks arrive, then resolves
// to a final summary when reasoning ends. Uses the activity-indicator
// row but reserves it specifically for reasoning so it can't conflict
// with tool-spinner output.
type ReasoningFold struct {
    indicator *ActivityIndicator
    startedAt time.Time
    tokenEstimate int // running estimate; updated on each chunk
    active bool
    mu sync.Mutex
}

func (r *ReasoningFold) Start() // begin tracking; spawn updating line
func (r *ReasoningFold) Chunk(text string) // ingest one reasoning chunk
func (r *ReasoningFold) Resolve() // finalize: emit summary, clear pinned line
```

Token estimate uses a cheap byte-length heuristic (~4 chars per token); we don't need precision here since the value is for UX feel, not billing.

### Wiring

In `SetupAgentEvents`:

```go
fold := console.NewReasoningFold(indicator)

chatAgent.SetReasoningCallback(func(chunk string) {
    if reasoningMode == ReasoningFold {
        fold.Chunk(chunk)
    } else if reasoningMode == ReasoningFull {
        fmt.Fprint(os.Stderr, dim(chunk))
    }
})

chatAgent.EnableStreaming(func(chunk string) {
    if reasoningMode == ReasoningFold {
        fold.Resolve()
    }
    indicator.Stop()
    // existing stream-chunk path
})
```

The router already knows the `contentType` of each chunk; we just need to add a separate callback for reasoning chunks so the existing assistant-text callback stays clean.

### Visual

```
ⓘ Provider: anthropic | Model: claude-opus-4-7

[user] Implement a graph search algorithm with shortest-path queries.

⋯ thinking · 847 tokens · 2.1s                  ← updates in place every 100ms

[after reasoning ends, assistant text begins:]
⋯ thought for 1.2k tokens · 3.4s                 ← resolved summary stays in scrollback
Sure, here's an A* implementation that handles weighted edges…
```

The fold line lives above the assistant text in the scrolling region (not pinned at the very bottom — that's the status footer's job). It uses `⋯` (GlyphDim from Phase 2) so it reads as "ambient progress, not primary content."

### Edge cases

- **Reasoning that ends with no assistant text** (rare but possible — model returns only a tool call after thinking). Resolve on the first tool-event instead of waiting for stream chunks.
- **Multiple reasoning bursts in one turn** (model thinks, calls tool, thinks again). Each burst gets its own resolved line so the scrollback shows the sequence.
- **NO_COLOR / non-TTY**: the fold mode degrades to a single Fprintln per chunk burst — no fancy in-place updates, but the summary line still resolves at end. Stays correct in log captures.
- **Mid-turn interrupt** (Ctrl+C): the fold's pinned line should resolve to `⋯ thinking interrupted (847 tokens)` rather than leaving an orphan "thinking" line.

## Out of Scope

- **Expandable reasoning** — pressing a key to reveal the full CoT after a turn. That's a richer terminal UI problem (alt-screen or pager integration) and belongs in its own spec.
- **Per-provider reasoning style** — some models stream reasoning as a separate field, others mix it into the response. This spec handles the existing event-bus contract; provider-specific normalization is upstream.
- **Token-count accuracy** — using bytes/4 estimate is fine for a UX indicator. Real per-model tokenization belongs in the cost-tracking path.

## Success Criteria

1. With `--reasoning=fold` (or once it's the default), a user running a reasoning-heavy turn sees a single live-updating progress line during the thinking phase, not silence and not a wall of dim text.
2. The resolved summary stays in the scrollback so the user can see *how long* a turn spent thinking after it completes.
3. The fold line never clobbers tool-spinner rows or assistant streaming — the three live on independent renders.
4. `--show-reasoning` (legacy flag) still works and routes to `--reasoning=full`.
5. No regression in the default-hidden behavior for users who don't opt in.
6. Tests cover: Start/Chunk/Resolve lifecycle, NO_COLOR degradation, interrupt path, multi-burst sequences.

## Notes for the implementer

- The `OutputRouter.RouteStreamChunk` path already differentiates `contentType: "reasoning"` from `"assistant_text"`. The cleanest hook point is adding a `SetReasoningCallback` parallel to `EnableStreaming` so the CLI can plumb fold updates without changing what the WebUI receives.
- The existing `Update(msg)` method on `ActivityIndicator` updates the spinner text in place but only while the spinner is *active*. For fold, we want the line to update without a spinning frame — consider adding `ActivityIndicator.SetStatic(line)` that pins a non-animated line to the same row.
- Token counter resets to 0 on `Start()`. Multiple `Start()` calls in one turn (multi-burst) should each show their own count; the per-burst lines accumulate in the scrollback.
- The flag goes in `cmd/agent_command.go` next to the other agent flags. Default to `hidden` for compatibility, or `fold` if we want to ship the better default — open question for the implementer.
