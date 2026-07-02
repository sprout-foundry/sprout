# SP-076: WebUI Streaming Fix + Verbosity Modes

**Status:** ✅ Implemented (2026-06-26; streaming pipeline fixed, compact/default/verbosity modes shipped)

Two bugs degraded the WebUI chat experience: inter-tool-call narration text was lost (never reached the browser), and the model's final answer arrived as one block instead of streaming character-by-character. Both had the same root cause: `stream_chunk` events were never published to the EventBus for WebUI agents because `doChatStream` called the raw streaming callback directly instead of routing through `OutputRouter.RouteStreamChunk`. The fix routes both `doChatStream` and `ChatStream` callbacks through `RouteStreamChunk`, which publishes `stream_chunk` events AND calls the streaming callback. Additionally, three verbosity modes (`compact`, `default`, `verbose`) were added as a user-configurable display setting, controlling inter-tool narration visibility, reasoning expansion, and tool detail level.

## Key decisions

- Route through `RouteStreamChunk` instead of inventing a new event type — the infrastructure was already there, just unused.
- Frontend needed no changes for the streaming fix itself — `useEventHandler.ts` already handled `stream_chunk` correctly.
- Verbosity stored in `configuration.Config` (backend) and synced to `AppState` on connection open — no new WebSocket event type needed.
- Compact mode filter: any assistant message under 120 chars with `toolRefs` and not the terminal answer is filtered (slightly more permissive than the spec's original heuristic).

## Artifacts

- code: `pkg/agent/seed_provider.go` — `doChatStream` and `ChatStream` callbacks routed through `RouteStreamChunk`
- code: `pkg/configuration/config.go` — `OutputVerbosity` field with `compact`/`default`/`verbose` validation
- code: `webui/src/components/chat/MessageItem.tsx` — verbosity-aware rendering filter
- code: `webui/src/components/settings/AgentBehaviorSettingsTab.tsx` — verbosity dropdown in settings
- code: `webui/src/types/app.ts` — `outputVerbosity` type on `AppState`
- code: `webui/src/App.tsx` — sync verbosity from server on settings load
- tests: `pkg/agent/seed_provider_streaming_test.go` — `doChatStream` publishes `stream_chunk` events
- tests: `pkg/webui/settings_api_verbosity_test.go` — backend round-trip tests (4 tests, 6 subtests)

Full specification archived — see git history for original content.
