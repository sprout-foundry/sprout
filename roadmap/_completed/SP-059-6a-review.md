# SP-059-6a: Delegate Feature Porting Review

**Status:** ✅ Implemented (2025-10-17; delegate fully superseded by run_subagent)

The old `delegate` / `delegate_status` tool was removed from the codebase. This review confirmed that `run_subagent` and `run_parallel_subagents` fully supersede the old delegate system with no missing functionality. All delegate source files (8 files in `pkg/agent/`) were deleted, all stale registrations cleaned, and all useful `DelegateResult` fields ported to `SubagentReturn` with equivalent or improved representations. Four delegate-only capabilities were intentionally dropped: async polling (superseded by `run_parallel_subagents`), freeform role strings (was a bug), per-call tool allowlist overrides (superseded by per-persona allowlists), and pre-scheduled follow-up injection (superseded by `InjectInputIntoActive`).

## Key decisions

- Async delegate polling intentionally dropped — `run_parallel_subagents` provides better coordination without model-driven polling overhead.
- Freeform `role` parameter dropped as a bug fix — personas must be registered configurations for uniform allowlist and spawn-authority gating.
- Per-call `tools` allowlist override replaced by per-persona allowlist system — single source of truth instead of per-call overrides.
- `SubagentReturn.FilesModified` uses richer `[]FileChange` type with diff info instead of plain `[]string`.
- `SubagentReturn.ProgressLog` uses structured `[]ProgressEntry` instead of per-call streaming updates.

## Artifacts

- code: `pkg/agent/subagent_types.go` — `SubagentReturn` type with all ported fields
- code: `pkg/agent/conversation.go` — per-persona allowlist system replacing delegate overrides
- tests: `pkg/agent/delegate_followup_test.go` — deleted (delegate removed)

Full specification archived — see git history for original content.
