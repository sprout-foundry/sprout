# SP-112: Platform Parity — Resolve Stubbed Feature Gaps

**Status:** 🔵 Proposed
**Created:** 2026-07-04
**Effort:** Phased (~5–7 days total)

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
