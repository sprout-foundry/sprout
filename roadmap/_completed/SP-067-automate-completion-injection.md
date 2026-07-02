# SP-067: Automate Workflow Completion Injection

**Status:** ✅ Implemented (2026-06-06; completion messages auto-injected into agent context)

When `run_automate` is invoked as a tool call, a self-contained completion message is now injected back into the model via `InjectInputContext` when the background workflow finishes. This eliminates the need for the model to actively poll via `shell_command(check_background=...)` and enables autonomous watchdog patterns. The injection fires in the same goroutine that watches `proc.Done()`, carries workflow name/description/session ID/status/exit code, and includes the last 2KB of output on failure only. CLI-launched workflows are unaffected since they bypass the agent tool path entirely.

## Key decisions

- Injection is self-contained — carries all context needed since the model's context may have been compacted.
- Output tail bounded at 2048 bytes (`completionMessageTailLimit`) to avoid injecting enormous logs.
- Success messages omit the output tail block — only failure messages include diagnostic output.
- Context cancellation guard: if agent's `ctx` is cancelled (shutting down), injection is skipped.
- `readOutputTail` helper is error-tolerant — returns empty string on file-not-found or read errors.

## Artifacts

- code: `pkg/agent/tool_handlers_automate.go` — `buildAutomateCompletionMessage`, `readOutputTail`, injection in `proc.Done()` goroutine
- code: `pkg/agent/tool_registrations.go` — `run_automate` tool description mentions completion injection
- tests: `pkg/agent/tool_handlers_automate_test.go` — injection format, output tail, context-cancel skip tests

Full specification archived — see git history for original content.
