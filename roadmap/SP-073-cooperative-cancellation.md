# SP-073: Cooperative Cancellation — Thread Context So Stop Actually Aborts

**Status:** ✅ Implemented (zero TODO(SP-034-1c) markers remain; all 10 sites threaded)
**Date:** 2026-06-15
**Depends on:** none (completes the deferred SP-034-1c work)
**Priority:** Medium-High (closes a real "Stop doesn't stop" UX gap)
**Effort Estimate:** ~2-3 days

## Problem

Several long-running pipelines ignore the caller's context and pass
`context.Background()` (or no context) to the LLM/provider call, so **Ctrl+C /
Stop cannot abort them**. The work was explicitly deferred — there are **10
`TODO(SP-034-1c)` markers** across the codebase, each a variant of *"thread
caller ctx through X so Stop aborts."*

The agent already holds the right cancellation source: `a.interruptCtx` (used
correctly by, e.g., `Agent.GenerateResponse`). The gap is purely that these
sites don't receive or forward it — and the fix was deferred because changing
the signatures "ripples into many callsites."

Concretely, a user who kicks off a multi-page PDF OCR, a commit review, or a
spec extraction has no way to interrupt it; Stop is silently ignored until the
pipeline finishes on its own.

## Affected sites (the `TODO(SP-034-1c)` set)

| File:line | Pipeline | Current call |
|---|---|---|
| `pkg/agent_tools/vision_pdf.go:290` | PDF OCR, page-by-page loop | `SendVisionRequest(context.Background(), …)` |
| `pkg/agent_tools/vision_pdf.go:446` | PDF OCR, sibling path | same |
| `pkg/agent_tools/vision_analyze.go:135` | image analysis | background ctx |
| `pkg/agent_commands/commit_review.go:89` | commit-review pipeline | background ctx |
| `pkg/agent_commands/shell.go:94` | shell-script generation | background ctx |
| `pkg/spec/extractor.go:65` | `SpecExtractor` LLM call | `SendChatRequest(context.Background(), …)` |
| `pkg/spec/validator.go:68` | `ScopeValidator` LLM call | background ctx |
| `pkg/codereview/prompts.go:37,66` | `ReviewContext` build | background ctx |
| `pkg/agent/agent_getters.go:633` | `GenerateResponse` signature note | (already uses `interruptCtx`; ripple origin) |

## Proposed Solution

Thread a real `context.Context` from the caller (ultimately `a.interruptCtx`
or the active turn context) down to each provider call, replacing
`context.Background()`. Do it in dependency order so the signature ripple is
contained:

### Phase 1: Leaf signatures accept `ctx`

Add a leading `ctx context.Context` parameter to the leaf functions that today
fabricate a background context:

- `SpecExtractor.Extract` / `ScopeValidator.Validate` (`pkg/spec/`)
- `ReviewContext` builders (`pkg/codereview/prompts.go`)
- The vision OCR/analyze helpers (`pkg/agent_tools/vision_pdf.go`,
  `vision_analyze.go`)
- The commit-review and shell-script-gen helpers (`pkg/agent_commands/`)

Each forwards `ctx` to `SendChatRequest`/`SendVisionRequest` instead of
`context.Background()`.

### Phase 2: Callers pass the cancellation context

Update callers to pass the context they already have:

- Tool handlers receive a context via `Execute(ctx, env, args)` — forward it.
- `Agent` methods use `a.interruptCtx` (the same source `GenerateResponse`
  already uses). Optionally accept an explicit `ctx` parameter where a handler
  context is available, per the `agent_getters.go:633` note.

### Phase 3: Verify abort behavior + remove the TODOs

- A canceled context propagates to the provider HTTP request (the generic
  provider already honors `ctx` on its request), so an in-flight call returns
  promptly with `context.Canceled`.
- Each pipeline surfaces cancellation as a clean "aborted" result, not a crash.
- Delete the 10 `TODO(SP-034-1c)` markers as each site is converted.

## Success Criteria

- Pressing Ctrl+C / Stop during a multi-page PDF OCR, a commit review, or a
  spec extraction aborts the in-flight provider call within ~1s.
- `grep -rn "TODO(SP-034-1c)" pkg/` returns nothing.
- No remaining `context.Background()` in the affected pipelines' provider calls.
- `make build-all` + `go test ./...` green.

## Out of Scope

- Cancellation of non-LLM work (file walks, embeddings index builds) — those
  have their own contexts; this spec is specifically the SP-034-1c set.
- Changing the `interruptCtx` plumbing itself; this spec consumes it.

## Open Questions

1. Where a handler has no obvious context (deep helper), prefer adding the
   parameter over reaching for `a.interruptCtx` so the dependency stays
   explicit. Confirm per-site during Phase 2.
