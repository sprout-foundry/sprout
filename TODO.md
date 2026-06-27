# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

**Status of related specs:** SP-063 (`computer_user` persona) is **partially implemented** — its core shipped; remaining work (panic key 4g, destructive-app denylist 4h) is tracked in `roadmap/SP-063-computer-use-persona.md`, not here. SP-073 (`cooperative cancellation`) shipped 2026-06-26 — all three phases green (TODO(SP-034-1c) markers cleared); further work would be new tickets, not this list.

## SP-022: Remote Provider Registry — Phase 1 Foundation
_Spec: `roadmap/SP-022-remote-provider-registry.md` (status: 📋 Proposed; Phases 1–2 partial)_

Foundation (Medium): publish provider connection configs to GitHub Pages alongside model data, fetch at startup, merge over embedded baseline with thread-safe factory. Eliminates the embedded-only model and unlocks community provider PRs without a code release.

### Phase 1 — package + factory thread-safety

- [ ] SP-022-1a: Create `pkg/providerregistry/` package — define `RemoteProviderConfig` struct (duplicated fields from `ProviderConfig` to avoid import cycle), `ToProviderConfig()` converter, `FetchProviderConfig(ctx, providerID)`, `FetchAllProviders(ctx)` with cache/singleflight/TTL/negative-cache.
- [ ] SP-022-1b: Add `sync.RWMutex` to `ProviderFactory` — protect `configs` and `registry.ProviderConfigs` maps; all read methods acquire RLock, all write methods acquire Lock.
- [ ] SP-022-1c: Add `UpsertConfig(name string, cfg *ProviderConfig)` to `ProviderFactory` — acquires write lock, updates both `f.configs[name]` and `f.registry.ProviderConfigs[name]`.
- [ ] SP-022-1d: Add SSRF validation to `pkg/providerregistry/` — reject non-HTTPS endpoints, private IPs, localhost.

### Phase 2 — async remote refresh + global factory

- [ ] SP-022-2a: Add async `refreshFromRemote()` to `factory.init()` with `inTestBinary()` guard — fetches all remote provider configs and upserts into the global factory.
- [ ] SP-022-2b: Export global factory accessor from `pkg/factory/` (e.g., `GlobalFactory() *providers.ProviderFactory` or `GlobalAvailableProviders() []string`).
- [ ] SP-022-2c: Fix `GetAvailableProviders()` in `pkg/configuration/init.go` to use the global factory instead of creating a throwaway instance.
- [ ] SP-022-2d: Add `PROVIDER_REGISTRY_URL` env var support — default reuses same base as `MODEL_REGISTRY_URL`; support `"off"`/`"none"`/`"disabled"` to disable.

### Phase 3 — publish pipeline

- [ ] SP-022-3a: Create `scripts/generate-provider-index.sh` — generates `providers/index.json` listing all provider config files with timestamps.
- [ ] SP-022-3b: Extend `.github/workflows/model-registry-publish.yml` — add step to copy `configs/*.json` to the GitHub Pages artifact with `schema_version` + `published_at` metadata injection via `jq`.
- [ ] SP-022-3c: Publish the 7 missing provider model files (cerebras, chutes, deepseek, lmstudio, mistral, ollama-turbo, openai) — ensure `refresh_provider_catalog` covers all 11 providers (may require adding API keys for missing providers to CI secrets).

### Phase 4 — provider-config cleanup

- [ ] SP-022-4a: Fix `lmstudio` API key inconsistency — update `pkg/agent_providers/configs/lmstudio.json` auth type to `"none"`, regenerate `provider_gen.go`, and update `credentials/resolve.go` to consistently mark lmstudio as not requiring a key.

### Phase 5 — docs + tests

- [ ] SP-022-5a: Add `CONTRIBUTING.md` section documenting the provider addition pattern: create JSON config → run `generate_providers.go` → open PR → CI auto-publishes.
- [ ] SP-022-5b: Unit tests for `pkg/providerregistry/` — cache hit/miss, negative cache, singleflight dedup, TTL expiry, offline fallback, SSRF rejection.
- [ ] SP-022-5c: Unit tests for `UpsertConfig()` — concurrent read/write safety, both maps updated atomically.
- [ ] SP-022-5d: Integration test: embedded-only mode (no remote) works correctly; remote configs merge over embedded.
- [ ] SP-022-5e: Verify `make build-all` passes after all changes.

## SP-054: LSP Language Coverage
_Spec: `roadmap/SP-054-lsp-language-coverage.md` (status: 📋 Proposed)_

Tooling (Medium-High, ~6 weeks across 3 phases): extend LSP beyond Go + TypeScript to Tier-1 languages (Python, Rust, C/C++, Java, Ruby, Go-template, C#, Kotlin, Swift, Elixir, PHP, Bash) plus semantic adapters so the agent can query diagnostics / hover / definition / references for the top 3.

### Phase 1 — Configuration Expansion

- [ ] SP-054-1a: Expand `DefaultLanguageServers()` in `pkg/lsp/proxy/discovery.go` to include configs for 12+ languages (each mapping language IDs to server binary + args; servers not on PATH report unavailable).
- [ ] SP-054-1b: Expand `LSP_SUPPORTED_LANGUAGES` in `webui/src/services/lspClientService.ts` so CodeMirror activates the LSP client for the new language IDs.
- [ ] SP-054-1c: Add `GET /api/lsp/status` returning which language servers are available vs not installed; surface in the editor footer / status bar.
- [ ] SP-054-1d: Graceful missing-server UX — clear log message with install instructions on `ResolveBinaryPath()` failure; structured error to the frontend.
- [ ] SP-054-1e: Acceptance: opening `.py`/`.rs`/`.cpp` files starts the appropriate server (pyright-langserver / rust-analyzer / clangd) if installed; existing Go and TypeScript LSP still works; `go test ./pkg/lsp/...` passes.

### Phase 2 — Auto-Install & Configuration

- [ ] SP-054-2a: `sprout lsp install <language>` CLI command + `POST /api/lsp/install` endpoint — each `LanguageServerConfig` gets an `InstallHint` field documenting the install command.
- [ ] SP-054-2b: `sprout lsp list` (and `--all`) shows installed/available status for every configured language.
- [ ] SP-054-2c: User-configurable servers via `languageServers` config map (binary, args, languageIds) — `Manager.SetConfig()` already exists; wire config file loading + merging with defaults.
- [ ] SP-054-2d: Workspace activation hints — detect `requirements.txt`/`pyproject.toml`/`Cargo.toml`/`*.sln`/`*.csproj` etc. and suggest relevant language servers on workspace open.
- [ ] SP-054-2e: Acceptance: `go test ./...` passes; a custom `elixir` server config in the user's `sprout.json` is loaded alongside defaults.

### Phase 3 — Semantic Adapters (Python, Rust, C/C++)

- [ ] SP-054-3a: Python semantic adapter (`pkg/lsp/semantic/python_adapter.go`) — diagnostics via `ruff check --output-format=json` (no server startup); hover / definition / references / rename via LSP proxy query to `pyright-langserver`.
- [ ] SP-054-3b: Rust semantic adapter (`pkg/lsp/semantic/rust_adapter.go`) — diagnostics via `cargo check --message-format=json`; hover / definition / references / inlay_hints via `rust-analyzer` LSP proxy query; optional `cargo fix --allow-dirty` code action.
- [ ] SP-054-3c: C/C++ semantic adapter (`pkg/lsp/semantic/cpp_adapter.go`) — diagnostics via `clang-tidy --export-fixes`; hover / definition / references via `clangd` LSP proxy query; formatting via `clang-format` CLI.
- [ ] SP-054-3d: Shared `lsp_query.go` helper in `pkg/lsp/semantic/` — takes language ID + LSP method + params, routes through `pkg/lsp/proxy/Manager`, returns parsed result. Reuses the existing gopls pattern for inlay hints.
- [ ] SP-054-3e: Register all three adapters in the semantic registry alongside Go and TypeScript.
- [ ] SP-054-3f: Acceptance: `go test ./pkg/lsp/semantic/...` passes with tests for all new adapters; agent can query diagnostics/hover/definition/references for Python/Rust/C++ files.

## SP-058: Selective Grammar Embed
_Spec: `roadmap/SP-058-selective-grammar-embed.md` (status: 📋 Proposed)_

Build hygiene (Low-Medium, ~1 day): the WASM binary is 53 MB and the daemon binary is 171 MB because ~8 MB of tree-sitter `.bin` grammar blobs are statically embedded. Move them to lazy-loaded external files. Target: WASM < 45 MB, daemon < 160 MB.

- [ ] SP-058-1: Add `pkg/ast/grammars/bin/` to `.gitignore`; create `scripts/prepare-grammars.sh` (executable) that downloads/fetches the grammars on first run.
- [ ] SP-058-2: Add `prepare-grammars` target to the Makefile; wire `build`, `build-wasm`, `test-unit`, `test-integration` to depend on it. Acceptance: a fresh `git clone` + `make build-all` succeeds without manual intervention.
- [ ] SP-058-3: Update `scripts/build-wasm.sh` — replace `WASM_TAGS=grammar_set_core` with `grammar_blobs_external`; tighten the size-threshold comment (100 MB → 50 MB warning).
- [ ] SP-058-4: Update Makefile `build:` target — add `-tags grammar_blobs_external` to the `go build` invocation.
- [ ] SP-058-5: Create `pkg/ast/grammars_embed.go` with the `//go:embed` + `register()` setup so the embed path remains available behind a build tag (graceful fallback for users who haven't run `prepare-grammars`).
- [ ] SP-058-6: Verify `make build-all` succeeds and binaries hit the size targets; run the full test suite (`pkg/ast/...`, `pkg/embedding/...`) and fix any regressions.
- [ ] SP-058-7: Add the `make prepare-grammars` note to `CLAUDE.md` (one line for IDE/gopls users).

## SP-061: Remove Static Embeddings
_Spec: `roadmap/SP-061-remove-static-embeddings.md` (status: 📋 Proposed)_

Build hygiene (Low-Medium, ~1 week): the static (embedded) embedding model adds ~1,500 lines to `pkg/embedding/`, complicates WASM builds, and is superseded by the ONNX runtime bridge. Consolidate on a single ONNX-backed store; remove `staticmodel` build tag and the in-process model load path.

### Phase 1 — single ONNX store wiring

- [ ] SP-061-1a: Verify `GetConversationStore` returns the ONNX-backed store after init; memory embedding (save/delete/migrate) writes to the single store.
- [ ] SP-061-1b: Acceptance: `go build ./...` succeeds with no references to deleted files; `go test ./pkg/embedding/...` passes (all non-static tests); `go test ./pkg/agent/...` passes.
- [ ] SP-061-1c: Acceptance: `go build -tags wasm ./cmd/wasm/` succeeds (WASM build without static model); semantic search via the ONNX provider returns correct results for indexed files.

### Phase 2 — WASM ONNX bridge

- [ ] SP-061-2a: Wire WASM builds to the `onnxruntime-web` bridge (manual verification via `browse_url` on localhost).
- [ ] SP-061-2b: Acceptance: existing index directories handled gracefully (new model hash = fresh index, old data left on disk until user clears); no data loss for conversations/memories already indexed.

### Phase 3 — cleanup

- [ ] SP-061-3a: Zero references to `StaticProvider`, `StaticModel`, `staticModelData`, `SetStaticModelData` in the remaining codebase.
- [ ] SP-061-3b: Zero references to `onnxConvoStore`, `onnxStore`, `onnxProvider` in `manager.go`.
- [ ] SP-061-3c: `RRFMergeResults` removed (or kept in a separate non-embedding package if needed elsewhere — it isn't).
- [ ] SP-061-3d: `BackfillMemoryONNX` removed from `pkg/agent/memory_embedding.go`.
- [ ] SP-061-3e: `staticmodel` build tag removed from all build configs.
- [ ] SP-061-3f: Line count of `pkg/embedding/` reduced by ~1,500 lines.

### Phase 4 — docs

- [ ] SP-061-4a: Update `docs/WASM_API.md`: remove `setStaticModel` section; document the ONNX bridge as the path; update error messages to be provider-agnostic; update the error section to reflect the ONNX-only path.

## SP-064: Automate CLI — Status / Stop / Logs
_Spec: `roadmap/SP-064-automate-cli-monitoring.md` (status: 📋 Proposed)_

UX (Medium, ~1–2 weeks): the user can launch `sprout automate run` but has no first-class CLI way to inspect / stop / tail a running session — only `stop_background` and `check_background`, which are awkward for long-running workflows. Adds CLI parity with the WebUI's running-session experience and unlocks SP-065's panel.

### Phase 1 — BPM Stop primitive

- [ ] SP-064-1: Extend `pkg/agent_tools/background_process.go` with `(*BackgroundProcessManager).Stop(sessionID) error` — send SIGINT to the process group (matching `runWorkflowByPath`'s `os/exec`), escalate SIGTERM → SIGKILL on a configurable grace period (default 10 s), update session status to `exited`, return nil on already-exited. Wire through `shell_command(stop_background=…)` so the same call works in both modes. Acceptance: skill's "stop_background not available for automate sessions in CLI mode" caveat can be reverted.

### Phase 2 — `sprout automate status`

- [ ] SP-064-2a: Add `kind string` to the BPM `Process` struct (default `"shell"`, set `"automate"` from `handleRunAutomate` and `runWorkflowByPath`'s CLI path).
- [ ] SP-064-2b: Add `sprout automate status [--all] [--json]` — table of sessions with WORKFLOW / STATUS / SPENT-CAP / ITER / ELAPSED. `--all` includes exited sessions still in BPM memory (existing TTL); `--json` emits structured output. Acceptance: `bg-automate-7f3a91  validate.json  running  $1.20/$5.00  23  4m12s` style output.

### Phase 3 — `sprout automate stop`

- [ ] SP-064-3: Add `sprout automate stop <session_id>` and `sprout automate stop --all`. Resolves the session via BPM (must be `kind=automate`), calls `Stop()`, prints the final captured output snippet (last N lines). `--all` stops every active automate session.

### Phase 4 — `sprout automate logs`

- [ ] SP-064-4: Add `sprout automate logs <session_id> [-f] [-n N]` — `-f` tails the running session's stdout/stderr by polling the BPM output file (matches `CheckBackgroundOutputWait` pattern); `-n` shows only the last N lines. For exited sessions, prints whatever's still buffered.

### Phase 5 — cross-session persistence

- [ ] SP-064-5a: Define `AutomateSessionInfo` in `pkg/automate/pid_file.go` (schema: workflow, pid, started_at, output_file_path?, budget_usd?, kind="automate"). Session ID format: `cli-automate-<16-hex>` for CLI launches, `bg-<sanitized-prefix>-<8-hex>` for agent launches.
- [ ] SP-064-5b: Write `.sprout/automate/<session_id>.json` immediately after `cmd.Start()` succeeds (CLI) or BPM `StartWithKind` returns (agent). Remove on clean exit via `defer RemoveSessionFile(...)`.
- [ ] SP-064-5c: `SweepStaleSessions()` at the start of every `sprout automate *` subcommand — removes stale files whose PID no longer exists.

### Phase 6 — tests + docs

- [ ] SP-064-6a: Unit test for BPM Stop primitive (mock process, signal sequencing, grace-period escalation).
- [ ] SP-064-6b: Integration test — launch a sleep-based workflow, status shows it, stop kills it, status shows it gone.
- [ ] SP-064-6c: Cross-process test — launch from terminal A, status from terminal B sees it via PID file.
- [ ] SP-064-6d: Update SKILL.md (drop the "stop_background not available" caveat) and add "Monitoring a running workflow" section to `workflow_properties.md`.

## SP-065: Automate WebUI Panel
_Spec: `roadmap/SP-065-automate-webui-panel.md` (status: 📋 Proposed)_

UX (Medium, ~2–3 weeks): give the WebUI a first-class Automations panel — discover workflows, launch with budget/heartbeat overrides, watch live budget/iteration/output, stop mid-run, click into detail. Builds directly on SP-064's BPM `kind=automate` + PID-file infrastructure.

### Phase 1 — Backend REST endpoints

- [ ] SP-065-1a: `GET /api/automate/workflows` — discover workflow JSON files in the project's `automate/` directory (reuses `automate.Discover`); returns name, description, requires_approval, parsed Summary (including price card + budget).
- [ ] SP-065-1b: `GET /api/automate/sessions[?status=running|exited|all]` — list automate sessions from BPM + PID-file directory (SP-064).
- [ ] SP-065-1c: `GET /api/automate/sessions/:id` — one session's details (status, spend/cap, current step, elapsed, last output snippet).
- [ ] SP-065-1d: `POST /api/automate/run` — body `{workflow, budget_usd_override?, warn_at_override?, heartbeat_seconds_override?}`. Goes through the same `run_automate` tool path so `requires_approval: false` still bypasses; otherwise an intent prompt routes through the existing webui approval channel. Returns the new session ID.
- [ ] SP-065-1e: `POST /api/automate/sessions/:id/stop` — invokes `BPM.Stop` from SP-064.
- [ ] SP-065-1f: `GET /api/automate/sessions/:id/output[?since=offset]` — paged read of captured output for cold-fetch fallback when WS is dropped.

### Phase 2 — Backend WS events

- [ ] SP-065-2a: Add event types: `automate.session_started`, `automate.budget_update` (every heartbeat + immediately on threshold crossings), `automate.output_chunk` (incremental output deltas, coalesce ≥250ms or ≥4KB), `automate.session_ended`. Only delivered to subscribers explicitly opted in (so chat sessions don't get spammed).

### Phase 3 — Frontend panel skeleton

- [ ] SP-065-3: Add `webui/src/components/AutomationsPanel.tsx` and a sidebar entry with three sections: **Available workflows** (with ⚠ no-approval tag + Run button → modal showing price card / budget / override inputs); **Running** (live budget bar `$X/$Y` with warning color past 50%, danger past 80%; current step / iteration / elapsed; Open + Stop); **Recent** (status badge, final spend, iterations, duration).

### Phase 4 — Frontend session detail view

- [ ] SP-065-4: Clicking "Open" pushes a detail view: header with budget bar; captured-output stream appended via `automate.output_chunk` (auto-scroll lock on manual scroll up); step timeline (checkmark per completed agent+shell step, current step highlighted); budget event log with timestamps.

### Phase 5 — Frontend chat-session linkage

- [ ] SP-065-5: When an agent in a chat calls `run_automate` and the user approves, the chat shows `▶ Started validate.json — open in Automations panel`. Clicking switches the sidebar to Automations with the session selected.

### Phase 6 — Tests

- [ ] SP-065-6a: Unit — workflow discovery endpoint (mixed valid/invalid JSON); session list with mock BPM + PID-file fixture; run endpoint enforces intent confirmation for `requires_approval: true`.
- [ ] SP-065-6b: WS — subscribe, kick off mock run, assert event ordering (`session_started` → budget updates → `session_ended`).
- [ ] SP-065-6c: React — AutomationsPanel renders states (empty, running, recent), budget bar color transitions, intent-confirmation modal flow.
- [ ] SP-065-6d: Integration — real sprout daemon, launch a shell-only workflow from the panel, watch it complete in the WS stream, verify status row updates without manual refresh.

### Phase 7 — Docs

- [ ] SP-065-7a: Add a WebUI section to the workflow-automation skill explaining the panel; add "WebUI usage" section to `workflow_properties.md`; one-paragraph README note.

## SP-078: Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion
_Spec: `roadmap/SP-078-steer-panel-ux-parity.md` (status: 📋 Proposed; Phases 1–3 shipped)_

UX parity (Medium): the pinned steer-input panel (`pkg/console/steer_input.go`, 1428 LOC) lacks the wrap-aware render path and Tab completion that the regular `InputReader` has across `pkg/console/input_*.go` (~6300 LOC across 18 files). User-visible: long single-line steers overflow horizontally, the caret lands off-column on wrapped multi-line steers, and there's no slash-command completion mid-turn (Tab is reserved for STEER↔QUEUE mode toggle). All five prior steer fixes (`e830d113`, `8f501bd3`, `6714f690`, `eb441143`, `ac75f0ed`) remain green.

### Phase 4 — close-out

- [ ] SP-078-4: `grep -rn "TODO(SP-078)" pkg/console/` is empty; `make build-all` + `go test ./...` green; add a recording-style screenshot or `browse_url` snapshot of a wrapped steer so future regressions surface at review time.

## Automation-Process: Workflow TODO Processor Issues (3 issues from workflow diagnostics)
_Inline diagnosis (handled directly by orchestrator, NOT delegated to workflow): during a workflow diagnostic run we observed (1) failing webui tests, (2) the workflow-automation skill lacks details of the actual coordinated flow, (3) subagent provider/model sometimes diverges from `subagent_overrides`. The orchestrator is fixing these in-place; they are tracked here for visibility only._

- [ ] SP-AUTO-1: Fix two failing webui tests `TestAutomateSessionsAll_DispatchEmptyPathToList` and `TestAutomateIntegration_FullWorkflow` — both decode bare arrays from API responses that actually return wrapped objects (`{"workflows":[...]}`, `{"sessions":[...]}`); update test decode to use the wrapped shape. Acceptance: `go test ./pkg/webui/...` green. _(orchestrator — fix in-place)_
- [ ] SP-AUTO-2: Expand `pkg/skills/library/workflow-automation/SKILL.md` with the canonical coordinated flow (coordinator → orchestrator → leaf workers) — currently the skill describes how to generate a workflow JSON but doesn't document the persona chain, the prompt-file structure, or the subagent_override resolution order that `pkg/agent/persona.go` enforces. Acceptance: the skill's "Fast Path" includes a persona-chain section, the resolution-order section explains the silent-skip cases (`unknown persona`, `disabled persona`, empty override fields), and a worked example walks the user through reading `automate/workflow.json` here. _(orchestrator — fix in-place)_
- [ ] SP-AUTO-3: Diagnose & fix subagent model/provider divergence — confirm by capturing the actual provider/model used by a running subagent vs the workflow JSON's `subagent_overrides`. Add observability (log line at spawn time showing the persona, the override source, the resolved provider/model, and which fallback fired if any). Fix the resolution chain so the override wins deterministically. Acceptance: a 1-item run through `automate/workflow.json` logs the resolved provider/model for each spawned subagent and matches the override. _(orchestrator — fix in-place)_