# SP-067: Automate Workflow Completion Injection

**Status:** ✅ Implemented (2026-06-06)
**Date:** 2026-06-06
**Depends on:** SP-065 (event bus, BPM, event types already defined)
**Priority:** Medium
**Effort Estimate:** ~1 day (single phase)

## Problem

When a model calls `run_automate` as a tool, the workflow starts in the background and the tool returns immediately with a `session_id`. To learn when it finishes, the model must **actively poll** via `shell_command(check_background=<id>)`. If the model doesn't poll — because it moved on to other work, or the user interrupted, or the context was compacted — it never learns the workflow completed.

This prevents autonomous watchdog patterns. The model cannot say "start this workflow and restart it until it passes" because it has no way to be notified of completion without spinning on a poll loop.

## Solution

When `run_automate` is invoked as a tool call (not the CLI), inject a self-contained completion message back into the model via the existing `InjectInputContext` pathway. The injection fires in the same goroutine that already watches `proc.Done()` and publishes the `automate.session_ended` event.

### Injection message format

The message must be **self-contained** — it cannot assume the model remembers launching the workflow (context may have been compacted). It carries:

- Workflow filename and description (captured at launch time)
- Session ID
- Status + exit code
- Last 2KB of captured output (only on failure, for diagnostics)

Example (failure):

```
[automate] Background workflow completed:
  Workflow: ci-validation.json
  Description: Run lints, tests, and builds with fail-fast
  Session: bg-automate-a1b2c3
  Status: error (exit code 1)
  Output (last 2KB):
    pkg/foo/bar_test.go:42: assertion failed
    FAIL    pkg/foo/bar_test.go
    make: *** [test] Error 2
```

Success messages omit the `Output (last 2KB)` block — the model gets the status and exit code without needing the tail.

### When injection fires

Only when the workflow was launched via the `run_automate` tool — i.e., when `handleRunAutomate` was called by the agent. CLI-launched workflows (`sprout automate run`) go through `cmd/automate.go` and never touch this code path, so no guard is needed.

### Guards

- **Event bus / agent nil check**: If the agent's event bus is nil (shouldn't happen in the tool-call path, but defensive), skip injection.
- **Context cancellation**: If the agent's `ctx` is cancelled (shutting down), the goroutine's `select` falls into the `<-ctx.Done()` case and the injection is skipped.
- **Output tail bounded**: `completionMessageTailLimit = 2048` (constant) caps the tail read by `readOutputTail` to avoid injecting enormous logs into the model's context.

### Output tail helper

`readOutputTail(path string, maxBytes int) string` reads the last N bytes of a file. Handles file-not-found and read errors gracefully (returns empty string), and strips non-printable control characters before returning. Used by the injection goroutine to include error context.

## Implementation status

| Phase | Status | Where |
|---|---|---|
| 1 Extract `buildAutomateCompletionMessage` so it can be unit-tested without real BPM | ✅ done | `pkg/agent/tool_handlers_automate.go` |
| 2 Inject via `a.InjectInputContext(injectMsg)` in the `proc.Done()` goroutine, with `ctx.Done()` fallback | ✅ done | same file, inside `handleRunAutomate` |
| 3 `readOutputTail` helper (2KB cap, control-char strip, error-tolerant) | ✅ done | same file |
| 4 `run_automate` tool description mentions completion injection so the model knows it can defer polling | ✅ done | `pkg/agent/tool_registrations.go:486` |
| 5 Tests — injection format (success + failure), output tail (exists/missing/empty), context-cancel skip, self-contained payload | ✅ done | `pkg/agent/tool_handlers_automate_test.go` |

## Future work

- The model could be instructed (via tool description or system prompt) to recognize these injections and autonomously retry failed workflows, creating a true watchdog loop.
- A `max_retries` field in workflow JSON could cap automatic retries.
