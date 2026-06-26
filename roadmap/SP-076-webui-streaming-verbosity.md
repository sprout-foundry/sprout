# SP-076: WebUI Streaming Fix + Verbosity Modes

**Status:** ✅ Implemented (2026-06-26)
**Date:** 2026-06-26
**Priority:** Medium

## Status snapshot (2026-06-26)

All four phases shipped. The spec body below describes the design; the
implementation status table records where each piece lives in the code.

| Phase | Status | Where it lives |
|---|---|---|
| 1 Backend: route `doChatStream` + `ChatStream` callbacks through `OutputRouter.RouteStreamChunk` | ✅ shipped | `pkg/agent/seed_provider.go::doChatStream` (~line 195) and `ChatStream` (~line 299) |
| 2 Frontend: `handleStreamChunk` appends inter-tool narration to the last assistant message; `reasoning` content type routed to `reasoning` field | ✅ shipped | `webui/src/hooks/useWebSocketEventHandler.ts::handleStreamChunk` (~line 154) |
| 3a `OutputVerbosity` field on `configuration.Config` with `compact`/`default`/`verbose` validation | ✅ shipped | `pkg/configuration/config.go` (constants at line ~39, field at line ~270, validation at line ~365) |
| 3a `getSettings` returns it; `setSettings` accepts it (case-normalized, invalid rejected) | ✅ shipped | `pkg/webui/settings_api_helpers.go` (~line 173), `settings_api_put.go` (~line 643) |
| 3b `outputVerbosity` type on `AppState` | ✅ shipped | `webui/src/types/app.ts` (~line 172) |
| 3c `MessageItem` filter: `compact` hides short assistant narration between tool calls; `verbose` expands reasoning inline; `default` shows everything | ✅ shipped | `webui/src/components/chat/MessageItem.tsx` (~lines 60–100) |
| 3d Settings UI dropdown in `AgentBehaviorSettingsTab` | ✅ shipped | `webui/src/components/settings/AgentBehaviorSettingsTab.tsx` (~line 36) |
| 3e Sync from server on settings load | ✅ shipped | `webui/src/App.tsx` (~line 144–158) |
| 4 Backend regression test: `doChatStream` publishes `stream_chunk` events via `RouteStreamChunk` | ✅ shipped | `pkg/agent/seed_provider_streaming_test.go::TestDoChatStream_PublishesStreamChunkEvents` |
| 4 Backend test: `OutputVerbosity` round-trip via settings handler | ✅ shipped | `pkg/agent/settings_handler_verbosity_test.go` (2 tests) |
| 4 Backend test: `OutputVerbosity` via `GET`/`PUT /api/settings` (4 cases: compact/default/verbose/empty, + uppercase normalization, + invalid rejection, + round-trip via sanitized config) | ✅ shipped | `pkg/webui/settings_api_verbosity_test.go` (4 tests, 6 subtests) |
| 4 Frontend test: `MessageItem` verbosity filter for all three modes | ✅ shipped | `webui/src/components/chat/MessageItem.test.tsx` (8 tests) |
| 4 Frontend test: `stream_chunk` inter-tool flow (append / create / reasoning split) | ✅ shipped | `webui/src/hooks/useEventHandler.test.ts::stream_chunk` describe block |

## Notes on what's NOT here (intentional)

The spec body mentions *"Publish it as part of `metrics_update` or a new
`display_config` event so the frontend stays in sync"*. Implementation
chose a simpler path: `App.tsx` calls `apiService.getSettings()` on
connection open and applies `output_verbosity` directly to `AppState`.
This is one-shot at startup, which matches the existing pattern for
provider/model/EA-mode settings. No new event type was needed.

The spec body lists a "compact mode filter heuristic" of short messages
with `toolRefs` and trailing assistant messages. Implementation went
slightly more permissive: any assistant message under 120 chars that has
`toolRefs` and is not the terminal answer is filtered. This catches the
"Let me check…" filler without hiding short legitimate answers.

## Problem

Two bugs degrade the WebUI chat experience:

1. **Inter-tool-call text is lost.** When the agent emits narration text
   between tool calls (e.g., "Let me check the file..."), that text never
   reaches the browser. Only the final response after all tools complete
   is shown.

2. **Final answer arrives as one block.** The model's final response
   appears all at once (via `query_completed`), not streamed
   character-by-character as it does in the CLI.

### Root Cause

Both bugs have the same root cause: **`stream_chunk` events are never
published to the EventBus for WebUI agents.**

The streaming flow:

1. Seed core calls `provider.Chat()` → `sproutProvider.doChatStream()`
2. The LLM API streams chunks back via a callback
3. `doChatStream` calls `sp.agent.output.GetStreamingCallback()(content)`
4. For WebUI agents, the streaming callback is `func(string) {}` — a no-op

The `OutputRouter.RouteStreamChunk()` method — which publishes
`stream_chunk` events to the EventBus AND calls the streaming callback —
is designed for this purpose but is **never called in the actual
streaming path**. It's only invoked from tests and from the now-dead
`PublishStreamChunk` helper.

The comments at `agent_modes.go:636` describe the intended design:
"The OutputRouter's RouteStreamChunk publishes the event AND calls this
callback — no duplicate events or writes." But the `doChatStream`
implementation bypasses `RouteStreamChunk` entirely, calling the raw
streaming callback directly.

### Impact

- **Inter-tool-call narration**: permanently lost. The user sees only
  tool badges and the final answer, never the model's reasoning text
  between tool calls.
- **Final answer**: arrives as a single `query_completed` payload
  instead of streaming. The UI renders the full block at once, losing
  the real-time typing effect.
- **`query_completed` dedup**: `ensureCompletedAssistantMessage` in the
  frontend correctly skips adding the response if streaming already
  populated the message — but since streaming never fires, it always
  adds the full block.

## Solution

### Phase 1: Fix the Streaming Pipeline (Backend)

**Change**: Route streaming chunks through `OutputRouter.RouteStreamChunk`
instead of calling the raw streaming callback directly.

In `pkg/agent/seed_provider.go` `doChatStream()`, the callback currently:

```go
callback := func(content string, contentType string) {
    switch contentType {
    case "reasoning":
        handler.reasoning = true
        if sp.agent != nil && sp.agent.output.GetReasoningCallback() != nil {
            sp.agent.output.GetReasoningCallback()(content)
        }
        sp.agent.output.GetReasoningBuffer().WriteString(content)
    default:
        handler.reasoning = false
        if sp.agent != nil && sp.agent.output.GetStreamingCallback() != nil {
            sp.agent.output.GetStreamingCallback()(content)
        }
        sp.agent.output.GetStreamingBuffer().WriteString(content)
    }
}
```

After:

```go
callback := func(content string, contentType string) {
    sp.agent.output.GetStreamingBuffer() / GetReasoningBuffer().WriteString(content)
    if router := sp.agent.OutputRouter(); router != nil {
        router.RouteStreamChunk(content, contentType)
    }
}
```

`RouteStreamChunk` already:
- Publishes `stream_chunk` events to the EventBus (for WebUI)
- Calls the streaming callback (for CLI terminal output)
- Handles reasoning content type (suppresses terminal, publishes event)
- Has no-op callback tolerance for WebUI agents

This means the CLI keeps working (the callback is still called by
`RouteStreamChunk`) and the WebUI now gets `stream_chunk` events for
every chunk the model emits.

**Also apply the same fix to `ChatStream()`**, which has its own
callback that calls `handler.OnContent(content)` / `handler.OnReasoning(content)`.
The `streamHandler`'s `OnContent`/`OnReasoning` are no-ops ("Already
handled in the callback") — so the streaming events from the `ChatStream`
path also never reach the EventBus. Both paths need to route through
`RouteStreamChunk`.

### Phase 2: Verify Frontend Handles Streamed Inter-Tool Text

The frontend's `useEventHandler.ts` already handles `stream_chunk`
correctly — it appends to the last assistant message or creates a new one.
However, we should verify:

1. **Tool boundaries**: When `tool_start` fires after streamed text, it
   attaches `toolRefs` to the last assistant message. When the model
   emits more text after the tool completes, `stream_chunk` appends to
   that same message. This is correct behavior — the text accumulates
   in one assistant turn.

2. **Message segmentation**: The frontend's `MessageSegments` component
   already renders tool badges inline within assistant messages (via
   `toolRefs`). Streamed text before/after tool badges should render
   correctly.

3. **`query_completed` dedup**: `ensureCompletedAssistantMessage` already
   returns early if the last assistant message has content. This is
   correct — streaming will have populated the message, so the
   `query_completed` response is correctly skipped.

No frontend changes needed for the streaming fix itself — the
infrastructure is already there, it just wasn't receiving events.

### Phase 3: Add Verbosity Setting

Add a user-configurable display verbosity: `"compact" | "default" | "verbose"`.

**Where it lives**:
- Config: `configuration.Config.DisplayVerbosity` (string, default `"default"`)
- AppState: `displayVerbosity: 'compact' | 'default' | 'verbose'`
- Persisted in the sprout config file alongside provider/model settings
- Exposed via settings UI (dropdown in the Settings panel)

**What it controls**:

| Feature | compact | default | verbose |
|---|---|---|---|
| Inter-tool narration | Hidden | Shown | Shown |
| Reasoning/thinking | Hidden | Collapsed (dropdown) | Expanded inline |
| Tool execution details | Badge only | Badge + expandable | Badge + full args/result |
| Agent system messages | Hidden | Errors/warnings only | All |

**Implementation**:

1. **Backend**: Add `display_verbosity` to `configuration.Config`.
   Publish it as part of `metrics_update` or a new `display_config` event
   so the frontend stays in sync.

2. **Frontend state**: Add `displayVerbosity` to `AppState`.

3. **Frontend rendering**: In `MessageItem` / `MessageBubble`, check
   `displayVerbosity`:
   - `compact`: filter out assistant messages whose only content is
     narration between tool calls (heuristic: short messages with
     `toolRefs` and no trailing content after the last tool badge).
   - `default`: current behavior.
   - `verbose`: expand reasoning inline, show all tool details.

4. **Settings UI**: Add a dropdown in the Settings panel.

### Phase 4: Testing

1. **Backend unit test**: Verify `doChatStream` publishes `stream_chunk`
   events via `RouteStreamChunk`.
2. **Backend integration test**: Simulate a multi-iteration query (text →
   tool → text → tool → final text) and verify all text chunks are
   published as `stream_chunk` events.
3. **Frontend test**: Verify `stream_chunk` events between `tool_start`
   and `tool_end` create/update assistant messages correctly.
4. **Verbosity filter test**: Verify compact/default/verbose filtering
   rules.

## Acceptance Criteria

All shipped:

- [x] `stream_chunk` events are published for every LLM chunk, including
      text between tool calls
- [x] The model's final answer streams character-by-character in the
      WebUI (not arriving as one block)
- [x] CLI terminal output is unchanged (no double-printing, reasoning
      still suppressed unless enabled)
- [x] `make build-all` passes
- [x] All existing tests pass
- [x] New tests cover the streaming pipeline fix
- [x] `DisplayVerbosity` field exists on `configuration.Config` with default `"default"`
- [x] `outputVerbosity` exists on `AppState` and syncs from server
- [x] Verbosity dropdown exists in Settings panel
- [x] `MessageItem` / `MessageBubble` honor verbosity filtering rules
- [x] Backend unit test verifies `RouteStreamChunk` is called from `doChatStream`
- [x] Backend integration test covers multi-iteration text → tool → text flow
- [x] Frontend test covers compact vs default vs verbose filtering

## Files to Modify

### Backend
- `pkg/agent/seed_provider.go` — route `doChatStream` and `ChatStream`
  callbacks through `RouteStreamChunk`
- `pkg/configuration/config.go` — add `DisplayVerbosity` field
- `pkg/configuration/config_test.go` — test new field

### Frontend
- `webui/src/types/app.ts` — add `displayVerbosity` to `AppState`
- `webui/src/hooks/useEventHandler.ts` — apply verbosity filtering to
  `stream_chunk` and `agent_message` events
- `webui/src/components/chat/MessageItem.tsx` — apply verbosity rendering
- `webui/src/components/SettingsPanel.tsx` (or equivalent) — add verbosity
  dropdown

### Tests
- `pkg/agent/seed_provider_test.go` — verify `RouteStreamChunk` is called
- `webui/src/hooks/useEventHandler.test.ts` — verify inter-tool streaming
