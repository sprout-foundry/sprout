# SP-077: ChangeTracker Reverts Committed Work During Git Operations

**Status:** ✅ Implemented (Phase 1 + Phase 2)
**Date:** 2026-06-26
**Depends on:** none
**Priority:** High (silent data loss of committed work)
**Effort Estimate:** ~1-2 days

## Problem

The `ChangeTracker`'s shell-mutation tracking subsystem
(`pkg/agent/change_tracking_shell*.go`) silently captures and persists
"original content" snapshots when git operations modify the working
tree. These snapshots then create a recurring failure mode: **committed
work appears in the working tree as a large set of unintended,
uncommitted changes that are actually stale reverts of recently merged
content.**

This has now happened across multiple sessions, and every attempt to
stop the automated reversion has so far failed. The user has had to
manually `git checkout .` after each session to undo the damage.

### Latest incident (this session, 2026-06-26)

Immediately after merging `feature/sp074-tool-registry` into `main`
(via `git merge --no-ff`), the working tree showed **81 files modified
and 16 files deleted** — all of which were stale reverts of committed
content:

- **Entire `pkg/agent_tools/computer_use/` package deleted** (14 files,
  ~2.3k lines) — a feature that was committed long ago.
- `pkg/agent_tools/history.go` — the `IsRevertSafe` staleness guard
  **removed** (a feature added in commit `718eb795` to prevent exactly
  this class of bug).
- `pkg/agent_tools/shell_native.go` — reverted from the concurrency-safe
  `syncBuffer` back to the racy `bytes.Buffer`.
- `pkg/agent_tools/read.go` — reverted a correct goroutine pattern to a
  broken pipe-based version.
- `pkg/agent_tools/rollback_staleness_test.go` and
  `shell_native_test.go` — deleted (tests for the very guards that
  protect against this).
- ~65 other files with 1-2 line diffs (mostly gofmt regressions to an
  older formatting state).

In short: the working tree was silently reset to **older versions of
files that are now committed at HEAD**.

### Evidence the ChangeTracker is the source

The workspace-local history store at `.sprout/changes/` contains **213
snapshot directories (14 MiB)**, with fresh entries written during this
very session:

- `13:09:44` — a burst of snapshots (aligned with the agent's `git fetch`)
- `13:30:38` — another burst (aligned with the agent's `git checkout .`)

Sample metadata from a snapshot captured at 13:09:44:

```json
{
  "filename": ".../pkg/agent_tools/computer_use/safety.go",
  "description": "delete via shell_command",
  "timestamp": "2026-06-26T13:09:44.722365283-05:00",
  "status": "active"
}
```

The tracker interpreted the working-tree restoration (the merge bringing
committed content into the tree) as **tracked deletions** and recorded
their pre-state as recoverable `OriginalCode`. The persisted snapshots
are the raw material that later reverts/recoveries draw from.

### Why prior "stops" failed

The repo already has a config kill-switch — `change_tracking.enabled =
false` in config — and the code path
(`isChangeTrackingEnabledByConfig`, `EnableChangeTracking`) honors it.
But it was **never set** in either the global config
(`~/.config/sprout/config.json`) or the workspace config
(`.sprout/config.json`); `change_tracking` is absent from both. So the
subsystem kept running, kept snapshotting, and kept the failure mode
live. Merely knowing the flag exists is not enough if it isn't applied.

## Root Cause Analysis

### Mechanism

1. **Shell-walk diffing around every `shell_command`.** `TrackShellTurn`
   (`change_tracking_shell.go:147`) walks the workspace before and after
   each shell command, captures file bytes (up to 1 MiB/file, 32 MiB
   total), and records any diff as a `TrackedFileChange` carrying
   `OriginalCode`.

2. **Git operations look like bulk mutations.** `git merge`, `git
   checkout`, `git fetch`-triggered index updates, and `git reset` all
   rewrite working-tree files. The walker sees this as a wave of
   "edits"/"deletes" and dutifully snapshots the *pre-operation* bytes
   as `OriginalCode` — even though those bytes are stale relative to
   the now-merged HEAD.

3. **Snapshots persist to `.sprout/changes/`.** `Commit()`
   (`change_tracking.go:294`) flushes each `TrackedFileChange` to the
   on-disk history store via `history.RecordChangeWithDetails`. These
   records carry `status: "active"` and survive across sessions.

4. **Revert/restore paths write `OriginalCode` back to disk.**
   `handleRevisionRollback`, `handleRevisionRestore`, `recover_file`,
   and `revert_my_changes` all call `os.WriteFile`/`filesystem.SaveFile`
   with the captured `OriginalCode`.

### The staleness guards are insufficient

The existing guards (`IsRevertSafe`, `isFileStaleForRestore`,
`isStaleForRevert`) layer two checks:

1. **Content-identity**: disk ≠ `NewCode` → stale, skip.
2. **Git-awareness** (`git.IsFileContentCommitted`): disk == `NewCode`
   but matches HEAD → committed, refuse.

These guards are correct *for the revert path*, but they do not address
the upstream problem: **the snapshot itself should never have been
recorded for content that is committed at HEAD.** The guards are a
last-line defense on the write-back path; they don't stop the pollution
of the snapshot store, and they can't help if a later session's
`NewCode` happens to match a transient working-tree state.

The deeper issue: the snapshot pipeline treats **any** working-tree
delta as agent-authored work, with no concept of "this delta was caused
by git bringing committed content into the tree, not by the agent
editing a file."

### Why it surfaces as "unintended uncommitted changes"

The most likely path to the observed symptom (stale content sitting in
the working tree as uncommitted modifications) is a revert/restore that
writes `OriginalCode` back to disk *before* the staleness guard can
catch it — for example, a `recover_file` / `revert_my_changes` /
revision-rollback invocation (manual or tool-driven) that runs against
snapshots captured during a git operation. Once written, the stale
content stays until someone notices and runs `git checkout .`.

## Proposed Solution

Three layers, in order of importance. **Phase 1 alone stops the
bleeding**; Phases 2-3 harden the design.

### Phase 1: Don't snapshot deltas caused by git operations (the fix)

The shell-walk diff must distinguish "the agent (or a tool the agent
ran) edited this file" from "git rewrote this file as part of a merge /
checkout / reset / pull." The latter must not produce recoverable
snapshots.

**Option A — Snapshot-source attribution (preferred).** After the
post-shell walk, for each detected delta, run a cheap git check: is
this file's new on-disk content identical to HEAD (i.e. the delta
brought it *to* a committed state)? If so, classify the delta as
git-sourced and either skip recording entirely or record it with
`recoverable: false` (no `OriginalCode` written to the history store).

Concretely, reuse `git.IsFileContentCommitted(path)` (already in
`pkg/git/git.go:303`) on the *post*-operation state. If the post-state
matches HEAD, the operation aligned the tree with committed content —
there is nothing legitimate to "recover" back to.

**Option B — Skip the walk entirely for known git commands.** Extend
the existing `isGitStashOperation` special-case
(`change_tracking_shell.go:184`) to cover merge/checkout/reset/pull by
re-priming the cache instead of diffing. Simpler, but loses tracking of
any *real* edits a git hook or merge conflict resolution might have
introduced (rare, but non-zero).

**Recommendation: Option A.** It's precise (only git-sourced deltas are
suppressed) and reuses the existing git-awareness primitive.

### Phase 2: Make the snapshot store self-cleaning for committed content

Even with Phase 1, the existing 213 snapshot dirs remain and any future
git-sourced snapshot that slips through persists forever. Add a sweep
that runs on `Commit()` (or session start) which marks `status:
"superseded"` for any snapshot whose `NewCode` matches the current HEAD
content — i.e. the change has been committed and is no longer a
recoverable agent edit.

This mirrors the existing `IsRevertSafe` git-awareness but applies it
at *record time*, not just at revert time.

### Phase 3: Default the config kill-switch ON for git repos (defense in depth)

For workspaces inside a git repo, git itself is the authoritative
change-tracking layer. The ChangeTracker's recovery value is primarily
for *untracked* or *uncommitted* work. Consider defaulting
`change_tracking.enabled = false` when a `.git` directory is present,
surfacing the feature as opt-in via config. (This is the most invasive
change and the most debatable — the tracker does add value for
uncommitted edits — so it's listed last.)

## Acceptance Criteria

- [ ] After a `git merge` / `git checkout` / `git reset` run via
      `shell_command`, **no new `TrackedFileChange` with non-empty
      `OriginalCode` is recorded** for files whose post-operation
      content matches HEAD.
- [ ] A test reproduces the incident: merge a branch, run the
      shell-tracker, assert the `.sprout/changes/` store gains no
      recoverable snapshots for committed files.
- [ ] The existing staleness guards (`IsRevertSafe`,
      `isFileStaleForRestore`) continue to pass their current tests.
- [ ] A sweep (Phase 2) marks existing committed-content snapshots as
      superserseded so they can't be reverted.
- [ ] Documentation: a note in `docs/` (or AGENTS.md) explaining that
      the ChangeTracker defers to git for committed content.

## Investigation Notes

- **No git hooks installed** (`.git/hooks/` has only samples) — the
  reversion is not hook-driven.
- **No automated replay on session start** — `persistence.go` does not
  touch `changeTracker` or `TrackedFileChange`; session restore loads
  conversation state only, not file snapshots.
- **The snapshot store is the source of truth** — `.sprout/changes/`
  carries the `OriginalCode` payloads; the in-memory tracker is
  ephemeral per session.
- **The guards are correct but downstream** — they protect the
  write-back path, not the recording path. The pollution happens
  upstream.
- **The config kill-switch works but was never set** — confirming the
  subsystem runs by default in production.

## Files Involved

| File | Role |
|---|---|
| `pkg/agent/change_tracking_shell.go` | `TrackShellTurn` — the walk + diff entry point (Phase 1 target) |
| `pkg/agent/change_tracking_mutations.go` | `RecordShellMutations` — records deltas as `TrackedFileChange` |
| `pkg/agent/change_tracking.go` | `Commit()` — persists snapshots to history store (Phase 2 target) |
| `pkg/history/changetracker.go` | `IsRevertSafe`, `handleRevisionRollback/Restore` — write-back paths |
| `pkg/agent/tool_handlers_recover.go` | `recover_file` / `revert_my_changes` — write-back paths |
| `pkg/git/git.go` | `IsFileContentCommitted` — the git-awareness primitive to reuse |
| `pkg/agent/agent_change_methods.go` | `isChangeTrackingEnabledByConfig` — the kill-switch (works, unused) |

## Immediate Workaround (for affected sessions)

Until Phase 1 ships, the snapshot pollution can be stopped by setting
the kill-switch in the workspace config:

```bash
# Add to .sprout/config.json
{
  "change_tracking": { "enabled": false }
}
```

This disables the entire subsystem (including the legitimately-useful
direct-tool tracking), so it's a blunt instrument. It does, however,
reliably stop the automated reversion — the config gate is honored at
every entry point.
