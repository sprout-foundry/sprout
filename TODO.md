# TODO

Active work tracked here. Completed items are removed once their parent spec is moved to ✅ Implemented in `roadmap/README.md` — the spec file itself is the historical record.

## SP-064: Automate CLI — Status, Stop, Logs
_Spec: `roadmap/SP-064-automate-cli-monitoring.md`_

### Phase 1: BPM Stop primitive
- [x] SP-064-1a: Add `(*BackgroundProcessManager).Stop(sessionID string, grace time.Duration) error` in `pkg/agent_tools/background_process.go`. SIGINT → grace → SIGTERM → 5s → SIGKILL. Updates status to `exited`. No-op on already-exited sessions.
- [x] SP-064-1b: Wire `BPM.Stop` into the `shell_command(stop_background=…)` tool path in `pkg/agent_tools/shell_handler.go` so CLI mode reaches parity with the WebUI TerminalManager.
- [x] SP-064-1c: Revert the "stop_background not available for automate sessions in CLI mode" caveat in `pkg/skills/library/workflow-automation/SKILL.md`.
- [x] SP-064-1d: Unit tests — signal sequencing on a controlled sleep subprocess (mock or real with very short grace periods), no-op on exited, error on unknown session.

### Phase 2: Session-kind tagging
- [x] SP-064-2a: Add `Kind string` field to BPM `Process` struct, default `"shell"`.
- [x] SP-064-2b: Set `Kind = "automate"` in `pkg/agent/tool_handlers_automate.go` `handleRunAutomate` BPM `Start` call.
- [x] SP-064-2c: Set `Kind = "automate"` in `cmd/automate.go` `runWorkflowByPath` — but this path uses `exec.Command` not BPM; either move CLI launches through BPM or write the same `kind=automate` marker to the PID file (Phase 3) and treat that as the source of truth for CLI-launched runs.

### Phase 3: Cross-process discovery (PID files)
- [x] SP-064-3a: On every workflow launch (CLI or agent tool), write `.sprout/automate/<session_id>.json` containing `{workflow, pid, started_at, output_file_path, budget_usd?, kind: "automate"}`.
- [x] SP-064-3b: Remove the PID file on clean shutdown (workflow process exit handler).
- [x] SP-064-3c: Stale-PID sweep at startup of any `sprout automate *` subcommand — `kill -0` each PID, remove files whose process is dead.
- [x] SP-064-3d: Document the PID-file schema in `roadmap/SP-064-automate-cli-monitoring.md` so SP-065's webui consumer doesn't drift.

### Phase 4: status / stop / logs subcommands
- [x] SP-064-4a: `cmd/automate.go` — add `automateStatusCmd` (`sprout automate status [--all] [--json]`). Reads PID files + BPM in-memory state, prints table.
- [x] SP-064-4b: `cmd/automate.go` — add `automateStopCmd` (`sprout automate stop <session_id>` or `--all`). Calls `Stop` (or sends signals directly when only the PID file is known).
- [x] SP-064-4c: `cmd/automate.go` — add `automateLogsCmd` (`sprout automate logs <session_id> [-f] [-n N]`). Reads the captured output file; `-f` polls at 500ms ticks.
- [x] SP-064-4d: Add subcommands to `automateCmd.AddCommand` and update help text.

### Phase 5: Tests + docs
- [x] SP-064-5a: Integration test — launch a sleep-based workflow, status shows it, stop kills it, status reflects exit, output file persists.
- [x] SP-064-5b: Cross-process test — launch from terminal A (real subprocess), assert `sprout automate status` from a separate process sees it via the PID file.
- [x] SP-064-5c: Update `workflow_properties.md` with a "Monitoring a running workflow" section.
- [x] SP-064-5d: Run `make build-all` and the full automate test suite; verify green.

## SP-065: WebUI Automations Panel
_Spec: `roadmap/SP-065-automate-webui-panel.md`_
_Blocked by: SP-064 (Phases 1–3 are prerequisites for cross-process session discovery)_

### Phase 1: Backend REST
- [x] SP-065-1a: `pkg/webui/automations_handlers.go` — `GET /api/automate/workflows` reuses `automate.Discover` + `automate.Summarize`.
- [x] SP-065-1b: `GET /api/automate/sessions` and `GET /api/automate/sessions/:id` — read BPM + PID files (SP-064-3a).
- [x] SP-065-1c: `POST /api/automate/run` — body validation, optional overrides, dispatches through the `run_automate` tool path so `requires_approval` and the security gate are honored.
- [x] SP-065-1d: `POST /api/automate/sessions/:id/stop` — calls `BPM.Stop`.
- [x] SP-065-1e: `GET /api/automate/sessions/:id/output?since=offset` — paged output read for WS-drop fallback.
- [x] SP-065-1f: Wire endpoints into the existing webui router with auth/origin checks.

### Phase 2: Backend WS events
- [x] SP-065-2a: Define event types in `pkg/events/`: `automate.session_started`, `automate.budget_update`, `automate.output_chunk`, `automate.session_ended`.
- [x] SP-065-2b: Publish `session_started` / `session_ended` from `handleRunAutomate` and CLI launch.
- [x] SP-065-2c: Publish `budget_update` from the existing budget warning + exceeded callbacks AND from the heartbeat tick in `cmd/agent_workflow.go`.
- [ ] SP-065-2d: Tee captured-output writes through a `automate.output_chunk` publisher with coalescing (≥250ms or ≥4KB).
- [ ] SP-065-2e: Subscription opt-in so chat sessions don't see automate events by default.

### Phase 3: Frontend panel
- [x] SP-065-3a: `webui/src/components/AutomationsPanel.tsx` — three sections (Available / Running / Recent). Wire to REST endpoints + WS subscription.
- [x] SP-065-3b: Add Automations entry to sidebar nav.
- [x] SP-065-3c: Run modal — shows price card + budget, allows per-run budget/heartbeat override, calls `POST /api/automate/run`.
- [x] SP-065-3d: Budget bar component with 50%/80% color transitions.
- [x] SP-065-3e: Running-row Stop button → `POST stop` with confirmation dialog.

### Phase 4: Session detail view
- [x] SP-065-4a: Detail panel route — header with status/budget/iteration/elapsed.
- [x] SP-065-4b: Captured-output stream component, auto-scroll-lock on user scroll-up.
- [x] SP-065-4c: Step timeline when `steps` exists — checkmarks for completed, highlight for current.
- [x] SP-065-4d: Budget event log — threshold crossings + cap-hit timestamps.

### Phase 5: Chat ↔ automate linkage
- [x] SP-065-5a: When `run_automate` succeeds in a chat, emit an inline chat message containing a link to the Automations panel with the new session id.
- [x] SP-065-5b: Sidebar nav handler — clicking the link switches to Automations and focuses the session.

### Phase 6: Tests
- [x] SP-065-6a: Handler unit tests — workflow discovery, run with requires_approval=true triggers intent prompt, run with requires_approval=false skips, stop terminates.
- [x] SP-065-6b: WS event ordering test — start → updates → end.
- [x] SP-065-6c: React component tests — AutomationsPanel renders empty / running / recent states; budget bar color transitions; intent confirmation modal flow.
- [x] SP-065-6d: Integration test against a real daemon with a shell-only workflow.

### Phase 7: Docs
- [x] SP-065-7a: Add a "WebUI usage" section to `workflow_properties.md`.
- [x] SP-065-7b: Add a WebUI paragraph to `SKILL.md` explaining the panel exists and how it relates to the agent tool path.
- [x] SP-065-7c: One-paragraph README mention.

## SP-066: Preserve Key Order in Structured File Tools
_Spec: `roadmap/SP-066-structured-file-key-order.md`_

### Phase 1: Foundation — Ordered Map Dependency + Type Definition
- [ ] SP-066-1a: Add `github.com/wk8/go-ordered-map/v2` dependency via `go get`.
- [ ] SP-066-1b: Create `pkg/agent/ordered_map.go` — define `OrderedMap` type alias for `*orderedmap.OrderedMap[string, interface{}]` with helpers: `New()`, `Get/Set/Delete/Keys()`, `ToMap()` / `FromMap()` conversion utilities.
- [ ] SP-066-1c: Add `ParseJSONOrdered(content string) (*OrderedMap, error)` — use `json.Decoder` with iterative token walk that inserts keys into ordered map in parse order (avoids `json.Unmarshal` which loses order).
- [ ] SP-066-1d: Unit tests for `ParseJSONOrdered` — verify key order preserved for nested objects and arrays.

### Phase 2: Ordered YAML Parsing
- [ ] SP-066-2a: Add `ParseYAMLOrdered(content string) (*OrderedMap, error)` — use `yaml.Unmarshal` into `yaml.Node`, then walk the node tree into an ordered map (preserves key order from source).
- [ ] SP-066-2b: Replace `normalizeYAMLValue` with `NormalizeYAMLOrdered(*OrderedMap) *OrderedMap` — recursively normalize `map[interface{}]interface{}` YAML quirks while preserving order.
- [ ] SP-066-2c: Unit tests for `ParseYAMLOrdered` — verify key order preserved from source file, nested objects, arrays, and scalar normalization.

### Phase 3: Ordered Serialization
- [ ] SP-066-3a: Add `SerializeJSONOrdered(om *OrderedMap) (string, error)` — custom recursive JSON encoder that walks ordered map in insertion order, uses `encoding/json` for leaf values, preserves indentation.
- [ ] SP-066-3b: Add `SerializeYAMLOrdered(om *OrderedMap) (string, error)` — construct `yaml.Node` tree with keys in insertion order, then `yaml.Marshal` the node tree.
- [ ] SP-066-3c: Replace `serializeStructuredContent` in `tool_handlers_structured.go` to use the new ordered functions.
- [ ] SP-066-3d: Unit tests for serialization — round-trip a known-ordered structure, verify output key order matches input order.

### Phase 4: Tool Argument Parsing — Preserve Order at Entry Point
- [ ] SP-066-4a: In `pkg/agent/tools.go` (line ~24 where `json.Unmarshal` parses tool args), add an ordered-parse path: after the existing `map[string]interface{}` parse, also produce an ordered version and store it in a context value for structured file tools to use.
- [ ] SP-066-4b: Update `handleWriteStructuredFile` to check context for ordered args first; if available, use `data` as an ordered map directly. Fallback: parse from existing `map[string]interface{}` args.
- [ ] SP-066-4c: Update `handlePatchStructuredFile` similarly — use ordered args for `patch_ops` and `data` when available.
- [ ] SP-066-4d: Unit tests — verify that `write_structured_file` with ordered input produces output in the same key order.

### Phase 5: Patch Mutation Functions — Ordered Map Operations
- [ ] SP-066-5a: Rewrite `applyPatchOperation`, `applyMutation`, `mutateAtLeaf`, `readPointerValue` in `tool_handlers_structured.go` to operate on `*OrderedMap` instead of `map[string]interface{}`. Maintain insertion order on `add` ops.
- [ ] SP-066-5b: Replace `deserializeStructuredContent` to return `*OrderedMap` using the new parse functions from Phase 2.
- [ ] SP-066-5c: Rewrite `validateDataAgainstSchema` to accept `*OrderedMap` — update type assertions throughout.
- [ ] SP-066-5d: Run existing tests in `tool_handlers_file_events_test.go` and `tool_handlers_file_json_test.go` — fix breakage from type changes.

### Phase 6: Integration Tests & Verification
- [ ] SP-066-6a: Write `pkg/agent/ordered_structured_test.go` — end-to-end: write a JSON file with specific key order, verify written file preserves order.
- [ ] SP-066-6b: Patch order preservation test — start with ordered file, apply a patch, verify all original keys remain in order and new keys appear at the patched location.
- [ ] SP-066-6c: YAML round-trip test — parse YAML with non-alphabetical keys, patch a value, verify output preserves original key order.
- [ ] SP-066-6d: Run `make build-all` + `go test ./...` — verify no regressions across the full test suite.
