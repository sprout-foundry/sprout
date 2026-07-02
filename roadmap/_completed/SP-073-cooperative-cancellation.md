# SP-073: Cooperative Cancellation — Thread Context So Stop Actually Aborts

**Status:** ✅ Implemented (2026-06-15; all 10 TODO(SP-034-1c) sites threaded, zero remain)

Ten long-running pipelines ignored the caller's context and passed `context.Background()` to LLM/provider calls, making Ctrl+C / Stop unable to abort them. The agent already held the right cancellation source (`a.interruptCtx`), but these sites didn't receive or forward it. The fix threaded a real `context.Context` from the caller (ultimately `a.interruptCtx` or the active turn context) down to each provider call, replacing `context.Background()`. Work was done in dependency order — leaf signatures accept `ctx` first, then callers pass the cancellation context — to contain the signature ripple. All 10 `TODO(SP-034-1c)` markers were removed as each site was converted.

## Key decisions

- Leaf functions add a leading `ctx context.Context` parameter rather than reaching for `a.interruptCtx` directly — keeps dependency explicit.
- Tool handlers forward their `Execute(ctx, env, args)` context; `Agent` methods use `a.interruptCtx` (same source as `GenerateResponse`).
- A canceled context propagates to the provider HTTP request (generic provider already honors `ctx`), returning promptly with `context.Canceled`.
- Defensive `context.Background()` fallbacks remain only where `ctx` could genuinely be nil (e.g., `ShellCommand.getContext()` when `SetContext` was never called).

## Artifacts

- code: `pkg/agent_tools/vision_pdf.go` — PDF OCR page-by-page loop threads `ctx` to `SendVisionRequest`
- code: `pkg/spec/extractor.go` — `SpecExtractor.Extract` accepts and forwards `ctx`
- code: `pkg/codereview/prompts.go` — `ReviewContext` builders accept `ctx` parameter
- code: `pkg/agent_commands/commit_review.go` — commit-review pipeline threads `ctx`
- tests: `pkg/agent_tools/vision_pdf_test.go` — context cancellation abort tests

Full specification archived — see git history for original content.
