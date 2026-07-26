# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

---

## SP-103: Vision Pipeline Reliability

_3 of 6 sub-items remain open (D1, D2, D3). B2 and A9 shipped; see git history._

### Remaining Work

- **D1: Inline-Image Cost into Budget Tracker** — When `processImagesAsMultimodal` embeds images into chat messages, per-image `image_tokens` / `cache_read_input_tokens` come back in the provider response but are dropped before reaching `BudgetTracker.Deduct`. Bridge them so users see actual vision cost. **~1.5 days.** Touches `conversation.go`, provider response structs, and `pkg/budget/budget.go`.

- **D2: Batch Splitting with Fallback** — When a user pastes N images and the provider's vision context window is exceeded, the inline path fails with 400. Add automatic batch splitting: try inline; on vision-context overflow, split — keep first K images inline, call `analyze_image_content` for the rest. **~1 day.** New `vision_batch_split.go` helper.

- **D3: Provider Vision-Capability Tables** — `SupportsVision()` is a binary flag. Add a `VisionCapabilities` struct per provider (max image bytes, max image count, max dimensions, detail tiers). Use it to drive resize and batch splitting. The `VisionCapabilities` struct exists in `pkg/agent_api/interface.go` but populating per-provider values remains. **~0.5 day.**

### Acceptance

- `go test -race ./pkg/agent_tools/...` passes.
- A 4K pasted screenshot bills as ~1500 visual tokens (not ~4800).
- `classifyPDFProcessingErrorCode` uses `errors.As(*TypedError)` instead of `strings.Contains` (shipped).
- `make test-race` is a required CI check.

---

## SP-094: Typed Error Hierarchy in `pkg/agent`

_Full migration of ~512 `fmt.Errorf` sites to typed errors (~1 week). Foundation + retry/backoff shipped; the full tree is NOT._

`pkg/agent/errors.go` is 392 bytes with just one sentinel (`errProviderStartupClosed`). The ~250-line tree called for by the spec has not landed. The `fmt.Errorf`-migration work (~512 sites across `pkg/agent_tools/*_handler.go`, `pkg/agent/api_client*.go`, `pkg/agent/subagent_*.go`, etc.) is genuine remaining work.

### Scope

**Define the full tree in `pkg/agent/errors.go` (~250 lines new):**
- `AgentError` (already exists in `pkg/errors/types.go` — extend, not duplicate).
- Categories: `RetryableError`, `RateLimitError`, `AuthError`, `ContextCancelledError`, `InvalidInputError`, `ToolError`, `ProviderError`, `FileSystemError`, `NetworkError`, `WorkspaceError`.
- Each implements `Error()`, `Unwrap()`, and `IsRetryable()` (bool).
- `Wrap(base error, msg string) error` helper that returns the right typed wrapper based on `errors.As`.

**Migrate sites in waves, each with tests:**
1. Tool handlers — `pkg/agent_tools/*_handler.go` (~80 sites).
2. Provider clients — `pkg/agent/api_client*.go` (~40 sites).
3. Subagent + delegator — `pkg/agent/subagent_*.go` (~60 sites).
4. Remaining `pkg/agent/*.go` files (~330 sites).

**Wire classification into the broker:**
- `pkg/agent/approval_broker.go` — trigger exponential backoff on `ProviderError+RateLimitError`.
- `pkg/agent/metrics.go` — emit a label per error category.
- `pkg/agent/seed_provider.go::ChatStream` — surface "rate-limited, retrying" to WebUI event bus with `EventTypeRateLimited`.

**`sprout explain <hash>` integration (SP-068):** typed hierarchy instead of raw stack traces.

### Acceptance

- `grep -rn "fmt.Errorf" pkg/agent --include="*.go" | wc -l` returns a number ≥80% smaller than today.
- Every entry in `pkg/errors/types_test.go` passes.
- Provider 429 triggers 1-2 automatic retries with backoff instead of a hard failure.

---

## SP-098: SP-075 Large-File Decomposition — Second Pass

_~1 week, 5-7 phases. Most of SP-075's original worst offenders were already split since the spec was written. A new batch of files has grown large — split these in priority order._

### Current state (2026-07-05 audit — refresh)

| File | Lines | Recommendation |
|---|---|---|
| `pkg/console/markdown_formatter.go` | 1217 | Split: `markdown_table.go` (table rendering, ~400 lines) + `markdown_highlight.go` (syntax highlighting, ~200 lines). Keep core Format() loop in place. |
| `pkg/configuration/config_risk_subagent.go` | 1035 | Split into `risk_heredoc.go`, `risk_profile.go`, `risk_classify.go`. |
| `pkg/webui/websocket_handler.go` | 1008 | Split: `websocket_conn.go` (lifecycle) + `websocket_message.go` (message handling). |
| `pkg/configuration/manager.go` | 949 | Split: `manager_load.go` + `manager_save.go` + `manager_provider.go`. |
| `pkg/agent_api/ollama_local.go` | 940 | Split per-feature: `ollama_models.go`, `ollama_chat.go`, `ollama_embed.go`. |
| `pkg/agent/seed_tool_registry.go` | 926 | Per SP-109-2/3: tool descriptions by domain into separate `tool_registry_*.go` files. |
| `pkg/webui/chat_sessions_api.go` | 920 | Split: CRUD + `chat_sessions_messages.go` + `chat_sessions_search.go`. |
| `pkg/filediscovery/filediscovery.go` | 897 | Split by phase: `filediscovery_walk.go`, `filediscovery_filter.go`, `filediscovery_index.go`. |
| `pkg/agent/agent_getters.go` | 886 | Split: `agent_session_getters.go` + `agent_state_getters.go`. |
| `pkg/agent/tool_security.go` | 873 | Split: `tool_security_policy.go` + `tool_security_paths.go` + `tool_security_audit.go`. |
| `pkg/webui/ssh_launch.go` | 867 | Split: `ssh_launch_config.go` + `ssh_launch_exec.go` + `ssh_launch_api.go`. |
| `pkg/providerregistry/registry.go` | 865 | Split: `registry_models.go` + `registry_providers.go` + `registry_aliases.go`. |
| `pkg/credentials/encrypt.go` | 861 | Split: `encrypt_aes.go` + `encrypt_keyring.go` + `encrypt_migrate.go`. |
| `pkg/events/events.go` | 857 | Split: `events_types.go` + `events_bus.go` + `events_filter.go`. |
| `pkg/embedding/manager.go` | 853 | Split: `embedding_models.go` + `embedding_batch.go` + `embedding_cache.go`. |
| `pkg/agent/change_tracking.go` | 850 | Per SP-077: `change_tracking_record.go` + `change_tracking_revert.go` + `change_tracking_persist.go`. |
| `pkg/agent_tools/background_process.go` | 848 | Split: lifecycle + `background_process_log.go` + `background_process_pty.go`. |
| `pkg/agent/submanager_state.go` | 848 | Split: state machine + persist + query. NOTE: also listed in **StateManager interface refactor** (SP-115 shipped) — the file is large AND the StateManager interface needs splitting into focused sub-managers. |
| `cmd/mcp_add.go` | 847 | Split per-tool: `mcp_add.go` + `mcp_list.go` + `mcp_remove.go`. |
| `pkg/history/changetracker.go` | 843 | Split: `changetracker_record.go` + `changetracker_revert.go` + `changetracker_persist.go`. |
| `pkg/agent/persistence.go` | 843 | Split: `persistence_session.go` + `persistence_message.go` + `persistence_index.go`. |
| `pkg/webui/settings_api_mcp.go` | 841 | Split: `settings_api_mcp.go` + `settings_api_mcp_test.go` + `settings_api_mcp_oauth.go`. |
| `pkg/console/select_list.go` | 840 | Split: `select_list.go` + `select_list_filter.go` + `select_list_keymap.go`. |
| `pkg/agent_tools/security_classifier.go` | 834 | Split: `security_classifier.go` + `security_classifier_path.go` + `security_classifier_shell_patterns.go`. |
| `pkg/agent/scripted_playback.go` | 832 | Split: `scripted_playback.go` + `scripted_record.go` + `scripted_assert.go`. |

Total: 25 files ≥800 lines. Additional borderline files (`repo_map.go` 801, `workspace_sync.go` 797) can be folded into phase work but are not strictly above 800.

### Acceptance

- Every targeted file ends under 800 lines.
- `go build ./...` clean after each extraction (per AGENTS.md refactoring protocol).
- All existing tests in each split file's package continue to pass.

---

## Things to consider after SP-091 → SP-095 ship

- **WASM stub-tools** — running the WASM build against `pkg/agent_tools/` with CGO-only handlers stubbed (grammar embed + static-embed removal shipped per SP-058/SP-061; remaining work is handler-stub coverage).
- **Subagent webui panel** — there's an active conversation indicator but no per-subagent detail view; SP-051 shipped depth in CLI but not WebUI.
- **Multi-workspace sprout daemon** — feature requested twice in the past month.

---
