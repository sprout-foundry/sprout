# SP-126 — Effective Context Cap (Honor User's MaxContextTokens End-to-End)

**Status:** 🔵 Scoping — not yet approved for implementation
**Created:** 2026-07-20
**Type:** Bug fix + design clarification

## TL;DR

The user-facing `Config.MaxContextTokens` setting — which lets a user cap
sprout's effective context window below the model's native size (e.g.
"this 1M-token model should never use more than 300K") — is **partially
wired up and silently broken at runtime**. The cap is computed at agent
creation, but the seed library's per-iteration `OnIteration` callback
reports the **native (uncapped)** context size every turn, and
`seed_query.go:266` writes that value back into `state.MaxContextTokens`,
clobbering the user's cap on every iteration. As a result, the UI shows
the cap is honored for one turn, then the displayed max jumps back to
the native window and all downstream budget math (compaction trigger,
proactive recall, semantic recall char budget) reverts to the uncapped
native size. **The cap doesn't actually cap anything past turn 1.**

This spec scopes the fix. The fix is small (one resolver, two call
sites, ~30 lines) but the design choices around *where the cap is
resolved and who reads it* are not, and they affect how SP-125's
`ContextProfile` interacts with the cap. So this is scoped here as a
standalone spec that **extends the SP-125 pattern** rather than
patching around it.

## Problem (concrete)

User sets:
```json
{ "max_context_tokens": 300000 }
```
on a model that advertises a 1M-token native context window.

What happens today:

| Turn | `state.MaxContextTokens` | Effective cap? |
|------|--------------------------|----------------|
| 0 (agent creation) | `min(native=1_000_000, cap=300_000)` = **300K** | ✅ correct |
| 1 (`OnIteration` fires) | **1_000_000** (seed reported native) | ❌ cap lost |
| 2+ | 1_000_000 | ❌ cap lost |

Downstream code that reads `state.GetMaxContextTokens()` to size budgets
then operates on 1M:

- `computeCompactionTriggerFraction` (via `seed_query.go:248`) — uses
  reservation math against the native window.
- `seed_provider.go:476` `ctxLimit, _ := sp.currentClient().GetModelContextLimit()`
  — reports the native window to seed itself.
- `output_router.go:411` "context usage %" — shows ~30% forever instead
  of 100%, because the denominator jumped back to 1M.
- `semantic_recall.go:341` `computeRecallMaxChars(a.GetMaxContextTokens())` —
  recall budget is sized against the native window.
- `summary.go:91` — final summary shows "X / 1.0M tokens" instead of
  "X / 300K tokens".

The user-visible symptom: **the `/max-context` slash command sets the
cap, `getModelContextLimit()` reports the capped value, the first
iteration uses it correctly, then it silently reverts to the native
window.** This is what you observed in testing.

## Root Cause

Two call sites that should respect the cap but don't — and fixing the
root site makes the symptom site self-correct.

### Bug 1 (root cause): `seed_provider.Info()` reports the native window to seed itself

```go
ctxLimit, _ := sp.currentClient().GetModelContextLimit()
return core.ProviderInfo{
    Model:       sp.currentClient().GetModel(),
    ContextSize: ctxLimit,  // ← raw, uncapped
    HasVision:   sp.currentClient().SupportsVision(),
}
```

This is the value seed uses for its internal budget math and what it
passes into `OnIteration` as `contextSize`. It bypasses sprout's
capped `getModelContextLimit()` and goes straight to the client,
which has no knowledge of `Config.MaxContextTokens`.

### Bug 2 (visible symptom): `seed_query.go:266` clobbers the cap every iteration

```go
opts.OnIteration = func(iteration, messages, tokenEstimate, contextSize int) {
    a.state.SetCurrentIteration(iteration)
    a.state.SetCurrentContextTokens(tokenEstimate)
    a.state.SetMaxContextTokens(contextSize)  // ← contextSize is the
                                                //   native window; cap lost
    a.PublishContextManagementDiagnostic(tokenEstimate, contextSize, ...)
}
```

`contextSize` here comes from seed's `core.ProviderInfo.ContextSize`
(see `seed_provider.go:476`). Because Bug 1 leaks the native window
into `ProviderInfo`, this callback overwrites sprout's correctly-capped
`state.MaxContextTokens` with the uncapped native value on every turn.

**Fixing Bug 1 fixes Bug 2** because `OnIteration`'s `contextSize` will
then already be the capped value. We still apply a defensive re-cap at
the `OnIteration` boundary (see §"The fix" below) to protect against
future seed internal paths that bypass `ProviderInfo`.

The audit of all seven `SetMaxContextTokens` call sites (see §"All call
sites" below) confirms that **only `seed_query.go:266` bypasses the
cap**. Five sites correctly route through `getModelContextLimit()`;
one is a pass-through API on the subagent state manager. The fix is
narrow: one resolver + one field + two call sites.

## Goal

The user sets `max_context_tokens: 300000` and gets:

1. **`getModelContextLimit()` always returns 300K** — already works.
2. **Every per-iteration callback and every downstream reader sees 300K** — needs fix.
3. **The cap is "set once, read everywhere"** — no scattered re-checks
   of `Config.MaxContextTokens` at downstream call sites. This is
   achieved by routing the cap through `state.MaxContextTokens`
   (the existing per-iteration write path) so all readers that
   consult `state.GetMaxContextTokens()` — footer, output router,
   summary, pruning config, semantic recall char budget — see the
   capped value automatically. The `Agent.GetMaxContextTokens()`
   path already applies the cap because it routes through
   `getModelContextLimit()`.
4. **The cap and SP-125's `ContextProfile` are independent levers** —
   a user can run a 1M model in full mode with a 300K cap, or a 32K
   model in LCM (no cap, because LCM sessions are short).
5. **The cap survives provider fallback** — if the primary model falls
   back to a different model with a different native window, the cap
   is reapplied to the new native value, not silently lost.

## Non-Goals

- **Not introducing a per-model cap matrix.** One user-facing
  `MaxContextTokens` (or `nil`). A `{"model-x": 300000, "model-y": 100000}`
  map is a future possibility, not v1.
- **Not changing the cap into a soft hint.** It must always be enforced
  as a hard ceiling — every request uses at most this many tokens of
  effective context.
- **Not introducing dynamic per-turn adjustment.** The cap resolves once
  at agent creation and stays fixed for the session, matching SP-125's
  `ContextProfile` precedent.
- **Not modifying `computeCompactionTriggerFraction`.** The trigger
  fraction (0.70 default, 0.85 in LCM) is invariant to window size —
  it scales proportionally with whatever effective window is in
  effect, so 70% × 300K = 210K and 70% × 1M = 700K both yield
  correctly-sized trigger points. See R3 for the verification test
  that confirms this during implementation.

## Design

Mirrors the SP-125 pattern exactly: **one resolved struct, one central
resolver, call sites read the struct, no scattered `if cfg.MaxContextTokens
!= nil` checks.**

### Resolved struct

Add a single field to `Agent` (alongside `contextProfile`):

```go
// pkg/agent/agent.go — add alongside contextProfile

// effectiveContextCap (SP-126) is the resolved maximum number of context
// tokens sprout will use for any request in this session. Always equal to
// the smaller of (a) the model's native context window and (b) the user's
// configured MaxContextTokens cap. Zero means "unknown / not set" and is
// treated by every call site as "no constraint" (the native value flows
// through).
//
// Resolved exactly once at agent creation by ResolveEffectiveContextCap and
// re-read by every call site that needs the ceiling. Call sites must NEVER
// re-derive it from Config.MaxContextTokens and must NEVER call
// SetMaxContextTokens with anything other than this value (with the one
// documented exception of the OnIteration callback in seed_query.go, which
// reads it from the Agent field, not from Config).
//
// Independent of ContextProfile (SP-125): a 1M model can run in full mode
// with a 300K cap; a 32K model can run in LCM with no cap. Both are valid.
effectiveContextCap int
```

### Central resolver

Add a resolver function next to `ResolveContextProfile` (SP-125), in the
same `pkg/configuration/context_profile.go` file (since it's a related
"context-engine" concern):

```go
// pkg/configuration/context_profile.go — add to existing package

// ResolveEffectiveContextCap returns the smaller of the model's native
// context window and the user's configured MaxContextTokens cap. Both
// inputs may be zero/negative, meaning "unknown" — in that case the
// return is the non-zero input, or zero if both are unknown.
//
// This is the single source of truth for the cap. Call sites MUST read
// the value returned here (typically once at agent creation, then stored
// on the Agent for hot-path access) and MUST NOT re-derive it from
// Config.MaxContextTokens or call client.GetModelContextLimit() directly —
// those paths bypass the cap.
//
// Precedence (highest first):
//
//  1. If cfg.MaxContextTokens is non-nil and > 0, AND the native window
//     is known (> 0), return min(native, *cfg.MaxContextTokens).
//  2. If only one of the two is known, return that one.
//  3. If neither is known, return 0. Call sites treat 0 as "unknown" and
//     fall back to whatever default is appropriate (typically the native
//     value reported by the client, or a hardcoded fallback).
//
// No error return: this is a pure resolver, not a gatekeeper. The hard
// floor (ContextFloor) is enforced separately by ResolveContextProfile.
func ResolveEffectiveContextCap(cfg *Config, nativeContextWindow int) int {
    if cfg == nil {
        return nativeContextWindow
    }
    cap := 0
    if cfg.MaxContextTokens != nil && *cfg.MaxContextTokens > 0 {
        cap = *cfg.MaxContextTokens
    }
    switch {
    case nativeContextWindow > 0 && cap > 0:
        if cap < nativeContextWindow {
            return cap
        }
        return nativeContextWindow
    case cap > 0:
        return cap
    default:
        return nativeContextWindow
    }
}
```

The function lives in `pkg/configuration` because `Config` lives there
and we don't want to introduce a `cfg → agent` import edge.

### Wire into agent creation

In `agent_creation.go`, immediately after the existing
`agent.state.SetMaxContextTokens(agent.getModelContextLimit())` line
(currently line 121):

```go
// SP-126: resolve the effective context cap once, store on the Agent.
// Every downstream call site reads a.effectiveContextCap instead of
// re-deriving from Config or calling client.GetModelContextLimit().
// This makes the cap "set once, read everywhere" — the same shape as
// SP-125's contextProfile.
agent.effectiveContextCap = configuration.ResolveEffectiveContextCap(
    cfg,
    agent.getModelContextLimit(), // uncapped native, for comparison
)

// If the user explicitly set the cap and it's lower than the native
// window, emit a one-time notice so they can verify it's active. Skip
// when the cap equals the native window (cap is a no-op) and skip when
// the cap wasn't set (no point announcing "you're using the full window").
if cfg != nil && cfg.MaxContextTokens != nil && *cfg.MaxContextTokens > 0 &&
    agent.effectiveContextCap > 0 &&
    agent.effectiveContextCap < agent.getModelContextLimit() {
    _, _ = fmt.Fprintf(os.Stderr,
        "⚡ Context cap active: %s (native: %s)\n"+
            "  All requests will use at most %s of context.\n"+
            "  /max-context clear to remove, /max-context <N> to change.\n",
        formatTokensCompact(agent.effectiveContextCap),
        formatTokensCompact(agent.getModelContextLimit()),
        formatTokensCompact(agent.effectiveContextCap),
    )
}
```

The existing `agent.state.SetMaxContextTokens(agent.getModelContextLimit())`
line **stays**. It writes the resolved cap into state (because state is
what the UI reads). What changes is *who writes what, when* — see
§"The fix" below.

### Agent-level getter (for new code)

Add a thin wrapper that exposes the resolved cap alongside the existing
`Agent.GetMaxContextTokens()`:

```go
// pkg/agent/metrics.go — add alongside GetMaxContextTokens()

// GetEffectiveContextCap returns the user-facing effective context cap
// for this session — the smaller of the model's native window and the
// user's MaxContextTokens setting. Returns 0 when no cap was set
// (native window flows through unconstrained). This is the same value
// state.MaxContextTokens is initialized with and updated with on every
// OnIteration; the Agent field is the authoritative source, the state
// field is a copy kept in sync by the callback. Prefer reading the
// Agent field directly when both are available, and prefer this method
// over Agent.GetMaxContextTokens() for new code that wants to render
// "effective" vs "native" window values distinctly.
func (a *Agent) GetEffectiveContextCap() int {
    if a.effectiveContextCap > 0 {
        return a.effectiveContextCap
    }
    return a.getModelContextLimit()
}
```

This is additive only — no existing callers change. It's documented
as the preferred path for future code so the cap-vs-native distinction
becomes explicit at the call site rather than buried in a method
named after one or the other.

### The fix — one call site changes, one new helper

**File 1: `pkg/agent/seed_provider.go`**

Change the `Info()` method so `ContextSize` is the capped value:

```go
func (sp *sproutProvider) Info() core.ProviderInfo {
    ctxLimit := sp.currentClient().GetModelContextLimit()
    // SP-126: apply the effective context cap so seed's internal budget
    // math and the per-iteration OnIteration callback receive the capped
    // value, not the model's native window.
    if cap := sp.agent.effectiveContextCap; cap > 0 && ctxLimit > cap {
        ctxLimit = cap
    }
    return core.ProviderInfo{
        Model:       sp.currentClient().GetModel(),
        ContextSize: ctxLimit,
        HasVision:   sp.currentClient().SupportsVision(),
    }
}
```

`sproutProvider` already has an `agent` field (verified — see R1),
so this fix is purely a one-line addition inside `Info()`. No struct
or constructor changes needed.

**File 2: `pkg/agent/seed_query.go:266`**

Make the `OnIteration` callback defensive — even though fixing Bug 2
means `contextSize` will already be the capped value, the callback
should re-apply the cap from the Agent field rather than trusting the
incoming value. This protects against two scenarios:

1. **Future seed releases** that introduce a new internal path
   bypassing `ProviderInfo` (e.g., a fallback provider that calls
   `client.GetModelContextLimit()` directly).
2. **The test suite** constructing mock providers without going
   through `Info()`, where the `contextSize` parameter would be the
   raw mock value.

```go
opts.OnIteration = func(iteration, messages, tokenEstimate, contextSize int) {
    a.state.SetCurrentIteration(iteration)
    a.state.SetCurrentContextTokens(tokenEstimate)

    // SP-126: re-apply the effective context cap here as a defensive
    // measure. The cap is also applied at the source (seed_provider.Info()),
    // but this guard catches any path that bypasses ProviderInfo — future
    // seed internal changes, mock providers in tests, etc. Reading from
    // a.effectiveContextCap keeps the cap authoritative regardless of
    // how contextSize reaches us.
    if cap := a.effectiveContextCap; cap > 0 && (contextSize == 0 || contextSize > cap) {
        contextSize = cap
    }
    a.state.SetMaxContextTokens(contextSize)

    a.PublishContextManagementDiagnostic(tokenEstimate, contextSize, iteration, messages, a.GetCachedTokens(), a.GetPromptTokens(), 0)
}
```

That's the entire fix: two files, ~25 lines of changes. Everything else
stays as-is because the cap is now authored once and read from state.

### All call sites — audit and reconciliation

The seven `SetMaxContextTokens` sites, after the fix:

| File:line | What it writes | Verdict |
|-----------|----------------|---------|
| `agent_creation.go:121` | `agent.getModelContextLimit()` (already capped) | ✅ keep — initial seed |
| `models.go:258` (SetProvider) | `agent.getModelContextLimit()` (already capped) | ✅ keep — model switch |
| `models.go:351` (SetProviderPersisted) | `agent.getModelContextLimit()` (already capped) | ✅ keep — same |
| `models.go:435` (some fallback) | `agent.getModelContextLimit()` (already capped) | ✅ keep |
| `models.go:500` (some fallback) | `agent.getModelContextLimit()` (already capped) | ✅ keep |
| `seed_query.go:266` (OnIteration) | raw `contextSize` (BUG — uncapped) | ❌ **fix per §"The fix"** |
| `submanager_session.go:282` (SetMaxContextTokens) | int passed in | ✅ keep — pass-through API |

The five `models.go` and `agent_creation.go` sites are already correct
because they route through `getModelContextLimit()` which applies the
cap. They become the model: every site that wants the effective cap
should funnel through one helper, and the OnIteration site is the only
one that was bypassing it.

### Subagents

The cap applies to the **primary** agent. Subagents (via
`subagent_runner`) inherit from the primary's resolved cap if they
share the same model, or recompute via `getModelContextLimit()` if
they're on a different model. No change to subagent creation is
required — `subagent_runner` already calls `getModelContextLimit()`
through its state init, which already applies the cap.

If a subagent explicitly opts into a model with a *larger* native
window than the primary's cap, the subagent will be capped to the
primary's effective cap. That's the desired behavior (cost control
should propagate), and it's the natural consequence of "the cap is a
user preference, not a model property."

### Daemon / multi-window

Each chat session creates a fresh `Agent` via
`NewAgentWithConfig`, so the cap is re-resolved per session. If the
user changes `Config.MaxContextTokens` mid-session, the new value
takes effect on the next session — not retroactively, matching SP-125's
"resolve once at agent creation" pattern.

## Why this is clean

1. **One resolver, one struct field.** Mirrors SP-125's `ResolveContextProfile`
   + `contextProfile` pattern exactly. The same design rationale applies:
   "scattered `if cfg.MaxContextTokens != nil` checks would multiply across
   every downstream reader; a single resolved field eliminates that."
2. **Two bug sites, one fix.** `seed_provider.Info()` is the root cause;
   `seed_query.OnIteration` is the visible symptom. Fixing both means the
   cap survives even if seed's internal call paths change.
3. **Cap and `ContextProfile` are independent.** The cap lives on the
   `Agent` directly (not on `ContextProfile`) because it's a constraint
   that applies to *every* mode — full and LCM. A `ContextProfile` field
   would have been the wrong abstraction: it'd imply LCM users might set
   a different cap, which is rarely true.
4. **Defensive at the OnIteration boundary.** Even after `Info()` returns
   the capped value, `OnIteration` re-applies the cap from
   `a.effectiveContextCap` rather than trusting the incoming value.
   Belt-and-suspenders: if a future seed release introduces a new
   internal path that bypasses `Info()`, the cap still holds.
5. **Notice at activation.** A user who explicitly set a cap lower than
   the native window sees a one-time stderr notice confirming the cap
   is active. Same shape as SP-125's auto-detected-LCM notice — the
   pattern is established.
6. **No behavior change for users without a cap.** The cap is `nil` →
   `effectiveContextCap == 0` → every guard `cap > 0 && ...` is false
   → behavior identical to today.

## Edge Cases

### EC1: Cap higher than native window

User sets `max_context_tokens: 2_000_000` on a 1M model. Effective
cap = native (1M). Behavior identical to no cap. **No notice** in this
case (the `if ... agent.effectiveContextCap < agent.getModelContextLimit()`
guard already prevents it).

### EC2: Cap exactly equal to native window

Same as EC1. No notice, no effective change.

### EC3: Cap = 0 (explicit "no cap")

`cfg.MaxContextTokens == &0` is handled by the `SetValue` in
`settings_defs.go` (sets to `nil`). Same as no cap.

### EC4: Native window unknown (0 / negative)

`getModelContextLimit()` already handles this via its own fallback
(32K hardcoded). The cap resolver returns the cap if set, or the
fallback. No change in behavior.

### EC5: Model switch with cap set

Primary switches from a 1M model to a 128K model. `models.go:351` calls
`a.state.SetMaxContextTokens(agent.getModelContextLimit())` which
returns `min(128K, cap)`. If `cap = 300K`, effective stays at 128K. If
`cap = 100K`, effective drops to 100K. This is the natural recomputation;
no explicit "re-resolve on model switch" needed because
`getModelContextLimit()` does it.

### EC6: Provider fallback to a different model

Same as EC5 — `getModelContextLimit()` re-applies the cap to whatever
the new model's native window is. If a 1M model falls back to a 32K
model and the cap is 300K, effective drops to 32K (the natural min).
The notice at activation does NOT re-fire on fallback (one-time only).

### EC7: Cap changed mid-session

The `effectiveContextCap` field on the `Agent` is set once at creation
and doesn't re-read `Config` on every iteration. This is intentional
(matches SP-125) — mid-session config changes take effect on the next
session, not retroactively. The `/max-context` slash command persists
to `Config.MaxContextTokens` and the change is picked up at the next
`NewAgentWithConfig` call.

### EC8: Cap below ContextFloor (8K)

A user sets `max_context_tokens: 4096`. `ResolveContextProfile` runs
*before* the cap resolver and errors out if the model window is below
8K. With a cap below 8K on a large model, the effective cap is 4096,
but `ResolveContextProfile` already validated the *model's native*
window is ≥ 8K. The cap then drops effective to 4K, which is below
the floor. **Should this error?**

**Recommendation:** **No** — the floor applies to the model's
*capability*, not the user's cost preference. A user who explicitly sets
a 4K cap on a 128K model probably knows what they're doing (small
test, tight budget, deliberate degradation). The agent will produce
short answers and compact aggressively; that's the user's choice.

**Alternative considered:** Add a `MinimumCap = 1024` constant (matching
the `/max-context` set-validator which rejects values below 1024). If
`effectiveContextCap < MinimumCap`, error at resolution time. This
aligns with the existing validator and prevents the "set 100, watch
sprout break" failure mode. **Recommended as part of this spec** —
see R2.

The error message must match the existing `/max-context` wording
exactly so users see the same message regardless of which surface
they used to set the cap: *"value must be at least 1024 when setting
a cap (got X)"* (from `max_context.go:64`). The resolver's error
returns this string; `agent_creation.go` wraps it with
`fmt.Errorf("resolving effective context cap: %w", err)` to preserve
the chain.

### EC9: Negative cap

`MaxContextTokens` is `*int`. `settings_defs.go` validates
`val < 0` → error. Not reachable through normal paths. Defensive: the
resolver guards `*cfg.MaxContextTokens > 0`.

## Risks

### R1: `sproutProvider` already has a back-pointer to the Agent ✅ verified

`seed_provider.go:476`'s `Info()` method currently calls
`sp.currentClient().GetModelContextLimit()` but doesn't access the
Agent. The proposed fix needs `sp.owner.effectiveContextCap`.
**Verified during review:** `sproutProvider` already has an `agent`
field (referenced in the same file at `seed_provider.go:33-34`),
so the back-pointer exists. The fix can use `sp.agent.effectiveContextCap`
directly — no struct changes needed.

### R2: Cap below the floor

A user setting `max_context_tokens: 100` against a 128K model gets
silent breakage — every request is over the cap immediately,
compaction fires every turn, the model produces nothing. See EC8.
**Mitigation:** Add `MinimumCap = 1024` (matching the existing
`/max-context` validator) and have `ResolveEffectiveContextCap` return
an error if `effectiveContextCap < MinimumCap` and the cap was
explicitly set. The `nil` and `0` cases (no cap) still resolve to the
native window. The error message mirrors the existing
`/max-context` error: *"value must be at least 1024 when setting a
cap (got X)"*.

### R3: `computeCompactionTriggerFraction` is independent of the cap

`seed_query.go:248` calls `a.computeCompactionTriggerFraction()`,
which returns a **fraction** (0.70 by default, per SP-125 LCM: 0.85)
— not an absolute token count. Seed multiplies this fraction by the
context window to determine when to trigger compaction. Since the
fraction is invariant to the window size, 70% of 1M (700K) and 70%
of 300K (210K) both yield correctly-sized trigger thresholds
relative to the effective window. **No fix needed.** The cap
correctly reduces the absolute trigger point proportionally.

**Verification during implementation:** add a unit test that runs
seed with `MaxContextTokens = 300K` on a 1M model and asserts the
trigger fires near 210K (70% × 300K) rather than near 700K. This
exercises both the cap path and the trigger-fraction interaction.

(Note: the reviewer flagged my original R3 explanation as conflating
"it's a fraction" with "ContextProfile stores it". Both are true but
the *reason it works regardless of cap* is specifically that it's a
fraction of whatever window size seed is told — not anything
specific to `ContextProfile`.)

### R4: Semantic recall char budget is sized from the cap

`semantic_recall.go:341` calls `computeRecallMaxChars(a.GetMaxContextTokens())`.
After this spec, `GetMaxContextTokens()` returns the capped value, so
the recall budget is also capped. **Is this correct?**

**Yes.** Recall is sized in proportion to the window — a smaller
window should get a smaller recall block, otherwise recall alone can
fill the window. The cap correctly reduces recall proportionally.

### R5: Output router "% used" denominator

`output_router.go:411` shows "X / Y tokens" where Y is the max.
After this spec, Y is the capped value, so the display shows the
correct percentage. The display string is unchanged.

### R6: Diagnostic event fields

`PublishContextManagementDiagnostic` (called from `OnIteration`)
publishes `contextSize` as the max. After the fix, this is the capped
value. Subscribers (WebUI footer, prune config UI) read the capped
value. **Verify the WebUI doesn't expect the native value somewhere.**
If a subscriber uses the diagnostic to render "native window" (vs
"effective window"), it'd be wrong after the fix. **Mitigation:**
audit the WebUI usage during implementation; if found, distinguish the
two in the event payload (add `nativeContextSize` field alongside
`effectiveContextSize`).

### R7: Cap-vs-floor ordering

`ResolveContextProfile` runs first (errors below ContextFloor on the
*native* window). Then `ResolveEffectiveContextCap` runs (caps on top).
If the native window is 8K and the cap is 4K, `ResolveContextProfile`
succeeds (8K ≥ ContextFloor) and the cap drops effective to 4K. Is
4K below floor? Yes, but per EC8 we don't error — the user opted in.

If the user *also* has `context_mode: "low_context"`, the cap applies
on top of LCM. LCM's compaction trigger (0.85) is still sized off the
effective window, which is fine.

## Implementation Cost Estimate

| Work item | Files | Effort | Notes |
|-----------|-------|--------|-------|
| `ResolveEffectiveContextCap` resolver | `pkg/configuration/context_profile.go` | ~30 min | Pure function; small unit tests |
| `effectiveContextCap` field on Agent | `pkg/agent/agent.go` | ~5 min | One int field |
| `GetEffectiveContextCap()` getter | `pkg/agent/metrics.go` | ~5 min | Thin wrapper |
| Wire resolver at agent creation | `pkg/agent/agent_creation.go` | ~20 min | One call + one notice |
| Fix `seed_provider.Info()` to apply cap | `pkg/agent/seed_provider.go` | ~15 min | The bug-fix epicenter |
| Defensive re-cap in `seed_query.OnIteration` | `pkg/agent/seed_query.go` | ~10 min | Belt-and-suspenders |
| Unit test: resolver edge cases | `pkg/configuration/context_profile_test.go` (extend) | ~30 min | EC1–EC9 |
| Unit test: `Info()` returns capped value | `pkg/agent/seed_provider_test.go` (extend) | ~30 min | Mock client with native=1M, cap=300K → expect 300K |
| Regression test: OnIteration sees capped value | `pkg/agent/seed_query_test.go` (extend) | ~1 hr | N-iteration run, assert every contextSize ≤ cap |
| Regression test: trigger fraction honors cap | `pkg/agent/context_budget_test.go` (extend) | ~20 min | 1M model + 300K cap → trigger fires near 210K (70% × 300K) |
| Audit diagnostic event subscribers | `webui/**`, `pkg/events/**` | ~15 min | Verify no subscriber expects the native window value; if found, add `nativeContextSize` field to event (per R6) |
| Update `FALLBACKS.md` §5 | `docs/FALLBACKS.md` | ~10 min | Update the cap line |
| **Total** | | **~4 hrs** | |

No call sites change. No config surface changes. No slash command
changes. No migration. **The user-facing feature is unchanged; only
its enforcement is fixed.**

## What this deliberately does NOT do

- **No per-model cap matrix.** One user-facing `MaxContextTokens` field.
  A future `ContextCaps map[string]int` is the obvious extension if a
  user runs mixed-context models; out of scope for v1.
- **No dynamic cap adjustment.** Resolves once at agent creation,
  matching SP-125.
- **No re-cap on model switch.** The `getModelContextLimit()` path
  naturally re-applies the cap (because it consults
  `Config.MaxContextTokens` on every call); no explicit recomputation
  needed. Verified in EC5.
- **No minimum cap below 1024.** The existing
  `/max-context` validator and `settings_defs.go` both reject
  values < 1024. The resolver honors that. See R2.
- **No CLI flag.** The cap is a config field + slash command, both
  already present. No new `--max-context-tokens` flag.
- **No new webui field.** The settings API already exposes
  `max_context_tokens` via `settings_defs.go`. No new UI work.

## Recommendation

**Ship.** This is a 4-hour fix that makes an existing user-facing
feature actually work. The cap is documented, the slash command works,
the `getModelContextLimit()` cap logic works — but the
`OnIteration` callback undoes it on every turn, and that's the bug
the user observed. The fix mirrors SP-125's design exactly so the two
features compose cleanly (full mode + 300K cap on a 1M model is a
totally valid configuration).

**Phased:** Phase 1 = the fix (resolver + Info + OnIteration + tests).
Phase 2 (out of scope) = per-model cap matrix if there's evidence of
demand.

---

## Appendix: Reproducing the Bug

```bash
# Set the cap to 300K against a 1M model
sprout config set max_context_tokens 300000

# Start a chat session and ask anything
sprout
> hello

# In debug mode, observe:
#   iteration 0: state.MaxContextTokens = 300000 ✓
#   iteration 1: state.MaxContextTokens = 1000000 ✗  ← cap lost
#   iteration 2: state.MaxContextTokens = 1000000 ✗

# Verify in /usage or in the footer:
> /usage
Native context window: 1.0M tokens
Max context cap: 300K tokens (30% of native window)  ← reported correctly
Current usage: X / 1.0M tokens                        ← but the math is wrong
```

After the fix:
```bash
> /usage
Native context window: 1.0M tokens
Max context cap: 300K tokens (30% of native window)
Current usage: X / 300K tokens                         ← now correct
```

The diagnostic event payload also normalizes: the `effectiveContextSize`
field always equals `min(native, cap)`.