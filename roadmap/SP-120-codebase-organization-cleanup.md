# SP-120: Codebase Organization & Test Infrastructure Cleanup

**Status:** ✅ Phase 1 + Phase 2a + Phase 2b complete (2026-07-15). Phase 2c deferred. Phase 3 shipped (package guide).

## Problem

The codebase has grown rapidly through 75+ shipped specs. Three organizational
issues have accumulated:

1. **`cmd/` is a 199-file god package (59K lines)** mixing Cobra command wiring
   with workflow engine logic, terminal display, service management, and
   automate orchestration. No `pkg/` package imports `cmd/` (verified), so
   extraction is safe with no circular-dependency risk.
2. **`pkg/` has 60 flat top-level directories** with no sub-organization.
   Related packages (`agent_*`, `model*`, `provider*`, code-intelligence
   cluster) aren't grouped, making navigation harder.
3. **Test infrastructure has dead artifacts** from multiple migrations —
   orphaned Python test runners referencing deleted directories, dead Makefile
   targets, stale doc references, and unreferenced JS test files.

## Scope

Three phases, ordered by ROI. Each phase is independently shippable.

---

## Phase 1: Test Infrastructure Cleanup (Low Risk)

**Goal:** Remove dead/orphaned test artifacts, fix stale references. Pure
deletion + documentation. Zero functional risk.

### Tasks

- [ ] Delete orphaned Python test runners (their test dirs don't exist):
  - `e2e_test_runner.py` — references `e2e_tests/` (deleted)
  - `integration_test_runner.py` — references `integration_tests/` (deleted); also refers to "ledit" (old project name)
- [ ] Remove dead Makefile targets:
  - `test-integration` — already neutered, prints "removed" message
  - `test-e2e` — calls orphaned `e2e_test_runner.py`
  - Update `test-all` and `test-ci` to drop these targets
- [ ] Delete unreferenced JS test artifacts:
  - `test/websocket_test.html` — standalone HTML, not wired to anything
  - `test/ui_e2e_workflow.js` — not referenced by any runner or config
  - `test/websocket_direct_test.js` — not referenced
- [ ] Update `docs/TESTING.md`:
  - Fix file-tree section to match reality (remove `integration_tests/`, `e2e_tests/`)
  - Remove references to deleted Python runners
- [ ] Update `AGENTS.md` line ~16:
  - Remove `python3 e2e_test_runner.py` reference
- [ ] Verify: `make build-all` passes, `go test ./...` passes, `make test-smoke` still works

### What stays

- `test/*.spec.js` (3 Playwright files) — referenced by `test-desktop-smoke`
- `smoke_tests/` (2 files) — referenced by `test-smoke`
- `go test ./...` — canonical unit test command

---

## Phase 2: `cmd/` Package Extraction (Medium Risk)

**Goal:** Extract the four largest non-CLI concerns out of `cmd/` into proper
`pkg/` packages. Extends SP-075 (file decomposition) to package-level moves.

**Principles:** Same as SP-075 — pure moves only, build + test after each
extraction, one extraction unit per PR-sized batch. No logic changes.

### Task 2a: Workflow Engine → `pkg/workflow/`

The 8 `agent_workflow_*.go` files are a self-contained mini-framework:

| File | Lines | Responsibility |
|------|-------|---------------|
| `agent_workflow_runtime.go` | 669 | Budget overrides, model settings, runtime config |
| `agent_workflow_loop.go` | 570 | Gate/triage LLM calls, step execution loop |
| `agent_workflow_loader.go` | 465 | Workflow JSON loading + validation |
| `agent_workflow_runner.go` | 431 | Step execution, shell step runner |
| `agent_workflow_types.go` | 210 | Config/state types |
| `agent_workflow.go` | 3 | Entry-point stub |

Dependencies: `pkg/agent`, `pkg/agent_api`, `pkg/configuration`, `pkg/console`,
`pkg/events`, `pkg/utils`. Clean boundary — no cobra dependencies in the core
logic.

- [ ] Create `pkg/workflow/` package
- [ ] Move non-test `.go` files, update package declarations
- [ ] Move `_test.go` files
- [ ] Update `cmd/` to import from `pkg/workflow`
- [ ] Build + test green

### Task 2b: Service Management → `pkg/service/`

The 13 `service_*.go` files handle daemon/service lifecycle (launchd, systemd,
etc.) with platform-specific build tags.

- [ ] Create `pkg/service/` package
- [ ] Move `service_*.go` (respecting `//go:build` tags)
- [ ] Update `cmd/` imports
- [ ] Build + test green (on all platforms if possible)

### Task 2c: Agent Display → `pkg/cliui/`

Terminal rendering files that are display logic, not CLI commands:

| File | Lines |
|------|-------|
| `agent_terminal_subscriber.go` | 773 |
| `agent_tool_display.go` | 722 |
| `agent_subagent_display.go` | 250 |
| `agent_turn_stats.go` | 239 |

- [ ] Create `pkg/cliui/` package
- [ ] Move display files, update imports
- [ ] Build + test green

### Task 2d: Automate CLI Consolidation

The 10 `automate_*.go` files in `cmd/` overlap with `pkg/automate/` (which
already exists with discovery/pid/process management). Assess whether the CLI
layer can be consolidated into `cmd/automate/` subpackage or should stay flat.

- [ ] Audit: what in `cmd/automate_*.go` is CLI wiring vs. logic?
- [ ] Move logic to `pkg/automate/` where appropriate
- [ ] Keep cobra command definitions in `cmd/`
- [ ] Build + test green

---

## Phase 3: `pkg/` Navigation & Documentation (Low Priority)

**Goal:** Improve discoverability without expensive import-path changes.

Go's import system makes physical reorganization (e.g., `pkg/agent/` →
`pkg/agent/core/`) extremely high-churn — every `import` statement in every
file changes. The payoff is navigation, not functionality. This phase improves
navigation without restructuring.

### Tasks

- [ ] Write `docs/PACKAGE_GUIDE.md` documenting:
  - Package groupings (agent, provider/model, code-intelligence, infra, UI)
  - Dependency graph between groups
  - Which packages are public API vs. internal
- [ ] Evaluate `internal/` migration: which `pkg/` packages should be
      `internal/`? (Currently only `internal/hnsw/` exists.) Candidates:
      packages only imported by `cmd/` and `pkg/agent/` — these have no
      external consumers and should be encapsulated.
- [ ] Consider consolidating tiny packages (3-5 files) with natural groupings
- [ ] Add package-level doc comments to the main packages

---

## Success Criteria

- Phase 1: Zero dead test artifacts. `make build-all` + `go test ./...` green.
- Phase 2: `cmd/` reduced by ~12K lines and 4 fewer concern-clusters. All
  extractions are pure moves — zero behavior change. Each extraction verified by
  build + test.
- Phase 3: A developer can locate any package's purpose and dependencies without
  reading source. `internal/` migration started for encapsulation.

## Out of Scope

- Renaming exported symbols or changing public APIs (SP-075 territory)
- Splitting `pkg/agent/` (432 files) — that's a separate, much larger effort
- Web UI reorganization (`webui/`, `packages/`)
- Adding CI lint enforcement for file size (advisory per SP-075)

## Related Specs

- [SP-075](./SP-075-large-file-decomposition.md) — file-level decomposition (same
  principles applied to package-level moves)
- [SP-114](./SP-114-unify-command-execution.md) — command execution unification
  (may interact with workflow extraction)
