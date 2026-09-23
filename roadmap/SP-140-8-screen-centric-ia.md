# SP-140-8 — Screen-Centric IA: The Design Shell Organizes Around Screens

> **Status (2026-09-23):** In progress. Follow-up to SP-140-6/7 (shipped).
> Item 8.1 (rail rework — data-driven Screens group + Library group, status
> dot + open-annotation badge, selection routing) has landed; the workbench
> (8.2) and adaptive chrome (8.3) remain.
> The token-display rework that motivated this (tile grid, proportional spacing
> scale, schema-wall removal) landed in the same review pass on
> `feat-design-workspace` and is *not* part of this spec.
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-3 (shell),
> SP-140-6 (health strip, live tree), SP-140-7 (co-editing affordances).
> Coordinates with SP-140-9 (format rework) — this spec is an IA change and
> assumes no format change; §8b's facet aggregation is defined against
> whatever screen/flow format SP-140-9 settles.

## Problem

The DesignView shell organizes by **artifact kind**: the nav rail is four
tabs — Tokens / Screens / Flows / Feedback — each a separate surface with
its own state. The work in a design workspace, though, is organized by
**screen**: a designer (or an agent turn) works *on a screen* — inspect its
wireframe, its flows in and out with triggers, its open annotations, the
tokens it references, its README status — and the current shell makes that
mean navigating up to four surfaces and stitching the picture by hand.

Concrete, at the current HEAD:

1. **The primary axis is the wrong one.** Doing anything about `login`
   means: Screens tab → find `login` → (switch to) Flows tab → find the
   edges that touch it → (switch to) Feedback tab → find
   `design/feedback/login.json` → (switch to) Tokens tab → check the
   `{color.*}` refs it carries. The rail's four tabs are a file-browser
   metaphor; the work is not a file browser.
2. **The aggregation already exists server-side and is not exposed.**
   `design_brief` (SP-140-5 §5g) returns exactly the per-screen picture —
   purpose, wireframe path, flows IN/OUT with triggers, token refs, open
   annotations, status. The shell has the data for a screen workbench and
   shows none of it.
3. **Global state is global-only.** The §6c health strip and the agent
   panel are workspace-wide; there is no per-screen view of "what is open on
   this screen" (unresolved annotations, draft status, dangling token refs).
4. **The empty state is four dead tabs.** A workspace without `design/`
   renders four disabled tabs instead of one guided empty state.

The goal is not a new product surface. It is that **the shell's primary
axis is the screen**, the way the code shell's primary axis is the file:
pick a screen, and its facets (render, flows, feedback, tokens, status,
agent state) are one pane away — while the kind views keep working as
library surfaces for the cross-screen work they're actually good at (the
token palette, the whole-flow map, the annotation queue).

## Premise: screens are the unit of work; kinds are facets, plus three globals

- **Screen facets** (per selected screen): render (wireframe or HTML
  screen), status, open annotations, flows in/out with triggers, token
  refs, agent-state slice. These compose into one workbench.
- **Globals** (no selection, or explicitly switched): the token palette,
  the flows map (the whole graph, not one screen's neighborhood), and the
  feedback queue (every target, not one screen's). These stay kind views —
  they are the right shape as global surfaces.
- **The data contract is already settled.** The workbench is a view over
  `design_brief` output + the feedback JSON + the README status markers;
  no new file formats are introduced here (SP-140-9 owns format changes).

## §8a — The rail: screen axis with a library group

The rail becomes:

```
DESIGN
  Screens                        ← primary axis (wireframe stems)
    login        ● draft
    sign-up      ● review
    dashboard    ● ready
    ...
  Library                      ← kind views, secondary
    Tokens
    Flows
    Feedback
  Health strip (§6c, unchanged)
```

- Screen entries come from the wireframe inventory (SP-140-1b stems), with
  the README status marker (draft/review/ready) as a dot, and the open-
  annotation count (from `design/feedback/<stem>.json`) as a badge.
- Selecting a screen opens its workbench (§8b). Re-selecting the same
  screen while in a Library view returns to the workbench.
- The Library group holds today's three kind tabs unchanged in behavior
  (Tokens display rework, Flows canvas, Feedback list). They are now
  explicitly *global* surfaces.

## §8b — The screen workbench

For the selected screen, the content area shows one pane with facets in a
fixed order (render first — the screen is what you came to see):

1. **Render** — the wireframe/HTML render (existing `design_render` path,
   cached under `design/.cache/renders/`), with the §7b/§7c annotation pins
   overlay.
2. **Status** — the README marker with the §7.3 structured status editor.
3. **Open feedback** — the unresolved annotations from
   `design/feedback/<stem>.json` (note + location), each actionable
   (resolve, or "ask the agent" → a §6f agent-panel query scoped to the
   screen).
4. **Flows** — the edges touching this screen (IN with triggers, OUT with
   triggers), as links into the Flows canvas pre-scrolled/zoomed to the
   screen's node.
5. **Tokens** — the `{group.token}` refs this screen carries (known vs.
   unknown against `design/tokens/`), as links into the Tokens library.
6. **Agent state slice** — §6f agent panel filtered to this screen's
   files (its wireframe, feedback, and the flows it touches).

Data contract: `design_brief(screen, depth=full)` is the wire; the
workbench is its presentational view. The facet links are navigation only —
editing stays in the existing surfaces (render pin, §7c editors, canvas).

## §8c — Adaptive chrome

- No screen selected → the workbench shows the **library views** (the three
  kind tabs as the default content), so a workspace opened "cold" lands on
  the token palette / flow map instead of a dead screen.
- Screen selected → the workbench; the Library group remains reachable.
- The health strip stays global (it is workspace state, not screen state);
  a screen-relevant subset (this screen's open annotations + status)
  appears in the workbench header instead.

## §8d — Empty states

- No `design/` → one guided empty state (scaffold via the
  `design-system` skill; "create your first screen"), replacing four
  disabled tabs.
- `design/` without screens (tokens only) → the Library views are the
  default; the Screens group shows its own empty state ("no screens yet —
  the token palette is ready").

## Non-goals

- No new file formats, no tree-contract changes (that is SP-140-9).
- No changes to the Flows canvas editor or the render/critique pipeline.
- No multi-screen workbench (one screen at a time; cross-screen work is
  the Flows canvas's job).
- The Library surfaces keep their current behavior; only their position in
  the IA changes.

## Items

- **8.1** Rail rework: screen group (inventory + status dot + annotation
  badge) + Library group; selection state shared with the workbench.
  (webui; testids per the registry; vitest on the rail state machine.)
  > **Progress (2026-09-23):** rail side landed — the rail is now data-driven
  > from the inventory: a Screens group (one entry per wireframe stem with a
  > status dot and an open-annotation count badge) + a Library group
  > (Tokens / Flows); screen selection drives the shared selection and lands
  > on the Screens section, degrading gracefully to Library-only when there
  > is no workspace. The workbench-side selection handoff completes in 8.2.
  > `DesignRail.test.tsx` pins the state machine.
- **8.2** Screen workbench: facet pane over `design_brief(full)` + feedback
  JSON + status; facet links; agent-state slice. (webui + the existing
  brief/feedback services; no new endpoints.)
- **8.3** Adaptive chrome: default-to-library when unselected; workbench
  header with the screen-relevant health subset.
- **8.4** Empty states: no `design/` and screens-less `design/`.
- **8.5** Migration of the existing four-tab rail (state preservation: a
  deep link into "Flows" still lands on the Flows library view).
- **8.6** Meta: a wireframe of the new layout (an HTML screen in
  `design/screens/` of the dogfood workspace — the tool's own IA dogfoods
  SP-140-9's format once it lands; until then, SVG per the current
  charter), plus a critique pass (`design_critique`).

## Acceptance criteria

- [ ] From the rail, selecting `login` shows its render, status, open
      annotations, flows in/out with triggers, token refs, and agent-state
      slice in one pane — no tab switching.
- [ ] The three library views (Tokens / Flows / Feedback) behave exactly
      as before, now under the Library group; deep links still resolve.
- [ ] A workspace without `design/` shows the single guided empty state.
- [ ] The workbench's data is `design_brief` (no second aggregation path —
      one truth, per the SP-140-5 premise).
- [ ] All new testids registered; vitest green; the DesignView lazy chunk
      pin (`designChunk.test.ts`) still holds.
- [ ] A `design_critique` pass on the new layout reports no
      consistency/hierarchy errors against the existing design language.
