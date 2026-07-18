# SP-066: Never-Ending Context — Substitution-First Context Management, Hierarchical Rollups, and Embedded Memory Recall

**Status:** ✅ Shipped (Phases 1–3 + 3a/3b/3c/3d/3e; full spec archived 2026-07-18; Phase 3d integration fixed 2026-07-18)

Sprout's long-context pipeline composes three previously-distinct operations
(substitution, rollup, compaction) into a coherent default behavior. Phase 1
makes substitution the primary auto-trigger with model-aware response-budget
reservation; Phase 2 introduces a hierarchical `TurnCheckpoint.Level` field
plus a background rollup worker; Phase 3 wires the existing embedding store
into per-turn ingest so the conversation store functions as persistent memory
that survives `/compact`. Phase 3a additionally embeds rollup summaries so
they surface through semantic recall; Phase 3d adds embedding-clustered
rollup boundary detection so each rollup stays topically coherent when the
candidates span a user-driven topic shift.

Phase 3d shipped in three layers: the boundary-detection math
(`rollup_boundary.go`), the embedding of rollup summaries
(`rollup_embedding.go`, Phase 3a), and the lookup plumbing that joins
`TurnCheckpoint.ID` ("cp-<uuid>") with the embedding record's
`metadata["checkpoint_id"]` field. The plumbing was silently broken in the
initial landing — per-turn `EmbedAndStoreTurn` did not stamp the
checkpoint ID into the record's metadata, so `collectCheckpointVectors`
always fell back to the default range on real workloads. Fixed 2026-07-18
by adding a `checkpointID` parameter to `EmbedAndStoreTurn`, passing
`checkpoint.ID` from the call site in `recordTurnCheckpointFromMessages`,
and adding a `r.ID` fallback that strips the `"rollup:"` prefix so legacy
records and rollup entries both resolve.

## Key decisions

- **Three operations stay distinct.** Substitute (free, every prompt build),
  Rollup (LLM call, amortized, background), Compact (LLM call on raw
  history, explicit `/compact` hammer). The original failure was conflating
  them; the spec keeps each in its lane.
- **Embedding store is orthogonal permanent memory.** A summary that has been
  embedded persists regardless of subsequent `TurnCheckpoint` list manipulation
  (including `/compact` wipes). Recall (Phase 3c) re-surfaces relevant items
  even after they're gone from the active substitution window.
- **Conservative response reservation (15% + 10% + 5% = 30%)** rather than
  per-provider precision, because under substitution-first the reservation
  only matters for the rare fall-through to LLM compaction.
- **Calibrated rollup thresholds (15+5=20) after the original 10+20=30 never
  fired on real workloads** — see the long comment block above `recentTurnsToPreserve`
  in `pkg/agent/rollup.go`. The first rollup now fires within a moderate
  coding session, exercising the hierarchy as a real safety net instead of
  dormant code. Phase 3d's boundary detection runs over the calibrated
  threshold, so the work pays off: rollups now actually roll up, and the
  refinement keeps each rollup topically coherent.
- **Phase 3d landed as opt-in refinement with the lookup plumbing fixed.**
  `refineRollupEnd` shrinks the candidate range to the largest
  pairwise-similarity drop above `rollupBoundarySimilarityDrop=0.20`, only
  when the candidate still has ≥ `rollupBoundaryMin=5` items on each side.
  Falls back silently to the default first-N window when embeddings aren't
  available, when a candidate lookup misses, or when the largest drop is
  below threshold — so the worker never blocks on a misbehaving embedding
  provider. Verified by four complementary tests: `TestEmbedAndStoreTurn_StampsCheckpointID`
  and `TestEmbedAndStoreTurn_OmitsEmptyCheckpointID` (turn_embedding_test.go)
  prove the production-side metadata stamping and the empty-string sentinel
  skip; `TestRefineRollupEnd_FindsTopicShift` and `TestRefineRollupEnd_EndToEnd_ThroughEmbedAndStoreTurn`
  (rollup_boundary_integration_test.go) prove the lookup path resolves those
  metadata keys to vectors and that boundary detection finds topic shifts;
  `TestCollectCheckpointVectors_LegacyRollupRecord` (rollup_boundary_test.go)
  proves the `"rollup:"`-prefix-stripping fallback handles pre-fix records.
  The stamping + end-to-end tests both fail when the metadata-stamping is
  reverted, proving they catch the regression class that let the broken
  plumbing ship originally.

## Artifacts

- code: `pkg/agent/turn_checkpoints.go::BuildCheckpointCompactedMessages` — Phase 1 substitution at every prompt build
- code: `pkg/agent/context_budget.go::computeCompactionTriggerFraction` — 30% reservation math
- code: `pkg/agent/rollup.go` — Phase 2 background worker (calibrated 15+5=20, wires Phase 3a + 3d)
- code: `pkg/agent/rollup_embedding.go::embedRollupCheckpoint` — Phase 3a rollup embeddings
- code: `pkg/agent/rollup_boundary.go::refineRollupEnd` + `collectCheckpointVectors` — Phase 3d topic-boundary refinement and dual-ID lookup
- code: `pkg/agent/turn_embedding.go::EmbedAndStoreTurn` — production-side `checkpointID` metadata stamping (Phase 3d plumbing fix)
- code: `pkg/agent/semantic_recall.go::InjectSemanticRecall` — Phase 3c recall injection
- code: `pkg/agent/testing_state_isolation.go` — test pollution fix discovered during the audit
- tests: `pkg/agent/turn_checkpoints_test.go`, `pkg/agent/rollup_test.go`,
  `pkg/agent/rollup_boundary_test.go`, `pkg/agent/rollup_boundary_integration_test.go`,
  `pkg/agent/turn_embedding_test.go`, `pkg/agent/semantic_recall_test.go`
- telemetry: `ContextManagementDiagnosticEvent`, `RecallDiagnosticEvent` in `pkg/events/events.go`
- docs: AGENTS.md "Context Architecture (SP-066)" section (Phase 2g deliverable)

Full specification archived — see git history for original content.