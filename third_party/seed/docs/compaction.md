# Compaction

Context compaction reduces conversation history to fit within the
provider's context window. The pipeline is progressive and loss-minimizing:
preserve raw context as long as possible, and only collapse what's
strictly necessary, one step at a time.

## Design principles

1. **Raw history below the trigger.** Short and medium conversations,
   and long single-turn tool-iteration chains, see their *full* prior
   tool outputs and file reads. The model can refer back instead of
   re-reading.
2. **Iterative, minimum-loss compaction at and above the trigger.** When
   pressure forces us to compact, apply the *smallest possible* lossy
   step (substitute the oldest turn with its summary), re-estimate,
   and stop the moment we're back under target.
3. **Substitute before drop, drop before truncate.** Each tool
   preserves more information than the next; exhaust the gentler ones
   first.
4. **Provider-overflow retry is the safety net.** If the token
   estimator is off and we send too much, the retry loop catches
   `ContextOverflowError` and runs an aggressive recovery compaction.
   The proactive buffer doesn't have to absorb every estimator drift —
   the retry path will.

## High-level pipeline

```
state.Messages()  (raw; checkpoint indices valid against this slice)
        │
        │  prepareMessages():
        │    1. If estimate > trigger × ContextSize, run Phase 0:
        │         Phase 0a: IterativelySubstituteCheckpoints  ← loss-min
        │         Phase 0b: IterativelyMaskOldestConsumedToolResults
        │       Each stops the moment estimate ≤ target.
        │    2. Strip system messages, prepend current system prompt,
        │       drain transients, ANSI-sanitize, dedupe (always safe).
        ▼
prepared messages (system + body, post-Phase-0)
        │
        │  Conversation loop:
        │    if estimate(prepared) > trigger × ContextSize:
        │      Phase 1 → CompactWith() drops summaries
        │      Phase 1.5 → drops oldest turns
        │      Phase 2 → emergency truncate
        ▼
final ChatRequest.Messages
        │
        │  If provider returns ContextOverflowError:
        │    tryContextOverflowRecovery() runs CompactWith() with
        │    target = recoveryCompactionTargetFraction (0.70).
        │    Retry with the slimmer slice.
        ▼
provider call (success)
```

Two structural invariants keep this design coherent:

- **Phase 0 (iterative, loss-min) lives in `prepareMessages()`** because
  it operates on the raw `state.Messages()` slice where checkpoint
  `StartIndex` / `EndIndex` references remain valid.
- **Phase 1+ (drops, truncate) lives in `CompactWith()`** and runs from
  the conversation loop on the post-prepareMessages slice. The loop
  does NOT pass the checkpoint list to `CompactWith` — Phase 0 already
  ran on raw indices. Drops operate on the inserted summary messages
  directly (marked via `Meta["checkpoint"]="true"`).

Code that calls checkpoint-substitution APIs from inside `CompactWith`
when invoked from the loop would double-substitute or operate on
invalid indices.

## The opening task anchor is never dropped

The opening anchor is the system message plus the first genuine user
message and the assistant reply that immediately follows it without
tool calls (`compactionAnchorRange`). It is the only verbatim record of
what the session was asked to do, so **every** drop and truncate phase
treats it as permanent:

- Phase 1 (`dropOldestCheckpointSummaries`)
- Phase 1.5 (`dropOldestTurns` → `findOldestCompleteTurn` /
  `findDropableMessage`)
- Phase 2 (`emergencyTruncateWithTarget`, both the trim and the drop
  loop)
- the pruner's settle pass

Before this rule, each of those phases scanned from index 1 and would
happily remove the opening turn first — the summarizers preserved the
anchor, but the drop pipeline then deleted it, and the model carried on
from summaries that still claimed to have kept the framing. Synthetic
summary messages are *not* the task statement, so they are skipped when
locating the anchor: a summary that sits early in the list stays
droppable even when it precedes the first genuine user message.

**Known corner — an anchor that is itself the overage.** If the first
user message alone exceeds the target, protection means the pipeline
cannot settle and the trigger re-fires. That is the deliberate
trade-off, and the same mitigations apply as for the short-but-fat
corner above.

## Structural summarization (`CompactWithLLMSummary`)

`CompactWithLLMSummary` rewrites the middle into one summary message,
preserving the anchor and the last `StructuralRecentToKeep` messages.
Two rules keep repeated compaction from compounding its own loss:

1. **Prior summaries are carried forward, not re-summarized.**
   `spliceSummary` inserts the summary at the anchor boundary, so
   earlier passes accumulate as a leading run of checkpoint-marked
   messages. `splitLeadingSummaries` separates that run from the raw
   history after it; only the raw span is summarized, and the carried
   blocks are re-emitted verbatim ahead of the new summary. Otherwise a
   third compaction would be a summary of a summary of a summary.
   Carried blocks stay checkpoint-marked, so Phase 1 remains the release
   valve when they grow too many.
2. **The graduated layered split is for a first compaction.** Once
   summaries are already present the middle is consolidated in a single
   pass (the merge-into-an-anchor shape) rather than split into three
   layers that would each re-narrate the carried blocks.

### Artifact records

A summary is prose, and prose loses file state first: which paths were
touched, and how. `StructuralOptions.ArtifactBlock` lets the consumer
supply that record, which seed appends to the summary body **once per
pass**, after the layered merge. Attaching the record from inside the
summarizer instead would put one copy per layer into the single merged
body — the layered split calls the summarizer three times.

The hook is skipped when the summarizer produced nothing. A body that is
only an artifact record would otherwise be spliced in place of the
narrative as if it were a summary, suppressing the rule-based fallback
that an empty summary is meant to trigger.

Note that `spliceSummary` demotes the anchor's user message to a
past-tense stub that tells the model to read the summary instead. That
is deliberate (it stops the model re-reading a stale prompt as a live
instruction), but it means the anchor's *content* is paraphrased after
the first compaction even though its *position* is permanent.

## Turn Checkpoints

Turn checkpoints are summaries of completed conversation turns, stored
in `State` and consumed by Phase 0 substitution.

`TurnCheckpoint` (`core/turn_summary.go`):

- `StartIndex` / `EndIndex` — indices into `state.Messages()`
- `UserMessage` — original user query (truncated to 2000 chars)
- `Summary` — narrative description of what happened
- `ActionableSummary` — bullet list of accomplishments (file paths,
  commands, results)
- `FileChanges []FileChange` — git-style file manifest (M / A / D / R)
  for the turn. Optional; populated by the consumer via OnCheckpoint or
  by constructing the checkpoint directly.
- `RevisionID string` — consumer's pointer to the persisted change set
  for this turn. When set, the summary text should reference it so the
  model can call the history tool to recover the exact diff.
- `ArchiveLine string` — single-line condensation produced by
  meta-compaction. When non-empty,
  `BuildCheckpointCompactedMessages` prefers it over Summary /
  ActionableSummary.

`TurnSummaryBuilder.Build()` parses the turn's messages and produces
the narrative + actionable summaries. Consumers wanting to attach
`FileChanges` / `RevisionID` either populate them via the
`OnCheckpoint` callback or construct the checkpoint directly and call
`state.AddCheckpoint()`.

Checkpoint indices reference the raw `state.Messages()` slice. The
state slice only appends — never inserts or deletes in the middle — so
indices remain stable across calls.

## Phase 0a — Iterative checkpoint substitution

`IterativelySubstituteCheckpoints(messages, checkpoints, target, estimateFn)`
(`core/checkpoint_compaction.go`):

```
for n := 1; n ≤ len(sorted_checkpoints); n++ {
    candidate := BuildCheckpointCompactedMessages(messages, sorted[:n])
    if estimateFn(candidate) ≤ target { return candidate, n, true }
}
return last_candidate, len(sorted), false
```

Each iteration substitutes the *oldest n* checkpoints; the rest stay
raw. The first iteration `n=1` collapses only the very oldest turn —
the minimum-information-loss step that can relieve pressure.

The caller supplies `estimateFn`. The conversation handler passes
`provider.EstimateTokens` so Phase 0 and the loop's trigger decision
agree on whether we're under target.

## Phase 0b — Iterative observation masking

`IterativelyMaskOldestConsumedToolResults(messages, nameFn, target, estimateFn)`
(`core/conversation_optimizer.go`):

Same pattern as 0a but masks one big consumed tool result at a time,
oldest first. A result is eligible when:

- Its content exceeds `observationMaskMaxChars` (3000 bytes)
- It sits before the last assistant message (i.e., "consumed")
- It's outside the last `observationMaskKeepLast` (5) tool-result
  window

The mask replaces the result body with a compact
`[PREVIOUS RESULT: <tool_name>, N chars, M lines]` placeholder so the
model still knows *what* tool ran without re-reading its full output.

Phase 0b runs after Phase 0a — checkpoint substitution collapses
entire turns (including their tool results), so doing it first avoids
wasted masking on results that are about to be collapsed.

## Phase 1+ — Drop-based compaction (CompactWith)

`CompactWith(CompactInputs)` (`core/compaction.go`) is the drop-based
fallback. It runs when Phase 0 in `prepareMessages` couldn't relieve
enough pressure, or directly from the loop for non-Phase-0 paths
(pruner / llmSummarizer branches).

Callers from the conversation loop pass `CompactInputs` with
`Checkpoints` and `MaskNameFn` *omitted* — Phase 0 already ran on the
raw slice. The recovery path passes them because its message list is
the prepared slice from the failed request and substitution can still
help shrink it.

- **Target**: 85% of the context window
  (`emergencyTargetFraction`).
- **Phase 0a/0b inside `CompactWith`**: only fire when the caller
  supplies Checkpoints / MaskNameFn. Used by the recovery path.
- **`TargetTokens` override**: when non-zero and below the derived
  target, every phase — early exit, Phase 0a/0b stop conditions, and
  the Phase 1+ drop/truncate targets — uses it instead of the derived
  85%. The conversation loop sets it on proactive passes when the
  configured trigger sits below the derived target (see
  [Settle-below-trigger](#settle-below-trigger)); the recovery path and
  default configurations (trigger 0.85 = derived target) never set it.

### Settle-below-trigger

A proactive compaction pass must land the estimate BELOW the loop's
trigger. Without that guarantee the pass settles over the trigger and
the loop re-fires compaction every iteration — thrash (observed as
several auto-compactions within seconds). Two mechanisms enforce it:

- **Pruner branch** (`PruneCallOptions.TargetTokens`): after the
  strategy's own keep-set is built, a final `settleBelowTarget` pass
  drops whole turns (turn-aware, same `findOldestCompleteTurn` machinery
  as Phase 1.5 — an assistant-with-tool-calls is never orphaned from
  its results) outside the protected recent window until the estimate is
  at or below the target. Best effort: `MinMessagesToKeep` and the
  recent window always bind.
- **Compact branches** (`CompactInputs.TargetTokens`): set only when
  the configured trigger sits below the derived 0.85 target — the only
  configuration where those branches can settle over the trigger. At the
  default trigger the derived target already equals the trigger and
  behavior is byte-identical to the historical pipeline.

The settle target is `trigger − 5%` of the window: enough growth room
for a few tool-heavy iterations, no more — drop-based settling is lossy,
so tighten only as far as the thrash fix requires. The overflow-recovery
path keeps its own more aggressive target (`recoveryCompactionTargetFraction`)
unchanged.

**Known corner — short-but-fat histories.** Settle can only drop outside
the protected recent window. A conversation with ≤ `recentMessagesToKeep+1`
messages whose overage lives inside that window (e.g. 15 messages
carrying hundreds of thousands of tokens of tool output) can never
settle; the trigger re-fires each iteration. That is inherent to "don't
eat the live causal chain" — the consumer's output-budget floor and the
overflow-recovery path are the mitigations for that corner, not further
drops.

### Phase 1 — Drop Checkpoint Summaries
- Removes oldest checkpoint summary messages
  (`Meta["checkpoint"]="true"`) outside the recent boundary (last 24
  messages)
- Strategy label: `"checkpoint_drop"`

### Phase 1.5 — Drop Oldest Turns
- Drops complete (user + assistant + tool chain) turns oldest-first
- Strategy label: `"tool_trim"`

### Phase 2 — Emergency Truncation
- Trims tool results to 1500 chars (head 750 + tail 500)
- Trims older user/assistant messages to 1200 chars (head 600 + tail
  400)
- If still over, drops oldest non-system messages one at a time
- Strategy label: `"truncation"` (content trimming only) or
  `"emergency"` (messages dropped)

### Token estimation
- `roughTokens()`: 4 chars ≈ 1 token, +20 per tool call, +10 per
  message overhead. Used internally by the drop functions (Phase 1+).
- `provider.EstimateTokens()`: real-provider tokenizer when available.
  Used by Phase 0 (`IterativelySubstituteCheckpoints`,
  `IterativelyMaskOldestConsumedToolResults`) and by the loop's
  trigger decision.

The two estimators can disagree (different word/token mappings).
Inter-phase early-exit checks use whichever estimator the *next*
phase will also use, so we don't no-op in the middle of an
inconsistent comparison.

## max_tokens math

When deriving `max_tokens` for the provider request
(`conversation.go`):

```
maxTokens = ContextSize − estimate(prompt) − safetyBuffer
safetyBuffer = max(estimate × 0.10, 1024 floor)
maxTokens = min(maxTokens, ProviderInfo.MaxOutputTokens || 16384)
maxTokens = max(maxTokens, 1)
```

- **Proportional buffer (10%)**: the rough estimator can be off by
  ~10% for code-heavy or non-English content. Scaling the buffer with
  the prompt keeps absolute headroom realistic where it needs to be.
- **1024 floor**: tiny prompts still need *some* margin for the
  model's response budget.
- **MaxOutputTokens cap**: on large-context models with empty
  conversations the derived value would be hundreds of thousands of
  tokens — providers often reject those requests as malformed. Cap at
  the model's stated max-output (when known) or 16k by default.

## Meta-compaction (archive of old summaries)

`ArchiveOldCheckpoints(ctx, checkpoints, keepRecent, summarizer)`
(`core/checkpoint_archive.go`):

Constants: `MetaCompactionThreshold = 100`, `MetaArchiveBatchSize = 25`.

When a long-running session accumulates more than ~100 checkpoint
summaries, the prompt becomes dominated by past summaries with little
recent raw context. The fix: collapse each old summary into a single
commit-title-style line (`ArchiveLine`), ~80 chars max.

This is a consumer-driven op — `ArchiveOldCheckpoints` exposes the
primitive but doesn't auto-trigger. Recommended trigger points:

- After each new checkpoint is added when state's checkpoint count
  crosses the threshold (consumer-side maintenance pass).
- At session save / restore boundaries when the consumer can afford a
  blocking LLM call.

The function is idempotent: a checkpoint that already has a non-empty
`ArchiveLine` is skipped. Errors from the summarizer are absorbed (the
affected checkpoints keep their original `Summary`).

When `ArchiveLine` is set, `BuildCheckpointCompactedMessages` uses it
verbatim, bypassing the `Summary` / `ActionableSummary` picker.

## Compaction events

When compaction runs and changes messages, publishes `compaction`
event:

- `strategy` — which phase resolved it (e.g. `"substitute"`,
  `"substitute+mask"`, `"checkpoint_drop"`, `"emergency"`)
- `messages_before` / `messages_after` — counts before/after
- `message_count_delta` — number removed
- `tokens_saved` — estimated tokens freed

Strategies are concatenated with `+` when multiple phases fire in one
call (e.g. `"substitute+checkpoint_drop"`).

## Conversation Optimizer

`ConversationOptimizer` (`core/conversation_optimizer.go`)
deduplicates redundant content. Always-on dedupe (file reads,
transient shell commands) runs in `prepareMessages()` via
`OptimizeConversationWithOptions(MaskConsumedToolResults: false)`. The
masking pass is gated separately and runs only as part of Phase 0b.

### File-read dedup
- Tracks file reads by path; replaces earlier reads with identical
  content (SHA-256 hash comparison) with `[Earlier file read: path]`
- Bounded to max 5 records per file per content hash

### Shell-command dedup
- Only applies to transient commands (`ls`, `find`, `pwd`, `cat`,
  `echo`, `head`, `tail`, `wc`)
- Replaces earlier output matching the latest output with
  `[Earlier command output: cmd]`
- Bounded to max 10 records per command

## What to test when you touch this

- `core/compaction_progressive_test.go` — Phase 0 iteration semantics,
  meta-compaction primitives, max_tokens math
- `core/checkpoint_compaction_test.go` — substitution mechanics
- `core/conversation_test.go` — `prepareMessages` integration
- `core/conversation_optimizer_test.go` — gated masking + dedupe
- `internal/test/e2e_test.go` — checkpoint compaction over multi-turn
  conversations, recent-turns-intact, drops-when-over-context
