# SP-035: Persona System Tightening

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** Medium (correctness/observability; no live exploit, but several silent-failure modes)
**Depends on:** None
**Related:** SP-026 (Executive Assistant — adds docs refresh), SP-033 (trust boundaries — shares the cascade model)

## Problem

The persona / subagent-type system works correctly today, but several behaviors that *should be loud are silent*. Specifically:

1. **The Executive Assistant inherits its risk-cascade rules implicitly.** `pkg/personas/configs/executive_assistant.json` declares `allowed_tools`, `local_only`, and a `system_prompt`, but **no `auto_approve_rules`**. `GetAutoApproveRules()` at `pkg/configuration/config.go:236` falls back to `DefaultAutoApproveRules()` (lines 195–213). Today the defaults are comprehensive (`force_flag`, `rm_recursive`, `git_reset_hard`, `git_push_force`, etc. all in `high_risk_never`), so the EA is safe. **But a future change to `DefaultAutoApproveRules` silently changes EA's behavior with no test alerting that the persona's effective policy shifted.** For a role explicitly described as having "elevated approval authority," implicit inheritance is the wrong default.

2. **Two risk gates run independently, with no test pinning the invariant.** Verified:
   - *Persona gate*: `EvaluateOperationRisk` at `pkg/agent/tool_handlers_shell.go:90,195,381` — rejects `RiskLevelHigh`.
   - *Global gate*: `ClassifyToolCall` at `pkg/agent/tool_definitions.go:541` — `ShouldBlock` / `ShouldPrompt`.
   - They're additive (both must allow), which is correct defense-in-depth. **No test asserts that.** A future refactor could short-circuit one based on the other ("optimization") and nothing in CI would catch it.

3. **`containsForceFlag` has the right logic but thin tests.** `pkg/configuration/config_risk_test.go:119,143` cover the exact-flag and combined-short-flag cases. Real-world ambiguity (e.g. `tar -fvz`, `grep -f pattern`, `git -f commit` malformed orderings) isn't covered. The current code handles them correctly *because* of restrictions in the force-capable command list, but a future change to that list could regress quietly.

4. **Silent overrides drop user intent without warning.**
   - `pkg/configuration/config.go:1408-1414` deliberately refuses to apply user-supplied `AllowedTools` to built-in personas (safety-by-default — correct). A user editing `.sprout/config.json` to add a tool to the EA sees no warning; the override is silently discarded.
   - `mergeLegacyStructuredToolsIntoPersonaAllowlists` at `pkg/configuration/config.go:1462` auto-migrates default personas from `write_file` → `write_file` + `write_structured_file`. **Custom user personas are skipped.** They keep working with the legacy form and the structured-file tool silently absent — no warning.

5. **SP-026 documentation is stale.** Phase E references `subagent_prompts/executive_assistant.md` at repo root. The file actually lives at `pkg/agent/prompts/subagent_prompts/executive_assistant.md`. New contributors will look in the wrong place.

## What's already correct (preserve this)

- `local_only: true` in `executive_assistant.json` plus the `LocalOnly && !isLocal` filter at `pkg/agent/persona.go:128-145` correctly prevents EA from being available in cloud mode (`SPROUT_CLOUD` set).
- `--force-with-lease` is correctly excluded from the force-flag check (tested in `config_risk_test.go`).
- Sub-agent depth gating works: EA (depth 0) and orchestrator (depth 1) can spawn; coder/tester (depth 2) cannot.
- `pkg/personas/catalog.go` and `pkg/configuration/config.go SubagentType` are *layered*, not redundant — catalog is the embedded definitions registry, SubagentType is the runtime merge with user overrides.
- The auto-activation guard at `pkg/agent/agent_creation.go:408-452` checks all three preconditions before activating EA.

## Goals / Non-Goals

**Goals**
- The EA persona declares its risk cascade explicitly. Drift between EA's intended policy and the defaults becomes a test failure.
- A regression that lets a persona-defined "low risk" override the global classifier's DANGEROUS verdict fails CI.
- `containsForceFlag` has property-based coverage; the intent is documented in tests, not just code.
- A user who edits config in a way that gets silently dropped sees a warning.
- A new contributor opening the codebase can find every prompt, persona JSON, and config field in one doc.

**Non-Goals**
- Redesigning the persona/subagent model. The layered architecture is fine.
- Adding cryptographic signing to personas or skills (covered separately by SP-033).
- Implementing per-persona override of `AllowedTools` for built-in personas. The current "create a custom persona with a new ID" path is the right answer.
- Migrating the legacy tool format aggressively for custom personas. The fix is *warning*, not auto-migration of user data.

## Proposed Solution

### Track A — Make EA's risk cascade explicit

#### A1: Add `auto_approve_rules` to `executive_assistant.json`
Declare EA's policy in the JSON, matching what `DefaultAutoApproveRules` returns today **plus any EA-specific tightenings the team wants**. The act of declaration forces a review decision. If the team agrees with the current defaults, the JSON ends up redundant — which is correct. The next change to defaults will require an explicit decision: "should EA follow?"

Suggested EA cascade (starts as a literal copy of defaults; adjust during review):

```json
"auto_approve_rules": {
  "low_risk_ops":   ["git_add", "git_status", "git_log", "git_diff", "read_file"],
  "medium_risk_ops":["git_commit", "git_push", "git_pull", "git_fetch",
                    "write_file", "edit_file", "shell_command",
                    "rm_command", "docker", "subagent_spawn", "cross_directory"],
  "high_risk_never":["force_flag", "rm_recursive", "git_reset_hard",
                    "git_clean", "docker_prune", "git_push_force",
                    "git_checkout", "git_switch", "git_restore", "git_branch_delete"]
}
```

#### A2: Audit the other two persona JSONs the same way
`pkg/personas/configs/default_personas.json` and `pkg/personas/configs/project_planner.json` — do they declare rules or also inherit? Make the inherit-vs-declare decision per persona consciously, not by omission.

#### A3: Drift-detection test
Add a test in `pkg/configuration/config_subagents_test.go` (or wherever the EA persona is loaded) that asserts EA's effective rules exactly match the team-approved baseline. If `DefaultAutoApproveRules` or the JSON drifts, this fails CI with a clear "EA's risk policy changed — confirm the change is intentional" message.

### Track B — Pin the two-gate invariant in tests

#### B1: Additivity test
A test that:
1. Builds a synthetic persona declaring `rm_command` in `LowRiskOps` (bypassing the persona gate).
2. Submits `rm -rf /` as a shell command.
3. Asserts the global classifier still blocks via `ClassifyToolCall` at `pkg/agent/tool_definitions.go:541`.

This pins "neither gate can suppress the other."

#### B2: Order-of-evaluation test
A second test that asserts both gates run for every dangerous command — neither short-circuits the other. Test by counting how many times each function is called for a representative set of dangerous commands.

#### B3: Document the model in code
Add a package-level doc comment to `pkg/agent/tool_handlers_shell.go` describing the two-gate model so a future contributor doesn't assume one is redundant.

### Track C — Fuzz the force-flag detector

#### C1: Property-based tests for `containsForceFlag`
Add to `pkg/configuration/config_risk_test.go` using `testing/quick` or a hand-built table-driven set:
- `tar -xzf file.tar.gz` → not force
- `tar -fvz path` → not force
- `grep -f patterns.txt file` → not force
- `git -f commit` (force flag before subcommand, malformed) → not force (verifies position-sensitivity)
- `rsync --force` → force (in force-capable list)
- `rsync --force-with-lease` → not force (excluded variant)
- `cp -rf src dst` → force
- `mv -f a b` → force
- `git push --force` → force
- `git push --force-with-lease` → not force
- `docker rm --force <id>` → force
- `docker rm -f <id>` → force

#### C2: Document intent
Each test case gets a one-line comment explaining *why* it should/shouldn't trigger. The test becomes the spec for the function's behavior.

### Track D — Loud failures on silent overrides

#### D1: Warn on dropped `AllowedTools` override for built-in personas
At `pkg/configuration/config.go:1408-1414`, when the merge logic detects a user-supplied `AllowedTools` for a built-in persona, log a warning naming the persona ID and the requested tool list, with a one-liner: "AllowedTools override ignored — built-in persona tool sets are curated for safety. To customize, create a new persona ID."

Use the existing logging infrastructure (`pkg/logging`), not `fmt.Printf`.

#### D2: Warn on legacy-format custom personas
At `pkg/configuration/config.go:1462` (`mergeLegacyStructuredToolsIntoPersonaAllowlists`), when iterating personas, if a *custom* persona has `write_file` but not `write_structured_file`, log a one-time warning: "Custom persona '%s' uses legacy tool list; add 'write_structured_file' to keep parity with built-in personas."

#### D3: Surface warnings in CLI startup
For both warnings: emit once at config-load time so the user sees them on next `sprout` invocation. Not on every config read (that would spam).

### Track E — Documentation

#### E1: Update SP-026
Edit `roadmap/SP-026-executive-assistant.md` Phase E text — change `subagent_prompts/executive_assistant.md` to `pkg/agent/prompts/subagent_prompts/executive_assistant.md`. Add a "Where prompts live" note in the spec near the top.

#### E2: Write `docs/PERSONAS.md`
Topic outline:
- The three layers: catalog (embedded), config (runtime), session (per-agent)
- How merge resolution works (defaults vs user; what's overridable, what isn't and why)
- The two-gate risk model (persona `EvaluateOperationRisk` + global `ClassifyToolCall`)
- Sub-agent depth model (0 = root/EA, 1 = orchestrator, 2 = leaf workers; what each can spawn)
- `LocalOnly` + `IsLocalMode` (`SPROUT_CLOUD` env var)
- How to define a custom persona (without redefining a built-in one)
- Cost/provider override considerations
- Cross-link to SP-033's `docs/SECURITY.md` where appropriate

#### E3: Cross-reference from `docs/SECURITY.md` (SP-033)
When SP-033's `docs/SECURITY.md` lands, add a "Persona-level risk gates" subsection linking out to `docs/PERSONAS.md`.

## Implementation Phases

### Phase 1: Explicit EA rules (Day 1)
- [ ] **SP-035-1a**: Add `auto_approve_rules` block to `pkg/personas/configs/executive_assistant.json`. Initial values: literal copy of `DefaultAutoApproveRules()` from `pkg/configuration/config.go:195-213`. Open as a PR for review; the discussion is "should EA differ?"
- [ ] **SP-035-1b**: Audit `pkg/personas/configs/default_personas.json` and `project_planner.json`; for each persona, decide explicitly whether it declares its own rules or inherits. Document the decision in the JSON with a `"_rules_source": "inherits-default" | "explicit"` comment field (or in a sibling doc).
- [ ] **SP-035-1c**: Add `TestPersona_EA_RiskCascadeBaseline` in `pkg/configuration/` — load EA, call `GetAutoApproveRules()`, deep-equal against the team-approved baseline. Failure message names the diff.

### Phase 2: Two-gate invariant tests (Day 2)
- [ ] **SP-035-2a**: Add `TestRiskGates_GlobalClassifierIsNotBypassedByPersona` in `pkg/agent/` — synthetic persona with `rm_command` in `LowRiskOps`; submit `rm -rf /`; assert global classifier still blocks.
- [ ] **SP-035-2b**: Add `TestRiskGates_BothGatesEvaluate` — counter wrappers around `EvaluateOperationRisk` and `ClassifyToolCall`; assert both run for each command in a dangerous-commands fixture.
- [ ] **SP-035-2c**: Add a package-level doc comment to `pkg/agent/tool_handlers_shell.go` describing the two-gate model and why neither may suppress the other.

### Phase 3: Force-flag fuzz tests (Day 2-3)
- [ ] **SP-035-3a**: Extend `pkg/configuration/config_risk_test.go:119,143` with the table from Track C1. Each entry carries a `why:` comment.
- [ ] **SP-035-3b**: Add a `TestContainsForceFlag_Property` fuzz test using `testing/quick`: generate random combinations of {command, flag-set, args} and assert the function's verdict matches a simpler reference implementation for documented cases.

### Phase 4: Loud-warning paths (Day 3-4)
- [ ] **SP-035-4a**: At `pkg/configuration/config.go:1408-1414`, after the comment block, detect `len(userOverride.AllowedTools) > 0` and log a warning via `pkg/logging` naming the persona ID and the dropped tool list.
- [ ] **SP-035-4b**: In `mergeLegacyStructuredToolsIntoPersonaAllowlists` at `pkg/configuration/config.go:1462`, iterate every persona (not just defaults). For custom personas missing `write_structured_file` when `write_file` is present, log a one-time warning.
- [ ] **SP-035-4c**: Tests for both warning paths — `TestAllowedToolsOverride_WarnsAndDrops`, `TestLegacyCustomPersona_WarnsOnce`.

### Phase 5: Documentation (Day 4-5)
- [ ] **SP-035-5a**: Update `roadmap/SP-026-executive-assistant.md` Phase E — correct the prompt path to `pkg/agent/prompts/subagent_prompts/executive_assistant.md`. Add a "Where prompts live" section near the top of the spec.
- [ ] **SP-035-5b**: Write `docs/PERSONAS.md` per the E2 outline.
- [ ] **SP-035-5c**: When SP-033's `docs/SECURITY.md` lands (coordinate), add a cross-link from the "trust boundaries" section to the persona docs.

## Success Criteria

| Metric | Target |
|--------|--------|
| EA persona JSON declares `auto_approve_rules` | Present |
| Test pinning EA's effective rules against an approved baseline | Passes; failure prints the diff |
| Test pinning global classifier blocks despite persona "low risk" claim | Passes |
| `containsForceFlag` table size | ≥ 12 cases, each with `why:` comment |
| `containsForceFlag` property test | Present |
| User-supplied `AllowedTools` for built-in persona | Logs warning, doesn't silently drop |
| Custom persona missing `write_structured_file` | Logs one-time warning |
| `docs/PERSONAS.md` | Exists, covers all 7 topics in E2 outline |
| SP-026 prompt-path reference | Matches actual location on disk |

## Risks

- **The EA `auto_approve_rules` declaration could disagree with what the system prompt actually says.** Mitigation: the system prompt's role is to teach the model *how to reason about the cascade* (auto-approve low, ask about medium, refuse high); the JSON's role is the actual policy enforced before the model sees the command. Document this division clearly in `docs/PERSONAS.md`.
- **Warning fatigue.** Two new warnings emit on config load. Mitigation: emit *once* per config-load lifecycle (use a sync.Once), not per persona lookup. Both only fire for users who've actually customized things.
- **Property test for `containsForceFlag` is slow.** Mitigation: keep iteration count modest (1000); the function is fast.
- **Cross-spec coordination with SP-033.** `docs/PERSONAS.md` (this spec) and `docs/SECURITY.md` (SP-033) overlap. Mitigation: write PERSONAS.md first; SP-033 links to it from the trust-boundary section rather than duplicating.

## Files Reference

| File | Action |
|------|--------|
| `pkg/personas/configs/executive_assistant.json` | Modify: add `auto_approve_rules` block |
| `pkg/personas/configs/default_personas.json` | Modify: audit per-persona; declare or annotate inheritance |
| `pkg/personas/configs/project_planner.json` | Modify: same audit |
| `pkg/configuration/config_subagents_test.go` (or wherever EA loading is tested) | Create test: EA risk-cascade baseline |
| `pkg/agent/tool_handlers_shell.go` | Modify: add package-level doc comment describing the two-gate model |
| `pkg/agent/persona_test.go` or new `risk_gates_test.go` | Create: two-gate additivity test |
| `pkg/configuration/config_risk_test.go` | Modify: extend force-flag tables (lines 119, 143); add property test |
| `pkg/configuration/config.go` | Modify: warn at line 1408-1414 on dropped `AllowedTools` override; warn at line 1462 for legacy custom personas |
| `roadmap/SP-026-executive-assistant.md` | Modify: correct prompt path in Phase E |
| `docs/PERSONAS.md` | Create: per the E2 outline |
| `docs/SECURITY.md` (SP-033) | Cross-link when it lands |
