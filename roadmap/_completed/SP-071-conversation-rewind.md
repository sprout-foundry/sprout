# SP-071: Conversation Rewind & Edit-and-Resend — Undo a Wrong Turn

**Status:** ✅ Implemented
**Date:** 2026-06-14
**Depends on:** SP-066 (TurnCheckpoint structure), SP-024 (context management), Change Tracking (`pkg/agent/change_tracking*.go`)
**Priority:** Medium-High
**Effort Estimate:** ~4-5 days (3 phases)

## Problem

When the agent goes down a wrong path, there is no way to **rewind the
conversation**. Users can revert *files* — `recover_file`, `rollback_changes`,
`revert_my_changes` (Change Tracking) — but the *conversation itself* marches
forward. There is no "jump back to turn N, fix my prompt, and re-run, throwing
away the bad branch."

This is the rewind that Claude Code / Cursor users reach for constantly: the
agent misunderstood three turns ago, and rather than spend more turns talking
it out of a hole, you want to snap back to the last good state — both the
message history **and** the files — and try a better prompt. Today the only
tools are `/compact` (collapse, doesn't rewind) and `/clear` (nuke
everything).

## Current State

| Primitive | Where | Reusable for rewind? |
|---|---|---|
| Per-turn checkpoints w/ `StartIndex`/`EndIndex` | `pkg/agent/turn_checkpoints.go`, `types.go:40` | ✅ marks turn boundaries in `messages[]` |
| Transcript snapshot | `pkg/agent/transcript_snapshot.go` | ✅ full-fidelity history |
| File change manifest per turn | `ChangeTracker`, `TurnCheckpoint.FileChanges` | ✅ which files each turn touched |
| File restore | `recover_file` / `rollback_changes` | ✅ reverses file edits |
| Conversation rewind | — | ❌ **missing** |
| Edit-and-resend a past user message | — | ❌ missing |

Everything needed exists in pieces; nothing composes them into "rewind to turn
N (messages + files)."

## Proposed Solution

A rewind operation that truncates `messages[]` to a chosen turn boundary,
prunes the now-orphaned `TurnCheckpoint`s, and **optionally** reverts the file
changes those discarded turns made — using the per-turn `FileChanges`
manifests that already exist.

### Phase 1: Backend rewind primitive

**New file:** `pkg/agent/rewind.go`

```go
type RewindOptions struct {
    ToTurn       int  // rewind so this turn becomes the last completed turn
    RevertFiles  bool // also undo file edits made by discarded turns (default true)
}

type RewindResult struct {
    TurnsDiscarded   int
    FilesReverted    []string
    FilesSkipped     []string // not recoverable (e.g. later overwritten outside agent)
}

// Rewind truncates messages[] at the chosen turn boundary, drops the
// TurnCheckpoints for discarded turns, and (if RevertFiles) reverses the
// union of their FileChanges via the ChangeTracker. A pre-rewind
// transcript_snapshot is captured so the rewind itself is undoable.
func (a *Agent) Rewind(ctx context.Context, opts RewindOptions) (*RewindResult, error)
```

Key rules:
- Use `TurnCheckpoint.StartIndex` to find the truncation point in `messages[]`.
- Revert files in **reverse turn order** (newest discarded turn first) so
  stacked edits unwind cleanly; skip files modified outside the agent since the
  turn (report them in `FilesSkipped`, never clobber unknown edits — matches
  the Change Tracking safety contract).
- Snapshot before rewinding so `/rewind` is itself reversible (one level of
  "un-rewind").
- Embeddings are **not** deleted (SP-066 contract: the store is permanent
  memory; rewinding the active window doesn't wipe history).

### Phase 2: CLI `/rewind`

- `/rewind` with no arg → interactive picker listing recent turns (number,
  age, one-line summary from `ActionableSummary`, files touched), built on the
  `select_list` primitive (SP-057). Selecting a turn rewinds to it.
- `/rewind N` → rewind directly to turn N.
- After rewind, print the `RewindResult` (turns discarded, files reverted) and
  drop the user at a fresh prompt positioned right after turn N — so they can
  immediately type a corrected instruction.
- Register in `pkg/agent_commands/commands.go` next to `RollbackCommand`.

### Phase 3: WebUI edit-and-resend

- Each user message in the chat gets an "Edit & resend from here" action
  (`webui/src/components/` chat message menu — extends the existing
  `ChatMessageContextMenu`).
- Choosing it shows which later turns and file edits will be discarded
  (confirmation), then calls `POST /api/query/rewind { to_turn, revert_files }`,
  pre-fills the input with the original message text for editing, and resubmits.
- The discarded branch is visually collapsed (not deleted from the transcript
  snapshot) so a user can expand "previous attempt" if they want to compare.

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/rewind.go` | **New** — rewind primitive |
| `pkg/agent/rewind_test.go` | **New** — truncation + file-revert + skip-unknown tests |
| `pkg/agent/turn_checkpoints.go` | Modify — helper to resolve turn→message index |
| `pkg/agent_commands/commands.go` | Modify — register `RewindCommand` |
| `pkg/agent_commands/rewind_command.go` | **New** — `/rewind` (picker + `N`) |
| `pkg/webui/` query API | Modify — `POST /api/query/rewind` |
| `webui/src/components/ChatMessageContextMenu.tsx` | Modify — "Edit & resend from here" |
| `webui/src/services/api/` | Modify — rewind API call |

## Success Criteria

- `/rewind 3` truncates the conversation to turn 3 and reverts files turns 4+
  created/modified (reporting any it couldn't safely revert).
- The rewind is itself undoable once (snapshot restore).
- WebUI "Edit & resend" rewinds, pre-fills the original prompt, and resubmits;
  the discarded branch is collapsed, not lost from the snapshot.
- Files modified outside the agent are never clobbered by a rewind.
- Embeddings/conversation store are untouched by rewind (SP-066 invariant).

## Out of Scope

- Branching conversations (keeping multiple live branches simultaneously) —
  this is linear rewind, not a tree. A "fork to new chat" already exists via
  SP-027 handoff.
- Partial-turn rewind (mid-tool-call). Rewind operates on completed turn
  boundaries only.
- Rewinding shell side effects beyond tracked file changes (a discarded turn
  that ran `curl … | sh` can't be magically undone — document this).

## Open Questions

1. Default for `RevertFiles` — true (full snap-back) or false (rewind chat
   only, keep edits)? Recommendation: true, with a `--keep-files` escape hatch.
2. How many rewind snapshots to retain? One level (un-rewind) is the minimum;
   a small ring (e.g. 3) is cheap and friendlier.
