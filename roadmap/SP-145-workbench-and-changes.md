# SP-145 — Workbench Sidebar and One Changes Surface

> **Status (2026-09-30):** Proposed — for owner review before build.
> Builds on the layered layout (project rail + project sidebar) and SP-142
> chat lanes. First slice shipped ahead of this spec: sibling tools switch in
> one click from the drill-in header (`cd7d8ac53`), and a turn's single
> changed file opens its diff from the chat strip (`dc898ee10`).

## Problem

**Depth.** Common paths go several levels deep and back out again:

| Path | Today |
|---|---|
| Files → Source control | Back, then Source control (2 clicks, sidebar re-renders) — fixed by the sibling strip |
| See what the agent changed | chat strip → expand → path → diff (3); or Source control → Changes → file (3) |
| A setting | Settings → accordion → sub-tab → field (3–4, then scroll) |
| Past conversation | History icon → popover → search → pick (3) |

**No cohesion for edits.** A change shows up in six places, each partial:
the chat's per-turn strip, the inline tool detail, Agent Changes (local
daemon only), Source control, the editor's gutter, and the file tree. None of
them answers "what changed since I last looked, who changed it, keep or
undo?" in one view. User edits and agent edits were indistinguishable until
UI-origin `file_changed` events gained `source: "user"` (`823aeb5b0`).

## Principles

1. **Two-click rule.** Every common destination is at most two clicks from
   anywhere in the project.
2. **No Back between siblings.** Back only exists *inside* a tool (a folder,
   a commit). Moving between tools is always one click.
3. **One place per concept.** Edits have one home (Changes); conversations
   have one home (the sidebar list); settings have one home (one searchable
   list).
4. **The main view follows what you open** (a file → editor, a change →
   diff, a screen → canvas), never a global mode switch.

## Design

### 1. Sidebar = Conversations + Workbench

The project sidebar has two stacked regions:

```
┌ my-app ▾                 [New] ┐
│ CONVERSATIONS             ⟲   │  ← list, resizable split
│  • Add checkout               │
│  • Fix CI              ●      │
├───────────────────────────────┤
│ [Files][Search][Changes][✎]   │  ← workbench tool strip, one click
│  src/                         │
│   checkout.tsx          M     │  ← the open tool's panel, in place
│   api.ts                      │
└ ⚙ Settings · Logs ─────────────┘
```

- The **tool strip** replaces the Code and Design sections and the drill-in:
  Files · Search · Changes · Design (screens, flows, tokens, feedback as the
  Design tool's own tabs). Workflows and Terminal move into the strip's `⋯`
  overflow (they are occasional).
- The strip remembers the open tool per project. There is always one open;
  the drill-in (and its Back) disappears.
- The split between Conversations and the workbench is draggable and
  collapsible (collapsing Conversations gives a full-height file tree).
- **Phone:** the drawer shows the same two regions; the tab bar's project tab
  opens it.

### 2. Changes — one surface for every edit

Replaces the Source control panel and the Agent Changes panel; the chat's
turn strip and the editor gutter link into it.

- **Grouped by origin, newest first:** "Agent · turn 12 (Add checkout)",
  "Agent · turn 11", "You", then committed history below. A group row shows
  file count and ±lines; expanding lists files.
- **Per file / per group:** open diff (main view), keep, revert. Revert per
  turn reuses today's `revert-since` machinery; per-file revert reuses
  Agent Changes' restore.
- **Commit** lives at the top of Changes (message, stage-all toggle), not in
  a separate panel. Branch switcher stays in its header.
- **Entry points:** the chat strip's "N files changed" opens Changes filtered
  to that turn; a gutter change marker opens that file's diff with Changes
  showing its group.
- **Hosted:** browser git has no index distinct from the working tree — the
  "staged" split is hidden there (as `GitSidebarPanel` already does for the
  unsupported actions).

Data: `fileEdits` (turn attribution, `source`), the change tracker's manifest
(local: `/api/changes`; hosted: working-tree diff against HEAD), and git
status. A change the tracker doesn't know (edited outside sprout) groups
under "Outside sprout".

### 3. Settings — one searchable list

- One scroll of all sections with sticky section headings; no accordions, no
  sub-tabs. Sections render lazily as they scroll into view (several load
  their own data today).
- Search matches **fields** (label and help text), not just section names,
  and shows matching fields in place with their section heading.
- Scope badges (session / workspace / global) stay, per field.

### 4. History

Past conversations join the Conversations list as a collapsed "Earlier"
group with search, replacing the History popover (one home for
conversations).

## Migration (slices, each shippable)

1. **Tool strip** in place of Code/Design sections + drill-in; Workflows and
   Terminal into overflow. Tests: every tool reachable in ≤2 clicks from any
   other; no Back between tools.
2. **Changes**, local daemon first (tracker manifest + git), then hosted.
   Remove Source control and Agent Changes panels behind it. Tests: agent vs
   user grouping; revert per file and per turn; chat strip deep link.
3. **Settings list** with field search and lazy sections.
4. **History** into Conversations "Earlier".

## Open questions (owner)

1. Does Design belong in the workbench strip at all, or should the design
   canvas be reached only by opening a screen (like opening a file)? Ties to
   the Design | Code | Ship discussion.
2. Committed history: inside Changes (below uncommitted groups) or its own
   tool in the strip?
3. Should Changes show *other* conversations' pending changes (SP-142 lanes)
   or only the current one's plus "You"?
