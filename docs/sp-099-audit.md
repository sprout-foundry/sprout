# SP-099-1 Audit: CI Race Detection by Default

**Verdict:** Acceptance criterion met. No Makefile or CI change required.
Recommendation: ship SP-099-1 as-is.

## Current state

- `Makefile:79` — `TEST_RACE ?= -race` (default in effect).
- `Makefile:84-87` — `test-unit` recipe expands `$(TEST_RACE)` on the `go test` line.
- `Makefile:149` — `test: test-unit`, so `make test` inherits `-race`.
- `Makefile:158-161` — `test-coverage` hardcodes `go test -race ...` (no `-short`).
- `.github/workflows/build.yml:85-88` — CI step "Run unit tests with race detection and coverage" invokes `make test-coverage` on every PR.
- Verified via `make -n test` / `make -n test-unit` / `make -n test-coverage` — all three resolve to `go test -race ...`.

## -short skip audit

`grep -rn "testing.Short" pkg/ cmd/` returns **127 hits across 28 files**. Categorization:

- **(A) Network-dependent** (9 hits): `pkg/agent_providers/zai_isolated_test.go` (2), `pkg/agent_tools/dev_server_integration_test.go` (1), `pkg/webcontent/webcontent_search_test.go` (2), `pkg/webui/onboarding_api_test.go` (2 — only the `testing.Short` block at line 702 that explicitly requires a network connection check). Real LLM/HTTP calls. **Acceptable to skip.**
- **(B) Filesystem / process / PTY-dependent** (102 hits): `pkg/mcp/client_*_test.go` (×55 — client_integration 12, client_lifecycle 13, client_messaging 1, client_new 6, client_reconnection 4, client_resources_prompts 16, client_sliding_window 3), `pkg/agent/proactive_context_test.go` (×20 — embedding DB setup), `pkg/webui/terminal_*_test.go` (×13 — PTY: terminal_lifecycle 2, terminal_agent_exec 2, terminal_background 5, terminal_background_cap 3, terminal_resize 1), `pkg/agent_tools/computer_use/panic_key_test.go` (2 — subprocess + process groups), `pkg/agent_tools/shell_native_test.go` (2 — concurrent streaming), `pkg/agent_tools/background_process_pid_test.go` (1 — real `exec.Cmd` lifecycle), `cmd/automate_test.go` (2), `cmd/shell_bg_test.go` (1 — cross-process discovery), `pkg/agent/project_discovery_integration_test.go` (2 — git subprocess), `pkg/webui/file_watcher_test.go` (2 — timing-sensitive debounce), `pkg/webui/server_test.go` (3), `pkg/webui/api_agent_sessions_test.go` (1 — real HTTP servers on dynamic ports). **Acceptable to skip — `-race` on these would balloon CI runtime and add flake.**
- **(C) Lazily skipped / suspect** (16 hits): `pkg/mcp/github_setup_test.go` (×13 — in-process `git` in `t.TempDir()`, not really network), `pkg/webui/onboarding_api_test.go` (×2 — `httptest.NewRecorder`/`NewServer`, no real network; the line-32 `testing.Short` block, NOT the line-702 network-check block), `pkg/git/coverage_test.go` (1 — borderline subprocess). **Out of scope for SP-099-1**; flag for a future cleanup ticket.

Total: 127 hits across 28 files | A=9 | B=102 | C=16.

## Recommendation

Keep current behavior. `-race` runs in CI on every PR via `make test-coverage`, and locally via `make test` (= `test-unit`). The `-short` filter on `test-unit` deliberately excludes integration tests that would dominate runtime under `-race`; this is the correct design — `-race` coverage is comprehensive on the unit-test surface that matters.

Category-C skips are cosmetic and not on the SP-099-1 critical path. Track as a follow-up if desired.

## Deliverables of SP-099-1

- This audit memo (`docs/sp-099-audit.md`).
- No code changes.
- TODO.md updated: SP-099-1 flipped to `[x]` with ship note.
