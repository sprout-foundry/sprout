# SP-059-6a — Delegate Feature Porting Review

**Spec Reference:** `roadmap/SP-059-subagent-interaction.md` (Phase 6 review)
**Task:** Review delegate features for porting to run_subagent
**Reviewer:** Sprout Agent (automated review)

## Background

The delegate tool (`"delegate"` / `"delegate_status"`) has been fully removed from the codebase. This review confirms that no needed delegate functionality is missing from the `run_subagent` replacement.

## Delegate Source File Audit

The following delegate source files have been confirmed deleted:
- `pkg/agent/tool_handlers_delegate.go`
- `pkg/agent/tool_handlers_delegate_status.go`
- `pkg/agent/delegate_factory.go`
- `pkg/agent/delegate_types.go`
- `pkg/agent/delegate_stream.go`
- `pkg/agent/delegate_nesting.go`
- `pkg/agent/async_delegate_tracker.go`
- `pkg/agent/delegate_followup_test.go`

**Tool registration audit:** Grep for `"delegate"` / `"delegate_status"` in `pkg/agent/tool_registrations.go` and `pkg/agent_api/tools.go` returns nothing — no stale registrations remain.

## Delegate-Only Capabilities Audit

| Capability | Status | Rationale |
|---|---|---|
| Async execution + `delegate_status` polling | Intentionally dropped | Superseded by `run_parallel_subagents`. Async adds coordination overhead with no model-driven benefit. |
| Freeform `role` string parameter | Intentionally dropped | Was a bug — personas must be registered configurations so per-persona allowlists and spawn-authority gating apply uniformly. |
| Per-call `tools` allowlist override | Intentionally dropped | Superseded by the per-persona allowlist system (`conversation.go:97-100`). One source of truth instead of per-call overrides. |
| `FollowUpMessages` pre-scheduled injection | Intentionally dropped | Superseded by Phase 1b's `InjectInputIntoActive` for interactive steering. No production caller used pre-scheduled follow-ups. |

All four dropped capabilities are explicitly listed as non-goals in the spec (`roadmap/SP-059-subagent-interaction.md` Phase 6 + Non-goals section).

## SubagentReturn Field Reconciliation

| DelegateResult Field | SubagentReturn Equivalent | Status |
|---|---|---|
| `Iterations int` | `SubagentReturn.Iterations` (`pkg/agent/subagent_types.go:43`) | ✅ Ported |
| `Summary string` | `SubagentReturn.Summary` | ✅ Ported |
| `FilesChanged []string` | `SubagentReturn.FilesModified []FileChange` | ✅ Ported (richer — includes diff info) |
| Per-call streaming updates | `SubagentReturn.ProgressLog []ProgressEntry` | ✅ Ported (structured replacement) |
| `ExitStatus` enum | `SubagentReturn.SubagentStatus` enum | ✅ Ported |

All genuinely useful `DelegateResult` fields have equivalent or improved representations in `SubagentReturn` (per spec Phase 5).

## Conclusion

**Acceptance criterion met:** A thorough review of delegate features confirms nothing needed is missing from `run_subagent`.

- All useful delegate functionality is already present in `SubagentReturn`/`run_subagent`
- All dropped capabilities are explicitly documented as non-goals with clear rationale
- No stale code or registrations remain in the codebase

The `run_subagent` and `run_parallel_subagents` system fully supersedes the old delegate tool.

**Date:** 2025-10-17
**Reviewer:** Sprout Agent (automated codebase audit)
