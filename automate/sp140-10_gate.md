# SP-140-10 Gate — Item Selector

Read the "Progress (gate scans this list)" section of
`feat-design-workspace/roadmap/SP-140-10-chat-ux.md` and select the
first unchecked `[ ]` item (SP140-10a → 10e, in order).

Output the item ID and its one-line description, then let the
orchestrator implement it.

If all five items are checked `[x]`, output "ALL SP140-10 ITEMS COMPLETE"
and stop.

## Rules
- Only select IDs matching SP140-10a through SP140-10e, from the
  Progress section's list — ignore any other checkboxes elsewhere in the
  file.
- Select in order (10a before 10b, etc.) — each builds on the last, and
  10d touches files 10b modified, so order matters.
- Skip items already `[x]`.
- An item counts as done ONLY when its files are committed in the
  `feat-design-workspace` worktree AND its checkbox is `[x]` in the spec
  (that checkbox edit committed too).
- Never skip ahead. If an item is blocked, stop and report the blocker
  instead of jumping to the next item.
