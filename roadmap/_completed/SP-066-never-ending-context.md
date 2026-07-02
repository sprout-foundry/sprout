# SP-066: Never-Ending Context — Substitution-First Context Management, Hierarchical Rollups, and Embedded Memory Recall

**Status:** ✅ Substantially Shipped (Phase 3d deferred)
**Date:** 2026-06-05 (proposed), 2026-06-08 (status update)
**Depends on:** existing `TurnCheckpoint` / seed structural compaction (`pkg/agent/turn_checkpoints.go`, `pkg/agent/seed_integration.go`), existing embedding manager (`pkg/agent/turn_embedding.go`, `pkg/embedding/`)
**Priority:** High

## Status snapshot (2026-06-08)

Every phase except 3d landed:

| Phase | Status | Where it lives |
|---|---|---|
| 1a Substitute every prompt build | ✅ shipped | `pkg/agent/turn_checkpoints.go::BuildCheckpointCompactedMessages` |
| 1b Model-aware response budget (15+10+5% = 30% reservation) | ✅ shipped | `pkg/agent/context_budget.go::computeCompactionTriggerFraction` |
| 1c Pre-turn prediction + hysteresis | ✅ shipped | Same file, drives the trigger at 70% of effective max |
| 1d Fall-through compaction (rare) | ✅ shipped | seed's LLM compaction fires only when substitution can't free enough |
| 1e `context_management_diagnostic` telemetry | ✅ shipped | `pkg/events/events.go::ContextManagementDiagnosticEvent` |
| 2a `TurnCheckpoint` ID/Level/CoveredTurns/SourceCheckpointIDs | ✅ shipped | `pkg/agent/types.go:64-76` |
| 2b `/compact` keeps current behavior | ✅ shipped | `pkg/agent_commands/compact.go` unchanged |
| 2c Background rollup worker | ✅ shipped | `pkg/agent/rollup.go` + `prompts/rollup_prompt.md` |
| 2d Recency window (recent K turns full-fidelity) | ✅ shipped | `recentTurnsToPreserve` in rollup config |
| 2e FileChanges manifest propagates up the hierarchy | ✅ shipped | rollup.go preserves union of source set |
| 2f Persistence migration (additive `omitempty` fields) | ✅ shipped | Old state JSON deserializes cleanly |
| 2g AGENTS.md "Context architecture" section | ✅ shipped | Present in CLAUDE.md |
| 3a Embedding store as permanent memory | ✅ shipped (doc + behavior) | Store survives `/compact` and rollup by design |
| 3b Embed at every level (rollups too) | ✅ shipped | `pkg/agent/rollup_embedding.go` |
| 3c Recall on user-turn ingest | ✅ shipped | `pkg/agent/semantic_recall.go::InjectSemanticRecall`, called from `seed_integration.go:590` |
| **3d Embeddings as rollup tie-breaker** | **⏸ Deferred** | See "Why 3d is deferred" below |
| 3e `recall_diagnostic` telemetry | ✅ shipped | `pkg/events/events.go::RecallDiagnosticEvent`, emitted via `agent_events.go::PublishRecallDiagnostic` |

## Why 3d is deferred

Auditing the user's real session corpus on 2026-06-08 produced the data
that argued against shipping 3d:

- **379 persisted sessions, 5,651 total checkpoints, only 84 rollups
  (1.5%).** Every single rollup came from sprout's own `cmd/` test suite
  hitting the mock LLM provider — `"working_directory": ".../sprout/cmd"`,
  checkpoint summaries literally reading `"Test response from mock provider"`.
- **Zero rollups have ever fired on real user workloads.** The threshold
  is `recentTurnsToPreserve (10) + rollupTriggerCount (20) = 30` Level-0
  checkpoints. The longest observed real session reached 21 — nine short
  of the trigger.
- Phase 1 substitution is doing all the work: 5,567 Level-0 checkpoints
  recorded across the corpus, fired on every prompt build by
  `BuildCheckpointCompactedMessages`. Rollups are a dormant safety net.

3d would optimize a code path that doesn't execute in real usage. The
ROI is zero until either (a) the threshold is lowered enough to make
rollups common, or (b) we see telemetry from another deployment where
sessions routinely cross 30 checkpoints. Neither precondition exists.

The roadmap item stays open conceptually but tagged ⏸ to make the
"don't pick this up without first revisiting whether rollups even
fire" reasoning durable.

## Adjacent question raised by the audit (not part of this spec)

The data suggests `rollupTriggerCount + recentTurnsToPreserve = 30` may
be set higher than real workloads exercise. The right next experiment —
separate from 3d — is to drop the threshold (e.g., to 15+5=20 or 10+5=15)
and see whether rollups become useful in practice. That's a one-line
constant change in `rollup.go`. Filed as adjacent rather than as part of
SP-066 because it's a calibration question, not a feature gap.

## Test isolation note (2026-06-08)

The audit of real sessions surfaced that `~90` test-generated session
JSONs had been leaking into the developer's real `~/.sprout/sessions/`
because `cmd/` tests built real Agents without overriding
`getStateDirFunc`. Fixed in the same patch series:

- `pkg/agent/testing_state_isolation.go` (new): `SetTestStateDirHook`,
  `NewTestStateDir(t)`, `SnapshotRealStateDir`, `AssertNoStateLeak`.
- `cmd/main_test.go`, `pkg/agent_commands/main_test.go`,
  `pkg/commands/main_test.go`, `pkg/webui/main_test.go` (new): package-
  wide TestMain that installs the hook and runs the Layer-5 leak detector.
- 215 polluted session JSONs deleted from `~/.sprout/sessions/`,
  preserving 163 real ones.

Documented here because the audit that produced these findings was
inseparable from the SP-066 3d evaluation: without the test pollution
the rollup data would have looked different (84 → 0 rollups in the
corpus), and the deferral argument would have been even stronger.

## Background

Sprout already has all the primitives needed for indefinite-length conversations; they're just not composed into a coherent pipeline.

1. **Per-turn `TurnCheckpoint`** — every completed user turn writes a `Summary` + `ActionableSummary` + `FileChanges` manifest into `AgentState.TurnCheckpoints` (`pkg/agent/types.go:30`). These are the granular building blocks. Summary generation itself is an LLM call, but it happens once per turn in the background and the result is reused indefinitely.
2. **Seed structural substitution** — fires when `current_context_tokens / max_context_tokens` crosses a pruning threshold (`pkg/agent/pruning_config.go:15`). Uses `BuildCheckpointCompactedMessages` to substitute checkpoint summaries for original messages in-place (`pkg/agent/turn_checkpoints.go:267`). This is the cheap, deterministic operation that ought to be the default lever.
3. **Manual `/compact`** — `pkg/agent_commands/compact.go`. Runs a fresh whole-history LLM summarization over everything before the latest user turn and then *wipes* all `TurnCheckpoint`s (`compact.go:121`). Coherent recap, but expensive — it should be a deliberate user choice, not an automatic survival mechanism.
4. **Per-turn embeddings** — `EmbedAndStoreTurn` at `pkg/agent/turn_embedding.go:15` writes a `VectorRecord` to the conversation store for every turn's prompt + actionable summary. This persists independently of `TurnCheckpoint` list manipulation. The store is unused by the context-management path today.

The system has three distinct operations available, but treats them as one. Naming them precisely is the prerequisite for the rest of this spec:

- **Substitute** — replace older message ranges with their existing per-turn or rollup summaries at prompt-build time. No new LLM call. Deterministic. Free at runtime; the LLM cost was paid once when each checkpoint was recorded.
- **Rollup** — fold N existing summaries into 1 coarser summary. One LLM call, amortized across every future prompt build that substitutes the result. Background, scheduled by count threshold per level.
- **Compact** — full whole-history LLM summarization, today's `/compact` behavior. Expensive, on-demand, wipes the active `TurnCheckpoint` list. The user's deliberate "collapse this conversation to one summary" hammer.

Embeddings are a fourth, orthogonal layer: a persistent memory store independent of the active context window.

## Problem

Three observed failure modes:

1. **Auto-substitution fires too late, the model runs out of budget on thinking, and replies empty.** When `current/max` context usage approaches the pruning threshold, the model's remaining budget gets consumed by thinking tokens before it can emit any user-visible text. The user sees an empty reply and reaches for manual `/compact`. Two root causes interact:
   - **(1a) Threshold ignores headroom needed for thinking + tool calls + output.** The trigger compares prompt size against context limit, but doesn't reserve a worst-case allotment for model thinking + the response itself.
   - **(1b) Threshold doesn't fire pre-emptively enough.** By the time `current_context_usage` crosses the configured pruning fraction, the *next* turn's user input + tool output is already too big to fit usefully.

2. **`/compact` is used as a survival tool.** Because substitution doesn't fire reliably, users invoke `/compact` to escape the failure in (1). The expensive whole-history LLM call ends up being a daily workflow instead of an occasional deliberate reset.

3. **No hierarchical rollup.** After enough turns, even the list of per-turn summaries becomes large. There's no mechanism to fold N summaries into one coarser summary, so unbounded-length chats grow unboundedly. We need a recursive structure: turn → multi-turn rollup → multi-rollup-rollup, with each level steered by recency.

A side-effect of fixing 1–3 is that **embeddings can plausibly do useful work** — currently they're computed but never read by the context-management path. Once the rollup hierarchy exists, semantic retrieval can re-surface old-but-relevant detail that lives only in the embedding store, even after `/compact` has wiped the active checkpoint list.

## Proposed Solution

### Phase 1: Substitution-first context management (`substitution_default`)

Make substitution the default, automatic, every-prompt-build operation. Compaction becomes a rare fall-through (only when substitution can't free enough room) plus the explicit `/compact` command.

#### 1a. Substitute aggressively at prompt build time

Today, substitution only fires when the pruning threshold is crossed. Change to: at every prompt build, walk the message list and substitute any range covered by a `TurnCheckpoint` whose `Summary` exists, regardless of current headroom. The recent-K window stays full-fidelity (see Phase 2d). Substitution is free; doing it every prompt build means we always present the smallest viable context to the model.

#### 1b. Reserve a model-aware response budget when computing headroom

The current trigger:

```
should_act = current_context_tokens / max_context_tokens > threshold
```

Replace with:

```
effective_max = max_context_tokens − reserved_for_response − reserved_for_thinking − reserved_for_tool_io
should_act = current_context_tokens / effective_max > threshold
```

Conservative initial defaults (we settled on conservative-by-default rather than per-provider precision, because under substitution-first the reservation only matters for the rare fall-through to LLM compaction):

- `reserved_for_response`: 15% of `max_context_tokens`
- `reserved_for_thinking`: 10% of `max_context_tokens` (or the provider-reported value when available — Gemini `thinking_config.thinking_budget`, Claude per-call thinking config)
- `reserved_for_tool_io`: 5% of `max_context_tokens`

Total: 30% reservation. Model-registry-level overrides can refine this later, telemetry-driven.

#### 1c. Trigger earlier and pre-emptively

- **Pre-turn prediction.** Before sending a new prompt, estimate post-substitution size for the *next* prompt (current substituted size + expected user input + expected tool result sizes from the previous turn). If predicted post-turn usage exceeds the threshold, schedule a rollup *now* (Phase 2) so it's ready before the next substitution pass needs it.
- **Hysteresis.** Once the trigger fires and an action runs, drop below a *lower* secondary threshold (e.g., 50% of `effective_max`) so we don't bounce on every turn.

#### 1d. Fall-through to compaction is rare-by-design

If after substitution the prompt is *still* over `effective_max`, fall through to an LLM-driven compaction over the un-substituted recent window (the K most-recent turns, since older content is already substituted). This is the only automatic LLM-compaction path; in healthy operation it should fire approximately never.

#### 1e. Telemetry

Emit `context_management_diagnostic` events with `{current_tokens, max_tokens, effective_max, reserved_response, reserved_thinking, substituted_count, fallthrough_to_compact: bool}`. Surface in the WebUI metrics panel so we can verify substitution is doing the heavy lifting and the fall-through fires approximately never.

### Phase 2: Hierarchical rollup (`rollup_machinery`)

Treat a rollup *as* a turn checkpoint whose `StartIndex`/`EndIndex` describe a historical span rather than a slice of the current `messages[]`. Substitution then operates uniformly on Level-0 and Level-N items.

#### 2a. Generalize `TurnCheckpoint` to support multi-turn rollups

Add fields to `TurnCheckpoint`:

```go
type TurnCheckpoint struct {
    // ... existing fields ...

    // ID is a stable identifier for this checkpoint, independent of
    // its position in the TurnCheckpoints slice. Required so rollups
    // can reference their sources unambiguously.
    ID string `json:"id,omitempty"`

    // Level is the rollup depth. 0 = per-turn (existing behavior).
    // 1 = rollup of per-turn checkpoints. 2 = rollup of rollups. Etc.
    Level int `json:"level,omitempty"`

    // CoveredTurns is the count of original per-turn checkpoints
    // this entry replaces. Useful for budgeting and for UI display.
    CoveredTurns int `json:"covered_turns,omitempty"`

    // SourceCheckpointIDs lists the checkpoint IDs this rollup
    // consumed. Lets the UI drill down and lets a re-roll-up
    // operate on the right source.
    SourceCheckpointIDs []string `json:"source_checkpoint_ids,omitempty"`
}
```

`StartIndex` / `EndIndex` semantics change for `Level > 0` checkpoints: they describe the historical range that *was* covered, not a live message slice. The index-into-`messages[]` model is dropped for rollups (which is the unblocker for the next change).

#### 2b. `/compact` keeps its current behavior

`/compact` continues to do exactly what it does today: a fresh whole-history LLM summarization, replace the head of `messages[]` with the recap, and wipe `TurnCheckpoint`s for the compacted range. Under the new framing this is the user's deliberate "collapse to one summary" knob, alongside `/clear`. It is no longer the survival tool — auto-substitution handles that — but it stays available as a heavy hammer for power users who want to forcibly minimize context footprint.

**Important user-facing note (and an AGENTS.md doc deliverable):** `/compact` deletes `TurnCheckpoint`s from the *active substitution window*, but the embedded representation of every wiped summary remains in the conversation store. Phase 3's semantic recall can still surface them. The user is not "losing" the work; they're just clearing the active substitution table.

#### 2c. Background rollup trigger

Configurable threshold on the per-level checkpoint count. Default values to tune from telemetry:

```
rollup_thresholds:
  level_0_to_1: 20   # 20 per-turn → 1 rollup
  level_1_to_2: 10   # 10 rollups → 1 super-rollup
  level_2_to_3: 5    # 5 super-rollups → 1 mega-rollup
```

When the count of any level exceeds its threshold, schedule a rollup in the background (queue + worker goroutine, not the model-call path) over the *oldest* N items at that level. The LLM call uses a **dedicated rollup prompt template** (separate from the per-turn summarizer) with explicit slots for "narrative thread," "key decisions," "files touched," "open threads." Inputs are the level-N items' `Summary` + `ActionableSummary` + `FileChanges`, not raw messages. Output substitutes the N items with one Level-(N+1) checkpoint.

The rollup prompt template lives in `pkg/agent/prompts/` alongside the per-turn summarizer prompt and is versioned so we can tune it from telemetry without code changes.

#### 2d. Recency bias in rollup

Always preserve the most-recent K per-turn checkpoints at full fidelity (no rollup). The threshold is on count-of-old-checkpoints. So a session of 50 turns with `K=10`, threshold `20` ends up as roughly:

- 1 Level-1 rollup covering turns 1–20
- 1 Level-1 rollup covering turns 21–40 (or remains 20 individual until count exceeds threshold again)
- 10 per-turn checkpoints (41–50) at full fidelity

When the agent emits a prompt, recent turns are concatenated whole; older turns appear only as their rollup summaries, in chronological order.

#### 2e. File-change manifests propagate up the hierarchy

`ExtractFileChangesFromMessages` already round-trips through summary text (`compact.go:103`). Rolled-up checkpoints preserve the union of `FileChanges` from their source set so the manifest doesn't get lost as rollups stack.

#### 2f. Persistence migration

`AgentState.TurnCheckpoints` is the only schema field affected. New `ID` / `Level` / `CoveredTurns` / `SourceCheckpointIDs` fields are additive and `omitempty` — old saved sessions deserialize cleanly with `Level=0`, etc. No migration script needed.

#### 2g. Documentation: AGENTS.md "Context architecture" section

Add a section to AGENTS.md describing the three operations (substitute / rollup / compact), the recency-preserved active window, the rollup hierarchy, and the embedding store as orthogonal permanent memory. Goal: any contributor touching `compact.go`, `turn_checkpoints.go`, `seed_integration.go`, or `turn_embedding.go` understands which operation they're modifying and doesn't accidentally re-collapse them into one mechanism.

### Phase 3: Embedded memory recall (`semantic_recall`)

Embeddings are currently computed (`EmbedAndStoreTurn`) but unread by the context-management path. Phase 3 closes the loop: the conversation store is the persistent memory; recall surfaces relevant entries from it when the current turn needs them.

#### 3a. Treat the conversation store as permanent memory

A summary that has been embedded persists in the conversation store regardless of subsequent `TurnCheckpoint` list manipulation. `/compact` wiping the active checkpoint list does **not** delete the embeddings of the wiped summaries. The active checkpoint list is just the substitution window; the embedding store is the long-term memory layer.

This is reflected in the spec language and the AGENTS.md doc (Phase 2g): "wipe" is always qualified as "wipe from the active substitution window."

#### 3b. Embed at every level

Today `EmbedAndStoreTurn` embeds only Level-0 per-turn data. Extend to embed each Level-N rollup's summary at creation time, with a `level` field on the `VectorRecord` metadata. The conversation store then holds embeddings at multiple resolutions, and per-turn embeddings survive the rollups that absorbed them.

#### 3c. Recall on user-turn ingest

When a new user message arrives:

1. Embed the user message.
2. Query the conversation store for the top-K similar past summaries, filtered by a recency-decayed score: `score = cosine_similarity × exp(−age_days / half_life)`. Default `half_life = 7` days, `K = 3`.
3. If any retrieved summary is *not present* in the current prompt's substitution window (because it was either rolled up into a coarser level, or wiped by an earlier `/compact`), inject the summary into the prompt as a pinned context block:

   ```
   [recalled from session history — turn 14, 2 weeks ago]
   <summary text>
   ```

4. If the retrieved summary is already present in the current prompt, skip (no duplication).

This is the "selective expansion" of compressed history: high-confidence semantically-relevant items get re-surfaced exactly when they matter, regardless of whether they live in a rollup, a wiped checkpoint, or elsewhere in the store.

#### 3d. Embeddings as a rollup tie-breaker

When the rollup worker chooses what to fold, it can use embeddings to:

- **Cluster before summarizing.** Group the N per-turn checkpoints by embedding similarity, then summarize each cluster separately. Produces tighter per-cluster summaries than one monolithic LLM call.
- **Detect topic shifts as natural rollup boundaries.** If embeddings show a sharp similarity drop between turns *i* and *i+1*, prefer to roll up *1..i* and *i+1..N* as separate rollups rather than mixing them.

#### 3e. Telemetry & evaluation

Capture `recall_diagnostic` events for each turn ingest: `{query_embed_time_ms, candidates_considered, top_k_scores, injected_blocks, total_injected_tokens}`. We'll need a way to evaluate whether recall actually helps — likely an opt-in side-by-side eval where two agents run the same conversation, one with recall and one without, and we measure tokens-to-task-completion or human preference. Defer the eval framework to a follow-up if Phase 3 produces obvious wins on inspection.

## Out of Scope

- **Replacing seed structural substitution.** Phase 1 reframes it as the primary mechanism and tunes its trigger; the underlying substitution code stays.
- **Cross-session memory.** Embeddings are scoped to the current chat session's conversation store. Pulling in context from *other* chats is its own design question.
- **Vector index optimization.** The existing store works; if recall becomes hot path, profile then.
- **Lossless replay.** Rolled-up turns are not recoverable in full from the active state. The transcript snapshot (`pkg/agent/transcript_snapshot.go`) remains the source-of-truth for full-fidelity recovery.
- **UI for inspecting rollups.** A debug view of the checkpoint hierarchy is nice-to-have; defer until Phases 1–2 are stable.
- **Renaming `/compact`.** Today's name and today's behavior are preserved; the user-facing menu is `/clear` (nuke), `/compact` (collapse to one LLM-summary), nothing (auto-substitute).

## Success Criteria

- **Phase 1.** In a session that previously produced an empty-reply failure within ~20 turns, the user can now run >50 turns at high context usage without seeing an empty reply. The `fallthrough_to_compact` telemetry counter stays near zero across normal sessions.
- **Phase 2.** A 500-turn session's `AgentState.TurnCheckpoints` length stays bounded under ~40 entries (per-turn recent window + rollups), independent of total turn count. AGENTS.md has a Context architecture section that survives code review.
- **Phase 3.** In a long session where the user references work done >100 turns ago — including across a prior `/compact` — the relevant summary appears as a pinned recall block in the next prompt without the user having to scroll up or re-explain. Recall latency stays under 50ms per turn.
- `make build-all` and `go test ./...` green at each phase boundary.

## Effort Estimate

- Phase 1 (`substitution_default`): ~2 days. Substitute-every-prompt-build is small; the work is the headroom math, pre-turn prediction, hysteresis, and telemetry to verify the fall-through stays rare.
- Phase 2 (`rollup_machinery`): ~3 days. Schema addition + background worker + dedicated rollup prompt template + tests covering 0→1, 1→2 rollups + the persistence round-trip + AGENTS.md doc.
- Phase 3 (`semantic_recall`): ~3 days. Hooks into the existing embedding manager exist; the work is multi-level embedding, recall scoring, pinning blocks into prompts, and instrumenting eval data.

Total: ~8 days. Phases are independently shippable; each is useful without the next.

## Open Questions

1. **Should the per-turn embed step be moved off the hot path?** `EmbedAndStoreTurn` is currently synchronous on checkpoint record. Under Phase 2, rollup embedding adds further cost. If embedding latency becomes non-trivial under load, move both into the same background worker queue the rollup task uses.
2. **Rollup of *recent* low-cost turns.** When a user runs five trivial back-to-back turns (e.g. quick file reads), should those collapse to a single rollup earlier than the threshold? Possibly — but rollup-by-cost (or by token-volume rather than turn-count) is a second-pass refinement.
