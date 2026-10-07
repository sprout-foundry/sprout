# SP-150 — Model Roles

> **Status (2026-10-07):** Shipped.
> Related: SP-137 (no provider names in product logic), SP-125
> (low-context mode resolves from each agent's own model window).

## Problem

Sprout already lets some tasks use a different model from the main
conversation, but each one is a separate, ad-hoc setting:

- `subagent_model` and per-type subagent models
  (`pkg/configuration/config_subagent.go`: `GetSubagentModel`,
  `GetSubagentTypeModel`).
- `commit_model`, the review model and the completion model
  (`pkg/configuration/config_commit_review.go`: `GetCommitModel`,
  `GetReviewModel`, `GetCompletionModel`).

Each setting has its own getter, merge logic (`config_merge.go`) and
fallback rules. There is no general way to say "use a stronger model for
planning and a cheaper one for execution", and usage is not attributed to
the purpose it served, so it is hard to see which choice pays off.

## Design

### 150a. Named roles

A `roles` section in configuration mapping role names to a provider and
model:

```json
"roles": {
  "planner":    { "provider": "…", "model": "…" },
  "coder":      { "provider": "…", "model": "…" },
  "summarizer": { "provider": "…", "model": "…" }
}
```

- Built-in role names: `planner` (plan mode and plan edits, SP-148),
  `coder` (the main agent loop), `summarizer` (progress summaries and
  change summaries, SP-151, SP-157), `reviewer`, `commit`.
- Unset roles fall back to the main conversation's provider and model,
  exactly as today.
- Existing settings (`subagent_model`, `commit_model`, review and
  completion models) are read as aliases for the matching roles, so no
  configuration breaks.

### 150b. Resolution

One resolver (`ResolveRole(name) → provider, model`) replaces the
per-setting getters internally. Context profile resolution (SP-125) runs
against the resolved model. No code branches on a provider name
(SP-137).

### 150c. Metering

Every model call carries its role. The usage ledger
(`pkg/agent/usage_ledger.go`) and cost model record tokens and cost per
role, exposed in `/cost`-style views and in usage events so embedding
environments can attribute spend.

### 150d. Selection surfaces

- CLI: `/model --role planner <model>` and `sprout config` support.
- Web UI: role models in settings, collapsed by default.

## Acceptance criteria

- [ ] Role config parsed and merged across global/project config.
- [ ] Old settings map to roles (tests for each alias).
- [ ] Plan mode uses the `planner` role model when set; main loop uses
      `coder`.
- [ ] Usage ledger entries carry the role; per-role totals visible.
- [ ] Provider-name grep test still passes.

## Non-goals

- No automatic model routing by task difficulty.
- No per-role system prompts beyond what each feature already defines.

## Open questions

- Whether user-defined role names are allowed or the set stays fixed.
- Whether personas should declare a default role.
