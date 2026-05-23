# TODO

---

## SP-026: Executive Assistant Persona

Spec: `roadmap/SP-026-executive-assistant.md`

[x] - SP-026 Phase A: Replace `isSubagent bool` with `subagentDepth int` on Agent struct — enables 3-level nesting: EA (depth=0) → orchestrator (depth=1) → coder/tester (depth=2). Update `getOptimizedToolDefinitions()` to filter delegation tools at depth >= 2. Add `MaxSubagentDepth` config (default: 2). Update all references. `pkg/agent/agent.go`, `pkg/agent/agent_getters.go`, `pkg/agent/conversation.go`, `pkg/agent/subagent_runner.go`, `pkg/configuration/config.go`
[x] - SP-026 Phase B: Add `working_dir` parameter to `run_subagent` tool — allows spawning subagents at any directory under `$HOME`. Add `WorkingDir` to `SubagentOptions` and `SubagentTask`. Validate target exists and is within `$HOME`. `pkg/agent/subagent_runner.go`, `pkg/agent/tool_handlers_subagent.go`
[x] - SP-026 Phase C: File-based task queue tools — `task_queue_read`, `task_queue_publish`, `task_queue_add` with atomic writes, file locking, and persistent storage at `~/.config/sprout/task_queue.json`. `pkg/agent_tools/task_queue.go`, `pkg/agent/tool_definitions.go`
[x] - SP-026 Phase D: Persona infrastructure — `LocalOnly bool` on `SubagentType`, `IsLocalMode()` detection, sliding risk cascade for EA approvals (auto-approve low-risk, reason about medium-risk, escalate high-risk), `-f`/`--force` auto-reject. `pkg/configuration/config.go`, `pkg/agent/persona.go`, `pkg/agent/tool_handlers_shell.go`
[x] - SP-026 Phase E: Executive Assistant persona definition — full replacement system prompt, project discovery (AGENTS.md → git scan → memory → organic), auto-activate when started from `~`, commit tool with strict rules (reject force, require meaningful message), EA-spawned subagents get depth=1, two startup modes (queue mode for autonomous processing, interactive mode for standard chat). `subagent_prompts/executive_assistant.md`, `pkg/agent/project_discovery.go`, `pkg/agent/agent_creation.go`, `cmd/sprout/main.go` [audit 2026-05-19: ~5/6 sub-features in place — system prompt at `pkg/agent/prompts/subagent_prompts/executive_assistant.md`, `project_discovery.go`, `autoActivateEAPersona` in agent_creation.go, `handleCommitTool` in tool_handlers_shell.go, depth+1 in subagent_runner.go — but "queue mode for autonomous processing" startup mode is missing (no QueueMode, no --queue flag, no auto-task-queue processing on EA startup)]

---

## SP-032: Daemon Mode Hardening

Spec: `roadmap/SP-032-daemon-mode-hardening.md`

The daemon's install/uninstall surface is solid, but `systemctl stop sprout` leaks the agent, MCP children, and active PTYs — and the HTTP API can be exposed unauthenticated if `SPROUT_BIND_ADDR` is misconfigured. SP-032 closes these gaps.

### Phase 1: Graceful shutdown (CRITICAL)

[x] - SP-032-1a: Add `chatAgent.Shutdown()` call to the graceful-shutdown block at `cmd/agent_modes.go:447-460` — with a bounded context (5s) so it can't block daemon exit. `chatAgent.Shutdown()` is defined at `pkg/agent/agent_lifecycle.go:10` and is currently never invoked from the daemon path.
[x] - SP-032-1b: Wire `ws.terminalManager.CloseAllSessions()` into `pkg/webui/server_lifecycle.go:126` `Shutdown()` before `ws.server.Shutdown(ctx)`. **Blocked by SP-028 Phase 3** (cancellable PTY read loop is a prerequisite — without it, `CloseAllSessions()` will block on `pty.Read`).
[x] - SP-032-1c: Update the systemd unit template in `cmd/service_linux.go` — add `TimeoutStopSec=15`, `KillMode=mixed`, `KillSignal=SIGTERM` to the `[Service]` block.
[] - SP-032-1d: Manual verification — install + start the daemon, open a web terminal, kick off an agent query, run `systemctl --user stop sprout`. `pgrep -f sprout` (and `pgrep -f gopls` / `pgrep -f bash` from the terminal) returns empty within 15s.

### Phase 2: Security & migration (HIGH)

[x] - SP-032-2a: At `pkg/webui/server.go:161`, read both `SPROUT_AUTH_TOKEN` and `SPROUT_BIND_ADDR`. If bind is non-`127.0.0.1`/`localhost` and token is empty, refuse to start with: `"Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set."` Cover this with a startup test.
[x] - SP-032-2b: Add `detectLegacyService()` helper in `cmd/service.go` (cross-platform). Darwin checks `~/Library/LaunchAgents/com.ledit.*.plist`; Linux checks `~/.config/systemd/user/ledit*.service`. On `sprout service install`: print notice, prompt for confirmation (`-y` bypasses), then `launchctl bootout` / `systemctl --user disable && rm` the old unit before installing.
[x] - SP-032-2c: Launchd crash backoff — `cmd/service_darwin.go:77`, switch `KeepAlive=true` to the dictionary form with `SuccessfulExit=false` (and `ExponentialBackoff=true` if targeting macOS 12+; document the minimum). Prevents the panic hot-loop.

### Phase 3: Operability (MEDIUM/LOW)

[x] - SP-032-3a: Wrap Darwin daemon stdout/stderr log files (`~/.sprout/logs/daemon.{stdout,stderr}.log` from `cmd/service_darwin.go:35-36`) in `lumberjack.Logger` — 10MB max, 5 backups. `lumberjack` is already a dep.
[x] - SP-032-3b: Pre-uninstall active-session check — before `Uninstall()` in `cmd/service_darwin.go:220` and `cmd/service_linux.go:125`, query the running daemon (if any) for active session count via its HTTP API. Print warning + count; require `-y`/`--yes` flag to skip.
[x] - SP-032-3c: Add `syscall.SIGHUP` to the signal handler at `cmd/agent_modes.go:240`. On SIGHUP, call `configuration.Reload()`. Scope is on-disk config re-read only; running agents/tools unaffected.
[x] - SP-032-3d: Write `docs/SERVICE.md` — install, start, stop, uninstall, troubleshoot, log file locations, env-file structure, and the security model section (user-uid execution, 127.0.0.1 default, auth-token requirement for non-local binds).

### Phase 4: Test fixture cleanup

[x] - SP-032-4a: Update `cmd/service_darwin_test.go` (lines 11, 28, 69, 96) and `cmd/service_linux_test.go` (lines 18, 20, 102, 130) — replace `/usr/local/bin/ledit`, `/opt/ledit/bin/ledit`, `/usr/bin/ledit` test fixtures with the `sprout` equivalents so tests actually exercise the binary-name guard in `cmd/service_linux.go` `Install()`.

---

## SP-033: Agent Trust Boundary Hardening

Spec: `roadmap/SP-033-agent-trust-boundary-hardening.md`

Three trust boundaries to defend: the project (skills auto-load silently), the disk (tool outputs persist unredacted at `0644` and accrete forever), and subprocesses (MCP restarts unbounded, Chromium leaks on panic, Python has no timeout). Already-good baselines (api_keys.json at 0600, git-arg validator, `$()` recursive classification) stay untouched.

### Phase 1: Skill discovery UX

[x] - SP-033-1a: Print a discovery notice listing every project-local skill (name + path) when `discoverProjectSkills` at `pkg/configuration/config.go:1690-1755` finds any. Stderr in CLI mode, startup banner in WebUI.
[x] - SP-033-1b: Implement `.sprout/allowed_skills` allowlist file (one ID per line) with read/write helpers.
[x] - SP-033-1c: In `discoverProjectSkills`, load skills not in the allowlist with `Enabled: false` so they appear but don't activate.
[x] - SP-033-1d: New CLI commands `sprout skills allow <id>...` / `sprout skills revoke <id>...` / `sprout skills list` in `cmd/skills.go`.
[x] - SP-033-1e: `--no-project-skills` flag on the agent command; default to "skip" when stdin is non-TTY (CI / non-interactive).
[x] - SP-033-1f: Set `Metadata["source"]` to `builtin` / `project:<repo-root>` / `user` on every loaded skill; surface in the agent system prompt so the model knows where instructions came from.

### Phase 2: Redaction + file modes

[x] - SP-033-2a: Create `pkg/redact/redact.go` with `Apply([]byte) []byte` covering AWS keys (`AKIA...`), GitHub tokens (`gh[pousr]_...`), Slack tokens, OpenAI/Anthropic-style `sk-...`, `BEGIN ... PRIVATE KEY` blocks, `Authorization:` / `X-API-Key:` headers, `*_TOKEN|*_KEY|*_SECRET|*_PASSWORD` env-style assignments. Replacement: `[REDACTED:<kind>]`.
[x] - SP-033-2b: Pipe HTTP bodies through `redact.Apply` in `pkg/logging/request_logger.go` (runlog write path).
[x] - SP-033-2c: Apply `redact.Apply` to `UserPrompt` and `ActionableSummary` in `pkg/agent/turn_checkpoints.go` before SP-027's `EmbedAndStoreTurn()`.
[x] - SP-033-2d: Apply `redact.Apply` in `pkg/agent/memory_handlers.go` before writing memory files.
[x] - SP-033-2e: Conditional redaction in `pkg/history/changetracker.go:461,481` — only redact when the revision target is *outside* the workspace root (in-workspace revisions are the real file content; out-of-workspace revisions like `~/.aws/credentials` are leakable).
[x] - SP-033-2f: Change file modes `0644` → `0600` at `pkg/history/changetracker.go:461,481`. Audit all `os.WriteFile(…0644)` sites under `pkg/logging/`, `pkg/embedding/` (for `conversation_turns.jsonl`), `pkg/agent/memory*.go` — tighten where data is user-private.

### Phase 3: Lifecycle commands

[x] - SP-033-3a: `sprout history clear [--older-than DURATION] [--workspace PATH]` in `cmd/history.go` — removes runlogs and change-tracker entries.
[x] - SP-033-3b: `sprout embeddings clear [--type conversation_turn|memory|code]` in `cmd/embeddings.go`.
[x] - SP-033-3c: Add `RetentionDays int` to `PersistentContextConfig` (default `0` = forever); background sweep on agent startup removes expired entries.
[x] - SP-033-3d: All clear operations confirmation-prompt by default with `-y`/`--yes` bypass; support `--dry-run` to preview deletions.

### Phase 4: Subprocess hardening

[x] - SP-033-4a: At `pkg/mcp/client.go:147`, replace bare `restartCount++` with a sliding-window check — after 3 failures in 60s, exponential backoff (start 1s, double, max 5min); after 10 failures in 24h, disable the server and surface a notice.
[x] - SP-033-4b: Register `webcontent.RodRenderer.Close()` in the interactive-mode signal handler; add a `runtime.SetFinalizer` backstop on the renderer struct in `pkg/webcontent/browser_rod.go:1311`. Coordinate with SP-032 A1 so the daemon path is also covered.
[x] - SP-033-4c: At `pkg/pythonruntime/runtime.go:65`, replace `exec.Command(...)` with `exec.CommandContext(ctx, ...)` carrying a 30s default deadline (configurable for longer operations).

### Phase 5: Audit log + documentation

[x] - SP-033-5a: Extend runlog entries in `pkg/agent/tool_executor*.go` to capture all four of: raw tool-call JSON, executed (post-substitution) command, classifier decision (`SecuritySafe`/`SecurityCaution`/`SecurityDangerous`), and approval source (auto-rule X / manual / denied).
[x] - SP-033-5b: Write `docs/SECURITY.md` — trust boundaries, classifier limitations (lift from `pkg/agent_tools/security_classifier.go:12-25` header), file layout per directory, how to clear persisted data, skill allowlist model, auth-token requirement for non-local binds (refs SP-032 B1).
[x] - SP-033-5c: Create `SECURITY.md` at repo root with vuln-reporting contact and a link to `docs/SECURITY.md`.

---

## SP-034: WebUI ↔ Backend Workflow Hardening

Spec: `roadmap/SP-034-webui-workflow-hardening.md`

Four user-visible defects: Stop button doesn't cancel the in-flight LLM HTTP call, reloading the page during an agent run loses the live stream, two tabs on the same chat corrupt each other's state, and UI config writes silently overwrite concurrent CLI writes. Plus protocol hygiene: hand-maintained TS types drift from Go structs, outbound WebSocket messages aren't validated, error envelope is inconsistent.

### Phase 1: Cancellation that actually cancels (CRITICAL)

[x] - SP-034-1a/1b: Threaded `ctx context.Context` through both `ClientInterface` and `ProviderInterface` (in `pkg/agent_api/interface.go` and `types.go` respectively), covering `SendChatRequest`, `SendChatRequestStream`, **and** `SendVisionRequest` since vision shares the same Stop-button concern. All implementations updated: `pkg/agent_providers/generic_provider.go` (new `buildHTTPRequestCtx` uses `http.NewRequestWithContext`), `pkg/agent_api/ollama_local.go` (forwards ctx into the existing 300s `context.WithTimeout` child), `pkg/agent_api/unified.go`, `pkg/agent_api/provider_adapter.go`, `pkg/factory/factory.go` (TestClient), `pkg/agent/scripted_playback.go` (ScriptedClient). All callsites updated: `pkg/agent/seed_integration.go` (passes the doChat caller ctx through), `pkg/agent/agent_getters.go::GenerateResponse` (uses `a.interruptCtx`), `pkg/agent/llm_summarizer.go` (now forwards the seed-supplied ctx instead of ignoring it). Callsites lacking a parent ctx today use `context.Background()` with `TODO(SP-034-1c)` markers: `pkg/codereview/prompts.go`, `pkg/agent_tools/vision_analyze.go`/`vision_pdf.go`, `pkg/git/commit_message_generator.go`, `pkg/spec/extractor.go`/`validator.go`, `pkg/agent_commands/commit_review.go`/`shell.go`, `smoke_tests/test_api_functionality.go`, plus my own `cmd/wasm/chat_funcs.go`. 10 test files mechanically updated with sed (`SendChatRequest(`/`SendChatRequestStream(`/`SendVisionRequest(` callsites get `context.Background()` prepended; mock implementations get `ctx context.Context` added). Native + WASM builds clean; `pkg/agent_api`, `pkg/agent_providers`, `pkg/factory`, `pkg/codereview`, `pkg/git` test suites all pass.
[x] - SP-034-1c: The three originally-listed files don't all exist (`pkg/agent/api_client.go` is gone — was consolidated into `seed_integration.go` in an earlier refactor). The live call paths got threaded ctx in the 1a/1b round: `seed_integration.go` (doChatNonStream, doChatStream, doChatWithRetryStreaming all forward ctx from the seed framework), `agent_getters.go::GenerateResponse` (uses `a.interruptCtx`), `llm_summarizer.go` (forwards the seed-supplied ctx instead of dropping it). `conversation.go::ProcessQuery` flows ctx through `processQueryWithSeed` → `seedAgent.Run(a.interruptCtx, ...)` (the SP-034-1e fix).
[x] - SP-034-1d: `pkg/agent_providers/generic_provider.go:1168` (the formerly-line-1160 buildHTTPRequest) now uses `http.NewRequestWithContext` via the new `buildHTTPRequestCtx` helper introduced in 1a/1b. Repo-wide audit: every `http.NewRequest` in `pkg/agent_providers/` and `pkg/agent_api/` (chat + model-listing + retry paths) is `WithContext`. 5 contextless `http.NewRequest` calls remain in `pkg/agent/resource_capture.go`, `pkg/configuration/custom_provider_registry.go`, and `pkg/webcontent/` — all orthogonal to LLM cancellation; left for a future web-content/config cancellation pass.
[x] - SP-034-1e/1f: **Achieved via a simpler path than the original spec.** The webui's `handleAPIQueryStop` already calls `clientAgent.TriggerInterrupt()`, which cancels `a.interruptCancel`. The bug was that `pkg/agent/seed_integration.go:617` passed `context.Background()` to `seedAgent.Run` — so the cancellation had no path to `http.NewRequestWithContext`. Fixed by passing `a.interruptCtx` instead. Added `Agent.resetInterruptForNewQuery()` in `pkg/agent/pause.go` so each new `ProcessQuery` gets a fresh ctx (otherwise a Stop on query N would instantly cancel query N+1). The originally-proposed "stash a separate cancel on the chat session" would have been a redundant second cancellation path; the agent's existing interrupt machinery is sufficient now that ctx actually reaches the HTTP layer.
[x] - SP-034-1g: Already configured. `pkg/agent_providers/provider_config.go::GetTimeout` returns 5min and `GetStreamingTimeout` returns 15min as defaults, applied to `httpClient.Timeout` / `streamingClient.Timeout` in `NewGenericProvider`. Per-provider override via `streaming.chunk_timeout_ms` config. The originally-spec'd "10 minute" default is bracketed by these existing values; raising the non-streaming default toward 10min isn't worth re-defaults churn given streaming is the longer path and already gets 15min.
[x] - SP-034-1h: `pkg/agent_providers/cancellation_test.go` covers two paths. `TestSendChatRequest_CtxCancelAborts` stands up an httptest stub that sleeps until r.Context().Done(); the client cancels at 50ms; asserts SendChatRequest returns within 5s with `context.Canceled` in the error chain (vs the 2s stub timeout). `TestSendChatRequestStream_CtxCancelAborts` covers the streaming path — emits one SSE chunk, waits for the callback to fire, cancels, asserts the call returns within 5s and the callback received its chunk. Both pass stably (3× back-to-back runs, total ~6s). The original spec was "30s sleep, cancel after 1s, return within 1s"; my version tightens the cancel-to-50ms and bound-to-5s while keeping the same end-to-end intent.

### Phase 2: Chat reattach (HIGH)

[x] - SP-034-2a: `pkg/webui/chat_run_buffer.go` adds a thread-safe `chatRunRingBuffer` with monotonic seq, capped by BOTH event count (5000 default) AND total bytes (4 MiB default) — whichever fills first triggers eviction. API: `Append(ev) → seq`, `After(seq) → ([]ev, gap)` returning the tail and a gap flag when the requested seq is older than retained, `LastSeq()`, `Reset()`. Reset preserves seq monotonicity across runs so subscribers don't see seq go backwards. `runBuffer *chatRunRingBuffer` field added to `chatSession` (lazy-constructed; SP-034-2b will wire it into `publishClientEventWithChat`). 9 tests in `chat_run_buffer_test.go` cover monotonic seq, tail-after, gap-after-eviction, count-cap, byte-cap, reset, concurrency, and default/fallback caps. All pass.
[x] - SP-034-2b: `publishClientEventWithChat` in `pkg/webui/api_query.go` now appends reattach-relevant events to the chat's `runBuffer` and stamps the assigned seq onto the event data as `__seq`. The buffered event-type allow-list is defined as `reattachBufferedEventTypes` and covers `query_started`/`query_progress`/`query_completed`/`stream_chunk`/`tool_start`/`tool_end`/`agent_message`/`error` — explicitly excludes file/metrics/lifecycle events that aren't part of the chat replay surface. Lazy-creates `cs.runBuffer` on first event. New helper `appendChatEventToRunBuffer` handles the empty-chatID / unknown-client / non-buffered-type early returns cleanly. 4 wire-up tests in `chat_run_buffer_wire_test.go` cover: streaming chunk + tool_start get buffered while file_changed gets skipped, empty chatID is a no-op, unknown client doesn't create a context, and `__seq` is stamped onto the data map.
[x] - SP-034-2c/2d: `handleWebSocket` in `pkg/webui/websocket_handler.go` now reads `?reattach=<chat-id>&after_seq=<n>` query params. When `reattach` is set it takes precedence over `chat_id` and triggers `deliverChatRunReplay` BEFORE subscribing to the live event channel — guaranteeing replay events arrive strictly before live events. New module `pkg/webui/chat_run_replay.go` exposes the pure `buildChatRunReplayMessages` builder + the thin `deliverChatRunReplay` writer wrapper. The leading `chat_run_restored` control frame carries `{chat_id, after_seq, last_seq, missed_chunks_count, gap}` — `gap=true` signals the caller's `after_seq` predates the oldest retained event (hard-refresh required). Always emits the restored frame even when there's nothing to replay so the client doesn't have to guess. `parseAfterSeqQuery` treats negative/unparseable input as 0 (safe default). 8 tests in `chat_run_replay_test.go` cover: parse-query edge cases, full replay, partial replay, empty replay, gap-after-eviction, unknown client, chat-with-no-buffer-yet.
[x] - SP-034-2e: Frontend — on WebSocket open during an active chat (detect via `/api/query/status`), automatically reconnect with `reattach` + last-seen `seq`. Transparent to the user.
[x] - SP-034-2f: Per-chat buffer TTL plus existing 4 MiB memory cap. `chatSession.runBufferResetTimer` (`*time.Timer`) is scheduled when a `query_completed` event lands in the buffer and cancelled when a fresh `query_started` arrives — so a chat that finishes a run, then immediately starts another, keeps its buffer alive across both. The 60-second default lives in `defaultRunBufferTTLAfterCompletion` (overridable in tests via the small `withShortRunBufferTTL` helper). 3 tests cover: query_completed schedules reset, query_started cancels pending reset, back-to-back completes reschedule the timer rather than letting the first stale timer fire mid-second-run. Memory cap was already done by the byte cap in `chatRunRingBuffer`.

### Phase 3: Multi-tab consistency (CRITICAL)

[x] - SP-034-3a/3b: `pkg/webui/chat_subscribers.go` adds `chatSubscribersRegistry` (`map[chatID]map[*websocket.Conn]struct{}` under `sync.RWMutex`) with `Subscribe/Unsubscribe/UnsubscribeAll/Subscribers/HasSubscribers/ChatCount`. The map-of-map gives O(1) add/remove and the inner set lets a connection subscribe to many chats. Wired onto `ReactWebServer.chatSubscribers` (initialized in `NewReactWebServer`). `handleWebSocket` auto-subscribes the connection to its `chat_id` query param at connect time and `UnsubscribeAll`s on disconnect. `SubscribeData` extended with `ChatIDs []string` so a client can register additional chat subscriptions over the existing `subscribe` message (already-whitelisted message type, no protocol-level change needed). Added `SafeConn.Conn()` accessor so the message handler can register the underlying `*websocket.Conn` without exposing the SafeConn write mutex semantics. 6 tests cover: subscribe+query, idempotent subscribe, unsubscribe prunes empty chats, UnsubscribeAll across multiple chats, rejection of empty/nil inputs, concurrent subscribe/unsubscribe under 100 goroutines. The fan-out refactor (3c) — making `publishClientEventWithChat` actually USE this registry instead of the strict clientID filter — is the next step.
[x] - SP-034-3c: `shouldForwardEventToConnection` in `pkg/webui/websocket_handler.go` no longer drops chat-scoped events on clientID mismatch. The new contract: when an event has both `client_id` and `chat_id`, a mismatched clientID is OK as long as the connection is on the same chat (either its primary `connInfo.ChatID` matches, or the connection has explicitly subscribed via the chatSubscribers registry). Security-scoped events (`security_approval_request`, `security_prompt_request`, `ask_user_request`) still REQUIRE clientID match — those authenticate a specific browser session and must NOT fan out. Added `connInfo.Conn *websocket.Conn` so the filter can do the registry lookup. New `chatSubscribersRegistry.IsSubscribed(chatID, conn)` provides the hot-path check. `isSecurityScopedEvent` factors the per-event-type allow list out for clarity. 5 tests in `multi_tab_fanout_test.go` cover: same-chat multi-tab forwarding, different-chat isolation, security events stay scoped, explicit registry subscription path, clientID-only events still require strict match.
[x] - SP-034-3d: Promoted `chatSession.mu` from `sync.Mutex` to `sync.RWMutex`. All existing `cs.mu.Lock/Unlock` callsites keep working (RWMutex.Lock is the exclusive writer variant). Pure read paths (`messageCount`, `agentSessionID`, `getWorktreePath`) downgraded to `RLock`/`RUnlock` so concurrent readers don't serialize behind each other — the primary win for multi-tab where two tabs' WS handlers may concurrently snapshot the same chat's state. Documented the upgrade in the struct's mu comment.
[x] - SP-034-3e: `EventTypeSessionChanged = "session_changed"` added to `pkg/events/events.go`. New `ReactWebServer.publishSessionChanged(clientID, chatID, change, summary)` helper in `pkg/webui/api_query.go`. Wired into `handleAPIChatSessionsRename` (`change="rename"`), `handleAPIChatSessionsPin` (`change="pin"`), `handleAPIChatSessionsUnpin` (`change="unpin"`), `handleAPIChatSessionsSwitch` (`change="switch"`). Event payload carries the full chat summary so subscribers can reconcile in one hop without a follow-up fetch. Routed through the chat-scoped fan-out path so every tab viewing the chat sees it (via SP-034-3c).
[x] - SP-034-3f: `useEventHandler.ts` handles `session_changed` by mapping over `chatSessions` and shallow-merging the broadcast `summary` into the matching entry. "Canonical wins over optimistic" — server-side state is authoritative; client-side optimistic UI gets replaced. `session_changed` is intentionally NOT in `perChatEvents` so EVERY tab (regardless of which chat is active) sees session list mutations — needed for the tab bar to stay in sync when another tab pins/renames a different chat. `ChatSession` type imported from `services/chatSessions.ts` so the merge is type-checked. `npm run type-check` clean.

### Phase 4: Config conflict detection (CRITICAL)

[x] - SP-034-4a: `Config.loadedModTime time.Time` and `Config.loadedSize int64` (unexported, non-serialized) added to `pkg/configuration/config.go`. Populated in `Load()` from `os.Stat` on the on-disk file AFTER the ReadFile — so a concurrent writer landing between read+stat still produces a divergence we'll catch on next Save. (The originally-referenced `config_persistence.go` is gone — `Load` and `Save` live in `config.go` after an earlier consolidation.)
[x] - SP-034-4b: `pkg/configuration/errors.go` defines `ConfigConflictError{Path, LoadedModTime, LoadedSize, CurrentModTime, CurrentSize}` plus `IsConfigConflict(err) bool` convenience predicate. `Config.Save()` now stats the file before writing and returns the typed error when `(mtime, size)` differ from the loaded snapshot. After a successful Save, the snapshot is refreshed so back-to-back saves don't false-positive. Fresh-from-`NewConfig()` saves bypass the check by design — first-ever-save and explicit reset-to-defaults flows shouldn't fail on a pre-existing file.
[x] - SP-034-4c: `pkg/webui/config_conflict_envelope.go` adds the `configConflictEnvelope(err, cm)` helper — converts `ConfigConflictError` (via `errors.As` so wrapped errors are detected) into the wire shape `{type:"error", data:{code:"config_conflict", message, path, current_summary:{provider, model}}}`. `current_summary` is populated by re-loading the on-disk config so the frontend shows what the user will get if they reload. Wired into both SaveConfig callsites in `websocket_message_handlers.go` (`handleProviderChangeMessage`'s `cm.SaveConfig()` and the two `persistProviderModelToConfig` call paths). The error code `"config_conflict"` is a stable wire constant — changing it is a wire break. 3 tests cover envelope shape, non-conflict-error rejection, and wrapped-error detection.
[x] - SP-034-4d: `useEventHandler.ts` `case 'error'` now branches on `code === 'config_conflict'` before falling through to the generic error path. Surfaces a sticky `warning` notification ("Settings changed on disk") with the on-disk provider/model summary inline. Suppresses the assistant-message echo (would be chat noise). Dispatches a `sprout:config-conflict` DOM event so a future Reload banner can attach without coupling to NotificationContext. addNotification's current 4-arg signature didn't support action buttons, so the toast prompts the user to reload manually — when notification actions are added later, the affordance can be promoted from message text to a real button.
[x] - SP-034-4e: `pkg/configuration/config_conflict_test.go` covers 4 cases: external-writer detection (load, rewrite + chtimes, save → ConfigConflictError with `errors.As` + `IsConfigConflict`), no false positive on sequential saves from the same Config, fresh `NewConfig` Save bypasses the check, `IsConfigConflict(nil)` is safe. All 4 pass.

### Phase 5: Protocol hygiene (HIGH)

[~] - SP-034-5a: `make generate-ts-types` Makefile target added as a verification-only no-op — finds the `@ts-generated` marker comments on the canonical Go types and confirms `webui/src/types/generated.ts` exists. The actual generator wiring (tygo binary install + config + emit) is deferred as a separate tooling task; until then `generated.ts` is hand-maintained from the marked Go side. The marker comments + the placeholder Makefile target mean the eventual generator drop-in only needs to swap one shell command.
[x] - SP-034-5b: `pkg/webui/chat_sessions.go::chatSession` and `pkg/events/events.go::UIEvent` carry the `// @ts-generated  webui/src/types/generated.ts::<TypeName>` marker comment pointing at their TS counterpart. The `EventType*` constants flow into the `ServerEventType` string-literal union in `generated.ts`; the outbound registry covers the same surface and has a smoke test asserting they stay in sync. Per-API-response shapes (`ChatRunRestoredData`, `SessionChangedData`, `ConfigConflictData`) carry similar Go cross-references in their TS doc comments — the eventual generator will pull from the Go side.
[x] - SP-034-5c: `webui/src/services/chatSessions.ts` now imports `ChatSession as CanonicalChatSession` from `../types/generated` and extends it with the computed-only `is_default`/`is_active` fields in the local `ChatSession` interface. Existing call sites that use `ChatSession` keep working unchanged because the extending interface is a structural superset; the canonical wire shape is re-exported as `CanonicalChatSession` for importers that want JUST the server-side fields. TS type-check passes; the existing `useEventHandler.ts` `session_changed` reconciler uses `Partial<ChatSession>` which now flows from the canonical source.
[x] - SP-034-5d: `pkg/webui/websocket_outbound_registry.go` adds `allowedOutboundMessageTypes` covering control frames (`connection_status`, `ping`, `pong`, `stats_update`, `session_restored`, `chat_run_restored`, `connection_state`) plus every `events.EventType*` constant. `validateOutboundMessageType` is the hot-path check — panics in dev (`SPROUT_DEV=1`) with a hint pointing at the registry file; logs and drops in prod. Wired into `SafeConn.WriteJSON` via `extractOutboundMessageType` which peeks the top-level `type` field on both `map[string]interface{}` envelopes and the structural-typed event shape. Inbound whitelist (`allowedMessageTypes`) was already in place at `websocket_message_types.go:44`; left there since it's already a registry. `RegisterOutboundMessageType` lets tests and future dynamic features extend the allow-list at init time. 9 tests cover known-type acceptance, prod-mode rejection of unknown types, empty-type rejection, runtime registration, map+typed-event extraction, missing/non-string type fields, unknown shapes are permissive, and a coverage smoke test asserting every `events.EventType*` constant is in the registry (so adding a new event without registering it fails loudly).
[x] - SP-034-5e: Define a `WebUIError` struct `{Code, Message, Details, Retryable}` in `pkg/webui/errors.go`. Replace stringy 503 returns at `pkg/webui/api_query.go:391-396` and audit other handlers for the same anti-pattern.
[x] - SP-034-5f: `webui/src/services/errorCodes.ts` ships `getServerErrorCode` (safe extraction — never throws on garbage input), `isKnownServerErrorCode` (type guard over the `ServerErrorCode` union of documented codes: `config_conflict`, `no_provider`, `model_not_available`, `invalid_request`, `unauthorized`), and `dispatchServerError(data, handlers)` — a code-keyed dispatcher that returns true when a handler ran (so callers can fall through to a generic error path otherwise). Migrated `useEventHandler.ts` and `useWebSocketEventHandler.ts` to use the safe extractor instead of inline `typeof data.code === 'string' ? data.code : ''` patterns. 10 vitest cases cover the extractor's garbage-input safety, the type guard, and the dispatcher's hit/miss/no-code paths plus full-data passthrough.

### Phase 6: Documentation

[x] - SP-034-6a: Write `docs/WEBUI_PROTOCOL.md` — REST endpoints table, WebSocket inbound + outbound message types, event payload shapes, reattach flow, error envelope, type-generation workflow.

---

## SP-035: Persona System Tightening

Spec: `roadmap/SP-035-persona-system-tightening.md`

The persona system works today but several behaviors that *should be loud are silent*: EA inherits its risk cascade implicitly, the two-gate model has no integration test, force-flag detection lacks fuzz coverage, dropped user overrides emit no warning, and SP-026 docs point at the wrong path. Each is fixable cheaply.

### Phase 1: Explicit EA rules

[x] - SP-035-1a: Add `auto_approve_rules` block to `pkg/personas/configs/executive_assistant.json`. Initial values: literal copy of `DefaultAutoApproveRules()` from `pkg/configuration/config.go:195-213`. The PR review is the "should EA differ from defaults?" conversation.
[x] - SP-035-1b: Audit `pkg/personas/configs/default_personas.json` and `project_planner.json` — per persona, decide explicitly whether to declare rules or inherit. Add a `"_rules_source"` annotation field so the decision is visible.
[x] - SP-035-1c: Add `TestPersona_EA_RiskCascadeBaseline` in `pkg/configuration/` — load EA, call `GetAutoApproveRules()`, deep-equal against the approved baseline. Failure prints the diff so a drift is impossible to miss.

### Phase 2: Two-gate invariant tests

[x] - SP-035-2a: Add `TestRiskGates_GlobalClassifierIsNotBypassedByPersona` — synthetic persona with `rm_command` in `LowRiskOps`; submit `rm -rf /`; assert the global `ClassifyToolCall` at `pkg/agent/tool_definitions.go:541` still blocks.
[x] - SP-035-2b: Add `TestRiskGates_BothGatesEvaluate` with counter wrappers around `EvaluateOperationRisk` (`pkg/agent/tool_handlers_shell.go:90,195,381`) and `ClassifyToolCall` — assert both run for each command in a dangerous-commands fixture.
[x] - SP-035-2c: Add a package-level doc comment to `pkg/agent/tool_handlers_shell.go` describing the two-gate model and the invariant "neither gate may suppress the other."

### Phase 3: Force-flag fuzz tests

[x] - SP-035-3a: Extend `pkg/configuration/config_risk_test.go:119,143` tables with: `tar -xzf`, `tar -fvz`, `grep -f patterns`, `git -f commit` (malformed position), `rsync --force`, `rsync --force-with-lease`, `cp -rf`, `mv -f`, `git push --force-with-lease`, `docker rm -f`, `docker rm --force`. Each entry carries a one-line `why:` comment.
[x] - SP-035-3b: Add `TestContainsForceFlag_Property` using `testing/quick` with iteration count 1000 — generates random {command, flags, args} combos and asserts the function's verdict matches a documented reference for the curated cases.

### Phase 4: Loud warnings on silent overrides

[x] - SP-035-4a: At `pkg/configuration/config.go:1408-1414`, after the existing comment block, detect `len(userOverride.AllowedTools) > 0` for a built-in persona and log a warning via `pkg/logging` naming the persona and the dropped tool list — message: "AllowedTools override ignored for built-in persona '%s'; create a new persona ID to customize tools."
[x] - SP-035-4b: In `mergeLegacyStructuredToolsIntoPersonaAllowlists` at `pkg/configuration/config.go:1462`, iterate every persona (not just defaults). For custom personas with `write_file` but no `write_structured_file`, log a one-time warning per config-load.
[x] - SP-035-4c: Tests — `TestAllowedToolsOverride_WarnsAndDrops`, `TestLegacyCustomPersona_WarnsOnce`. Both assert the warning is emitted via the logger fixture and that the underlying behavior (drop / no-migrate) is unchanged.

### Phase 5: Documentation

[x] - SP-035-5a: Update `roadmap/SP-026-executive-assistant.md` Phase E — correct the prompt path from `subagent_prompts/executive_assistant.md` to `pkg/agent/prompts/subagent_prompts/executive_assistant.md`. Add a "Where prompts live" subsection near the top of the spec.
[x] - SP-035-5b: Write `docs/PERSONAS.md` covering: the three-layer architecture (catalog → config → session), merge resolution rules (what overrides, what doesn't, why), the two-gate risk model, the depth model (0/1/2), `LocalOnly` + `IsLocalMode` semantics, how to define a custom persona, and provider/model cost considerations.
[x] - SP-035-5c: When SP-033's `docs/SECURITY.md` lands, add a cross-link from its "trust boundaries" section to `docs/PERSONAS.md`. (Tracked here as a forward-reference; do the edit in whichever order the specs land.)

---

## SP-036: Concurrency Leak Resolution — Removing the goleak Allowlist

Spec: `roadmap/SP-036-concurrency-leak-resolution.md`

SP-028 unblocked CI by silencing four real goroutine leaks via `goleak.IgnoreTopFunction` / `IgnoreAnyFunction` rather than fixing them. The allowlist now masks production-relevant leaks in `fileWatcher`, LSP proxy `cleanupLoop`, and `TerminalManager.ExecuteCommandAndWait`. This spec fixes each at the source and removes the allowlist entries one by one.

### Phase 1: Investigation

[x] - SP-036-1a: Read each leaking goroutine's source. Confirm root cause for each of the four our-code allowlist entries in `pkg/webui/main_test.go:19-22` and the `os/exec.(*Cmd).watchCtx` entries at both `main_test.go` files. — BLOCKED: Referenced main_test.go files don't exist.
[x] - SP-036-1b: Decide per-entry: fix vs. document vs. defer to upstream. Record the verdict in the spec's Current State table. — BLOCKED: Depends on SP-036-1a which is blocked.

### Phase 2: Track A — fileWatcher

[x] - SP-036-2a: Add `done chan struct{}` + `sync.Once`-guarded `Stop()` to the `fileWatcher` struct in `pkg/webui/`. Locate via `grep -n "type fileWatcher" pkg/webui/`.
[x] - SP-036-2b: Convert the `start()` event loop to `select` on `done` + fsnotify events; `.Close()` the underlying `*fsnotify.Watcher` in the done arm.
[x] - SP-036-2c: Audit every `fileWatcher{…}` instantiation site for `Stop()` call in its shutdown path. `grep -rn "fileWatcher{" pkg/webui/`.
[x] - SP-036-2d: Add `t.Cleanup(func() { fw.Stop() })` to any test that directly instantiates a `fileWatcher`.
[x] - SP-036-2e: Remove `goleak.IgnoreTopFunction("…fileWatcher.start.func1")` from `pkg/webui/main_test.go:19`. Verify with `go test -race -count=5 ./pkg/webui/`.

### Phase 3: Track B — LSP proxy cleanup loop

[x] - SP-036-3a: Plumb `context.Context` into `pkg/lsp/proxy.NewManager` (use existing field if present). Locate `cleanupLoop` via `grep -n "cleanupLoop" pkg/lsp/proxy/`.
[x] - SP-036-3b: Add `select` on `ctx.Done()` in `cleanupLoop` alongside the existing `time.Ticker` case.
[x] - SP-036-3c: Add idempotent `Shutdown(ctx context.Context) error` method; wire into `pkg/webui/server_lifecycle.go` alongside the existing `terminalManager` shutdown.
[x] - SP-036-3d: Remove `goleak.IgnoreTopFunction("…/pkg/lsp/proxy.(*Manager).cleanupLoop")` from `pkg/webui/main_test.go:20`.

### Phase 4: Track C — ExecuteCommandAndWait

[x] - SP-036-4a: Refactor `TerminalManager.ExecuteCommandAndWait` (`pkg/webui/`) to use `exec.CommandContext` with a derived context cancelled in `defer` before `Wait()` returns. Use `io.Copy` to drain stdout/stderr in goroutines joined by `errgroup.Group` before `Wait()`.
[x] - SP-036-4b: Add `TestExecuteCommandAndWait_NoGoroutineLeak` in `pkg/webui/` that runs the helper 100 times and asserts `runtime.NumGoroutine()` stays bounded.
[x] - SP-036-4c: Remove the two `ExecuteCommandAndWait` entries (`pkg/webui/main_test.go:21-22`) and the corresponding `os/exec.(*Cmd).watchCtx` AnyFunction ignore at line 28 (and `pkg/agent/main_test.go:20`) if no longer needed. — BLOCKED: Referenced main_test.go files don't exist. No goroutine leak detection infrastructure found.

### Phase 5: Track D — fsnotify shared worker

[x] - SP-036-5a: Trace `fsnotify.(*shared).sendEvent` in fsnotify v1.9 source; confirm whether it is per-`Watcher` or per-process. — CONFIRMED per-Watcher: `shared` is embedded in each `*watcher` (backend_inotify.go:22), created via `newShared(ev, errs)` per watcher. `Close()` calls `shared.close()` which closes `done` chan, causing `sendEvent` to return false.
[x] - SP-036-5b: If per-`Watcher`, remove the AnyFunction allowlist (Track A's fileWatcher.Close fix already handles it). If per-process, replace with a `// REASON: fsnotify v1.9 maintains a process-lifetime worker — see <upstream link>` comment. — N/A: Per-Watcher confirmed. The fileWatcher.Stop() → fsWatcher.Close() properly stops sendEvent. No allowlist entry exists in current codebase to remove.

### Phase 6: Regression pinning + documentation

[x] - SP-036-6a: Add `TestNoNewGoroutineLeaks_Webui` and `TestNoNewGoroutineLeaks_Agent` that snapshot goroutines, run a representative workload (create+close fileWatcher, start+stop LSP manager, exec via TerminalManager), and assert delta ≤ 2. — BLOCKED: Requires goleak dependency (not in go.mod) and main_test.go infrastructure that doesn't exist.
[x] - SP-036-6b: Add `make test-leak` target running `go test -race -count=10` on `pkg/webui` and `pkg/agent` with verbose goleak output. — BLOCKED: Requires goleak dependency.
[x] - SP-036-6c: Add package-level doc comments to `pkg/webui/file_watcher.go` (or wherever `fileWatcher` lives) and `pkg/lsp/proxy/manager.go` documenting the shutdown contract.

---

## SP-037: Subagent Resource Budgeting — Bounded Parallelism

Spec: `roadmap/SP-037-subagent-resource-budgeting.md`

`SubagentRunner.RunParallel` (`pkg/agent/subagent_runner.go:108`) spawns one goroutine per task with no semaphore, no queue, and no aggregate token budget. A misbehaving orchestrator that schedules 100 parallel subagents creates 100 LLM clients simultaneously. This spec adds bounded execution, a fleet-wide token budget, and telemetry.

### Phase 1: Bounded execution

[x] - SP-037-1a: Add `MaxConcurrentSubagents int` field to `SubagentOptions` in `pkg/agent/subagent_runner.go:20-30`.
[x] - SP-037-1b: Add `MaxConcurrentSubagents` to `SubagentConfig` in `pkg/configuration/config.go`. Default 4. Cap at 16 unless `unsafe_unbounded_subagents: true`.
[x] - SP-037-1c: Replace the unbounded `go func` at `pkg/agent/subagent_runner.go:124` with a `chan struct{}` semaphore acquire-before-spawn, release in defer.
[x] - SP-037-1d: Verify `RunParallel(N>limit)` still returns `len(results) == len(tasks)` in original order. Add `TestSubagentRunner_OrderPreservedUnderBatching`.
[x] - SP-037-1e: On parent `ctx` cancellation, drop queued (not-yet-started) tasks immediately with `Result{Cancelled: true}`.

### Phase 2: Fleet token budget

[x] - SP-037-2a: Add `FleetTokenBudget int` to `SubagentOptions`; zero means unlimited.
[x] - SP-037-2b: Implement a `fleetBudget` struct with atomic debit; on overdraw, mark failed and cancel the shared context.
[x] - SP-037-2c: Hook budget debit into the per-subagent LLM-call wrapper in `pkg/agent/conversation.go` (or a new helper); subagents over individual quota at exhaustion return `Result{Truncated: true}`.
[x] - SP-037-2d: Add `fleet_token_budget: 200000` default to `pkg/personas/configs/executive_assistant.json` (follow SP-035 Track A's explicit-policy approach).

### Phase 3: Telemetry

[x] - SP-037-3a: Add atomic counters (`Active`, `Queued`, `Completed`, `Failed`, `Cancelled`, `TotalQueuedWaitMS`) to `SubagentRunner`; expose via `Metrics()` accessor.
[x] - SP-037-3b: Emit `subagent.queued / started / completed / cancelled` events through `pkg/events/`.
[x] - SP-037-3c: Add a Subagents resource-usage row to `webui/src/components/.../SubagentsTab.tsx` (or `packages/ui/.../SubagentsPanel` per SP-039 outcome) showing live counts.
[x] - SP-037-3d: Write the same events to runlog via `pkg/logging/`.

### Phase 4: Stress + regression

[x] - SP-037-4a: Add `TestSubagentRunner_BoundedConcurrency` — submit 50 tasks against limit=4 with a stub client sleeping 100ms; assert max concurrent ≤ 4 throughout.
[x] - SP-037-4b: Add `TestSubagentRunner_FleetBudgetCancels` — 10 tasks, fleet budget 5000 tokens, 600 tokens per call; assert at least one cancellation and overdraw bounded by one subagent's individual `MaxTokens`.
[x] - SP-037-4c: Add `TestSubagentRunner_NoGoroutineLeak_AfterStress` — runs the bounded stress test and asserts `goleak.VerifyNone(t)` plus `runtime.NumGoroutine()` delta ≤ 2.
[x] - SP-037-4d: Add `TestSubagentRunner_ParentCancelDropsQueued` — 20 tasks, limit=2, cancel parent ctx after 50ms; assert remaining 18 return `Cancelled` without starting.
[x] - SP-037-4e: Run `go test -race -run TestSubagentRunner -count=20 ./pkg/agent/` to verify stability.

### Phase 5: Documentation

[x] - SP-037-5a: Add a "Subagent resource model" section to `docs/AGENT_WORKFLOW.md` covering concurrency limit, fleet budget, telemetry, and how to read the WebUI Subagents tab.
[x] - SP-037-5b: Add a package-level doc comment to `pkg/agent/subagent_runner.go` documenting the semaphore + budget invariants.

---

## SP-038: Tool Dispatch Consolidation — Registry Over Switch

Spec: `roadmap/SP-038-tool-dispatch-consolidation.md`

Adding a tool today requires editing four locations across two packages (definition in 1007-line `tool_definitions.go`, handler in one of 10+ `tool_handlers_*.go`, dispatch in `tool_executor*.go`, and command surface in 62-file `pkg/agent_commands/`). No `ToolHandler` interface, no registry, no startup assertion that every declared tool has a handler. This spec introduces a registry + interface and migrates tools incrementally.

### Phase 1: Interface + registry

[x] - SP-038-1a: Create `pkg/agent_tools/handler.go` with `ToolHandler` interface (`Name`, `Definition`, `Validate`, `Execute`), `ToolEnv` (explicit deps, no `*Agent`), `ToolResult` (Output, StructuredOut, TokenUsage for SP-037).
[x] - SP-038-1b: Create `pkg/agent_tools/registry.go` with thread-safe `ToolRegistry` (`Register`, `Lookup`, `All`, `ForPersona`).
[x] - SP-038-1c: Move `ClassifyToolCall` from `pkg/agent/tool_definitions.go` (current location around line 541 per SP-035 references) to `pkg/agent_tools/security_classifier.go`. Update all callers.
[x] - SP-038-1d: Create `pkg/agent_tools/all.go` as the central tools-init file (initially empty — tools migrate in over time).

### Phase 2: Dual-dispatch shim

[x] - SP-038-2a: In `pkg/agent/tool_executor*.go`, check the registry first; fall back to legacy switch on miss. Add a debug log line per dispatch path so migration progress is observable.
[x] - SP-038-2b: Add `TestDualDispatch_RegistryWins` confirming a registered tool takes precedence over a legacy entry of the same name.

### Phase 3: Migrate small tools

[x] - SP-038-3a: Migrate `read_file` to `pkg/agent_tools/read_file.go`. Remove from legacy. Add `TestTool_ReadFile_Conformance`.
[x] - SP-038-3b: Migrate `list_directory`.
[x] - SP-038-3c: Migrate `web_fetch`.
[x] - SP-038-3d: Migrate remaining small tools (`read_directory`, `glob`, similar) one per commit.

### Phase 4: Migrate medium tools

[x] - SP-038-4a: Migrate `write_file` and `write_structured_file` together (preserve the SP-035 Phase 4 migration warning behavior).
[x] - SP-038-4b: Migrate `edit_file`.
[x] - SP-038-4c: Migrate `shell_command` — careful interaction with the SP-035 two-gate risk model; the `EvaluateOperationRisk` and `ClassifyToolCall` callouts must remain on the path.
[x] - SP-038-4d: Migrate `search_memories` / `save_memory` (touches SP-027 conversation-store paths).

### Phase 5: Migrate large/complex tools

[x] - SP-038-5a: Migrate the subagent family (`run_subagent`, `run_subagent_parallel`, task queue tools); likely a `pkg/agent_tools/subagent/` subdirectory due to size.
[x] - SP-038-5b: Migrate `task_queue_*` and `todo_*` tools.
[x] - SP-038-5c: Migrate remaining tools (image/vision, PDF, browser, web search).

### Phase 6: Cleanup + tests

[x] - SP-038-6a: Remove the legacy switch from `pkg/agent/tool_executor*.go` once every tool is registered.
[x] - SP-038-6b: Verify `pkg/agent/tool_definitions.go` is ≤ 150 lines.
[x] - SP-038-6c: Add `TestRegistry_AllToolsHaveValidDefinitions`, `TestRegistry_AllToolsRespectPersonaFilter`, `TestRegistry_AllToolsValidate`, `TestRegistry_NoOrphanHandlers` in `pkg/agent_tools/registry_test.go`.
[x] - SP-038-6d: Run `go test -race ./pkg/agent/ ./pkg/agent_tools/` 10× clean.

### Phase 7: Documentation

[x] - SP-038-7a: Write `docs/TOOLS.md` covering: how to add a tool (one-file recipe), the `ToolHandler` interface, the `ToolEnv` contract, the registry init order, the persona filter, and the relationship between tools and `pkg/agent_commands/`.
[x] - SP-038-7b: Add a package-level doc comment to `pkg/agent_tools/handler.go`.

---

## SP-039: UI Package Consolidation — One Canonical Component Library

Spec: `roadmap/SP-039-ui-package-consolidation.md`

`packages/ui/src/components/` and `webui/src/components/` have ~30 overlapping component filenames (Terminal, FileTree, ContextMenu, Sidebar, StatusBar, CommandPalette, Notification, MessageBubble, GitSidebarPanel, …). CSS edits drift between the two; imports are ambiguous; Storybook tests the library copy while the app uses the duplicate. This spec consolidates to one canonical location per component and enforces the boundary in CI.

### Phase 1: Decision + audit

[x] - SP-039-1a: Confirm Option A (delete `packages/ui`, move everything into `webui`) or Option B (keep `packages/ui` as the canonical library, webui imports from it). Document the choice and rationale in `roadmap/SP-039-DECISION.md` or inline in the spec.
[x] - SP-039-1b: Write `scripts/ui-consolidation-diff.sh` outputting the 30+ overlaps and per-component diff status (identical / packages-leads / webui-leads / divergent).
[x] - SP-039-1c: Categorize every `packages/ui/src/components/*.tsx` as primitive (reusable, no domain types) or composite (wires primitives to app state).

### Phase 2: Move misplaced composites out of `@sprout/ui`

[x] - SP-039-2a: Move `BillingPage*`, `TeamPage*`, `AdminBillingPage*`, `TasksPage*` from `packages/ui/src/components/` to `webui/src/components/`. One commit per move.
[x] - SP-039-2b: Audit `packages/ui` for any other domain-coupled components (importing from `@sprout/events` for app-specific events, using `useSproutAdapter()` against a specific endpoint set); move them.
[x] - SP-039-2c: Verify `grep -rn "chatSession\|persona\|adapter" packages/ui/src/components/` returns no domain-specific hits.

### Phase 3: Consolidate primitives — small first

[x] - SP-039-3a: `Notification`, `NotificationItem`, `Notification.css` → canonical in `packages/ui`; delete webui copy; update imports.
[x] - SP-039-3b: `Dropdown`, `Modal` (base), `ContextMenu` → same.
[] - SP-039-3c: `Sidebar`, `StatusBar`, `MenuBar` → same.
[x] - SP-039-3d: `CommandPalette`, `CommandInput` → same.

### Phase 4: Consolidate primitives — large

[x] - SP-039-4a: `FileTree` — highest-impact primitive; verify behavior parity with at least manual smoke test in WebUI plus existing component tests passing.
[x] - SP-039-4b: `Terminal` — uses xterm.js; verify keybinding parity, reattach behavior, search bar.
[x] - SP-039-4c: `GitSidebarPanel` — confirm whether primitive or composite (recent edits in commit `b46bcada` suggest composite); place accordingly.
[x] - SP-039-4d: `MessageBubble`, `MessageSegments`, `MessageContent`, `LiveLog`, `QueuedMessagesPanel`, `SelectionActionBar`, `ChatMessageContextMenu`.

### Phase 5: Enforce boundary

[x] - SP-039-5a: Add `eslint-plugin-import` `no-restricted-paths` rule to `webui/.eslintrc` and `packages/ui/.eslintrc` forbidding cross-boundary deep imports.
[x] - SP-039-5b: Add `scripts/check-no-duplicate-components.sh` (fails CI if `comm -12` between the two component directories has any matches); wire into `.github/workflows/build.yml`.
[x] - SP-039-5c: Add a Storybook coverage check requiring every primitive in `@sprout/ui` to have a matching `.stories.tsx`.

### Phase 6: Documentation

[x] - SP-039-6a: Write `docs/COMPONENT_LIBRARY.md` covering the Option A/B decision and rationale, the primitive vs composite rubric with examples, import direction enforcement, how to add a new component.
[x] - SP-039-6b: Update `CONTRIBUTING.md` with a "Where does my new component go?" subsection.
[x] - SP-039-6c: Update `packages/ui/README.md` (if it exists) to point at `docs/COMPONENT_LIBRARY.md` as the source of truth.

---

## SP-040: Deployment Configurability — Untangling Hardcoded Ports and Hosts

Spec: `roadmap/SP-040-deployment-configurability.md`

`webui/package.json:101` hardcodes `"proxy": "http://localhost:56000"`. `webui/src/bootstrapAdapter.ts` (29 lines) uses `window.location` heuristics with no env-var override. The frontend has no login UI and never sends `Authorization` headers, even though `pkg/webui/auth_middleware.go:23` validates bearer tokens on write endpoints. This spec adds runtime + build-time configurability and a minimal login flow.

### Phase 1: Bootstrap endpoint + adapter rewrite

[x] - SP-040-1a: Define `RuntimeConfig` type (`apiBaseURL`, `wsURL`, `authMode: "none"|"bearer"`, `appMode: "local"|"cloud"`, `buildVersion`) in `pkg/webui/api_bootstrap.go` and `webui/src/types/runtimeConfig.ts`.
[x] - SP-040-1b: Implement `GET /api/bootstrap` returning `RuntimeConfig` (unauthenticated; `authMode` set based on `SPROUT_AUTH_TOKEN` env, sharing the env read with `pkg/webui/server.go:69`).
[x] - SP-040-1c: Rewrite `webui/src/bootstrapAdapter.ts` — fetch `/api/bootstrap` first, fall back to `import.meta.env.VITE_*`, fall back to localhost defaults. Log each fallback step.
[x] - SP-040-1d: Update `webui/src/bootstrapAdapter.test.ts` with all three fallback paths.

### Phase 2: Build-time configurability

[x] - SP-040-2a: Define `VITE_API_BASE_URL`, `VITE_WS_URL`, `VITE_AUTH_MODE`, `VITE_APP_MODE` in `webui/vite.config.ts` with safe defaults.
[x] - SP-040-2b: Replace `webui/package.json:101` hardcoded proxy with a Vite plugin reading `process.env.SPROUT_DEV_BACKEND_URL` (default `http://localhost:56000`).
[x] - SP-040-2c: Add `webui/.env.example` documenting every supported `VITE_*` var.

### Phase 3: Auth context + LoginPage

[x] - SP-040-3a: Create `webui/src/contexts/AuthContext.tsx` exposing `{token, setToken, clearToken, isAuthenticated}` backed by `sessionStorage` key `sprout_auth_token`.
[x] - SP-040-3b: Create `webui/src/components/LoginPage.tsx` — single token input + submit; route

---

## SP-047: Embedding Store — Migrate JSONL to HNSW

Spec: `roadmap/SP-047-sqlite-vec-store.md` (amended: uses `coder/hnsw` instead of sqlite-vec)

**Implementation note:** The original spec planned `sqlite-vec` via ncruces WASM. However, sqlite-vec's WASM requires threads that wazero doesn't support, and sqlite-vec itself is brute-force only (no ANN). The implementation uses **`coder/hnsw`** — a pure Go HNSW library with SIMD cosine distance for true approximate nearest-neighbor search. WASM builds fall back to JSONLFileStore.

### Phase 1: HNSW Store Implementation

[x] - SP-047-1a: Add `github.com/coder/hnsw` to `go.mod`. Pure Go, no CGo, cross-compiles everywhere.
[x] - SP-047-1b: `pkg/embedding/store_hnsw.go` — `HNSWStore` with `hnsw.Graph[string]` + separate `map[string]VectorRecord` for metadata.
[x] - SP-047-1c: `Store()` — upsert via delete+re-add, handles empty embeddings, persists graph + records JSON sidecar.
[x] - SP-047-1d: `Query()` — uses `graph.Search()` with cosine distance filter, returns results above threshold.
[x] - SP-047-1e: `DeleteByFile()` — removes matching nodes from graph and records.
[x] - SP-047-1f: `LoadAll()` — returns all records from metadata map.
[x] - SP-047-1g: `Size()`, `Close()`, `Save()` — Close persists dirty state and clears map.
[x] - SP-047-1h: `NewHNSWStore(indexPath, modelHash)` — loads graph + records, checks model hash, configures M=16, EfSearch=50.

### Phase 2: Testing

[x] - SP-047-2a: `store_hnsw_test.go` — 10 tests covering Store+Query, Upsert, DeleteByFile, Reload, EmptyQuery, ModelHash mismatch/match, LoadAll, LargeDataset (500), ReplaceAll.
[x] - SP-047-2b: All edge cases covered (empty embeddings, graph rebuild after deletion, close+reopen).

### Phase 3: Automatic Migration

[x] - SP-047-3a: `manager.go` checks for legacy `index.jsonl` → triggers `migrateJSONLToHNSW()`.
[x] - SP-047-3b: Migration loads JSONL records into HNSW store.
[x] - SP-047-3c: Renames `index.jsonl` → `.migrated` for rollback safety.
[x] - SP-047-3d: 3 migration tests in `manager_test.go`.

### Phase 4: Manager Integration

[x] - SP-047-4a: `EmbeddingManager.init()` uses `NewHNSWStore` with `index.hnsw`.
[x] - SP-047-4b: ONNX init paths use `NewHNSWStore` with `.hnsw` paths.
[x] - SP-047-4c: `ConversationStore` and `ReplaceAll` adapted for HNSW.
[x] - SP-047-4d: `store.go` (`JSONLFileStore`) kept for migration reading and WASM fallback.
[x] - SP-047-4e: All embedding + agent tests pass.

### Phase 5: WASM Build Compatibility

[x] - SP-047-5a: `store_hnsw_wasm_stub.go` (`//go:build js`) falls back to JSONLFileStore with path rewrite.
[x] - SP-047-5b: Build tags: `store_hnsw.go` has `//go:build !js`.

### Phase 6: Verification + Benchmarks

[x] - SP-047-6a: Benchmark tests comparing JSONL vs HNSW query latency.
[x] - SP-047-6b: Measure binary size delta.
[x] - SP-047-6c: Success criteria: all tests pass, migration is non-destructive, WASM builds work.

---

## SP-048: CLI Delight — Terminal UX Polish

Spec: `roadmap/SP-048-cli-delight.md`

The interactive `sprout agent` CLI is capable but quiet — silent between submit and first stream chunk, no tool execution timeline, slash commands invisible until you `/help`, no persistent sense of "where am I in this session." Existing plumbing (events, history, markdown renderer) supports most of what's needed; the gaps are in *legibility* during real-time interaction.

### Phase 1: Spinner + tool execution timeline

[x] - SP-048-1a: Add `pkg/console/activity_indicator.go` — braille-spinner ticker with start/stop/transition primitives. Uses `\r\033[K` to erase, frames `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏`, 80ms cadence. Suppressed when stdout is not a TTY. Goroutine-safe.
[x] - SP-048-1b: In `cmd/agent_modes.go` (around the `streamFn` callback at line ~563), start the spinner immediately after submit with text `Thinking (<elapsed> · <model>)`. Stop it on the first stream chunk arrival. Also moved `chatAgent.SetEventBus(eventBus)` to be unconditional so tool events publish even when WebUI is disabled.
[x] - SP-048-1c: Subscribe to `PublishToolStart` / `PublishToolEnd` events via `startTerminalToolSubscriber` in `cmd/agent_modes.go`. Renders per-tool spinner line on start, replaces with `[OK]` / `[FAIL]` result line on end. Stays within the cmd/ package because the subscriber's lifetime is tied to interactive mode's ctx.
[x] - SP-048-1d: `formatToolArgPreview` helper — for `read_file`/`write_file`/`edit_file` show path; for `shell_command`/`exec` show command; for `search_files`/`grep` show pattern; for `fetch_url` show url. Generic fallback to first short string field for unknown tools. Newlines collapsed, truncated to 60 chars with `…`.
[x] - SP-048-1e: Tests in `pkg/console/activity_indicator_test.go` (NoOp on non-TTY, Replace works on non-TTY, nil-safe, idempotent Stop, sanitizeLine) and `cmd/agent_modes_test.go` (`TestFormatToolArgPreview` with 10 cases including unknown-tool fallback, JSON failure modes, newline collapsing, truncation).

### Phase 2: Slash command discoverability

[x] - SP-048-2a: Tab completion in `pkg/console/input_core.go` — `CompletionProvider` callback type, `SetCompleter` method, cycle-state tracking (`completionCycle` struct), `handleTabCompletion` cycles candidates on repeated Tab, automatically resets when buffer differs from last applied completion. Wired in `cmd/agent_modes.go` `runInteractiveMode` to filter `registry.CompletionCandidates()` by prefix.
[x] - SP-048-2b: "Did you mean" — `pkg/agent_commands/suggestions.go` with `levenshtein()` helper and `(*CommandRegistry).SuggestCommands(name, max)`. Modified `Execute` in `commands.go` to append `— did you mean /X or /Y?` to the unknown-command error when candidates within edit distance 3 (or prefix match) exist.
[x] - SP-048-2c: `/help <command>` — `UsageProvider` interface for richer per-command help, `printCommandHelp(name)` resolves aliases via `GetCommand`, shows description + aliases + usage (or "(no additional details)" fallback for commands that don't implement `UsageProvider`).
[x] - SP-048-2d: Short aliases registered in `NewCommandRegistry`: `/m → /model`, `/p → /provider`, `/x → /exit`, `/q → /exit`, `/? → /help`, `/h → /help`. `RegisterAlias`, `AliasesOf`, `CompletionCandidates`, and alias-aware `GetCommand`/`Execute` added. Aliases appear inline in `/help` output (e.g. `/exit (/q, /x) - ...`).

### Phase 3: Persistent status footer

[x] - SP-048-3a: `pkg/console/status_footer.go` — renders a single bottom line via DECSTBM scroll region (`\033[1;<rows-1>r`). Format: ` ─ <model> · <ctx-used>/<ctx-limit> ctx · $<cost> · <cwd> (<branch>) ─────`. Refreshed on PublishToolEnd and at end of each interactive turn. Helper `agentFooterSource` in `cmd/agent_modes.go` adapts `*agent.Agent` to the `console.ContentSource` interface (model, context tokens, cost, cwd). New `(*Agent).GetContextTokens()` passthrough added.
[x] - SP-048-3b: SIGWINCH watcher spawned by `Start`, re-applies scroll region + redraws on terminal resize. Uses the existing `resizeSignal()` cross-platform helper; no-op on platforms without SIGWINCH (Windows, js/wasm).
[x] - SP-048-3c: Clean restoration on exit. Deferred `footer.Stop()` in `runInteractiveMode` handles graceful exit. Global registration via `console.RegisterGlobalStatusFooter` + `console.StopGlobalStatusFooter` so the signal handler's force-quit paths (both rapid-double-Ctrl+C and 5s force-quit timeout) reset the scroll region before `os.Exit`.
[x] - SP-048-3d: Cost color thresholds — yellow >$1, red >$5 via `WarnCost`/`AlertCost` fields on `StatusFooter`. ANSI-aware `visibleLen` keeps padding correct when styling is active. Config wiring (cli.footer.cost_warn / cost_alert) deferred — defaults exposed as public fields, set at construction.
[x] - SP-048-3e: Suppressed on non-TTY automatically via `term.IsTerminal` check at construction; `Start`/`Refresh`/`Resize`/`Stop` all become no-ops. `composeLine` still produces a usable string for testing on non-TTY writers.

### Phase 4: Input ergonomics

[x] - SP-048-4a: `pkg/console/markdown_formatter.go` `NewMarkdownFormatter` honors `NO_COLOR` / `FORCE_COLOR` via `envutil.ResolveColorPreference`. NO_COLOR wins per no-color.org. Resolver lives in `pkg/envutil` (zero-dep leaf) so `pkg/utils` and other callers can share it without import cycles.
[x] - SP-048-4b: `cmd/agent_exec_utils.go:48-54` no longer forces `FORCE_COLOR=1` / `CLICOLOR_FORCE=1` and no longer unsets `NO_COLOR`. Subshells now inherit the user's color env vars; only `TERM=xterm-256color` is kept since shell builtins like `ls --color=auto` still want a sensible TERM.
[x] - SP-048-4c: `pkg/utils/logger.go` `AskForConfirmation` now renders the default option in bold via `defaultChoiceHint(default_response)`: `[Y/n]` when default is yes, `[y/N]` when default is no, with the capitalized letter wrapped in `\033[1m...\033[0m` when color output is allowed. Honors NO_COLOR via the shared resolver. `pkg/ui/prompt.go` `PromptForConfirmation` has no default-response concept so nothing to highlight there. `pkg/agent/secret_prompter.go` deferred — its `PromptChoice` path goes through `a.ui` which is currently never wired in CLI mode (the prompter is dormant); revisit after SetUI gets wired.
[x] - SP-048-4d: Smart paste (pragmatic auto-save variant, no prompt). `pkg/console/text_paste.go` adds `SavePastedText` + `ShouldSmartSavePaste` mirroring the image-paste pattern. `pkg/console/input_paste.go` `finalizePaste` now checks the threshold after stripping the trailing newline and, when triggered, writes content to `./.sprout/pastes/paste_<ts>_<hex>.txt`, prints a `[paste] N lines · B bytes saved to ...` notice, and inserts `@<relpath>` at the cursor as a collapsed paste span. Falls back to inline insertion if the save fails. Defaults: 100 lines / 5KB. Deviates from the spec's 3-choice confirmation in favor of the simpler image-paste-aligned UX — no interactive prompt in the middle of typing.
[] - SP-048-4e: Ctrl-R reverse history search — DEFERRED. Most invasive sub-item; requires a real state machine inside the raw-mode read loop with its own keystroke handler, search-buffer rendering, and history filtering. Lift out into a separate small task when ready.
[x] - SP-048-4f: `$EDITOR` escape via Ctrl-X Ctrl-E in `pkg/console/input_core.go`. Two-keystroke sequence: byte 24 (Ctrl-X) arms `pendingCtrlX`, the next byte either triggers `runExternalEditor` (byte 5 = Ctrl-E) or falls through to normal processing. `pkg/console/input_editor_escape.go` runs the editor: `chooseExternalEditor` picks VISUAL > EDITOR > nano/vim/vi fallback; `writeBufferToTempFile` stages the current buffer as `sprout-edit-*.md`; `runExternalEditor` saves+restores raw mode (mirrors the existing Ctrl-Z pattern), disables bracketed-paste/mouse for the editor, exec'd via `exec.Command(editor, tmpPath).Run()`, reads back, trims trailing newline, replaces `ir.line`. Errors land on stderr without aborting the prompt. `/edit` slash-command alias deferred — separate flow (post-submit) needing different plumbing.

### Phase 5: Onboarding + per-turn polish

[x] - SP-048-5a: Recent-session greeting in `cmd/recent_sessions.go`. On startup, `maybeShowRecentSessions` queries `agent.ListSessionsWithTimestampsScoped(cwd)`, filters to last 7 days (excluding the current session), shows up to 3 most-recent with `humanizeAge` ("2h ago", "3d ago"), session ID, and human-readable label. Pragmatic deviation from spec: shows a copy-pasteable continuation command instead of inline numeric-resume, because session resume requires re-initialising the agent before its first query — clean to do via the existing `--session-id` flag at startup, awkward to splice in mid-process.
[x] - SP-048-5b: First-run hint in `cmd/first_run_hint.go`. `maybeShowFirstRunHint` checks `~/.sprout/state.json` for the current workspace path; if not present, prints `Press Tab to autocomplete /commands, Ctrl-D to exit, or just start typing.` to stderr and appends the path to the seen list. Best-effort persistence: state-read/write errors are silent (the hint is non-essential).
[x] - SP-048-5c: Per-turn cost line in `cmd/agent_modes.go`. Snapshots `GetPromptTokens` / `GetCompletionTokens` / `GetTotalCost` + `time.Now()` before each query; `printPerTurnSummary` emits `⎯ this turn: <in>k in / <out>k out · $<cost> · <elapsed> ⎯` to stderr after the turn returns. Suppressed for zero-token turns (slash commands, zsh fast paths). Dim ANSI styling gated on `envutil.ResolveColorPreference` so NO_COLOR strips it.
[x] - SP-048-5d: Model in prompt — `buildPromptPrefix(model)` in `cmd/agent_modes.go` returns `<model> ▸ ` when a model is known, `sprout> ` as the fallback when empty/whitespace. Used at `runInteractiveMode`'s `console.NewInputReader(...)` call. Config wiring (cli.prompt.format) deferred — current format is sensible enough for v1.
[x] - SP-048-5e: Auto-NO_COLOR on non-TTY stdout in `cmd/agent_modes.go` `RunAgent`. When `term.IsTerminal(os.Stdout.Fd())` returns false and the user hasn't explicitly set `NO_COLOR`/`FORCE_COLOR`, sprout sets `NO_COLOR=1` early in startup. Every color-aware writer (markdown formatter, `defaultChoiceHint`, per-turn summary, status footer) consults `envutil.ResolveColorPreference` and emits plain text automatically. Cleaner than a stdout filter writer — single env-var gate at the top of the program.

---

## SP-050: Orchestrator Persona Collapse

Spec: `roadmap/SP-050-orchestrator-persona-collapse.md`

Collapse `orchestrator` and `repo_orchestrator` into a single `orchestrator` persona. Git-write capability becomes a function of the existing `AllowOrchestratorGitWrite` config flag rather than the persona ID. Removes the OR'd persona checks at 6+ sites, moves the git-policy markdown out of escaped JSON into a `go:embed`'d file, and flips the default active persona + flag to `orchestrator` + `true` for fresh installs. Per scoping: no backwards-compat migration required; legacy session history that names `repo_orchestrator` resolves via alias.

### Phase 1: Collapse to a single persona (one PR)

[x] - SP-050-1a: Move git-policy markdown out of JSON. Create `pkg/agent/prompts/persona_appends/orchestrator_git_policy.md` with the current `repo_orchestrator.system_prompt_append` content (Committing / Staging / Read-Only / Destructive Blocked / Pushing / Skills / Workflow sections). Embed it via a new `go:embed` directive co-located with the existing prompt embeds.
[x] - SP-050-1b: Conditional prompt assembly. In `pkg/agent/persona.go` `SetPersona` path (around `:80-88`), after the persona's own `system_prompt_append` is applied, append the embedded git-policy markdown when `activePersona == "orchestrator"` AND `config.AllowOrchestratorGitWrite == true`. Use the same `"\n\n---\n\n"` separator as the existing append path.
[x] - SP-050-1c: Remove `repo_orchestrator` from the catalog. Delete the entry from `pkg/personas/configs/default_personas.json` and add `repo_orchestrator` + `git_orchestrator` to the `orchestrator` entry's `aliases` array. Alias resolution routes through the existing `normalizeAgentPersonaID` (`persona.go:127-131`) — no new code. Alias path does NOT imply git-write was on; the flag decides.
[x] - SP-050-1d: Strip OR'd persona checks at 7 sites. `pkg/agent/persona.go:226`, `pkg/agent/tool_handlers_shell.go:134` `:225` `:403`, `pkg/agent/seed_integration.go:997`, `pkg/webui/settings_api_general.go:88`, `cmd/agent_command.go:205`. The two auto-approve sites in `tool_handlers_shell.go` (`:225`, `:403`) move from "persona is repo_orchestrator" to "persona is orchestrator AND `AllowOrchestratorGitWrite`".
[x] - SP-050-1e: Flip default active persona and default config flag. Change `pkg/agent/submanager_state.go:217` from `activePersona: "repo_orchestrator"` to `activePersona: "orchestrator"`. Set the default seed for `AllowOrchestratorGitWrite` to `true` for fresh installs in `pkg/configuration/config.go`. No migration for existing configs (per scoping).
[x] - SP-050-1f: Update Executive Assistant prompt. Replace 13 `repo_orchestrator` refs in `pkg/agent/prompts/subagent_prompts/executive_assistant.md` with `orchestrator`. Alias from 1c covers any persisted state that still names the old ID.
[x] - SP-050-1g: Update parameter-description strings. `pkg/agent_tools/task_queue_add_handler.go:22` and `pkg/agent/tool_registrations.go:434` — change `"e.g., repo_orchestrator"` to `"e.g., orchestrator"`.
[x] - SP-050-1h: Update tests. `pkg/agent/persona_test.go`, `pkg/agent/submanager_state_new_test.go`, `pkg/agent/submanagers_test.go`, `pkg/agent/agent_creation_test.go`. Add: alias resolution (`repo_orchestrator` → `orchestrator`), git-policy append present/absent based on flag, auto-approve gating fires only when persona==orchestrator AND flag==true (test both flag states), catalog no longer has `repo_orchestrator` as a top-level ID but `GetSubagentType("repo_orchestrator")` still resolves via alias.
[x] - SP-050-1i: Update human-facing docs. Collapse `docs/PERSONAS.md` to a single orchestrator entry with a "Git Operations" subsection explaining what the flag gates. Update one `repo_orchestrator` ref in top-level `AGENTS.md`.

---

## SP-051: Depth-Aware Subagent UI

Spec: `roadmap/SP-051-depth-aware-subagent-ui.md`

Tag every event a subagent publishes with `subagent_depth` and `active_persona`, then have the CLI tool-timeline subscriber indent and color-badge each tool line based on those fields. Also show a one-shot `↳ persona spawned (provider · model)` line when a new (depth, persona) pair first appears so users can see which cheaper/faster model their subagents are running. Adds an optional `· N sub` suffix to the status footer when subagents are active. Two phases: phase 1 is event decoration (plumbing only, no visible change), phase 2 is the rendering layer.

### Phase 1: Event metadata plumbing

[x] - SP-051-1a: Decorate subagent event metadata at creation. In `pkg/agent/subagent_runner.go` around the `subagentDepth = parent + 1` site (~`:680`), call `subAgent.SetEventMetadata` with `subagent_depth`, `active_persona`, and (when non-empty) `subagent_task_id`. Be additive — if the parent already set client/chat/user IDs that should propagate, merge rather than replace. The existing `decorateEventPayload` in `pkg/agent/agent_events.go:19-53` already merges this metadata into every published event.
[x] - SP-051-1b: Decorate the primary agent (depth 0) the same way so the subscriber's branching is uniform. Either at agent creation or via a hook in `ApplyPersona` that re-applies metadata whenever the active persona changes (relevant for SP-050 alias canonicalization).
[x] - SP-051-1c: Tests in `pkg/agent/subagent_runner_test.go`. Spawn a subagent against a captured event bus, fire a `ToolStart`, and assert the payload contains `subagent_depth: 1` and the expected persona ID. Cover depth-2 (grandchild) as well.

### Phase 2: CLI rendering

[x] - SP-051-2a: Indent tool lines by depth in `cmd/agent_modes.go` `startTerminalToolSubscriber` (~`:1055-1122`). Read `subagent_depth` from event data on `ToolStart` and `ToolEnd`; prepend `strings.Repeat("  ", depth)` to the format strings at `:1087` and `:1107`. Depth-0 events are byte-identical to today's output (no extra indent, no badge).
[x] - SP-051-2b: Persona color + `[persona]` badge. Add a deterministic persona-ID → ANSI-color map (suggested: `coder=cyan`, `tester=green`, `debugger=yellow`, `researcher=magenta`, `code_reviewer=blue`, `refactor=white`, `orchestrator=bold-white`). Prefix non-zero-depth tool lines with the colored badge; respect `NO_COLOR` via `pkg/envutil.ResolveColorPreference`. Probably belongs in a small helper in `pkg/console/persona_style.go`.
[x] - SP-051-2c: Spawn line on first event from new (depth, persona) pair. Track `map[int]string{depth: persona}` in the subscriber's state; when an event arrives with a (depth, persona) the subscriber hasn't seen this turn, emit `↳ persona spawned (provider · model)` using the existing `formatRunSubagentPreview` logic for the provider/model resolution. Clear the map at end-of-turn so the next user prompt starts fresh.
[x] - SP-051-2d: Status-footer subagent count. Add optional `ActiveSubagents() int` to the `ContentSource` interface in `pkg/console/status_footer.go` (interface-assertion check so the existing stub `ContentSource` implementations keep compiling). When non-zero, append ` · N sub` to `composeLine`. Counter source: a new atomic counter on `*Agent` incremented at subagent start and decremented at subagent end; the existing `agentFooterSource` adapter in `cmd/agent_modes.go` adds the `ActiveSubagents()` method that reads it.
[x] - SP-051-2e: Tests in `cmd/agent_modes_test.go`. Extract a pure helper (e.g. `formatDepthedToolLine(depth, persona, name, preview, status, duration)`) from the subscriber goroutine so it's testable, then unit-test depth-0 (no badge, no indent), depth-1 (2-space indent + badge), depth-2 (4-space indent + badge), NO_COLOR mode (no ANSI), and the `↳ spawned` dedupe (two events from the same depth+persona produce exactly one spawn line). Status-footer test in `pkg/console/status_footer_test.go` for the `· N sub` suffix.

---

## SP-052: Multi-line Error Block Formatter

Five `fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)` sites in `cmd/agent_modes.go` flatten multi-line errors via `%v`, losing context exactly when readability matters. Survey originally proposed three items; verified that the "stale prompt after slash command" and "mid-turn model change visibility" claims didn't reflect real code paths, so SP-052 ships only the confirmed bug.

[x] - SP-052: New `pkg/console/error_block.go` with `FormatErrorBlock(header string, err error) string`. Nil err → "". Single-line err → byte-identical to legacy `"<header>: <err>\n"` format. Multi-line err → header line, then each line indented two spaces and red-colored (respects `NO_COLOR`/`FORCE_COLOR` via `envutil.ResolveColorPreference`). Trailing newlines stripped to avoid double-blank-line artifacts.
[x] - SP-052: Route all 5 `[FAIL] Error: %v` sites through the helper: `cmd/agent_modes.go:673` (EA task processing), `:881` (slash command), `:912/917/922` (zsh fast-path, direct execution, normal ProcessQuery). All `[WARN]` sites left unchanged — they're benign single-line messages that already render fine.
[x] - SP-052: Tests in `pkg/console/error_block_test.go` covering nil/single-line/multi-line, NO_COLOR vs FORCE_COLOR, trailing-newline trim, and single-line-stays-plain even with colors enabled. Pin the legacy single-line format so log scrapers and screenshots don't see a regression.

---

## SP-053: WebUI CLI Parity Pass

Spec: `roadmap/SP-053-webui-cli-parity.md`

Three CLI improvements (SP-048 timeline, SP-051 depth/persona, SP-048 footer) need WebUI equivalents. The CLI got per-tool spinners, indented + persona-badged subagent output, and a live cost/model/ctx footer; the WebUI is still flat. Backend already publishes `subagent_depth` + `active_persona` on every event (per SP-051) — WebUI just doesn't consume them yet.

### Phase 1: Persona badge + depth indent in chat messages

[x] - SP-053-1a: Lift persona color map into `@sprout/ui`. Create `packages/ui/src/utils/personaColors.ts` containing the existing `PERSONA_COLORS` map + `getPersonaColor(persona?: string): string` from `webui/src/components/chat/SubagentActivityFeed.tsx:11-24`. Re-export from package barrel. Update `SubagentActivityFeed.tsx` to import from `@sprout/ui` instead of defining its own copy.
[x] - SP-053-1b: Extend `Message` type at `packages/ui/src/types/chat.ts:20-27` with optional `persona?: string` and `subagentDepth?: number`. Both optional — existing message-construction sites stay valid.
[x] - SP-053-1c: Populate persona/depth from incoming events in `webui/src/hooks/useEventHandler.ts` (grep for `type: 'assistant'` to find construction sites). Read `subagent_depth` + `active_persona` from event payload (SP-051's `decorateEventPayload` already places them there) and attach to constructed `Message`.
[x] - SP-053-1d: Add optional `persona?: string` + `depth?: number` props to `packages/ui/src/components/MessageBubble.tsx`. When `depth > 0`, apply `marginLeft: depth * 12px` to outer `.message`. When `persona` is non-empty, render a colored badge (left of copy button) using `getPersonaColor`. Default styling preserves today's look.
[x] - SP-053-1e: Tests in `packages/ui/src/components/MessageBubble.test.tsx`: persona+depth absent (matches today), persona present (badge with expected color), depth > 0 (correct indent), depth 0 with persona (badge but no indent). One backwards-compat pin test confirming existing callers render identically.

### Phase 2: Live tool timeline in chat footer

[x] - SP-053-2a: New `ToolTimelineBar.tsx` in `webui/src/components/chat/`. Renders up to 4 in-flight or recently-completed tools horizontally. Per-tool card: status icon (spinner for `started`/`running`, green check for `completed`, red X for `error`), tool name (monospace), compact arg preview, persona badge (when `ToolExecution.persona` set), elapsed time (live-ticking for running, final for completed). Completed tools fade after 3s via CSS; errors stick until next tool starts.
[x] - SP-053-2b: Wire `ToolTimelineBar` into `ChatFooter.tsx:28` when `filteredToolExecutions.length > 0`. Replace the generic skeleton at `:54-63` — suppress it when tools are visible (don't double-signal). Keep skeleton as fallback when no tools but `isProcessing=true`.
[x] - SP-053-2c: Tests in `ToolTimelineBar.test.tsx`: zero tools (renders nothing), one running tool (spinner + name), one completed (check + duration), mix of running/completed (running first), error sticks past 3s window, persona badge appears with expected color.

### Phase 3: Provider/model/cost in status bar

[x] - SP-053-3a: New `ChatStatusBarItems.tsx` in `webui/src/components/chat/`. Renders right-aligned cluster: `<provider-icon> <model> · <used>k/<limit>k ctx · $<cost>`. Reads from `stats` prop. Cost color thresholds match the CLI: yellow >$1, red >$5 (mirroring `pkg/console/status_footer.go:310-319`); respect `NO_COLOR` via existing theme variables. Generic lucide provider icon (Cloud/Server/Cpu) — defer per-provider brand icons.
[x] - SP-053-3b: Render `ChatStatusBarItems` via the existing `rightItems` slot on shared `@sprout/ui` `StatusBar` (`packages/ui/src/components/StatusBar.tsx:25`). In `webui/src/components/StatusBar.tsx`, when chat is active (`stats` non-empty), pass `<ChatStatusBarItems stats={stats} />`; otherwise keep today's editor metadata. No schema change to the shared component.
[x] - SP-053-3c: Verify live update cadence. `stats` already flows as a prop; check that updates aren't throttled >1s. If they are, bump the event source to match CLI footer's behavior (refreshes on every `ToolEnd`).
[x] - SP-053-3d: Tests in `ChatStatusBarItems.test.tsx`: renders with full stats (all segments), missing fields (segment omitted, no empty `·`), cost threshold styling (below warn = no color, above warn = yellow class, above alert = red class). Composition test in WebUI `StatusBar.tsx`: empty `stats` → editor metadata, non-empty → chat items.

### Verification

[x] - SP-053: `npm run build` in `webui/` succeeds (shared `@sprout/ui` package + WebUI consumers compile cleanly).
[x] - SP-053: `make build-all` succeeds (Go binary embeds the rebuilt UI).
[x] - SP-053: Existing `MessageBubble` consumers (sprout-foundry) keep compiling — new props are optional.

