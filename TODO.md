# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

---

## SP-103: Vision Pipeline Reliability

_3 of 6 sub-items remain open (D1, D2, D3). B2 and A9 shipped; see git history._

### Completed

- [x] **classifyPDFProcessingErrorCode** — Uses `errors.As(*TypedError)` with 17+ tests (`pkg/agent_tools/vision_utils.go:162`, `vision_typed_errors.go:28`)

### Remaining Work

- [ ] **D1: Inline-Image Cost into Budget Tracker** — When `processImagesAsMultimodal` embeds images into chat messages, per-image `image_tokens` / `cache_read_input_tokens` come back in the provider response but are dropped before reaching cost tracking. No `pkg/budget` package exists. Bridge them so users see actual vision cost. **~1.5 days.** Touches `conversation.go`, provider response structs, and a new budget tracking path.

- [ ] **D2: Batch Splitting with Fallback** — When a user pastes N images and the provider's vision context window is exceeded, the inline path fails with 400. Add automatic batch splitting: try inline; on vision-context overflow, split — keep first K images inline, call `analyze_image_content` for the rest. **~1 day.** New `vision_batch_split.go` helper. `pkg/agent_tools/vision_batch.go` exists but handles concurrency, not overflow.

- [ ] **D3: Provider Vision-Capability Tables (partial)** — `VisionCapabilities` struct exists (`pkg/agent_api/vision_capabilities.go:28`) with defaults, and `ClientInterface` declares the method. Ollama and OpenAI-compatible providers populate values. **Remaining:** per-provider values for Anthropic, Gemini, and other non-OpenAI providers return zero values and rely on defaults. **~0.5 day.**

### Acceptance

- `go test -race ./pkg/agent_tools/...` passes.
- A 4K pasted screenshot bills as ~1500 visual tokens (not ~4800).
- `make test-race` is a required CI check.

---

## SP-094: Typed Error Hierarchy in `pkg/agent`

_Foundation shipped; migration and broker integration remain (~1 week for full tree)._

### Completed

- [x] **Typed error type system** — `pkg/errors/types.go` (~600 lines) with dual taxonomy: legacy `AgentError` (7 categories) and new `TypedError` (10 codes, 4 severities). Full `error` interface, `Is()`, `Unwrap()`, constructors, and helpers.
- [x] **Vision handler typed errors** — `pkg/agent_tools/vision_batch.go` uses `TypedError`.
- [x] **EventTypeRateLimited** — Event type, `RateLimitedEvent` struct, publisher in `agent_events.go:164`, WebUI handler in `rate_limited_handler.go`, and tests.
- [x] **`pkg/agent/errors.go`** — 9-line sentinel-only file (correct; the hierarchy is in `pkg/errors/types.go`).

### Remaining Work

- [ ] **Migrate `fmt.Errorf` to typed errors** — **144 raw `fmt.Errorf` sites** remain in `pkg/agent/*.go`. The ~512 original count has been reduced, but migration is far from 80% reduction. Tool handlers (`pkg/agent_tools/*_handler.go`) are partially migrated — vision handlers use `TypedError`, but ~30+ general handlers (codegraph, edit, embedding, git, memory, settings) still use raw `fmt.Errorf`.

**Migration waves (each with tests):**
1. Tool handlers — `pkg/agent_tools/*_handler.go` (~80 sites, partial progress).
2. Provider clients — `pkg/agent/api_client*.go` (~40 sites).
3. Subagent + delegator — `pkg/agent/subagent_*.go` (~60 sites).
4. Remaining `pkg/agent/*.go` files (~330 sites).

- [ ] **Wire classification into the broker** — `pkg/agent/approval_broker.go` has no exponential backoff logic. A full `grep -rn "backoff\|Backoff\|BACKOFF"` across the codebase returns zero results. The `retry.go` file has `ClassifyError()` mapping `RateLimitError → ActionRetry`, but the actual backoff/retry loop is not in the approval broker.
  - `pkg/agent/approval_broker.go` — trigger exponential backoff on `ProviderError+RateLimitError`.
  - `pkg/agent/metrics.go` — emit a label per error category.
  - `pkg/agent/seed_provider.go::ChatStream` — "rate-limited, retrying" is surfaced (EventTypeRateLimited done), but backoff loop is missing.

- [ ] **`sprout explain <hash>` integration (SP-068)** — typed hierarchy instead of raw stack traces.

### Acceptance

- `grep -rn "fmt.Errorf" pkg/agent --include="*.go" | wc -l` returns a number ≥80% smaller than today (currently 144).
- Every entry in `pkg/errors/types_test.go` passes.
- Provider 429 triggers 1-2 automatic retries with backoff instead of a hard failure.

---

## SP-098: SP-075 Large-File Decomposition — Second Pass

_~1 week, 5-7 phases. 13 of 25 files resolved; 12 remain ≥800 lines (2 grew)._

### Current state (2026-07-27 audit)

| File | Lines | Status |
|---|---|---|
| `pkg/agent/tool_security.go` | 1142 | ❌ Grew (+269) — split: `tool_security_policy.go` + `tool_security_paths.go` + `tool_security_audit.go` |
| `pkg/events/events.go` | 1218 | ❌ Grew (+361) — split: `events_types.go` + `events_bus.go` + `events_filter.go` |
| `pkg/embedding/manager.go` | 902 | ⚠️ Stable — split: `embedding_models.go` + `embedding_batch.go` + `embedding_cache.go` |
| `pkg/agent_tools/security_classifier.go` | 900 | ⚠️ Stable — split: `security_classifier_path.go` + `security_classifier_shell_patterns.go` |
| `pkg/agent_tools/background_process.go` | 885 | ⚠️ Stable — split: `background_process_log.go` + `background_process_pty.go` |
| `pkg/providerregistry/registry.go` | 871 | ⚠️ Stable — split: `registry_models.go` + `registry_providers.go` + `registry_aliases.go` |
| `pkg/history/changetracker.go` | 868 | ⚠️ Stable — split: `changetracker_record.go` + `changetracker_revert.go` + `changetracker_persist.go` |
| `pkg/console/select_list.go` | 868 | ⚠️ Stable — split: `select_list_filter.go` + `select_list_keymap.go` |
| `pkg/credentials/encrypt.go` | 861 | ⚠️ Stable — split: `encrypt_aes.go` + `encrypt_keyring.go` + `encrypt_migrate.go` |
| `pkg/agent/persistence.go` | 857 | ⚠️ Stable — split: `persistence_session.go` + `persistence_message.go` + `persistence_index.go` |
| `pkg/webui/settings_api_mcp.go` | 847 | ⚠️ Stable — split: `settings_api_mcp_oauth.go` |
| `pkg/agent/scripted_playback.go` | 835 | ⚠️ Stable — split: `scripted_record.go` + `scripted_assert.go` |

**Resolved (now < 800 lines):**
- [x] `pkg/console/markdown_formatter.go` — 1217 → 230
- [x] `pkg/configuration/config_risk_subagent.go` — 1035 → 67
- [x] `pkg/webui/websocket_handler.go` — 1008 → 93
- [x] `pkg/configuration/manager.go` — 949 → 242
- [x] `pkg/agent_api/ollama_local.go` — 940 → 87
- [x] `pkg/agent/seed_tool_registry.go` — 926 → 65
- [x] `pkg/webui/chat_sessions_api.go` — 920 → 108
- [x] `pkg/filediscovery/filediscovery.go` — 897 → 142
- [x] `pkg/agent/agent_getters.go` — 886 → 2
- [x] `pkg/webui/ssh_launch.go` — 867 → 120
- [x] `pkg/agent/change_tracking.go` — 850 → 518
- [x] `pkg/agent/submanager_state.go` — 848 → 59
- [x] `cmd/mcp_add.go` — 847 → 688

**Remaining (12 files ≥800 lines, no sibling splits created):**
- [ ] `pkg/agent/tool_security.go` (1142) — highest priority (grew significantly)
- [ ] `pkg/events/events.go` (1218) — highest priority (grew significantly)
- [ ] `pkg/embedding/manager.go` (902)
- [ ] `pkg/agent_tools/security_classifier.go` (900)
- [ ] `pkg/agent_tools/background_process.go` (885)
- [ ] `pkg/providerregistry/registry.go` (871)
- [ ] `pkg/history/changetracker.go` (868)
- [ ] `pkg/console/select_list.go` (868)
- [ ] `pkg/credentials/encrypt.go` (861)
- [ ] `pkg/agent/persistence.go` (857)
- [ ] `pkg/webui/settings_api_mcp.go` (847)
- [ ] `pkg/agent/scripted_playback.go` (835)

### Acceptance

- Every targeted file ends under 800 lines.
- `go build ./...` clean after each extraction (per AGENTS.md refactoring protocol).
- All existing tests in each split file's package continue to pass.

---

## Other

- [ ] **WASM stub-tools** — Core I/O handlers are stubbed (browse_url, codegraph, vision, run_automate, shell, fs_compat, background_process — 15 files with WASM/js build tag pairs). Remaining work: CGO-only handler-stub coverage for large logic files (`security_classifier.go` at 900 lines has no WASM guard). **~1 day.**

- [ ] **Multi-workspace sprout daemon** — `daemonRoot` concept exists in `pkg/webui/server.go` with workspace scoping. `chat_sessions_worktree_api.go` provides worktree-based session isolation. Feature requested twice in the past month; needs a proper multi-workspace daemon design. **Scope TBD.**

---
