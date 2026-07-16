# SP-075: Large-File Decomposition — Bring the Worst Offenders Toward 500 Lines

**Status:** ⚠️ In Progress — Phase 1 (config + cmd) and Phase 2 (agent core) substantially shipped 2026-06; Phase 3 (providers + web) shipped for several files. Original 2833-line `config.go` reduced to ~396 lines via 12 domain files; `agent_workflow.go` 1519→3 lines (fully extracted); `tool_handlers_subagent.go` 1568→41 lines; `seed_integration.go` 1124→29 lines. Remaining files still over 600-line target tracked below.

**Current worst offenders (post-extraction):**

| File | Lines | Notes |
|---|---|---|
| `pkg/agent_tools/repo_map.go` | 1500 | Code intelligence graph generation (SP-107) |
| `pkg/webui/websocket_handler.go` | 1324 | WebSocket connection loop + event dispatch |
| `pkg/console/markdown_formatter.go` | 1217 | Markdown-to-terminal rendering + table parsing |
| `pkg/webui/chat_sessions_api.go` | 1086 | Chat session REST API (list/create/update/delete) |
| `pkg/configuration/config_risk_subagent.go` | 1035 | Risk-profile-driven config generation + rules |
| `pkg/configuration/manager.go` | 949 | Config loading, layered dirs, API key management |
| `pkg/agent/seed_tool_registry.go` | 926 | Tool registry initialization + rich event publishing |
| `pkg/agent_api/ollama_local.go` | 924 | Local Ollama API adapter (list/chat/stream) |
| `pkg/filediscovery/filediscovery.go` | 897 | Workspace file discovery, shell search, reranking |
| `pkg/webui/ssh_launch.go` | 896 | SSH workspace remote launch + process management |
| `pkg/console/input_core.go` | 892 | Console input processing (was 1264, partial extract) |
| `pkg/agent/agent_getters.go` | 890 | Agent accessor methods (Get* / Set*) |

Next phase: continue extracting these remaining offenders toward the 600-line target.

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
