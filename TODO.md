# TODO

Active work tracked here. Completed items are removed once their parent spec is moved to ✅ Implemented in `roadmap/README.md` — the spec file itself is the historical record.

## SP-059: Subagent ↔ Primary Interaction Overhaul + Delegate Retirement
_Spec: `roadmap/SP-059-subagent-interaction.md`_

Phases 1–3 are shipped. Phase 6 (delete the `delegate` tool) is the remaining high-priority work.

### Phase 6: Delete the `delegate` tool entirely
- [x] SP-059-6a: Port useful delegate-only features to `run_subagent`. Check `pkg/agent/tool_handlers_delegate.go` for any features that `run_subagent` lacks (e.g. `FollowUpMessages` — confirm it's intentionally dropped per SP-059 non-goals). If nothing needs porting, document that and move on. Acceptance: a review of delegate features confirms nothing needed is missing from run_subagent.
- [x] SP-059-6b: Delete the `delegate` tool registration. Remove `delegate` and `delegate_status` registrations from `pkg/agent/tool_registrations.go` and `pkg/agent_api/tools.go`. Delete the delegate handler files (`pkg/agent/tool_handlers_delegate*.go`). Acceptance: `make build-all` passes; the tool listing no longer includes `delegate` or `delegate_status`.
- [ ] SP-059-6c: Remove delegate-related agent fields. Remove `delegateDepth`, `delegateID` fields and `SPROUT_MAX_DELEGATE_DEPTH` env-var handling from `pkg/agent/agent.go` and related code. Acceptance: `make build-all` passes; `grep -r delegateDepth pkg/` returns nothing.
- [ ] SP-059-6d: Migrate webui delegate event consumers. In `webui/`, find any consumers of `delegate_spawn`/`delegate_activity`/`delegate_complete`/`delegate_tool` events and migrate them to the subagent equivalents (these events already exist for subagents). Acceptance: `make build-all` passes; no references to `delegate_*` events remain in webui code.
- [ ] SP-059-6e: Phase 6 tests and verification. Run `make build-all` and `go test ./...` — both must pass. Verify `run_subagent` still works via manual or automated test. Update `roadmap/SP-006-delegate-tool.md` to mark as superseded by SP-059. Acceptance: all tests pass; SP-006 spec marked superseded.

### Phase 4: Subagents can request user clarification mid-run
- [ ] SP-059-4: Wire clarificationManager into createSubagent. Currently subagents cannot call `request_clarification`. Wire the primary's `clarificationManager` into the subagent creation path (`pkg/agent/subagent_runner.go::createSubagent`) so subagents can request clarification from the user mid-run. Acceptance: a subagent calling `request_clarification` surfaces the question to the user via the existing primary agent event bus path.

## SP-060: Desktop App — Per-Workspace Server Mode
_Spec: `roadmap/SP-060-desktop-serve.md`_

Phase A (auth token + TCP random port) and Phase B (Unix domain socket) are largely implemented. Verify and close out.

- [ ] SP-060-A: Verify Phase A and Phase B completeness. Run the existing `desktop/backend_test.js` tests. Verify: (1) `generateSecret()` produces a 256-bit hex token, (2) `SPROUT_AUTH_TOKEN` env var is passed to the child process, (3) the HTTP proxy injects `Authorization: Bearer <token>` headers, (4) `--bind-socket` flag works in Go and the Electron proxy forwards to the socket, (5) Windows falls back to TCP+auth. If all pass, update the spec status to ✅ and move SP-060 in `roadmap/README.md` from "In Progress" to "Implemented". Acceptance: `node desktop/backend_test.js` passes; spec and README updated.

## SP-068: Security Check Consolidation
_Spec: `roadmap/SP-068-security-check-consolidation.md`_

Refactor to unify the two risk vocabularies (static classifier SAFE/CAUTION/DANGEROUS + persona cascade Low/Medium/High/Critical) into one scale, one resolver, one broker.

### Phase 1: One risk scale (non-behavioral mapping)
- [ ] SP-068-1a: Create `RiskAssessment` type. In `pkg/agent_tools/` (or `pkg/agent/`), create a `RiskAssessment` struct with fields: `Level` (Low/Medium/High/Critical), `Sources` ([]RiskSource — classifier, persona, git-gate, fs-tier, workspace-policy), `Reason` (human-readable string), `IsHardBlock` (bool). This is a pure type addition with no behavior change. Acceptance: type compiles; `go vet` clean; `go build ./...` passes.

### Phase 2: One resolver (collapse the two gates)
- [ ] SP-068-2a: Implement `ResolveToolRisk` function. Create `ResolveToolRisk(toolName, args, agent) RiskAssessment` that runs all security inputs (static classifier, persona cascade, git-gate, fs-tier, workspace policy) and returns the most restrictive result. Ship behind a `unified_risk_resolver` config flag (default off). Add shadow-mode logging comparing old-vs-new decisions. Acceptance: with flag off, behavior is unchanged. With flag on, the resolver returns a single decision; shadow-mode log shows zero decision changes except eliminated duplicate prompts on a test corpus.

### Phase 3: One broker + "explain" diagnostic
- [ ] SP-068-3a: Add `sprout explain` CLI subcommand. Add an `explain` subcommand that takes a command string and prints the full `RiskAssessment`: canonical level, every contributing source, and the exact rule that set the level. Complements SP-049's `sprout audit tail`. Acceptance: `sprout explain 'git reset --hard HEAD~5'` prints level `High/Critical` with annotated contributing sources.

## SP-066: Structured File Key Order Preservation
_Spec: `roadmap/SP-066-structured-file-key-order.md`_

- [ ] SP-066-keyorder: Preserve insertion key order in structured file tools. Update `write_structured_file` and `patch_structured_file` to preserve the key order the LLM sends, rather than alphabetically sorting keys. Use an ordered map implementation (e.g. `OrderedMap` with slice + map) instead of `map[string]interface{}` for JSON/YAML serialization. Acceptance: `write_structured_file` with keys `{name, version, description}` produces a file with keys in that order, not alphabetical. `patch_structured_file` preserves original key order on re-serialization. Existing tests pass.

## SP-015: Cloud Platform Integration
_Spec: `roadmap/SP-015-cloud-platform.md`_

- [ ] SP-015-R1: Add WASM interception in CloudAdapter. The `CloudAdapter` class in `webui/src/services/cloudAdapter.ts` classifies 17 endpoints as `wasm-local` but does NOT intercept them — they fall through to `fetch()` → 404. Add WASM interception: check `isWasmLocal()` and route to WASM shell methods instead of `fetch()`. The Service Worker path already does this correctly. Acceptance: file operations work through the CloudAdapter path in cloud mode; `cloudAdapter.test.ts` and `cloudAdapter.integration.test.ts` pass.
- [ ] SP-015-R3: Audit component-level feature flag adoption. Audit webui components that reference SSH, instances, local terminal, or settings. Ensure they use `supports*` flags from `webui/src/config/mode.ts` so local-only features are hidden in cloud mode. Acceptance: no local-only UI elements appear when running in cloud mode.
- [ ] SP-015-R5: Verify WebSocket routing across all three patterns. Three WebSocket patterns exist: (1) transparent reverse proxy, (2) JSON-over-websocket tunnel, (3) no WebSocket (browser IDE uses SSE + MessageChannel). Verify the webui's WebSocket client handles all three. Acceptance: terminal sessions work in all three deployment environments.

## SP-061: Remove Static Embedding Provider
_Spec: `roadmap/SP-061-remove-static-embeddings.md`_

Tech-debt reduction: remove the static embedding provider and consolidate on ONNX.

- [ ] SP-061-1: Delete static provider files. Remove `pkg/embedding/static_*.go` files (static provider implementation). Update imports and references throughout `pkg/embedding/manager.go`. Acceptance: `go build ./...` succeeds with no references to deleted files.
- [ ] SP-061-2: Update WASM embedding and memory code. Update `cmd/wasm/embedding_funcs.go`, `pkg/agent/memory_embedding.go`, and `pkg/agent/memory_search_handler.go` to remove static provider code paths. Acceptance: `go build -tags wasm ./cmd/wasm/` succeeds; WASM build works without static model.
- [ ] SP-061-3: Update tests and build system. Remove or update static-provider tests in `pkg/embedding/`. Ensure `go test ./pkg/embedding/...` and `go test ./pkg/agent/...` pass. Update any build scripts that reference static model paths. Acceptance: all tests pass; semantic search via ONNX provider returns correct results.
