# SP-030: Repository Hygiene — Stale Artifacts & Predecessor Cleanup

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** Medium
**Depends on:** None
**Related:** SP-028 (test runners are touched there too)

## Problem

The working tree is cluttered with stale build/test outputs and the `ledit → sprout` rename is still partially complete. None of this breaks the product, but it makes the repo confusing for new contributors and slowly accretes more cruft if left alone.

### What's actually wrong

1. **Stale compiled binaries on disk** (gitignored, not tracked — confirmed via `git ls-files`):
   - `agent.test` (28MB, May 13), `configuration.test` (12MB, May 11), `proxy.test` (10MB, May 11), `semantic.test` (6MB, May 11)
   - `sprout` (113MB, current build)
   - Total: ~175MB of disk junk in the project root
2. **Stale dev-debug outputs** (also gitignored):
   - `code_review_output.json` (last run May 11)
   - `examples/.todo_pipeline_checkpoint.json` (recent; in-flight working state but committed accidentally to disk in an `examples/` dir)
3. **Dead one-off scripts** referencing the predecessor `ledit` binary:
   - `update_and_test.sh` — entire script invokes a `./ledit` binary that no longer exists; was a one-time alt-screen smoke test
4. **Live scripts with stale help/docstrings**:
   - `replay_last_request.sh` help text says "Replay the last request that ledit sent…" and references `LEDIT_COPY_LOGS_TO_CWD` (the script itself is still useful — just wording needs an update)
   - `test_runner.py` docstring: "performs a robust test of the \`ledit\` workspace functionality"
   - `e2e_test_runner.py` / `integration_test_runner.py` docstrings: "Test Runner for ledit"
5. **Confusing test-runner trio.** `test_runner.py`, `e2e_test_runner.py`, `integration_test_runner.py` actually drive three different test suites (`testing/`, `e2e_tests/`, `integration_tests/`), but the names suggest redundancy. New contributors don't know which to run. `CLAUDE.md` only points at `test_runner.py`.
6. **Source-code `ledit` references** (~20 files). Some are valid historical fixtures inside test data; some are live references in prompts, skills, docs that should say `sprout`:
   - `pkg/agent/prompts/system_prompt.md`
   - `pkg/agent/skills/go-conventions/SKILL.md`
   - `docs/ELECTRON.md`, `docs/AGENT_WORKFLOW.md`, `docs/PROVIDER_CATALOG.md`, `docs/TESTING.md`, `docs/PRODUCT_BACKLOG.md`, `docs/subagent_personas.md`
   - `cmd/service_darwin_test.go`, `cmd/service_linux_test.go` (verify whether intentional)
   - `pkg/agent/conversation_image_test.go`, `pkg/agent/tool_handlers_search_new_test.go`, `pkg/history/history_tools_test.go`, `pkg/git/commit_helpers_test.go` (verify whether intentional)
   - `CHANGELOG.md`, `README.md`
7. **`.ledit/` directory at repo root** — not tracked in git (in `.gitignore`), but still on disk from the predecessor tool. Confuses `ls`.

## Goals / Non-Goals

**Goals**
- Repo root contains only files a contributor should look at. Build artifacts live in `dist/` or are excluded.
- Every `ledit` string in shipped artifacts (prompts, skills, docs) becomes `sprout` or is intentional and labelled as such.
- The three test runners have unambiguous names and `CLAUDE.md` describes when to use each.
- Stale dev scripts are either deleted or have current docstrings.

**Non-Goals**
- Migrating `TODO.md`/`TODO-DONE.md` to GitHub Issues (separate decision the team has already made — see SP-027 / TODO.md convention).
- Renaming the `.sprout/` directory (deliberate; mirrors `~/.sprout/`).
- Restructuring the `examples/` directory.
- Adding cleanup automation beyond a single `make clean` target.

## Inventory

### A. Disk-only artifacts to remove (one-shot cleanup)

| Path | Size | Action | Tracked? |
|------|------|--------|----------|
| `agent.test` | 28MB | Delete | No (matches `*.test`) |
| `configuration.test` | 12MB | Delete | No |
| `proxy.test` | 10MB | Delete | No |
| `semantic.test` | 6MB | Delete | No |
| `sprout` (binary at root) | 113MB | Move to `dist/local/` (existing Makefile target output) or delete; build always rebuilds | No (matches `sprout`) |
| `code_review_output.json` | 7KB | Delete | No (already in `.gitignore`) |
| `.ledit/` directory | varies | Delete | No |

### B. Files to keep but update

| Path | Action |
|------|--------|
| `replay_last_request.sh` | Replace `ledit`→`sprout` in help/docstring; rename `LEDIT_COPY_LOGS_TO_CWD` env var reference to `SPROUT_COPY_LOGS_TO_CWD` (check actual var name used by sprout) |
| `test_runner.py` | Update docstring; rename to `workspace_test_runner.py` to match its actual purpose |
| `e2e_test_runner.py` | Update docstring; keep name |
| `integration_test_runner.py` | Update docstring; keep name |
| `pkg/agent/prompts/system_prompt.md` | Replace `ledit`→`sprout` |
| `pkg/agent/skills/go-conventions/SKILL.md` | Replace `ledit`→`sprout` |
| `docs/ELECTRON.md`, `docs/AGENT_WORKFLOW.md`, `docs/PROVIDER_CATALOG.md`, `docs/TESTING.md`, `docs/PRODUCT_BACKLOG.md`, `docs/subagent_personas.md`, `README.md`, `CHANGELOG.md` | Audit each occurrence; replace where current, leave inside fenced historical sections of `CHANGELOG.md` |
| `cmd/service_darwin_test.go`, `cmd/service_linux_test.go` | Audit — these may be testing service-name strings that *must* stay `ledit` for backwards compatibility (if a `launchd`/`systemd` unit file in the wild still references it). Decide per file |
| `pkg/agent/conversation_image_test.go`, `pkg/agent/tool_handlers_search_new_test.go`, `pkg/git/commit_helpers_test.go`, `pkg/history/history_tools_test.go` | Audit — likely fixture data; replace if not load-bearing |
| `examples/.todo_pipeline_checkpoint.json` | Add `examples/.todo_pipeline_checkpoint.json` to `.gitignore` (currently appears as untracked in `git status`). Move to runtime dir (e.g. `.sprout/`) so it stops surfacing in examples/ |

### C. Files to delete

| Path | Reason |
|------|--------|
| `update_and_test.sh` | One-shot ledit-era alt-screen smoke test; entire body references `./ledit` binary; no current value |

### D. Makefile

Add a `clean` target (or extend the existing one) that removes:
- `*.test` in repo root
- `./sprout` binary in repo root
- `code_review_output.json`
- Build outputs in `dist/local/` and `dist/cloud/`

So future hygiene is a one-command operation.

## Backwards-compatibility note: service names

`cmd/service_darwin_test.go` and `cmd/service_linux_test.go` may reference `ledit` because the production service installer once registered units under that label. Renaming would break upgrades for anyone with an installed daemon. **Before changing service-name strings, search for service install/uninstall logic, decide on a migration story** (e.g. uninstall old + install new on upgrade), and either:

1. Keep `ledit` as the on-disk service name, document it, and only update docstrings; **or**
2. Add a one-time migration that renames the installed service and bumps a config version.

This is not a string-replace decision — it's a product decision. The audit step decides which path applies.

## Implementation Phases

### Phase 1: One-shot cleanup (Day 1)
- [ ] Delete stale `*.test` binaries and root-level `sprout` binary (or move to `dist/local/`)
- [ ] Delete `code_review_output.json`
- [ ] Delete `.ledit/` directory
- [ ] Delete `update_and_test.sh`
- [ ] Add `examples/.todo_pipeline_checkpoint.json` to `.gitignore` and relocate to `.sprout/`
- [ ] Add `make clean` target that removes the above class of files

### Phase 2: Docstring/prompt updates (Day 2)
- [ ] Update `replay_last_request.sh` help text and env var references
- [ ] Update `test_runner.py`, `e2e_test_runner.py`, `integration_test_runner.py` docstrings
- [ ] Rename `test_runner.py` → `workspace_test_runner.py`
- [ ] Update `CLAUDE.md` (project root) to describe what each runner is for
- [ ] Update `pkg/agent/prompts/system_prompt.md` and `pkg/agent/skills/go-conventions/SKILL.md`

### Phase 3: Documentation sweep (Day 3)
- [ ] Per-file audit and update of `docs/*.md` `ledit` references
- [ ] Update `README.md` where `ledit` appears outside historical sections

### Phase 4: Decide-then-act on service names (Day 4)
- [ ] Audit `cmd/service_*` code paths and decide whether service identifier stays `ledit`
- [ ] Either keep + document, or implement a migration

### Phase 5: Test fixtures (Day 5)
- [ ] Audit `pkg/agent/*_test.go`, `pkg/git/*_test.go`, `pkg/history/*_test.go` `ledit` references
- [ ] Replace where not load-bearing; leave where the literal string is being tested

## Success Criteria

| Metric | Target |
|--------|--------|
| `ls` in repo root | Only source dirs + canonical project files (no `.test` binaries, no `sprout`, no `code_review_output.json`, no `.ledit/`) |
| `grep -r ledit .` in shipped artifacts (prompts, skills, docs) | 0 unintentional matches (intentional ones are explicitly commented) |
| `git status` after a fresh build | Clean (no `.todo_pipeline_checkpoint.json` showing up) |
| `make clean` | Removes all build/test artifacts |
| `CLAUDE.md` | Names each test runner and its scope |

## Risks

- **Service-name rename breaks installed daemons.** This is the one place where a string replace would silently break users. The Phase 4 audit-then-decide step is non-negotiable.
- **Deleting `.ledit/`** may delete state the user wanted. Mitigation: this is project-local config from the predecessor tool; if a user genuinely had data there they can recover from git history of the predecessor. Worth a single grep first to make sure nothing references it.
- **Test fixture changes that look cosmetic but matter.** Some tests assert string output that includes the binary name. Mitigation: per-file audit (Phase 5), not blanket replace.

## Files Reference

| File | Action |
|------|--------|
| `agent.test`, `configuration.test`, `proxy.test`, `semantic.test`, `sprout`, `code_review_output.json` | Delete (Phase 1) |
| `.ledit/` | Delete (Phase 1) |
| `update_and_test.sh` | Delete (Phase 1) |
| `.gitignore` | Add `examples/.todo_pipeline_checkpoint.json` (Phase 1) |
| `examples/.todo_pipeline_checkpoint.json` | Move to `.sprout/` |
| `Makefile` | Add/extend `clean` target |
| `replay_last_request.sh` | Update help and env var (Phase 2) |
| `test_runner.py` → `workspace_test_runner.py` | Rename + docstring (Phase 2) |
| `e2e_test_runner.py`, `integration_test_runner.py` | Docstring (Phase 2) |
| `CLAUDE.md` | Document the three runners (Phase 2) |
| `pkg/agent/prompts/system_prompt.md` | Update (Phase 2) |
| `pkg/agent/skills/go-conventions/SKILL.md` | Update (Phase 2) |
| `docs/*.md`, `README.md` | Per-file audit (Phase 3) |
| `cmd/service_darwin_test.go`, `cmd/service_linux_test.go` | Audit + decide (Phase 4) |
| `pkg/agent/conversation_image_test.go`, `pkg/agent/tool_handlers_search_new_test.go`, `pkg/git/commit_helpers_test.go`, `pkg/history/history_tools_test.go` | Per-file audit (Phase 5) |
