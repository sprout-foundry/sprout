# SP-128: Compaction Drops User Query — Breaks Qwen3.5 (and strict chat templates)

**Status:** 🟢 Implemented (2026-07-21) — Option A + belt-and-suspenders fallback in `BuildCheckpointCompactedMessages`; covered by `TestBuildCheckpointCompactedMessages_*` in `pkg/agent/turn_checkpoints_orphan_test.go`.
**Severity:** High — blocks 50% of agentic tasks on Qwen3.5 models
**Discovered:** 2026-07-21
**Affected models:** Qwen3.5-9B, Qwen3.5-27B, any model with a strict chat template that validates user-message presence
**Not affected:** Gemma 4 (lenient template), DeepSeek (lenient template)

## Summary

When sprout's context compaction (rollup) fires during a long agentic session, it
can produce a message list that contains **no `role: "user"` message**. The
original user task message is consumed by the rollup checkpoint, and the
remaining messages are all `role: "assistant"` and `role: "tool"`. Models with
strict chat templates (Qwen3.5) reject this with `HTTP 400: No user query found
in messages.`

## Root cause

In `pkg/agent/turn_checkpoints.go`, `applyCheckpoints()` replaces a range of
messages (including the original user task) with a single `role: "assistant"`
summary message:

```go
// Line ~329
compacted = append(compacted, api.Message{
    Role:    "assistant",
    Content: summaryText,
})
```

If the user task message (typically `messages[0]` with `role: "user"`) falls
within the checkpoint range `[StartIndex, EndIndex]`, it gets replaced by the
assistant summary. The resulting compacted message list has no user message.

The Qwen3.5 chat template (`chat_template.jinja` lines 67-80) scans messages
in reverse looking for the last "real" user message (not a tool response). If
none is found, it raises:

```jinja
{%- if ns.multi_step_tool %}
    {{- raise_exception('No user query found in messages.') }}
{%- endif %}
```

Gemma 4's chat template doesn't have this validation, which is why this bug
wasn't observed during Gemma fine-tuning work.

## Reproduction

1. Serve Qwen3.5-9B via vLLM with `--tool-call-parser qwen3_coder --reasoning-parser qwen3`
2. Run sprout agent with a task that requires 8+ tool-call rounds (enough to
   trigger compaction at 32K context)
3. After compaction fires, the next LLM call returns HTTP 400

Observed on 3/6 broader agentic eval tasks (pyopenssl/security-audit,
promises/mrr-arc, Paint/err-ignore) — all failed after 20-40s of correct
tool use with this error.

## Evidence

From the Qwen3.5-9B baseline and SFT evals:
- Task completes several tool-call rounds correctly (grounded, real files cited)
- Compaction triggers when context approaches ~70% of 32K window
- Next LLM call returns: `client error: HTTP 400: No user query found in messages.`
- The model was doing the right thing before the error — this is not a model problem

## Proposed fix

**Option A (minimal): Preserve the original user task message** ✅ IMPLEMENTED

In `BuildCheckpointCompactedMessages()` (the spec referenced this as
`applyCheckpoints` — the function is actually named `BuildCheckpointCompactedMessages`
in `pkg/agent/turn_checkpoints.go`), always preserve `messages[0]` if it has
`role: "user"`, even if it falls within a checkpoint range. Insert it before
the summary:

```go
if nextIndex == checkpoint.StartIndex && nextIndex < len(messages) && messages[nextIndex].Role == "user" {
    compacted = append(compacted, messages[nextIndex])
}
```

**Belt-and-suspenders:** After the loop completes, scan the final compacted
slice for any `role: "user"` message. If none exists (defensive guard
against future code paths), prepend a synthetic fallback user message —
preferring `messages[0].Content` if it was a user message, otherwise a
generic `Continue the task.` placeholder.

**Option B (robust):** Subsumed by Option A + belt-and-suspenders above.

**Option C (cwd in compacted context):** ✅ Already implemented via the system
prompt (cwd is injected by `buildCurrentWorkingDirectorySection` and lives in
the cached system-prompt tail — see the prior perf commit that moved it there
to preserve prompt-prefix cache eligibility). Models can read the cwd from the
system prompt on every turn without it needing to be in user-message content.
**Status:** No further action needed.

## Impact

- Blocks 3/6 tasks on Qwen3.5-9B baseline eval (50% failure rate)
- Blocks 3/6 tasks on Qwen3.5-9B SFT eval
- Does NOT affect Gemma 4 models (lenient template)
- Will affect any model with a strict chat template that validates user-message presence

## Workaround (temporary)

Patch the served model's `chat_template.jinja` to not raise the exception:
replace `raise_exception('No user query found in messages.')` with a no-op.
Applied to the SFT model's merged directory for testing.

## Related

- `pkg/agent/turn_checkpoints.go:329` — the summary insertion point
- `pkg/agent/rollup.go:241` — the rollup checkpoint builder
- Qwen3.5 chat template: `raise_exception('No user query found in messages.')`
- Discovered during the sprout fine-tuning project (Qwen3.5-9B SFT eval)
