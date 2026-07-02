# SP-071: Conversation Rewind & Edit-and-Resend — Undo a Wrong Turn

**Status:** ✅ Implemented (2026-06-14; /rewind command, edit-and-resend UI, file reversion)

When the agent went down a wrong path, users could revert files but not the conversation itself. This spec added a rewind primitive that truncates the message history to a chosen turn boundary, prunes orphaned checkpoints, and optionally reverts the file changes those discarded turns made. The CLI got `/rewind N` (or interactive picker), and the WebUI added "Edit & resend from here" on user messages with a confirmation dialog showing what will be discarded.

## Key decisions

- Rewind uses `TurnCheckpoint.StartIndex`/`EndIndex` to find truncation points in messages[]
- Files are reverted in reverse turn order (newest first) so stacked edits unwind cleanly
- Files modified outside the agent since the turn are skipped (never clobbered)
- A pre-rewind transcript snapshot makes the rewind itself undoable (one level)
- Embeddings are not deleted — the store is permanent memory per SP-066 contract

## Artifacts

- code: `pkg/agent/rewind.go` — rewind primitive (truncate messages, revert files, snapshot)
- code: `pkg/agent/rewind_test.go` — truncation + file-revert + skip-unknown tests
- code: `pkg/agent_commands/rewind_command.go` — /rewind slash command with picker
- code: `webui/src/components/ChatMessageContextMenu.tsx` — "Edit & resend from here" action
- code: `pkg/webui/` — POST /api/query/rewind endpoint

Full specification archived — see git history for original content.
