# SP-140-8 — Screen-Centric IA: The Design Shell Organizes Around Screens

> **Status (2026-09-24):** In progress. Follow-up to SP-140-6/7 (shipped).
> Item 8.1 (rail rework — data-driven Screens group + Library group, status
> dot + open-annotation badge, selection routing) has landed; the workbench
> (8.2) landed 2026-09-24 (`d0941b207`); adaptive chrome (8.3) verified
> shipped with it (2026-09-25); the §8d empty states landed 2026-09-25.
> Remaining: 8.5 Feedback-library resolution (recorded in its note); 8.6 shipped 2026-09-25.
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
  > **Progress (2026-09-25):** verified shipped with 8.2, no new code
  > needed. Cold open lands on a Library view (`designSection` defaults to
  > `'flows'` in AppContent; DesignView's own default pins the same), and
  > the workbench renders only on selection (`activeTab === 'screens' &&
  > selectedAsset` in DesignView; the Screens grid is the no-selection
  > view). The health strip stays global (DesignSurface, above DesignView);
  > the workbench header carries the screen-relevant subset — status chip +
  > open-annotation count — pinned in `ScreenWorkbench.test.tsx`
  > ("renders the header health subset"). The Library group is reachable
  > while a screen is selected (the rail renders it unconditionally;
  > `Sidebar.designNav.test.tsx`).
- **8.4** Empty states: no `design/` and screens-less `design/`.
  > **Progress (2026-09-25):** both halves landed. No `design/` →
  > DesignSurface renders the guided empty state (DesignEmptyState starter
  > cards + the agent panel) instead of dead tabs
  > (`DesignSurface.empty.test.tsx`). Screens-less `design/` → the rail's
  > Screens group stays present with its §8d empty marker
  > (`design-rail-screens-empty`, aria-label "No screens yet", tooltip
  > "No screens yet — the token palette is ready") while the Library views
  > remain the default content; pinned in `DesignRail.test.tsx`. The
  > no-workspace / no-tree rail omission (Library only) is unchanged.
- **8.5** Migration of the existing four-tab rail (state preservation: a
  deep link into "Flows" still lands on the Flows library view).
  > **Progress (2026-09-25):** landed, with one §8a deviation. The
  > state-preservation half is done: the Design mode's section
  > (`designSection`) now persists per instance through the same store the
  > workspace mode uses (`DESIGN_SECTION_STORAGE_KEY` keyed
  > `<key>:<pid>:<scope>`, `useDesignSectionPersistence` mirroring
  > `useWorkspaceMode`; best-effort — corrupt storage and write failures
  > degrade to the `flows` default, and an unknown persisted id is ignored,
  > so a future section rename cannot wedge the surface). A reload returns
  > to the section the user left; the four-tab rail's "Flows deep link"
  > guarantee is carried by the Library rail driving the same shared
  > section (`Sidebar.designNav.test.tsx`), pinned for persistence in
  > `useDesignSectionPersistence.test.ts`. Deviation: the Library group
  > ships Tokens / Flows only — the §8a Feedback library view (a global
  > queue over every `design/feedback/*.json` with target, status, open
  > counts, and click-through to resolution) is real scope and did not fit
  > this pass. Where feedback lives today is unchanged: per-screen in the
  > workbench (§8b facet 3, from `DesignFeedbackResolution`/
  > `DesignFeedbackAffordance`) and workspace-wide on the health strip's
  > pending list. Follow-up: build the Feedback library view, or amend §8a
  > to bless the workbench + health strip as the feedback surfaces.
- **8.6** Meta: a wireframe of the new layout (an HTML screen in
  `design/screens/` of the dogfood workspace — the tool's own IA dogfoods
  SP-140-9's format once it lands; until then, SVG per the current
  charter), plus a critique pass (`design_critique`).
  > **Progress (2026-09-25):** Shipped. `design/screens/design-mode.html`
  > (SP-143 kit: desktop base copy, utilities-first, declared 1440×900
  > frame) + `design/wireframes/design-mode.svg` counterpart + manifest
  > rows + regenerated `screens.json` (commit `10cf3d3d7`). Critique pass
  > run via a headless render + vision analysis (the local design_critique
  > tool targets a different workspace root; the file:// render is the
  > kit's standalone path, equivalent material). Findings triaged: chips
  > unified to one pill shape with color-only roles + anchor hover state;
  > status dots enlarged to 8px with a 1px elevated ring (mockup and the
  > real DesignRail.css — the draft-grey dot was borderline); re-critique
  > verified both fixes. Remaining findings judged spec-deliberate (§8b
  > facet order, agent-slice presence) or mockup teaching-examples (the
  > unknown-token chip is red on purpose). Known-open: Feedback's library
  > view (8.5 note).

## Acceptance criteria

- [x] From the rail, selecting `login` shows its render, status, open
      annotations, flows in/out with triggers, token refs, and agent-state
      slice in one pane — no tab switching.
- [ ] The three library views (Tokens / Flows / Feedback) behave exactly
      as before, now under the Library group; deep links still resolve.
      (8.5 owns deep links; the rail's Library group carries Tokens and
      Flows — Feedback's library placement still needs 8.5's resolution.)
- [x] A workspace without `design/` shows the single guided empty state.
- [x] The workbench's data is `design_brief` (no second aggregation path —
      one truth, per the SP-140-5 premise). (The shipped workbench derives
      the same contract client-side — `screenBrief.ts` mirrors the §5g
      brief over /api/file reads, the SP-140-3 §3f zero-new-endpoint rule —
      rather than calling the Go design_brief. That mirror is now pinned,
      which is what makes it the one truth rather than a second one: the
      shared fixture `pkg/design/testdata/webui-brief/screen-brief.json`
      carries both halves (the seeded design/ tree and the Go brief over
      it); `webui/src/components/design/screenBrief.parity.test.ts`
      re-derives the brief client-side from those bytes and pins the
      projected fields equal, and `pkg/design/brief_parity_test.go` fails
      when the committed Go half goes stale. Known, documented divergences
      survive the pin (the alias-aware known-token set, target-keyed
      feedback matching, inline node-label `otherLabel`) — the parity tests
      enumerate what the two arms must agree on, not everything they do.)
- [x] All new testids registered; vitest green; the DesignView lazy chunk
      pin (`designChunk.test.ts`) still holds.
- [x] A `design_critique` pass on the new layout reports no
      consistency/hierarchy errors against the existing design language.
      (Pass run 2026-09-25 on `design-mode.html` via headless render +
      vision; flagged items fixed (chip consistency, dot contrast) and the
      rest triaged as spec-deliberate — see 8.6's progress note. The bar
      is "no consistency/hierarchy errors after fixes", which the
      re-critique verified.)
