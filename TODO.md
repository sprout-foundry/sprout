# TODO

Active work tracked here. Completed items are removed once their parent spec is moved to ✅ Implemented in `roadmap/README.md` — the spec file itself is the historical record.

## SP-067: Automate Workflow Completion Injection
_Spec: `roadmap/SP-067-automate-completion-injection.md`_

When a model calls `run_automate` as a tool, the workflow starts in the background and the tool returns immediately with a `session_id`. To learn when it finishes, the model must actively poll via `shell_command(check_background=<id>)`. If the model doesn't poll — because it moved on to other work, the user interrupted, or the context was compacted — it never learns the workflow completed. This spec injects a self-contained completion message back into the model via the existing `InjectInputContext` pathway.

### Phase 1: Completion injection
- [x] SP-067-1a: Add `readOutputTail(path string, maxBytes int) string` helper function in `pkg/agent/tool_handlers_automate.go`. Reads the last N bytes of a file. Handles file-not-found and read errors gracefully (returns empty string). Used by the injection goroutine to include error context in the completion message.
- [x] SP-067-1b: In `pkg/agent/tool_handlers_automate.go`, in the `proc.Done()` goroutine (the one that publishes the `automate.session_ended` event), after publishing the event, call `a.InjectInputContext()` with a self-contained completion message. Capture the workflow `desc` (description) in the goroutine closure at launch time. The message must include: workflow filename, workflow description, session ID, status + exit code, and the last 2KB of captured output (via `readOutputTail`). Only fire when `handleRunAutomate` was called by the agent (not CLI-launched). Skip injection if the agent's event bus is nil or the context is cancelled. Example message format is in the spec.
- [x] SP-067-1c: Update the `run_automate` tool description in `pkg/agent/tool_registrations.go` to mention that the model will receive a completion injection when the workflow finishes, so it knows it can optionally defer polling.
- [x] SP-067-1d: Write tests in `pkg/agent/tool_handlers_automate_test.go` covering: (1) injection message content and format on success, (2) injection message content and format on failure (exit code != 0), (3) output tail reading (file exists, file missing, empty file), (4) no injection when context is cancelled, (5) self-contained message includes workflow name and description. Use the existing BackgroundProcess test harness or a lightweight mock. Verify the injection channel receives the expected message and that the output tail is bounded to the configured max bytes.
- [x] SP-067-1e: Run `make build-all` and `go test ./...` — verify no regressions across the full build and test suite.
