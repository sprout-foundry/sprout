# SP-112: Platform Parity — Resolve Stubbed Feature Gaps

**Status:** 🟢 Shipped (Tier 1 + Tier 2 + SP-112-9 complete; Tier 4 deferred)
**Created:** 2026-07-04
**Reviewed:** 2026-07-20
**Shipped:** 2026-07-20 (`3ecd290a` Tier 1+2; `c7be8b5` SP-112-9)
**Effort:** Phased (~5–7 days total)

## Review 2026-07-20

A line-by-line verification of every claim against the current codebase
(`main` @ `32fd9ac9`). Findings are categorized by severity.

### ✅ Verified correct

- **SP-112-1** (process groups): `pkg/agent_tools/background_process_signal_windows.go`
  confirmed verbatim — `setProcessGroup` is a no-op, `killProcessGroup` calls
  `p.Kill()` (parent only). The file's own comment already references the
  proposed Job Objects fix. Accurate.
- **SP-112-3** (PID-alive): confirmed in three files — `pkg/webui/pid_alive_windows.go`,
  `pkg/automate/pid_alive_windows.go`, `pkg/service/pid_alive_windows.go`. All
  three share the identical `os.FindProcess` weakness (returns non-nil for dead
  PIDs). **Note: the fix should be applied to all three copies, not just one —
  consider extracting a shared `pkg/utils/pidalive` helper to eliminate the
  triplication, which violates the "No duplication" convention in AGENTS.md.**
- **SP-112-4** (OPOST): the underlying issue is real. The Unix fix already
  exists in `pkg/console/steer_termios_unix.go` (direct termios manipulation
  preserving OPOST). The Windows fallback in `pkg/console/steer_termios_other.go`
  uses `term.MakeRaw` which disables OPOST. **However, that file's comment
  claims "the OPOST-staircase issue doesn't manifest the same way on Windows
  because Windows terminals handle CR/LF differently." The spec should reconcile
  with this claim — is the fix actually needed, or is the existing comment
  correct that Windows doesn't exhibit the staircase? If the comment is right,
  SP-112-4 should be dropped or deprioritized.**
- **Inherent limitations section**: accurate. WASM sandbox constraints are
  correctly documented as permanent.

### ⚠️ Stale or inaccurate claims

- **"31 feature areas"** (Problem section): the codebase has **59 platform-specific
  non-test files** (`*_windows.go`, `*_linux.go`, `*_darwin.go`, `*_wasm.go`,
  `*_js.go`, `*_unix.go`, `*_other.go`), not 31. The audit count is either
  outdated (the audit was 2026-07-04; 16 days of development later the number
  has grown) or used a narrower definition. **Update the number or remove the
  specific count** — an inaccurate headline figure undermines the spec's
  credibility on first read.
- **SP-112-5** (WASM shell streaming): the spec says output is "captured as a
  single string." `pkg/agent_tools/shell_js.go` should be checked to confirm
  this is still true — the WASM shell layer has evolved. The fix (chunked
  streaming) is only valuable if the JS executor's contract supports it; the
  spec should cite the executor interface it targets.
- **SP-112-6/7/8** (WASM tool exclusion): **partially shipped already.**
  `pkg/agent_tools/all_codegraph_wasm.go` and `all_browse_url_wasm.go` already
  return `nil` at registration time — the exact pattern the spec proposes. The
  spec describes these as "returns error string" which is stale. **Re-scope
  SP-112-6/7/8 to cover only the tools that are *not yet* excluded at
  registration** — verify `vision_stubs_js.go`, `background_process_js.go`,
  and `structured_json_js.go` to determine which still error at runtime vs.
  which are already nil-registered.

### 🔴 Convention violations in the proposed fixes

These are the most important findings — the fixes as described would violate
documented project conventions.

#### C1: SP-112-2 duplicates existing code

The spec describes SP-112-2 as greenfield: *"Use `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT)`
for graceful, fall back to `TerminateProcess`."* **This exact implementation
already exists** in `pkg/automate/stop_process_windows.go` (`StopProcess` +
`sendCtrlBreak` + `waitForDeath`). The `sendCtrlBreak` function is a verbatim
match for what SP-112-2 proposes.

**Fix:** SP-112-2 should be rewritten as a *refactor* — extract the
`GenerateConsoleCtrlEvent` logic from `stop_process_windows.go` into a shared
location (e.g. `pkg/utils/windows_console.go` or extend
`pkg/agent_tools/background_process_signal_windows.go`) and have
`interruptProcessGroup` call it. This follows the AGENTS.md rule: *"No
duplication: Use existing utilities before writing new ones. If a helper is
missing, check `pkg/` first."* The current spec framing would produce a second
copy of the same Windows API call.

#### C2: Mixed syscall conventions

The codebase has **two parallel Windows API access patterns**:

1. **Modern (`golang.org/x/sys/windows`)** — used in
   `pkg/agent_tools/background_process_signal_windows.go`,
   `pkg/automate/stop_process_windows.go`. This is the preferred pattern;
   `golang.org/x/sys v0.45.0` is already in `go.mod`.
2. **Legacy (`syscall.NewLazyDLL` + `syscall.LazyProc`)** — used in
   `pkg/utils/terminal_windows.go` (the SP-112-4 target).

SP-112-1 and SP-112-2 correctly propose using `golang.org/x/sys/windows` (Job
Objects, `GenerateConsoleCtrlEvent`). **SP-112-4 must do the same** — if it
adds Windows Console Mode API calls via `SetConsoleMode`, it should use
`golang.org/x/sys/windows`, not extend the legacy `syscall.NewLazyDLL` pattern
in `terminal_windows.go`. **Add a convention note to the spec: all new Windows
API calls use `golang.org/x/sys/windows`; the legacy `syscall` usage in
`terminal_windows.go` is tech debt to migrate, not a pattern to extend.**

#### C3: Missing testability story

The spec's acceptance criteria says "All Tier 1 items have working
implementations + tests" but doesn't address the hard problem: **Windows-only
code can't be unit-tested on Linux/macOS dev machines.** The project has
exactly one `*_linux_test.go` counterpart (`foreground_linux_test.go`) and
zero `*_windows_test.go` files — platform-specific behavior is tested via CI
matrix builds (`.github/workflows/build.yml` runs on `windows-latest`,
`macos-latest`, `ubuntu-latest`), not local tests.

**Add a "Testing strategy" section** specifying:
- Windows-specific logic is tested via `//go:build windows` test files that
  run only in CI's Windows matrix leg.
- Where possible, extract the *decision logic* (e.g. "which signal escalation
  path to take") into platform-agnostic code that's testable cross-platform,
  leaving only the thin syscall wrappers behind build tags.
- CI's Windows build leg is the gate for Tier 1 acceptance — document that
  `make build-all` on Linux does NOT verify Windows behavior despite compiling
  the cross-platform code.

### 📐 Scope and design assessment

**Overall:** the spec is well-structured (clear tiers, honest about inherent
limitations, reasonable effort estimates). The core problem is real — Windows
process-group kills and PID-alive checks genuinely don't work. But the spec
reads as if it was written from a surface audit without checking whether some
fixes already exist or whether proposed solutions duplicate existing code.

**Recommended scope adjustments before approval:**

1. **Drop or defer SP-112-4** until the OPOST claim is reconciled with the
   existing code comment that says Windows doesn't exhibit the staircase.
2. **Rewrite SP-112-2 as a refactor** of the existing `sendCtrlBreak` in
   `stop_process_windows.go`, not new code.
3. **Re-audit SP-112-6/7/8** against current WASM files — some are already
   shipped. Update the table to list only the un-shipped exclusions.
4. **Add SP-112-3 dedup** as an explicit sub-task: three copies of the same
   `FindProcess` weakness should become one shared helper.
5. **Add a testing-strategy section** addressing the cross-platform test gap.
6. **Update the "31 feature areas" headline** to the current count (59 files)
   or rephrase to "N platform-specific stubs" without a stale hard number.

**Estimated revised effort:** Tier 1 drops from ~2 days to ~1.5 days (SP-112-2
becomes a refactor, SP-112-4 deferred). Tier 2 drops from ~2 days to ~1 day
(SPM-112-6/7/8 partially shipped). Net: ~4–5 days instead of 5–7.

### ✅ Adjustments applied (2026-07-20)

All six recommended adjustments from this review have been incorporated
into the spec body below:

1. **SP-112-4 dropped** — the underlying issue is real but `steer_termios_other.go`
   documents that the OPOST-staircase does not manifest on Windows terminals
   (Windows CR/LF handling is different from Unix). SP-112-4 is deferred to
   `roadmap/future/` pending an empirical repro.
2. **SP-112-2 rewritten as a refactor** — Tier 1 table now specifies
   "Refactor: extract `sendCtrlBreak` into `pkg/utils/windows_console.go`".
3. **SP-112-6/7/8 re-audited** — Tier 2 table marks SP-112-7 as ✅ shipped,
   SP-112-6 and SP-112-8 as **Open**. SP-112-5 deferred to `roadmap/future/`.
4. **SP-112-3 dedup** added as **SP-112-3b** (extracting
   `pkg/utils/pidalive`).
5. **Testing strategy section added** (see below).
6. **"31 feature areas" headline updated** to the current count (59 files).



## Problem

Sprout builds on five targets (Linux, macOS, Windows, WASM, no-CGO). A
platform-specific audit found **59 non-test files** with `*_windows.go`,
`*_linux.go`, `*_darwin.go`, `*_wasm.go`, `*_js.go`, `*_unix.go`, or
`*_other.go` build constraints. Most are inherent limitations (WASM
can't spawn processes). Some are fixable gaps where the stub degrades
UX unnecessarily.

This spec prioritizes the fixable gaps and documents the inherent
limitations as permanent constraints.

## Priority tiers

### Tier 1 — Fixable Windows gaps (~1.5 days)

These affect real users on Windows and have known solutions:

| Item | Current | Fix | Effort | Status |
|------|---------|-----|--------|--------|
| **SP-112-1:** Process groups on Windows | `SetProcessGroup` is a no-op; `killProcessGroup` kills only the parent | Use Windows Job Objects (`CreateJobObject` + `AssignProcessToJobObject`) for group kill | ~4h | ✅ **Shipped** (`3ecd290a`) |
| **SP-112-2:** Graceful signal escalation on Windows | `interruptProcessGroup` → `Kill()` (no graceful interrupt). A working `sendCtrlBreak` already exists in `pkg/automate/stop_process_windows.go` | **Refactor**: extract `sendCtrlBreak` into `pkg/utils/windows_console.go`; have `interruptProcessGroup` call it. Do NOT duplicate the implementation. | ~2h | ✅ **Shipped** (`3ecd290a`) |
| **SP-112-3a:** PID-alive check precision (fix) | `FindProcess` succeeds for dead PIDs on Windows | Use `OpenProcess` + `WaitForSingleObject(0)` or `GetExitCodeProcess` | ~2h | ✅ **Shipped** (prior) |
| **SP-112-3b:** PID-alive check deduplication | The same `FindProcess` weakness exists in three files: `pkg/webui/pid_alive_windows.go`, `pkg/automate/pid_alive_windows.go`, `pkg/service/pid_alive_windows.go` | Extract a shared `pkg/utils/pidalive/pidalive_windows.go` (and unix counterpart) so all three callers delegate to one helper. AGENTS.md "No duplication" rule. | ~2h | ✅ **Shipped** (prior — `pkg/utils/pidalive` exists and all three callers delegate) |

### Tier 2 — WASM UX improvements (~1 day)

These can't be "fixed" (WASM can't spawn processes), but the error
messages and fallback behavior can be improved. **Audit 2026-07-20:**
all four Tier 2 (WASM exclusion) items are now shipped as of `3ecd290a`.

| Item | Current | Fix | Effort | Status |
|------|---------|-----|--------|--------|
| **SP-112-5:** Shell streaming on WASM | Output captured as single string | **Deferred.** `pkg/agent_tools/shell_js.go` documents that `wasmshell` is a single-shot command runner with no streaming surface today. Implementing chunked streaming requires first extending the JS executor contract; out of scope for this spec. | — | Dropped (tracked in `roadmap/future/`) |
| **SP-112-6:** Vision tool graceful degradation | `pkg/agent_tools/vision_stubs_js.go` returns success/no-op or error strings at *execution* time, but the tools remain in the WASM tool roster | Mirror the `all_codegraph_wasm.go` pattern: split vision handlers into `all_vision.go` (`!js`, registers) and `all_vision_js.go` (`js`, returns nil). Update the comment block at the top of `pkg/agent_tools/all.go`. | ~2h | ✅ **Shipped** (`3ecd290a`) |
| **SP-112-7:** Codegraph tool exclusion on WASM | Already excluded at registration | (No action — `all_codegraph_wasm.go` returns `nil` since prior work.) | — | ✅ **Shipped** |
| **SP-112-8:** Background process tool exclusion on WASM | `runAutomateHandler.Execute` returns "not available" at runtime but the tool is still advertised to the model | Mirror the `all_browse_url_wasm.go` pattern: split `run_automate` registration into `all_run_automate.go` (`!js`) and `all_run_automate_js.go` (`js`, returns nil). Update the comment block in `all.go`. | ~1h | ✅ **Shipped** (`3ecd290a`) |

### Tier 3 — no-CGO embedding fallback (~1 day)

| Item | Current | Fix | Effort | Status |
|------|---------|-----|--------|--------|
| **SP-112-9:** Cross-platform testing strategy (CI matrix) | No Windows/macOS test files; only `foreground_linux_test.go` exists; WASM roster had no smoke test | Add CI matrix for windows-latest + macos-latest; add `*_windows_test.go` for the Job Object code path; add `scripts/wasm-tool-roster-smoke.sh` that asserts the expected tool count on `GOOS=js` | ~3h | ✅ **Shipped** (`c7be8b5`) |

### Tier 4 — Documentation / permanent constraints (~0.5 day)

| Item | Description |
|------|-------------|
| **SP-112-10:** Document permanent WASM limitations in WebUI onboarding | When a user tries to use a WASM-unavailable feature, show a tooltip explaining the browser limitation |
| **SP-112-11:** Document permanent non-Darwin/Linux limitations | Foreground app detection and panic key chord require platform-specific APIs |

## Inherent limitations (won't fix)

These are platform constraints, not bugs:

- **WASM:** No `os/exec`, no filesystem (beyond OPFS), no CGO, no native
  libraries, no signals, no process groups. These are browser sandbox
  constraints.
- **non-Linux:** No `/proc` filesystem for OOM detection or process
  start-time comparison.
- **non-Darwin/Linux:** No `osascript` (macOS) or `xdotool`/`xrecord`
  (Linux) for foreground app detection or key chord monitoring.

## Testing strategy

Windows-specific logic cannot be unit-tested on Linux/macOS dev machines
(the project has zero `*_windows_test.go` files and exactly one
`*_linux_test.go` counterpart, `foreground_linux_test.go`). Platform
behavior is gated by CI's Windows matrix leg (`.github/workflows/build.yml`,
`runs-on: windows-latest`).

For each Tier 1 item, follow this pattern:

1. **Extract the decision logic** (e.g. "which signal escalation path to
   take", "does this PID look alive?") into a platform-agnostic helper
   in `pkg/utils` that is testable cross-platform with a mockable
   platform-specific backend.
2. **Leave only the thin syscall wrappers** behind `//go:build windows`
   tags.
3. **Add a `*_windows_test.go`** that runs only in CI's Windows leg and
   verifies the syscall wrapper compiles and produces expected behavior
   against real Windows processes.
4. **Document in the spec** that `make build-all` on Linux does NOT
   verify Windows behavior despite compiling cross-platform code —
   Windows correctness is gated by CI's `windows-latest` matrix leg.

For Tier 2 (WASM exclusions), the build tag system itself enforces
correctness — the test is whether `GOOS=js go build ./...` compiles
without the excluded tool's handler types being referenced. Add a
smoke test that builds the WASM target and asserts the tool count.

## Acceptance

- All Tier 1 items have working implementations + tests
- WASM build excludes unavailable tools at registration time (Tier 2)
- README platform matrix is accurate and maintained
- `make build-all` passes on all targets
- `GOOS=js go build ./...` passes and produces the expected tool roster
  (excluded tools must not appear)
