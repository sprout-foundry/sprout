# SP-066: Never-Ending Context — Substitution-First Context Management, Hierarchical Rollups, and Embedded Memory Recall

**Status:** ✅ Substantially Shipped (Phases 1–3 + 3b/3c/3e shipped 2026-06-08; Phase 3d deferred)

Sprout's long-context pipeline composes three previously-distinct operations
(substitution, rollup, compaction) into a coherent default behavior. Phase 1
makes substitution the primary auto-trigger with model-aware response-budget
reservation; Phase 2 introduces a hierarchical `TurnCheckpoint.Level` field
plus a background rollup worker; Phase 3 wires the existing embedding store
into per-turn ingest so the conversation store functions as persistent memory
that survives `/compact`. Phases 3d (embedding-clustered rollup boundaries)
was deferred after auditing 379 real sessions showed rollups essentially
never fire on real workloads (84 rollups in the corpus — all from `cmd/`
test runs hitting the mock provider). The audit also surfaced ~90
test-generated session JSONs leaking into `~/.sprout/sessions/`; that
isolation bug shipped in the same patch series.

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
- **Phase 3d deferred.** The 379-session audit showed rollups don't fire on
  real workloads; optimizing the rollup-clustering code path has zero ROI
  until the threshold is lowered or we see real-world rollup traffic.
- **Threshold calibration is a separate experiment** (`rollupTriggerCount +
  recentTurnsToPreserve = 30` may be too high). Filed as adjacent, not part
  of SP-066, because it's a tuning question not a feature gap.

## Artifacts

- code: `pkg/agent/turn_checkpoints.go::BuildCheckpointCompactedMessages` — Phase 1 substitution at every prompt build
- code: `pkg/agent/context_budget.go::computeCompactionTriggerFraction` — 30% reservation math
- code: `pkg/agent/rollup.go` — Phase 2 background worker
- code: `pkg/agent/semantic_recall.go::InjectSemanticRecall` — Phase 3c recall injection
- code: `pkg/agent/testing_state_isolation.go` — test pollution fix discovered during the audit
- tests: `pkg/agent/turn_checkpoints_test.go`, `pkg/agent/rollup_test.go`, `pkg/agent/semantic_recall_test.go`
- telemetry: `ContextManagementDiagnosticEvent`, `RecallDiagnosticEvent` in `pkg/events/events.go`
- docs: AGENTS.md "Context Architecture (SP-066)" section (Phase 2g deliverable)

Full specification archived — see git history for original content.