# SP-050: Orchestrator Persona Collapse — One Persona, Configurable Git-Write

**Status:** 📋 Proposed
**Date:** 2026-05-22
**Depends on:** SP-026 (Executive Assistant), SP-049 (Shell Permission Overhaul — only loosely; this spec touches the persona layer, not the classifier)
**Priority:** Medium-High (cleanup that closes a real source of bugs and divergence)
**Effort Estimate:** ~6 hours (one phase, ships as one PR)

## Problem

There are currently two near-identical orchestration personas in
`pkg/personas/configs/default_personas.json`:

- **`orchestrator`** — process-oriented planning and delegation, no
  git-write tooling in its prompt, no `commit` in its allowlist.
- **`repo_orchestrator`** — same allowlist plus `commit`, plus a
  ~2KB `system_prompt_append` documenting the "Git Operations Policy"
  (commit tool usage, broad-add prohibition, blocked destructive ops,
  no-push rule, skill activation guidance, end-to-end workflow).

The two personas have diverged in three ways that the codebase is now
papering over:

### Issue 1: OR'd persona checks at every git-write site

Five sites in the agent special-case both names because the *real*
distinction is "is this an orchestrator?", not "which orchestrator?":

| Site | What it checks |
|---|---|
| `pkg/agent/persona.go:226` | `persona != "orchestrator" && persona != "repo_orchestrator"` — gate on git-write |
| `pkg/agent/tool_handlers_shell.go:134` | `persona == "orchestrator" \|\| persona == "repo_orchestrator"` — error message routing |
| `pkg/agent/tool_handlers_shell.go:225` | `persona == "repo_orchestrator"` — auto-approve git add/push/pull/fetch |
| `pkg/agent/tool_handlers_shell.go:403` | `persona == "repo_orchestrator"` — auto-approve commit tool |
| `pkg/agent/seed_integration.go:997` | `case "orchestrator", "repo_orchestrator", "coder":` — seed routing |
| `pkg/webui/settings_api_general.go:88` | `activePersona == "orchestrator" \|\| activePersona == "repo_orchestrator"` — settings default |
| `cmd/agent_command.go:205` | exclude both from subagent picker |

Every time a contributor adds a new orchestrator-only behavior, they
either remember to update both lists or they don't — and the second
case is invisible until a user reports drift. The "right" answer is
already encoded in the existing `AllowOrchestratorGitWrite` config
flag (`pkg/configuration/config.go:62-67`); the persona ID adds
nothing on top.

### Issue 2: System-prompt divergence is hand-maintained

`repo_orchestrator.system_prompt_append` lives as an escaped JSON
string in `default_personas.json` — currently 2,148 characters of
markdown wrapped in `"\n"` escapes. It's hard to edit (no markdown
preview, no syntax highlighting, no diff legibility) and hard to keep
aligned with the actual implementation in `tool_handlers_shell.go` and
the commit-tool handler.

The content of that append is *exactly* the policy that should apply
when the orchestrator has git-write enabled — there's no scenario
where the orchestrator gets git-write capability but should *not*
follow the policy.

### Issue 3: Active-persona default is `repo_orchestrator`

`pkg/agent/submanager_state.go:217` initializes new agents with
`activePersona: "repo_orchestrator"` — meaning even users who never
touched personas are on the "git-write" variant. The base
`orchestrator` exists in the catalog mostly as a vestigial
"safer-default" that almost nobody runs.

If we're going to default to git-write on, we should do it through the
config flag (which is user-discoverable in settings) rather than
through the persona ID (which is invisible).

### Issue 4: Executive Assistant prompt hard-codes the wrong target

`pkg/agent/prompts/subagent_prompts/executive_assistant.md` references
`repo_orchestrator` in 13 places (delegation examples, parallel-spawn
examples, capability documentation). When the EA spawns a subagent
with `persona: "repo_orchestrator"`, it's relying on the legacy ID.

After the collapse, the EA should spawn `orchestrator` and the
git-write capability follows from the user's `AllowOrchestratorGitWrite`
flag, not from a persona-ID choice in the prompt.

## Current State

### What Works (do not regress)

| Mechanism | File:Line | Notes |
|---|---|---|
| `AllowOrchestratorGitWrite` config flag | `pkg/configuration/config.go:62-67` | Boolean; merged at `:804-805` |
| Active-persona resolution at tool dispatch | `pkg/agent/persona.go:101-125` | Gates tool allowlist per persona |
| EA-spawned subagents inherit depth+1 | `pkg/agent/subagent_runner.go` | Already in place from SP-026 |
| `commit` tool handler with auto-approve gate | `pkg/agent/tool_handlers_shell.go:397-425` | Currently keys off `repo_orchestrator` |
| Git-write block via shell_command | `pkg/agent/tool_handlers_shell.go:126-157` | Calls `isOrchestratorGitWriteAllowed` |
| Persona prompt append assembly | `pkg/agent/persona.go:80-88` | `system_prompt_append` concatenation |

### What's Actually Missing

| Gap | Impact | Addressed by |
|---|---|---|
| Two persona IDs for one logical persona | High (code divergence, contributor confusion) | Phase 1 |
| Git-policy markdown trapped inside JSON | Medium (edit ergonomics, drift) | Phase 1 |
| `repo_orchestrator` as the default active persona | Low (signals "git-write" via persona ID, not flag) | Phase 1 |
| EA prompt references the to-be-removed ID | Medium (delegation breaks without alias) | Phase 1 |

## Proposed Solution

Single phase, single PR. The collapse is small and atomic; splitting
it would only create intermediate states where some sites have been
updated and others haven't.

### Phase 1: Collapse to a single `orchestrator` persona

**Scope:** one JSON file, one new markdown file embedded via `go:embed`,
six Go files updated, four prompt examples updated, one config flag
default flipped. No public API additions.

**1a. Move git-policy markdown out of JSON.** Create
`pkg/agent/prompts/persona_appends/orchestrator_git_policy.md`
containing the current `system_prompt_append` content of
`repo_orchestrator` (Committing / Staging / Read-Only / Destructive
Blocked / Pushing / Skills / Workflow sections). Embed it into the
binary via a new `go:embed` directive co-located with the existing
prompt embeds.

This makes the policy edit-friendly (real markdown file, real diff,
real syntax highlighting) and keeps it close to the code that
enforces it.

**1b. Conditional prompt assembly.** In `pkg/agent/persona.go`'s
`SetPersona` path (around `:80-88` where `system_prompt_append` is
applied), after the persona's own append is applied, check if
`activePersona == "orchestrator"` AND
`config.AllowOrchestratorGitWrite == true`. If both true, append the
embedded `orchestrator_git_policy.md` content with the same
`"\n\n---\n\n"` separator the existing append path uses.

When `AllowOrchestratorGitWrite == false`, the policy is *not*
appended — the orchestrator runs without the commit-tool guidance, and
the shell-side gate at `tool_handlers_shell.go:132` already blocks the
git-write commands anyway. Two layers of defense, both keying off the
same flag.

**1c. Remove `repo_orchestrator` from the catalog.** Delete the
`repo_orchestrator` entry from `default_personas.json`. Add
`repo_orchestrator` and `git_orchestrator` (the existing alias) to the
`orchestrator` entry's `aliases` array so existing config files,
session histories, and EA prompts that name the old ID continue to
resolve. Aliases route through the existing
`normalizeAgentPersonaID` path (`persona.go:127-131`) — no new code
required for resolution.

Important: the alias path **does not** imply git-write was on. The
behavior is "alias resolves to orchestrator, then the config flag
decides git-write." This is the answer to "what if a user's session
history names `repo_orchestrator`?" — it loads, runs, and respects
whatever `AllowOrchestratorGitWrite` is currently set to.

**1d. Strip OR'd persona checks.** Six sites become single-persona
checks:

| File:Line | Before | After |
|---|---|---|
| `pkg/agent/persona.go:226` | `persona != "orchestrator" && persona != "repo_orchestrator"` | `persona != "orchestrator"` |
| `pkg/agent/tool_handlers_shell.go:134` | `persona == "orchestrator" \|\| persona == "repo_orchestrator"` | `persona == "orchestrator"` |
| `pkg/agent/tool_handlers_shell.go:225` | `persona == "repo_orchestrator"` | `persona == "orchestrator" && AllowOrchestratorGitWrite` |
| `pkg/agent/tool_handlers_shell.go:403` | `persona == "repo_orchestrator"` | `persona == "orchestrator" && AllowOrchestratorGitWrite` |
| `pkg/agent/seed_integration.go:997` | `case "orchestrator", "repo_orchestrator", "coder":` | `case "orchestrator", "coder":` |
| `pkg/webui/settings_api_general.go:88` | `activePersona == "orchestrator" \|\| activePersona == "repo_orchestrator"` | `activePersona == "orchestrator"` |
| `cmd/agent_command.go:205` | `id == "orchestrator" \|\| id == "repo_orchestrator"` | `id == "orchestrator"` |

The two `tool_handlers_shell.go` auto-approve sites (`:225`, `:403`)
move from "persona is repo_orchestrator" to "persona is orchestrator
AND git-write is allowed" — preserving the existing UX (no prompt
during auto-commit / stage / push) when the user has opted into
git-write, while restoring the prompt path when they haven't.

**1e. Flip default active persona and default config flag.** At
`pkg/agent/submanager_state.go:217`, change `activePersona:
"repo_orchestrator"` to `activePersona: "orchestrator"`. At
`pkg/configuration/config.go` (where defaults are seeded for new
configs), set `AllowOrchestratorGitWrite: true` for fresh installs.

Per scoping: no migration path is required. Existing user configs
that have the field set (true or false) keep their value; existing
configs with the field absent get the new default. This is the same
zero-value behavior the field already has — the only change is the
seed default for new configs.

**1f. Update Executive Assistant prompt.** Replace the 13
`repo_orchestrator` references in
`pkg/agent/prompts/subagent_prompts/executive_assistant.md` with
`orchestrator`. The alias from step 1c means that if any persisted
state still names the old ID, it resolves correctly — but the prompt
itself should teach the new name so new sessions converge on it.

**1g. Update task_queue_add_handler description.** Two cosmetic
references in `pkg/agent_tools/task_queue_add_handler.go:22` and
`pkg/agent/tool_registrations.go:434` (parameter description strings)
update from `"e.g., repo_orchestrator"` to `"e.g., orchestrator"`.

**1h. Update tests.** Existing tests at `pkg/agent/persona_test.go`,
`pkg/agent/submanager_state_new_test.go`, `pkg/agent/submanagers_test.go`,
and `pkg/agent/agent_creation_test.go` reference `repo_orchestrator`
in fixtures and assertions. Update each to assert the new behavior:

- Alias resolution: passing `repo_orchestrator` resolves to
  `orchestrator` (new test).
- Git policy append: when `AllowOrchestratorGitWrite=true`, the
  orchestrator's system prompt contains the policy markdown's
  characteristic strings (`"Git Operations Policy"`,
  `"NEVER use 'git add .'"`). When false, those strings are absent.
- Auto-approve gating: commit and basic-git auto-approve fires only
  when persona==orchestrator AND flag==true. Test both flag states.
- Catalog: `repo_orchestrator` is no longer a top-level ID in the
  default catalog, but `GetSubagentType("repo_orchestrator")` still
  returns the `orchestrator` entry via alias resolution.

**1i. Update human-facing docs.** `docs/PERSONAS.md` describes both
personas today; collapse to a single entry with a "Git Operations"
subsection that explains the flag and what it gates. `AGENTS.md`
(top-level repo doc) has one passing reference to `repo_orchestrator`
in its persona listing — update to the new name.

## Out of Scope

Deferred or explicitly rejected for this round:

- **Backwards compatibility for old config files.** Per scoping, not
  needed. Existing configs with `AllowOrchestratorGitWrite` set
  (either value) keep their value; configs without the field get the
  new default at next load.
- **Migrating legacy session JSONL files.** Session history records
  the persona ID per-turn; old turns will reference
  `repo_orchestrator` forever. Acceptable: history is read-only and
  the alias resolves on replay. No migration script.
- **Removing the `system_prompt_append` JSON field.** Other personas
  may want it in the future; the field stays, only the
  `repo_orchestrator` use of it goes away.
- **Settings UI rework for the flag.** `AllowOrchestratorGitWrite` is
  already in the settings schema; visual treatment can wait for
  SP-017 (settings panel rework).
- **Per-workspace overrides for git-write.** A single boolean is
  sufficient for now. Per-workspace policy belongs in SP-049's
  workspace overlay system if it grows that far.

## Success Criteria

- **`grep -rn 'repo_orchestrator' --include='*.go'` returns zero
  matches** outside of (a) the alias declaration in
  `default_personas.json`, (b) comments referencing historical
  context, and (c) test fixtures that explicitly verify alias
  resolution.
- **A fresh install starts with
  `AllowOrchestratorGitWrite=true`** and the `orchestrator` persona
  active. Test asserts both via a config-creation integration test.
- **Loading a config that explicitly sets
  `AllowOrchestratorGitWrite=false`** keeps that value and the
  orchestrator does not auto-approve commit / git add / push. Test
  asserts via the auto-approve code paths.
- **Setting `persona: "repo_orchestrator"`** in a `run_subagent` call
  resolves to the `orchestrator` persona via alias. Test asserts the
  spawned subagent's `activePersona == "orchestrator"`.
- **The git policy markdown is present in the orchestrator's system
  prompt** when git-write is allowed, and absent when it's not. Test
  greps the assembled prompt for `"Git Operations Policy"`.
- **EA prompt examples spawn `orchestrator`,** and the EA continues
  to delegate successfully end-to-end in an integration test (or a
  unit test that exercises the prompt template + delegation tool).
- **No regression in `go test ./...`.** All existing persona/agent
  tests pass after the collapse, with updated fixtures.
- **`make build-all` succeeds** — both the Go binary and the embedded
  React UI compile cleanly with the persona collapse in place.
