# SP-140 — Design Workspace: Quick Mockups and Visual Ideas, On Brand

> **Status (2026-11-12):** Rewritten. The first implementation of this spec
> (SP-140-1 … SP-140-10, SP-143, SP-158) built a design *process* — flow
> sources, a screen kit runtime, a critique pipeline, a co-editing protocol,
> drift/sync reporting. It did not meet the goal. This spec now states the
> goal plainly and the earlier child specs are withdrawn.
>
> **Goal:** from a prompt, a few seconds later, the person is looking at a
> mockup that looks like it belongs to this product — and they can say
> "make the button bigger" and see that too.
>
> **The bar is speed and brand fit, not design process.** Anything that
> stands between "I want a screen" and "here is a screen I can react to" is
> out of scope, however well engineered.

## Problem

Design work in sprout was built as a lifecycle: tokens → screens → flow
sources → derived exports → critique → sync → co-edit → drift reporting.
Each phase was defensible on its own; together they made the simplest
request ("show me a checkout screen") cost a brief, a token pass, a screen
built from a kit template, a flow source, two regenerated derived files, a
validate pass, a render-and-critique pass, and a sync report.

What the person actually asked for was a fast way to see ideas. The
process is the reason they don't get one.

## What "on brand" means here

Brand fit comes from **a small, curated design kit** — not from a project's
own token tier. A kit is a fixed, checked-in style (a look: palette, type
scale, spacing, component styles, icon set) with a short brief the model
composes to. The model picks a kit and a palette, writes markup against the
kit's classes, and the platform adds the kit's CSS. The model never writes
the styling, so every mockup looks designed rather than generated, and the
first screen paints before the document is finished.

This is the existing `pkg/mockupkit` mechanism, already used by the
`planner` persona's `draw_mockup`. It is the whole brand story. There is no
second one.

## Design

### A. The mockup is one file, drawn by a tool

- `draw_mockup` draws `design/mockup.html`: one self-contained document,
  a `<section data-screen="id">` per screen, a small runtime for screen
  switching, tabs and forms.
- It streams. The model call is the tool's own, not an argument, so a host
  renders each piece as it is written and shows a first screen within
  seconds.
- A change ("bigger photos", "make the button green") is applied as
  find/replace edits against the existing document, falling back to a full
  redraw. The person's words are the input; no structured change format.
- The saved file opens on its own — no build step, no server, no runtime
  assets to copy in.

### B. The kit is the brand

- `pkg/mockupkit` owns the looks (`kits/`), the contrast-checked palettes
  (`palettes.json`), the icon set, and the runtime.
- A kit carries a one-paragraph brief the model composes to, so output is
  consistent across screens and across turns without a token pipeline.
- Adding a look means adding a kit file. It does not mean adding a format,
  a validator rule, or a spec.

### C. The surface shows the mockup

- Design mode shows the current `design/mockup.html` rendered, with the
  screen switcher the document already carries.
- The chat that drew it is beside it, so the next request is one sentence
  away.
- Nothing else: no rail of asset kinds, no flow canvas, no health strip, no
  annotation pins, no token editor, no conflict banner.

### D. Mockups are disposable until they aren't

- A mockup is a sketch for a conversation, not a deliverable. It lives at a
  fixed path, is overwritten freely, and is versioned by git like any file.
- When a mockup is approved, the code gets written from it by the normal
  agent loop. There is no handoff artifact, no generated theme, no sync
  step, and no drift state to report.

## What was removed (and why)

| Removed | Why |
|---|---|
| `design/` tree contract (tokens, screens, flows, brand, icons, feedback, runtime, generated) | A tree with eight tiers is a project, not a sketch. |
| `design_validate`, `design_assets`, `design_brief`, `design_export_tokens`, `design_sync`, `design_critique`, `design_render`, `design_import_sketch` | Eight tools whose job was to keep the tree consistent. With no tree, there is nothing to keep consistent. |
| DTCG token tier + generated CSS/TS/Tailwind exports | Brand fit comes from kits. A per-project token pipeline is a second brand system to maintain. |
| Flow sources + derived `.mmd` + flows canvas | A mockup shows screens. Flows were a separate authoring format with its own drift rules. |
| Screen kit runtime (`sprout-screens.js`, `chrome.css`, base templates, `screens.json`) | The mockup document carries its own runtime and its own device frame. |
| Critique pipeline, render cache, findings sidecars, health strip, drift rows | Reviewing a sketch is the person's job, and it takes one look. |
| Co-editing (revision-checked writes, conflict banner, token/status forms, pin drag, grid reorder), feedback annotations | The conversation is the edit channel. |
| Screen-centric IA, Library views, DesignRail | One surface: the mockup and the chat. |

## Non-goals

- No proprietary format or external design tool dependency.
- No pixel-level visual editor.
- No multi-user realtime collaboration.
- No design↔code synchronization machinery.

## Acceptance criteria

- [ ] From a prompt in Design mode, a first screen is visible in seconds and
      the finished mockup is a single `design/mockup.html` that opens on its
      own.
- [ ] A follow-up sentence changes the existing mockup without a redraw of
      the whole document.
- [ ] Every mockup is composed from a kit — the model writes no styling —
      so two mockups drawn in the same kit look like one product.
- [ ] The Design surface shows the mockup and the chat, and nothing else.
- [ ] No design tool other than `draw_mockup` is registered.
