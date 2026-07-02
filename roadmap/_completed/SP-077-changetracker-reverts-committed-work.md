# SP-077: ChangeTracker Reverts Committed Work During Git Operations

**Status:** ✅ Implemented (2026-06-26; git-sourced deltas now skipped during shell tracking)

The ChangeTracker's shell-walk diffing captured stale snapshots whenever git operations (merge, checkout, reset) rewrote the working tree, then later wrote those stale snapshots back to disk as "recoverable" edits — silently reverting committed work. The fix adds a git-awareness check at snapshot time: after the post-shell walk, each detected delta is compared against HEAD via `git.IsFileContentCommitted`. If the post-operation content matches HEAD, the delta is classified as git-sourced and recorded with `recoverable: false` (no `OriginalCode` persisted). A secondary sweep on `Commit()` marks existing committed-content snapshots as superseded.

## Key decisions

- Option A (snapshot-source attribution) chosen over Option B (skip walk for known git commands) — precise, only git-sourced deltas suppressed.
- Reused existing `git.IsFileContentCommitted()` primitive instead of building a new git-state checker.
- Snapshots for committed content recorded with `recoverable: false` rather than skipped entirely — preserves audit trail without enabling reverts.
- Phase 2 sweep runs on `Commit()` to clean existing pollution, not just prevent new pollution.
- Config kill-switch (`change_tracking.enabled = false`) left as manual opt-out, not auto-disabled for git repos (Phase 3 deferred).

## Artifacts

- code: `pkg/agent/change_tracking_shell.go` — `TrackShellTurn` git-sourced delta detection (Phase 1)
- code: `pkg/agent/change_tracking.go` — `Commit()` sweep for committed-content snapshots (Phase 2)
- code: `pkg/history/changetracker.go` — `IsRevertSafe` staleness guard (preserved)
- tests: `pkg/agent/change_tracking_shell_sp077_test.go` — regression test reproducing the merge-then-tracker incident

Full specification archived — see git history for original content.
