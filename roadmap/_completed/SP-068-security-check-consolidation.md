# SP-068: Security Check Consolidation — One Risk Scale, One Resolver, One Broker

**Status:** ✅ Implemented (Phases 1–3 shipped: single resolver, single broker, sprout explain)
**Date:** 2026-06-09
**Depends on:** SP-004 (Security), SP-049 (Shell Permission Overhaul), SP-058 (4-option approval)
**Priority:** Medium (no new capability; removes a recurring correctness/UX hazard)
**Scope:** Refactor of existing security-decision code. No new user-facing
features beyond a single diagnostic ("why was this gated?"). Three phases,
each independently shippable.

## Problem

A single tool call — most visibly `shell_command` — is judged by **two
independent risk taxonomies running in sequence**, plus four special-case
gates and a workspace policy. Each was added for a good reason; together they
form a tangle that is hard to reason about, double-prompts users, and forces a
pile of "did the other gate already approve this?" bookkeeping whose only job
is to undo the redundancy.

SP-049 already flagged the core of this — "Centralizing the four pattern
functions into a single registry" — and explicitly **deferred** it as "a pure
refactor of working code with no user-visible benefit." This spec picks that
debt back up, because the *delivery-layer* fixes (the SP-068 sibling work on
prompt timeouts) exposed how much accidental complexity the two-vocabulary
split creates at the approval call sites.

### The two vocabularies

| Gate | Scale | Entry point | File |
|---|---|---|---|
| **Gate 1** — static classifier | `SAFE / CAUTION / DANGEROUS` | `ClassifyToolCall` | `pkg/agent_tools/security_classifier.go:228` |
| **Gate 2** — persona risk cascade | `Low / Medium / High / Critical` | `EvaluateOperationRisk` | `pkg/agent/agent_getters.go:555` |

Both fire on the **same** `shell_command`, in sequence, with **different
words for the same idea**. There is no single function you can read to answer
"what will happen if the agent runs `X`?" — you have to simulate both gates and
their interaction.

### The anti-double-prompt plumbing

Because both gates can independently decide to prompt, `risk_prompt.go:96-122`
exists almost entirely to *suppress* the second prompt:

- `IsShellCommandAllowlisted(command)` — Always-approved list short-circuit.
- `HasUserApproval(ctx)` — Gate 1 already prompted in-context → skip Gate 2.
- `consumeShellCommandApproval(command)` — the seed pre-execute hook
  (`seed_tool_registry.go:617`) ran Gate 1 with no `ctx`, so it records
  approval in a per-agent map that Gate 2 drains here. "Same effect, different
  transport" (the code's own words).

Three separate mechanisms to carry one bit — "the user already said yes" —
across two gates that shouldn't have been separate.

### The special-case gates

Four more checks live outside both classifiers, each with its own shape:

| Gate | File |
|---|---|
| Git history-rewrite (`reset --hard`, `rebase`, `branch -D`) | `pkg/agent/tool_handlers_shell.go:152` |
| Git-write capability (persona must hold `git_write`) | `pkg/agent/tool_handlers_shell.go:162` |
| Filesystem path-tier (Sensitive / External) | `pkg/agent/tool_security.go` (`ClassifyPathAccess`) |
| Workspace security policy (allow/deny/prompt) | `pkg/configuration/security_policy.go` |

### Symptoms this produces

- **Double prompts** when the suppression plumbing misfires (the SP-058
  regression that added `HasUserApproval`).
- **Reasoning hazard**: changing a rule in one vocabulary can be silently
  overridden by the other (e.g. Gate 1 says CAUTION, Gate 2 says Critical → the
  Gate 1 prompt is moot).
- **No single audit answer**: "why did sprout gate `terraform destroy`?" has no
  one place to point at.

## Current State

### What Works (do not regress)

| Mechanism | File:Line | Notes |
|---|---|---|
| Static three-tier classifier | `security_classifier.go:228` | `SAFE/CAUTION/DANGEROUS`, no FS access |
| Critical-op hard-block | `security_classifier.go` / `config.go:505` | `rm -rf /`, `mkfs`, fork bombs — never overridable |
| Persona risk cascade | `agent_getters.go:555` | persona `AutoApproveRules` → risk profile → default |
| 4-option approval (Once/Deny/Always/Elevate) | `risk_prompt.go`, `security/approval_manager.go` | SP-058 |
| Persistent allowlist | `config.ApprovedShellCommands` | "Always approve this command" |
| WebUI + CLI approval surfaces | `tool_security.go`, `pkg/console/security_prompt.go` | unified timeout via SP-068 sibling work |
| Headless DANGEROUS hard-block | `tool_security.go` | non-interactive + ShouldBlock → SecurityError |

### What's Tangled

| Issue | Impact |
|---|---|
| Two risk vocabularies on the same call | High (reasoning + maintenance) |
| Three mechanisms to carry "already approved" | Medium (double-prompt bugs) |
| Four special-case gates with bespoke shapes | Medium |
| No single "explain this decision" path | Low-Medium |

## Proposed Solution

One **risk scale**, one **resolver**, one **approval broker**. The classifier
and the persona cascade become two *inputs* to a single resolver instead of two
sequential gates with separate prompting.

### Phase 1: One risk scale (non-behavioral mapping)

Pick `Low / Medium / High / Critical` as the canonical scale (it's the richer
of the two and already carries the hard-block semantics at Critical). Map the
static classifier's output onto it at the boundary:

| Classifier | Canonical |
|---|---|
| `SAFE` | `Low` |
| `CAUTION` | `Medium` |
| `DANGEROUS` | `High` |
| (critical-op hard-block) | `Critical` |

Introduce a `RiskAssessment` value that carries: canonical level, the
contributing sources (classifier, persona, git-gate, fs-tier, workspace
policy), a human reason string, and the `IsHardBlock` flag. **This phase
changes no decisions** — it's a pure representation change with golden tests
asserting byte-identical gating for a corpus of commands.

### Phase 2: One resolver (collapse the two gates)

Replace the Gate 1 → Gate 2 sequence with a single
`ResolveToolRisk(toolName, args, agent) RiskAssessment` that runs all inputs
and takes the **most restrictive** result (mirroring SP-049's resolution
order, extended to cover the persona cascade and the special-case gates):

1. Critical (built-in; never overridable)
2. Built-in DANGEROUS / git-history-rewrite / workspace-deny
3. Persona/profile gate (High)
4. User-DANGEROUS patterns (SP-049 Phase 2, when present)
5. Caution-tier (Medium)
6. Safe (Low)

The resolver returns **one** decision and **one** prompt. The anti-double-
prompt plumbing (`HasUserApproval`, `consumeShellCommandApproval`, the seed
pre-execute approval map) is deleted — there is no second gate to suppress.
Approval is requested **once**, through the unified broker (below), and the
result flows straight to execution.

This is the highest-risk phase; it ships behind a `unified_risk_resolver`
config flag (default off) for one release so the old path stays available, with
a shadow-mode log comparing old-vs-new decisions on every call to catch drift
before flipping the default.

### Phase 3: One broker + "explain" diagnostic

The approval *delivery* is already converging (SP-068 sibling work unified the
timeout across CLI surfaces and added webui→CLI fallback). Finish the job:

- A single `ApprovalBroker.Request(assessment) Decision` that owns surface
  selection (webui vs CLI), the timeout, the fallback, and the 4-option
  outcome. Gate 1 and Gate 2 call sites both collapse to one broker call.
- `sprout explain '<command>'` (and a `--why` on the security error) prints the
  full `RiskAssessment`: canonical level, every contributing source, and the
  exact rule that set the level. Closes the "why was this gated?" gap without
  debug verbosity, complementing SP-049's `sprout audit tail`.

## Out of Scope

- **Argument-level / injection analysis** — deferred by SP-049; unchanged here.
- **The workspace-policy trust model** — owned by SP-049 Phase 2; this spec
  consumes it as one resolver input, it does not redesign it.
- **Changing any actual gating decision.** Phases 1 and 3 are behavior-
  preserving; Phase 2's only intended behavior change is *removing duplicate
  prompts*, verified by the shadow-mode diff being empty except for de-duped
  prompts.

## Success Criteria

- **One function answers "what happens if the agent runs X?"** —
  `ResolveToolRisk` returns a single `RiskAssessment` for any tool call.
- **No double prompts.** A command that previously prompted at both Gate 1 and
  Gate 2 now prompts exactly once. Regression test asserts a single approval
  request is published per gated call.
- **Behavioral parity.** Shadow-mode diff over the test corpus (and a sample of
  real audit-log commands) shows zero decision changes except eliminated
  duplicate prompts.
- **The suppression plumbing is gone.** `HasUserApproval`,
  `consumeShellCommandApproval`, and the per-agent approval map are deleted;
  `grep` confirms no remaining references.
- **Critical invariant holds.** `rm -rf /` and the fork-bomb corpus still
  hard-block under the unified resolver, including with a user safe-pattern that
  tries to silence them.
- **`sprout explain 'git reset --hard HEAD~5'`** prints level `High/Critical`
  with the contributing source annotated.
```
