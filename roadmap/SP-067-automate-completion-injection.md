# SP-067: Automate Workflow Completion Injection

**Status:** 📋 Proposed
**Date:** 2026-06-06
**Depends on:** SP-065 (event bus, BPM, event types already defined)
**Priority:** Medium
**Effort Estimate:** ~1 day (single phase)

## Problem

When a model calls `run_automate` as a tool, the workflow starts in the background and the tool returns immediately with a `session_id`. To learn when it finishes, the model must **actively poll** via `shell_command(check_background=<id>)`. If the model doesn't poll — because it moved on to other work, or the user interrupted, or the context was compacted — it never learns the workflow completed.

This prevents autonomous watchdog patterns. The model cannot say "start this workflow and restart it until it passes" because it has no way to be notified of completion without spinning on a poll loop.

## Proposed Solution

When `run_automate` is invoked as a tool call (not the CLI), inject a self-contained completion message back into the model via the existing `InjectInputContext` pathway. The injection fires in the same goroutine that already watches `proc.Done()` and publishes the `automate.session_ended` event.

### Injection message format

The message must be **self-contained** — it cannot assume the model remembers launching the workflow (context may have been compacted). It carries:

- Workflow filename and description (captured at launch time)
- Session ID
- Status + exit code
- Last 2KB of captured output (so the model can reason about failures without a follow-up tool call)

Example:

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

### When injection fires

Only when the workflow was launched via the `run_automate` tool — i.e., when `handleRunAutomate` was called by the agent. CLI-launched workflows (`sprout automate run`) go through `cmd/automate.go` and never touch this code path, so no guard is needed.

### Guard: only inject when an active agent session exists

If the agent's event bus is nil (shouldn't happen in the tool-call path, but defensive), skip injection. If the context is cancelled (agent shutting down), skip injection.

### Output tail helper

A small function `readOutputTail(path string, maxBytes int) string` reads the last N bytes of a file. Handles file-not-found and read errors gracefully (returns empty string). Used by the injection goroutine to include error context.

## Implementation

### Files changed

1. **`pkg/agent/tool_handlers_automate.go`** — In the `proc.Done()` goroutine, after publishing the `session_ended` event, call `a.InjectInputContext()` with the self-contained message. Capture `desc` in the goroutine closure.

2. **`pkg/agent/tool_handlers_automate.go`** — Add `readOutputTail()` helper function.

3. **`pkg/agent/tool_registrations.go`** — Update `run_automate` tool description to mention that the model will receive a completion injection, so it knows it can optionally defer polling.

4. **`pkg/agent/tool_handlers_automate_test.go`** (new) — Tests for:
   - Injection message content and format on success
   - Injection message content and format on failure (exit code ≠ 0)
   - Output tail reading (file exists, file missing, empty file)
   - No injection when context is cancelled
   - Self-contained message includes workflow name and description

### Test strategy

Use the existing `BackgroundProcess` test harness (or a lightweight mock). Verify:
- The injection channel receives the expected message
- The message contains workflow name, description, session ID, status, and output tail
- Context cancellation suppresses the injection
- Output tail is bounded to the configured max bytes

## Future work

- The model could be instructed (via tool description or system prompt) to recognize these injections and autonomously retry failed workflows, creating a true watchdog loop.
- A `max_retries` field in workflow JSON could cap automatic retries.
