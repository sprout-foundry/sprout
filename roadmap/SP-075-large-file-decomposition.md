# SP-075: Large-File Decomposition — Bring the Worst Offenders Toward 500 Lines

**Status:** 📋 Proposed
**Date:** 2026-06-15
**Depends on:** none
**Priority:** Low-Medium (maintainability; no user-facing behavior change)
**Effort Estimate:** ~1 week (incremental, one file at a time)

## Problem

The project's own code-quality rule (AGENTS.md) targets **under 500 lines per
file** with one primary responsibility per file. Reality is far off: **20+
files exceed 800 lines**, several are 3-5× the target. Big files slow
comprehension, make review harder, and concentrate merge conflicts.

### Worst offenders (lines, non-test)

| File | Lines | ~× target |
|---|---|---|
| `pkg/configuration/config.go` | 2833 | 5.7× |
| `cmd/agent_modes.go` | 2344 | 4.7× |
| `pkg/wasmshell/commands.go` | 1633 | 3.3× |
| `pkg/agent/tool_handlers_subagent.go` | 1568 | 3.1× |
| `cmd/agent_workflow.go` | 1519 | 3.0× |
| `pkg/agent_providers/generic_provider.go` | 1449 | 2.9× |
| `pkg/webcontent/browser_rod.go` | 1398 | 2.8× |
| `pkg/agent/change_tracking_shell.go` | 1344 | 2.7× |
| `webui/src/components/Terminal.tsx` | 1320 | 2.6× |
| `pkg/console/input_core.go` | 1264 | 2.5× |
| `pkg/agent/seed_integration.go` | 1124 | 2.2× |
| `pkg/agent/subagent_runner.go` | 1059 | 2.1× |

(Plus ~10 more in the 800-1100 range.)

## Principles

This is **incremental, mechanical-but-careful** refactoring. The risk is
behavior drift, so every step is a pure extraction verified by build + tests
before moving on (AGENTS.md "Incremental refactoring: Build after each
extraction step").

- **Pure moves only** — extract cohesive groups of types/functions into new
  files in the same package; no logic changes.
- **One file per step, one PR-sized unit.** Build + `go test ./<pkg>/...`
  green after each.
- **Split by responsibility, not by line count.** A 600-line file with one
  clear job is fine; a 900-line file doing five things is the target.
- **No churn for its own sake.** Skip files that are large but genuinely
  single-responsibility (e.g. a generated table or one big state machine) —
  note them as accepted exceptions.

## Suggested first cuts (highest value, lowest risk)

| File | Extraction sketch |
|---|---|
| `pkg/configuration/config.go` | Split the nested config structs + their `Resolve()`/defaults into per-domain files (`config_embedding.go`, `config_persistent_context.go`, `config_computer_use.go`, `config_security.go`, …); keep the top-level `Config` + load/save in `config.go`. |
| `cmd/agent_modes.go` | Separate mode entry points (interactive loop, one-shot, workflow, JSON-output) into `agent_mode_*.go`; keep shared setup in one place. |
| `pkg/agent/tool_handlers_subagent.go` | Split spawn/dispatch, result-envelope handling, and depth/allowlist gating into focused files. |
| `pkg/agent_providers/generic_provider.go` | Separate request building, streaming/SSE parsing, retry/backoff, and vision into sibling files behind the same type. |
| `webui/src/components/Terminal.tsx` | Extract the per-pane session model + split/resize logic into hooks (`useTerminalPanes.ts`) — also unblocks SP-011 polish. |

## Phasing

1. **Phase 1 (config + cmd):** `config.go`, `agent_modes.go`, `agent_workflow.go` — the biggest wins, low coupling.
2. **Phase 2 (agent core):** `tool_handlers_subagent.go`, `seed_integration.go`, `subagent_runner.go`, `change_tracking_shell.go`.
3. **Phase 3 (providers + web):** `generic_provider.go`, `browser_rod.go`, `wasmshell/commands.go`, `Terminal.tsx`, `input_core.go`.

Each file is its own checklist item; partial completion is fine and useful.

## Success Criteria

- Each targeted file is either under ~600 lines or documented as an accepted
  single-responsibility exception.
- Zero behavior change: `make build-all` + `go test ./...` green after every
  extraction; no diff to public APIs beyond file moves.
- `find . -name '*.go' | xargs wc -l | awk '$1>1200'` (non-test) shrinks
  substantially.

## Out of Scope

- Rewriting logic, renaming exported symbols, or changing package boundaries —
  pure file-level extraction only.
- Test files (they have different size norms).

## Open Questions

1. Do we enforce the 500-line target going forward with a CI line-count lint,
   or keep it advisory? Recommendation: advisory now, revisit a soft CI warning
   after Phase 1 proves the target is realistic.
