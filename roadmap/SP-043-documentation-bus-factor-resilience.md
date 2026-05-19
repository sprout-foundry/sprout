# SP-043: Documentation & Bus-Factor Resilience

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** HIGH (single committer is the #1 structural risk to the project)
**Depends on:** None
**Related:** SP-042 (Self-Review Quality Gates), SP-044 (Roadmap Triage), all SP-XXX spec docs

## Problem

`git shortlog -sne --all` confirms a single committer. 1627+ commits as `Alan Price <alan@inicion.com>` (plus 356 + 176 + 75 + 61 commits under name variants of the same person, plus GitHub Action / Smoke Test / `auto` bot commits). Effective bus factor: **1**.

Sprout has substantial documentation already — `docs/` has 15 files including `ARCHITECTURE.md`, `CLI_REFERENCE.md`, `CONFIGURATION.md`, `MCP_INTEGRATION.md`, `AGENT_WORKFLOW.md`, etc. `CLAUDE.md` and `AGENTS.md` are well-maintained. The roadmap is excellent. **But** the docs are written for users of sprout, not for engineers who would inherit it.

### Concrete gaps that make handoff impossible

1. **Stale README**. `README.md:61` says `cd ledit` and line 72 invokes `ledit` as the binary name. The project was renamed; the front-door doc didn't get the memo. A new contributor following the README cannot reproduce the build.

2. **No "Day 1 / Week 1 / Month 1" onboarding path**. `CONTRIBUTING.md` (142 lines) is process-focused (style, tests). There is no "if you've never seen this codebase, here is how to ramp up" sequence. A new engineer doesn't know which directory to read first, which spec is mission-critical, which tests they should run to convince themselves the build is healthy.

3. **No Architecture Decision Records (ADRs).** Why is there a `pkg/wasmshell` *and* `packages/ui` *and* `webui` *and* an Electron desktop? Why did the project move from `ledit` to `sprout`? Why is the persona model the way it is? Why scoped instances at 56001 vs daemon at 56000? These decisions are in the author's head and (some) in git history. ADRs capture the *why* so a successor can revisit them with context.

4. **No "where does X live?" index.** `docs/ARCHITECTURE.md` is a 1000-foot-view; it doesn't say "if you want to add a tool, look at SP-038; if you want to debug an MCP connection, start in `pkg/mcp/client.go:147` retry logic; if you want to understand persona resolution, start at `pkg/configuration/config.go:1408`." A successor needs the *physical* map, not just the logical one.

5. **No documented build / debug recipes.** `make build-all` is the headline build command (per `CLAUDE.md`), but what about: how to attach a debugger to the daemon? how to replay the last LLM request via `replay_last_request.sh` (it exists at repo root)? how to inspect the embedding store? how to verify MCP connections? Each is in someone's tacit knowledge.

6. **No "what's running" map for production.** A user with a running daemon sees `sprout` processes, possibly LSP children, possibly MCP children. There's no `ps` / `ss` / `lsof` cheat sheet.

7. **Foundry coupling is undocumented.** The frontend hardcodes Foundry endpoints (per the original audit). What is Foundry? Where is it? Is sprout independently usable without it? A successor cannot answer.

### Why this matters

- **Acute risk**: if the author is unavailable for two weeks, no one can ship a critical patch.
- **Chronic risk**: a maintainer joining the project gives up after a week because the ramp is unsustainably steep.
- **Recruiting risk**: contributors won't try if the entry path is unclear.

### What this spec is *not*

This is not "write a CLAUDE.md and call it done." `CLAUDE.md` is already excellent. The gap is the **human-readable** counterpart: documents that a new engineer can read in order, on day one, and walk away knowing what to do next.

## Goals / Non-Goals

**Goals**
- A new engineer can follow a documented Day 1 → Week 1 → Month 1 path and ship a meaningful contribution within Month 1.
- Every major architectural decision (and rename, package split, dual-binary choice, etc.) has an ADR.
- The README is accurate and minimal — entry point only, links to specific docs for everything else.
- There is a single "where does X live?" index keyed by user task ("add a tool", "debug MCP", "understand personas", "run the daemon").
- Build, debug, and replay recipes are documented and verified end-to-end.
- The "second contributor test": someone non-author follows the docs, files issues for every place the docs are wrong, and the issues are resolved.

**Non-Goals**
- Recruiting actual co-maintainers (a process problem, not a docs problem).
- Reorganizing `docs/` from scratch — incremental additions.
- Documenting every line of code. The goal is conceptual ramps, not exhaustive coverage.
- Migrating documentation to a separate site (Docusaurus, etc.). Markdown in `docs/` is sufficient.

## Current State

| Doc | Path | Status |
|-----|------|--------|
| README | `README.md` | Stale (line 61 `cd ledit`, line 72 `ledit` binary name) |
| Contributor guide | `CONTRIBUTING.md` (142 lines) | Process-focused; no onboarding ramp |
| Agent contract | `AGENTS.md` (265 lines) | Good — for agents, not new humans |
| Working agreement | `CLAUDE.md` | Good — for the assistant, not new humans |
| Architecture | `docs/ARCHITECTURE.md` | 1000-foot view; no physical map |
| CLI reference | `docs/CLI_REFERENCE.md` | Reference-style, not learning-style |
| Other guides | `docs/*.md` (12 more) | Domain-specific |
| ADRs | (none) | No decision history |
| Onboarding | (none) | No new-contributor path |
| Where-to-look index | (none) | No task-keyed map |
| Build/debug recipes | (scattered) | `replay_last_request.sh` exists, undocumented |

## Proposed Solution

### Track A — Fix the README first

A1. **Replace `ledit` references**. `README.md:61` (`cd ledit` → `cd sprout`), `README.md:72` (`ledit` → `sprout`). Audit the whole README for any other stale name.

A2. **Re-validate every command in the README.** Run each one against a fresh clone in a sandbox; fix anything that doesn't work.

A3. **Trim the README to entry-point status.** It should answer: what is sprout, how do I install it, how do I run my first command, where do I go from here. Anything more goes into `docs/`.

A4. **Update the disclaimer.** The "use at your own risk, ideally in a container" line should reference `docs/DEPLOYMENT.md` (SP-040) and `docs/SANDBOX_THREAT_MODEL.md` (SP-041) once those land. Until then, soften the rationale.

### Track B — Onboarding ramp

B1. **`docs/ONBOARDING.md`** with three sections:
  - **Day 1**: clone, build, run, fire one query in the Web UI, find your way around `pkg/agent/` and `webui/src/`. Goal: confidence that the build works and you know where the code lives.
  - **Week 1**: read these specific docs in this order. Run these specific tests. Make this specific tiny change (e.g., adjust a string in the WebUI footer) and ship it. Goal: a merged PR that touches the loop end-to-end.
  - **Month 1**: pick one open spec from `roadmap/` (suggestions provided), scope a sub-phase, implement it under guidance. Goal: a substantive contribution and familiarity with the spec process.

B2. **Tested onboarding.** Run the onboarding doc on a fresh environment (a clean VM or container) end-to-end; record every place it fails; fix the doc.

B3. **Mentorship contract.** If no second human exists yet, document the "ask an AI" path: which prompts to use, which logs to share, how to find the right roadmap spec for a given symptom. (`AGENTS.md` partially covers this; `ONBOARDING.md` should reference it.)

### Track C — Architecture Decision Records

C1. **`docs/adr/` directory** with a README explaining the ADR format. Use the standard form: Context, Decision, Status, Consequences.

C2. **Backfill the most important ADRs.** Suggested list:
  - ADR-001: Why two run modes (scoped instance vs daemon, ports 56001+ vs 56000).
  - ADR-002: Why two UI packages (`packages/ui` library vs `webui` app) — pending SP-039 resolution.
  - ADR-003: Why an Electron desktop wrapper.
  - ADR-004: Why a WASM target (`pkg/wasmshell`, `cmd/wasm`).
  - ADR-005: Why the persona model the way it is (catalog → config → session layering).
  - ADR-006: Why multi-provider (vs Claude-only).
  - ADR-007: Why semantic memory uses time-decay scoring (SP-027).
  - ADR-008: Why MCP over a custom protocol.
  - ADR-009: Why the `ledit` → `sprout` rename and what is still pending cleanup.
  - ADR-010: Why Foundry exists, what it does, what sprout assumes about it.

C3. **Process for new ADRs.** Every roadmap spec that makes a structural decision should land an ADR alongside its implementation.

### Track D — "Where does X live?" index

D1. **`docs/CODE_MAP.md`** keyed by user intent:
  - "I want to add a new tool" → SP-038, `pkg/agent_tools/handler.go` (after SP-038 lands), today: `pkg/agent/tool_definitions.go` + the right `tool_handlers_*.go`.
  - "I want to debug MCP" → `pkg/mcp/client.go`, restart logic at line 147, SP-033 4a for backoff details.
  - "I want to understand personas" → `pkg/personas/`, `pkg/configuration/config.go:1408`, SP-026, SP-035.
  - "I want to add a new LLM provider" → `pkg/agent_providers/`, `pkg/agent_api/interface.go`.
  - "I want to add a slash command" → `pkg/agent_commands/`.
  - "I want to understand the agent loop" → `pkg/agent/conversation.go:ProcessQuery`, `pkg/agent/seed_integration.go` multi-turn loop.
  - "I want to write a new persona" → `pkg/personas/configs/*.json`, SP-035 docs (when landed).
  - "I want to inspect persisted state" → `~/.config/sprout/`, `~/.sprout/`, `.sprout/`.
  - "I want to replay the last LLM request" → `replay_last_request.sh` at repo root.
  - "I want to understand WebUI events" → `pkg/events/`, `pkg/webui/websocket_message_types.go`, SP-034.

D2. **Maintenance.** Every spec listing an entry point in CODE_MAP gets a link in the spec. Drift is caught by SP-042's link-check (if added to the lint suite).

### Track E — Build, debug, replay recipes

E1. **`docs/DEBUG_RECIPES.md`**:
  - Attach a debugger (delve / VS Code) to the agent and daemon.
  - Replay the last LLM request via `replay_last_request.sh`.
  - Inspect the embedding store (`~/.config/sprout/embeddings/conversation_turns.jsonl`).
  - Verify MCP connections (list active servers, check health, see message buffer).
  - Inspect runlogs (`~/.sprout/runlogs/*.jsonl`).
  - Trace a tool call from WebUI click through the agent loop to LLM and back.
  - Identify the dominant cost of a session (which tool calls, which token counts).

E2. **`docs/BUILD_RECIPES.md`** (if `Makefile` comments are insufficient):
  - Build for current platform.
  - Build for cross-platform release.
  - Build WASM target.
  - Build Electron desktop bundle.
  - Run integration / e2e / smoke test suites with explanation of what each covers.

E3. **Validate every recipe end-to-end.** Same as Track B — these get tested by the second contributor.

### Track F — Foundry/cloud-mode clarification

F1. **`docs/FOUNDRY.md`** (or a section in `docs/ARCHITECTURE.md`):
  - What Foundry is (the SaaS / platform sibling).
  - Which sprout features depend on Foundry (billing, team management, admin UIs).
  - Whether sprout is independently usable (yes — local mode); what breaks without Foundry (cloud features).
  - How `cloudAdapter` routes between WASM-local and Foundry endpoints.
  - Cross-link to SP-015 (Cloud Platform Integration).

### Track G — The "second contributor test"

G1. **Invite one person** (LLM agent, friend, prospective contributor) to follow `ONBOARDING.md` cold.

G2. **File an issue for every place they got stuck.** Treat each as a doc bug.

G3. **Iterate** until the onboarding sequence runs clean.

G4. **Re-run periodically.** Whenever the architecture moves materially (a new top-level package, a renamed binary, a removed feature), re-validate.

## Implementation Phases

### Phase 1: Stop the bleeding — fix the README
[ ] SP-043-1a: Replace `cd ledit` at `README.md:61` with `cd sprout`. Replace `ledit` at `README.md:72` with `sprout`. Audit the entire README for any other stale name references.
[ ] SP-043-1b: Run each command in the README against a fresh clone; fix anything that doesn't work.
[ ] SP-043-1c: Trim the README to entry-point status; move detail to linked `docs/`.

### Phase 2: ADR backfill
[ ] SP-043-2a: Create `docs/adr/` with a README explaining the format (Context / Decision / Status / Consequences).
[ ] SP-043-2b: Write ADR-001 through ADR-010 from the backlog above. One PR per ADR is fine; bundle if convenient.
[ ] SP-043-2c: Add a "Process for new ADRs" subsection to `CONTRIBUTING.md`.

### Phase 3: Code map
[ ] SP-043-3a: Write `docs/CODE_MAP.md` with the user-intent-keyed table.
[ ] SP-043-3b: Add bidirectional links between CODE_MAP and the relevant roadmap specs.

### Phase 4: Onboarding ramp
[ ] SP-043-4a: Write `docs/ONBOARDING.md` Day 1 / Week 1 / Month 1 sections.
[ ] SP-043-4b: Add an "Open specs suitable for new contributors" section listing 3-5 specs with scoped sub-phases.

### Phase 5: Recipes
[ ] SP-043-5a: Write `docs/DEBUG_RECIPES.md` (debugger attach, replay, embedding store, MCP, runlogs, trace).
[ ] SP-043-5b: Write `docs/BUILD_RECIPES.md` if Makefile is insufficient as a reference.

### Phase 6: Foundry clarification
[ ] SP-043-6a: Write `docs/FOUNDRY.md` or expand `docs/ARCHITECTURE.md`'s Foundry section.
[ ] SP-043-6b: Cross-link from `README.md` and `docs/DEPLOYMENT.md` (SP-040 dependency).

### Phase 7: Second contributor test
[ ] SP-043-7a: Identify a tester (LLM agent run cold, or a human reviewer).
[ ] SP-043-7b: Run them through `ONBOARDING.md` end-to-end; capture every stuck point as an issue.
[ ] SP-043-7c: Fix every issue. Re-run until clean.
[ ] SP-043-7d: Document the result; commit to re-running quarterly or whenever the architecture moves.

## Success Criteria

| Metric | Target |
|--------|--------|
| Stale `ledit` references in `README.md` | 0 |
| `docs/ONBOARDING.md` exists with Day 1 / Week 1 / Month 1 | Yes |
| ADRs in `docs/adr/` | ≥ 10 |
| `docs/CODE_MAP.md` covers the user-intent table | Yes |
| Second contributor test pass | First test surfaces issues; second test runs clean |
| Every spec landed after this references its ADR (when relevant) | Yes |

## Files Reference

| File | Action |
|------|--------|
| `README.md` | Modify: fix `ledit` references, trim to entry-point |
| `docs/ONBOARDING.md` | Create |
| `docs/CODE_MAP.md` | Create |
| `docs/DEBUG_RECIPES.md` | Create |
| `docs/BUILD_RECIPES.md` | Create (or merge into Makefile docstrings) |
| `docs/FOUNDRY.md` | Create (or merge into `docs/ARCHITECTURE.md`) |
| `docs/adr/README.md` | Create: ADR format explainer |
| `docs/adr/ADR-001-run-modes.md` through `ADR-010-foundry.md` | Create: backfill |
| `CONTRIBUTING.md` | Modify: add ADR process; cross-link ONBOARDING and CODE_MAP |

## Risks

- **Docs drift faster than code.** Every architectural change must remember to update the ADRs and CODE_MAP. Mitigation: SP-042's lint suite can include a link-check; each spec template includes "Doc updates required" as a checkbox.
- **Author writing for themselves.** Onboarding docs written by the only contributor reproduce blind spots. Mitigation: the second-contributor test is the explicit forcing function.
- **ADR sprawl.** 10 ADRs today, 50 ADRs in a year. Mitigation: ADR README has a "When to write one" rubric (structural decisions only, not feature-level choices).
- **Time investment with no immediate user-visible value.** Mitigation: the first PR (README fix) is high-leverage and small; subsequent docs unblock contributors that the project will need.
- **Foundry doc reveals coupling that should be removed.** That's a feature, not a risk — surface the coupling so it can be addressed (likely via SP-040).
