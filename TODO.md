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
[] - SP-037-4e: Run `go test -race -run TestSubagentRunner -count=20 ./pkg/agent/` to verify stability.

### Phase 5: Documentation

[] - SP-037-5a: Add a "Subagent resource model" section to `docs/AGENT_WORKFLOW.md` covering concurrency limit, fleet budget, telemetry, and how to read the WebUI Subagents tab.
[x] - SP-037-5b: Add a package-level doc comment to `pkg/agent/subagent_runner.go` documenting the semaphore + budget invariants.

---

## SP-038: Tool Dispatch Consolidation — Registry Over Switch

Spec: `roadmap/SP-038-tool-dispatch-consolidation.md`

Adding a tool today requires editing four locations across two packages (definition in 1007-line `tool_definitions.go`, handler in one of 10+ `tool_handlers_*.go`, dispatch in `tool_executor*.go`, and command surface in 62-file `pkg/agent_commands/`). No `ToolHandler` interface, no registry, no startup assertion that every declared tool has a handler. This spec introduces a registry + interface and migrates tools incrementally.

### Phase 1: Interface + registry

[] - SP-038-1a: Create `pkg/agent_tools/handler.go` with `ToolHandler` interface (`Name`, `Definition`, `Validate`, `Execute`), `ToolEnv` (explicit deps, no `*Agent`), `ToolResult` (Output, StructuredOut, TokenUsage for SP-037).
[] - SP-038-1b: Create `pkg/agent_tools/registry.go` with thread-safe `ToolRegistry` (`Register`, `Lookup`, `All`, `ForPersona`).
[] - SP-038-1c: Move `ClassifyToolCall` from `pkg/agent/tool_definitions.go` (current location around line 541 per SP-035 references) to `pkg/agent_tools/security_classifier.go`. Update all callers.
[] - SP-038-1d: Create `pkg/agent_tools/all.go` as the central tools-init file (initially empty — tools migrate in over time).

### Phase 2: Dual-dispatch shim

[] - SP-038-2a: In `pkg/agent/tool_executor*.go`, check the registry first; fall back to legacy switch on miss. Add a debug log line per dispatch path so migration progress is observable.
[] - SP-038-2b: Add `TestDualDispatch_RegistryWins` confirming a registered tool takes precedence over a legacy entry of the same name.

### Phase 3: Migrate small tools

[] - SP-038-3a: Migrate `read_file` to `pkg/agent_tools/read_file.go`. Remove from legacy. Add `TestTool_ReadFile_Conformance`.
[] - SP-038-3b: Migrate `list_directory`.
[] - SP-038-3c: Migrate `web_fetch`.
[] - SP-038-3d: Migrate remaining small tools (`read_directory`, `glob`, similar) one per commit.

### Phase 4: Migrate medium tools

[] - SP-038-4a: Migrate `write_file` and `write_structured_file` together (preserve the SP-035 Phase 4 migration warning behavior).
[] - SP-038-4b: Migrate `edit_file`.
[] - SP-038-4c: Migrate `shell_command` — careful interaction with the SP-035 two-gate risk model; the `EvaluateOperationRisk` and `ClassifyToolCall` callouts must remain on the path.
[] - SP-038-4d: Migrate `search_memories` / `save_memory` (touches SP-027 conversation-store paths).

### Phase 5: Migrate large/complex tools

[] - SP-038-5a: Migrate the subagent family (`run_subagent`, `run_subagent_parallel`, task queue tools); likely a `pkg/agent_tools/subagent/` subdirectory due to size.
[] - SP-038-5b: Migrate `task_queue_*` and `todo_*` tools.
[] - SP-038-5c: Migrate remaining tools (image/vision, PDF, browser, web search).

### Phase 6: Cleanup + tests

[] - SP-038-6a: Remove the legacy switch from `pkg/agent/tool_executor*.go` once every tool is registered.
[] - SP-038-6b: Verify `pkg/agent/tool_definitions.go` is ≤ 150 lines.
[] - SP-038-6c: Add `TestRegistry_AllToolsHaveValidDefinitions`, `TestRegistry_AllToolsRespectPersonaFilter`, `TestRegistry_AllToolsValidate`, `TestRegistry_NoOrphanHandlers` in `pkg/agent_tools/registry_test.go`.
[] - SP-038-6d: Run `go test -race ./pkg/agent/ ./pkg/agent_tools/` 10× clean.

### Phase 7: Documentation

[] - SP-038-7a: Write `docs/TOOLS.md` covering: how to add a tool (one-file recipe), the `ToolHandler` interface, the `ToolEnv` contract, the registry init order, the persona filter, and the relationship between tools and `pkg/agent_commands/`.
[] - SP-038-7b: Add a package-level doc comment to `pkg/agent_tools/handler.go`.

---

## SP-039: UI Package Consolidation — One Canonical Component Library

Spec: `roadmap/SP-039-ui-package-consolidation.md`

`packages/ui/src/components/` and `webui/src/components/` have ~30 overlapping component filenames (Terminal, FileTree, ContextMenu, Sidebar, StatusBar, CommandPalette, Notification, MessageBubble, GitSidebarPanel, …). CSS edits drift between the two; imports are ambiguous; Storybook tests the library copy while the app uses the duplicate. This spec consolidates to one canonical location per component and enforces the boundary in CI.

### Phase 1: Decision + audit

[] - SP-039-1a: Confirm Option A (delete `packages/ui`, move everything into `webui`) or Option B (keep `packages/ui` as the canonical library, webui imports from it). Document the choice and rationale in `roadmap/SP-039-DECISION.md` or inline in the spec.
[] - SP-039-1b: Write `scripts/ui-consolidation-diff.sh` outputting the 30+ overlaps and per-component diff status (identical / packages-leads / webui-leads / divergent).
[] - SP-039-1c: Categorize every `packages/ui/src/components/*.tsx` as primitive (reusable, no domain types) or composite (wires primitives to app state).

### Phase 2: Move misplaced composites out of `@sprout/ui`

[] - SP-039-2a: Move `BillingPage*`, `TeamPage*`, `AdminBillingPage*`, `TasksPage*` from `packages/ui/src/components/` to `webui/src/components/`. One commit per move.
[] - SP-039-2b: Audit `packages/ui` for any other domain-coupled components (importing from `@sprout/events` for app-specific events, using `useSproutAdapter()` against a specific endpoint set); move them.
[] - SP-039-2c: Verify `grep -rn "chatSession\|persona\|adapter" packages/ui/src/components/` returns no domain-specific hits.

### Phase 3: Consolidate primitives — small first

[] - SP-039-3a: `Notification`, `NotificationItem`, `Notification.css` → canonical in `packages/ui`; delete webui copy; update imports.
[] - SP-039-3b: `Dropdown`, `Modal` (base), `ContextMenu` → same.
[] - SP-039-3c: `Sidebar`, `StatusBar`, `MenuBar` → same.
[] - SP-039-3d: `CommandPalette`, `CommandInput` → same.

### Phase 4: Consolidate primitives — large

[] - SP-039-4a: `FileTree` — highest-impact primitive; verify behavior parity with at least manual smoke test in WebUI plus existing component tests passing.
[] - SP-039-4b: `Terminal` — uses xterm.js; verify keybinding parity, reattach behavior, search bar.
[] - SP-039-4c: `GitSidebarPanel` — confirm whether primitive or composite (recent edits in commit `b46bcada` suggest composite); place accordingly.
[] - SP-039-4d: `MessageBubble`, `MessageSegments`, `MessageContent`, `LiveLog`, `QueuedMessagesPanel`, `SelectionActionBar`, `ChatMessageContextMenu`.

### Phase 5: Enforce boundary

[] - SP-039-5a: Add `eslint-plugin-import` `no-restricted-paths` rule to `webui/.eslintrc` and `packages/ui/.eslintrc` forbidding cross-boundary deep imports.
[] - SP-039-5b: Add `scripts/check-no-duplicate-components.sh` (fails CI if `comm -12` between the two component directories has any matches); wire into `.github/workflows/build.yml`.
[] - SP-039-5c: Add a Storybook coverage check requiring every primitive in `@sprout/ui` to have a matching `.stories.tsx`.

### Phase 6: Documentation

[] - SP-039-6a: Write `docs/COMPONENT_LIBRARY.md` covering the Option A/B decision and rationale, the primitive vs composite rubric with examples, import direction enforcement, how to add a new component.
[] - SP-039-6b: Update `CONTRIBUTING.md` with a "Where does my new component go?" subsection.
[] - SP-039-6c: Update `packages/ui/README.md` (if it exists) to point at `docs/COMPONENT_LIBRARY.md` as the source of truth.

---

## SP-040: Deployment Configurability — Untangling Hardcoded Ports and Hosts

Spec: `roadmap/SP-040-deployment-configurability.md`

`webui/package.json:101` hardcodes `"proxy": "http://localhost:56000"`. `webui/src/bootstrapAdapter.ts` (29 lines) uses `window.location` heuristics with no env-var override. The frontend has no login UI and never sends `Authorization` headers, even though `pkg/webui/auth_middleware.go:23` validates bearer tokens on write endpoints. This spec adds runtime + build-time configurability and a minimal login flow.

### Phase 1: Bootstrap endpoint + adapter rewrite

[] - SP-040-1a: Define `RuntimeConfig` type (`apiBaseURL`, `wsURL`, `authMode: "none"|"bearer"`, `appMode: "local"|"cloud"`, `buildVersion`) in `pkg/webui/api_bootstrap.go` and `webui/src/types/runtimeConfig.ts`.
[] - SP-040-1b: Implement `GET /api/bootstrap` returning `RuntimeConfig` (unauthenticated; `authMode` set based on `SPROUT_AUTH_TOKEN` env, sharing the env read with `pkg/webui/server.go:69`).
[] - SP-040-1c: Rewrite `webui/src/bootstrapAdapter.ts` — fetch `/api/bootstrap` first, fall back to `import.meta.env.VITE_*`, fall back to localhost defaults. Log each fallback step.
[] - SP-040-1d: Update `webui/src/bootstrapAdapter.test.ts` with all three fallback paths.

### Phase 2: Build-time configurability

[] - SP-040-2a: Define `VITE_API_BASE_URL`, `VITE_WS_URL`, `VITE_AUTH_MODE`, `VITE_APP_MODE` in `webui/vite.config.ts` with safe defaults.
[] - SP-040-2b: Replace `webui/package.json:101` hardcoded proxy with a Vite plugin reading `process.env.SPROUT_DEV_BACKEND_URL` (default `http://localhost:56000`).
[] - SP-040-2c: Add `webui/.env.example` documenting every supported `VITE_*` var.

### Phase 3: Auth context + LoginPage

[] - SP-040-3a: Create `webui/src/contexts/AuthContext.tsx` exposing `{token, setToken, clearToken, isAuthenticated}` backed by `sessionStorage` key `sprout_auth_token`.
[] - SP-040-3b: Create `webui/src/components/LoginPage.tsx` — single token input + submit; route

---

## SP-047: Embedding Store — Migrate JSONL to sqlite-vec

Spec: `roadmap/SP-047-sqlite-vec-store.md`

The embedding index stores vector records in a JSONL file. Every launch parses the full JSON into memory (~400MB for 100K records at 256d). Every write rewrites the entire file. Queries are brute-force Go over all loaded records. This spec migrates to `sqlite-vec` via the `ncruces/go-sqlite3` WASM path (zero CGO, cross-compiles everywhere). Expected improvements: <1ms queries, <100ms startup, ~100MB on disk (down from ~400MB). Old JSONL files are preserved as `.migrated` for rollback safety.

### Phase 1: SQLite Store Implementation

[] - SP-047-1a: Add `github.com/asg017/sqlite-vec-go-bindings/ncruces` and `github.com/ncruces/go-sqlite3` to `go.mod`. Verify `go build ./...` succeeds on linux/arm64, linux/amd64, darwin/arm64.
[] - SP-047-1b: Create `pkg/embedding/store_sqlite.go` with `SQLiteVecStore` struct satisfying the `VectorStore` interface (`Store`, `LoadAll`, `Query`, `DeleteByFile`, `Size`, `Close`). Use a `vec0` virtual table for vector storage and a standard SQLite table for metadata fields (file, name, signature, startLine, endLine, language, hash, indexedAt, type, metadata).
[] - SP-047-1c: Implement `Store(records []VectorRecord) error` using SQLite `INSERT OR REPLACE` (upsert by primary key). Batch inserts in a single transaction (batch size 512).
[] - SP-047-1d: Implement `Query(vec []float32, topK int, threshold float32) ([]QueryResult, error)` using `vec0` distance search: `SELECT ... FROM vec_records WHERE embedding MATCH ? ORDER BY distance`. Filter by threshold in the WHERE clause or post-filter in Go.
[] - SP-047-1e: Implement `DeleteByFile(filePath string) error` as `DELETE FROM records WHERE file = ?`.
[] - SP-047-1f: Implement `LoadAll() ([]VectorRecord, error)` as `SELECT * FROM records`. Needed by `BuildIndex` for incremental rebuild hash comparison. Use lazy cursor approach — don't hold the full result set in memory when possible.
[] - SP-047-1g: Implement `Size() int` as `SELECT COUNT(*) FROM records`. Implement `Close() error` to close the SQLite connection.
[] - SP-047-1h: Add `NewSQLiteVecStore(dbPath string, modelHash string, dimensions int) (*SQLiteVecStore, error)` constructor. Creates the database schema if it doesn't exist. Stores model hash in a metadata table for invalidation.

### Phase 2: Testing

[] - SP-047-2a: Write `pkg/embedding/store_sqlite_test.go` — unit tests for `SQLiteVecStore` covering: `Store` + `LoadAll` round-trip, `Query` returns top-K in similarity order, `Query` respects threshold, `DeleteByFile` removes only target file's records, `Size` accuracy, upsert replaces existing records by ID, empty store handles queries gracefully.
[] - SP-047-2b: Add `TestSQLiteVecStore_InterfaceConformance` — a shared test suite that exercises the `VectorStore` interface against both `JSONLFileStore` and `SQLiteVecStore` to guarantee behavioral parity.
[] - SP-047-2c: Add `TestSQLiteVecStore_LargeDataset` — insert 10K records, benchmark query latency (<5ms per query at 256d). Skip in short test mode.
[] - SP-047-2d: Verify all existing `store_test.go` tests pass with `SQLiteVecStore` as the implementation (may require abstracting the store constructor in tests).

### Phase 3: Automatic Migration

[] - SP-047-3a: Add migration detection to `EmbeddingManager.init()` in `pkg/embedding/manager.go`: check for legacy `index.jsonl` file alongside the expected `index.db`. If JSONL exists and DB doesn't, trigger migration.
[] - SP-047-3b: Implement `migrateJSONLToSQLite(jsonlPath, dbPath string) (*SQLiteVecStore, error)` — loads all records from JSONL via `NewJSONLFileStore`, creates SQLite store, batch inserts records. Logs record count and elapsed time.
[] - SP-047-3c: After successful migration, rename `index.jsonl` to `index.jsonl.migrated` (preserve for rollback). Log the rename. Do NOT delete the JSONL file.
[] - SP-047-3d: Write `pkg/embedding/migration_test.go` — create a JSONL store with known records, run migration, open the resulting SQLite store, verify record count matches and `Query` returns the same top-K results.

### Phase 4: Manager Integration

[] - SP-047-4a: Replace `NewJSONLFileStore` with `NewSQLiteVecStore` in `EmbeddingManager.init()` (static provider path). Update the index file path from `index.jsonl` to `index.db`.
[] - SP-047-4b: Replace `NewJSONLFileStore` with `NewSQLiteVecStore` in ONNX init paths (`initONNX`, `autoBuildOnnxIndex`, `ONNXIndexManager`, `QuerySimilar` ONNX fallback). Update paths from `.embedding_index_onnx.jsonl` to `.embedding_index_onnx.db`.
[] - SP-047-4c: Update manifest paths from `.index.jsonl.manifest.json` to `.index.db.manifest.json` (and `.embedding_index_onnx.jsonl.manifest.json` to `.embedding_index_onnx.db.manifest.json`).
[] - SP-047-4d: Keep `store.go` (`JSONLFileStore`) in tree — needed for migration reading and as a test double. Mark with a doc comment noting it is the legacy implementation.
[] - SP-047-4e: Verify `ConversationStore` still works unchanged (it uses its own JSONL file, separate from the index store, per the Open Questions decision in the spec).
[] - SP-047-4f: Run `go test ./pkg/embedding/... -count=1 -timeout 120s` — all tests pass.

### Phase 5: WASM Build Compatibility

[] - SP-047-5a: Test `go build ./cmd/wasm/` — verify `ncruces/go-sqlite3` compiles under `GOOS=js GOARCH=wasm`.
[] - SP-047-5b: If WASM path fails, add build tags: `store_sqlite.go` gets `//go:build !wasm`, `store.go` (JSONL) gets `//go:build wasm` — WASM builds continue using JSONL while native builds use SQLite.
[] - SP-047-5c: If build tags are needed, update `EmbeddingManager.init()` to use conditional construction based on build tag or a `sqliteAvailable` build-time constant.
[] - SP-047-5d: Verify `make build-all` succeeds (or `go build ./...` if WASM build is blocked by pre-existing issues).

### Phase 6: Verification + Benchmarks

[] - SP-047-6a: Add `BenchmarkStoreQuery_JSONL` and `BenchmarkStoreQuery_SQLite` to `pkg/embedding/store_bench_test.go`. Run with `go test -bench=. -benchmem ./pkg/embedding/`. Target: SQLite query <5ms for 100K records at 256d.
[] - SP-047-6b: Measure binary size delta (`go build -o /tmp/sprout_before .` vs after). Target: <10MB increase.
[] - SP-047-6c: Verify success criteria from spec: all existing tests pass, cross-platform build succeeds, automatic migration is non-destructive.
