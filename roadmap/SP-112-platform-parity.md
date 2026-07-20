# SP-112: Platform Parity — Resolve Stubbed Feature Gaps

**Status:** 🔵 Proposed (under review — see §"Review 2026-07-20" below)
**Created:** 2026-07-04
**Reviewed:** 2026-07-20
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



## Problem

Sprout builds on five targets (Linux, macOS, Windows, WASM, no-CGO). An
audit (2026-07-04) found **31 feature areas** with platform-specific stubs.
Most are inherent limitations (WASM can't spawn processes). Some are
fixable gaps where the stub degrades UX unnecessarily.

This spec prioritizes the fixable gaps and documents the inherent
limitations as permanent constraints.

## Priority tiers

### Tier 1 — Fixable Windows gaps (~2 days)

These affect real users on Windows and have known solutions:

| Item | Current | Fix | Effort |
|------|---------|-----|--------|
| **SP-112-1:** Process groups on Windows | `SetProcessGroup` is a no-op; `killProcessGroup` kills only the parent | Use Windows Job Objects (`CreateJobObject` + `AssignProcessToJobObject`) for group kill | ~4h |
| **SP-112-2:** Graceful signal escalation on Windows | `interruptProcessGroup` → `Kill()` (no graceful interrupt) | Use `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT)` for graceful, fall back to `TerminateProcess` | ~2h |
| **SP-112-3:** PID-alive check precision | `FindProcess` succeeds for dead PIDs on Windows | Use `OpenProcess` + `WaitForSingleObject(0)` or `GetExitCodeProcess` | ~2h |
| **SP-112-4:** Terminal raw mode OPOST preservation | `term.MakeRaw` disables OPOST (staircase rendering) | Replicate the Unix ioctl approach using Windows Console Mode APIs (`SetConsoleMode`) | ~4h |

### Tier 2 — WASM UX improvements (~2 days)

These can't be "fixed" (WASM can't spawn processes), but the error
messages and fallback behavior can be improved:

| Item | Current | Fix | Effort |
|------|---------|-----|--------|
| **SP-112-5:** Shell streaming on WASM | Output captured as single string | Pipe output through the JS executor in chunks if the executor supports streaming | ~4h |
| **SP-112-6:** Vision tool graceful degradation | Returns error string | Detect WASM at registration time and exclude vision tools from the WASM tool roster instead of returning errors | ~2h |
| **SP-112-7:** Codegraph tool exclusion on WASM | Returns error string | Same as SP-112-6 — exclude from WASM tool roster | ~1h |
| **SP-112-8:** Background process tool exclusion on WASM | Returns error string | Same — exclude `run_automate`, background shell tools from WASM | ~1h |

### Tier 3 — no-CGO embedding fallback (~1 day)

| Item | Current | Fix | Effort |
|------|---------|-----|--------|
| **SP-112-9:** Embedding quality without CGO | Hash-based fallback (low quality) | Add a gopkg.in/yaml-based TF-IDF provider as a middle ground between hash and ONNX | ~6h |

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

## Acceptance

- All Tier 1 items have working implementations + tests
- WASM build excludes unavailable tools at registration time (Tier 2)
- README platform matrix is accurate and maintained
- `make build-all` passes on all targets
