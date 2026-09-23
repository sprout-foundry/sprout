# SP-140 — Design Workspace: Agent-Native UX Design on Open Formats

> **Status (2026-09-22):** SP-140-1 … SP-140-7 and SP-140-10 shipped
> (merged to `main` via `fe93ae98a`, released in v0.18.12);
> SP-140-8 (screen-centric IA) and SP-140-9 (HTML-first format rework)
> are drafted, not started. This spec is the coordination point:
> directory contract, phase map, cross-spec invariants, and the
> open-format charter.

## Problem

Sprout is a coding tool. Design work — branding, design tokens, wireframes,
navigational flows, screen designs — has no first-class surface, so it
happens outside sprout in tools whose formats are proprietary (Figma) or
disconnected from the code the agent writes. Three concrete gaps:

1. **No design asset convention.** Nothing tells the model where tokens,
   wireframes, or flows live, what formats they take, or how they relate.
   Asked to produce a wireframe today, a model improvises a location and a
   format, and the result is unfindable, unvalidated, and unrenderable.
2. **No visual surface.** The webui renders SVG/HTML only inside
   `LivePreview.tsx` when a single file is open. There is no graph surface
   for flows, no spatial overview of screens, no way for a human to see or
   mark up what the agent designed.
3. **No design literacy or loop.** The prompt and toolset steer toward code;
   the agent cannot see its own designs (no render→critique loop), cannot
   import a whiteboard sketch, and cannot hand tokens off to code.

The premise: this does **not** require a second product or a forked agent
mode. Sprout already has the hard parts — SP-137 shipped first-class vision
(registry-driven capability resolution, tool-result images, native OCR),
`analyze_ui_screenshot` renders and critiques HTML/SVG today, the persona
catalog (`pkg/personas/configs/*.json`) is the sanctioned way to specialize
the agent, the tool registry (SP-109) makes new tools cheap, and the file
workspace + ChangeTracker give design assets the same diff/revert/review
loop as code. What is missing is a **format charter**, a **designer
persona**, a **canvas**, and the **two loops**: the visual loop
(render→critique) and the design↔code loop (continuous sync — no
handoff; see the second premise below).

Naming note: the workspace-level `design/` directory, `design_*` agent
tools, and `designer` persona are distinct from `packages/design/` (the
`@sprout-foundry/design` npm package) and from the webui house token
system (`@sprout/ui`, rooted in `webui/src/App.css`). No coupling, no
shared code — just don't confuse them.

## Premise: files are the integration boundary

Everything in this spec treats design assets as plain files in an
open-format `design/` directory. The agent edits them with the existing
file tools. The webui renders them. The canvas is a viewer/editor over the
same files. No component holds secret state. Any external tool (Penpot,
Figma import, a text editor, `git diff`) can read and write the same tree.
This is the "default to open" commitment, stated as architecture:

- Text formats only, chosen for git diffability and tool portability.
- No proprietary format is ever load-bearing. Importers/exporters may exist
  around open formats; never instead of them.
- The directory contract below is the only coupling between the agent side
  and the canvas side.

## Premise: design and code co-evolve — there is no handoff

The industry treats "design handoff" as a wall: design finishes, code
begins, design goes quiet. Competitors institutionalize the wall because
their users sit on opposite sides of it. Sprout's agent works both sides
of the wall in one loop, so the wall has nothing to stand on here. Design
leads to dev and dev leads back to design, for the life of the product —
the `design/` tree is a living semantic layer that both directions keep
truthful (SP-140-5), not a deliverable that code slowly invalidates.

## Premise: git is the source control for design

There is no separate design version store. `design/` lives in the same
repository as the code it describes, so one commit can carry an
implementation change and the semantic-layer update that adopts it; a
PR diff shows wireframe, flow, token, and code changes side by side;
`git revert` rolls both back atomically; blame on a wireframe answers
which turn changed it and why (session/commit trail). "Which design
version was this build made from?" — the question that haunts separated
toolchains — is answered by the commit hash. This is why every format in
SP-140-1 is text-first: diffability and review are not conveniences,
they are the version-control contract. Sprout's existing machinery
makes this free: ChangeTracker, the per-turn Review/Revert strip
(SP-139), the commit tool's capability gate, and the in-browser git
client (SP-GIT-CLIENT) already operate on any text file in the
workspace — design assets qualify by existing.

## Directory contract (normative for all child specs)

```
design/
  README.md                 # manifest: inventory, links, status (SP-140-1e)
  tokens/                   # W3C DTCG design tokens (SP-140-1a)
    color.tokens.json
    typography.tokens.json
    spacing.tokens.json
    ...
  brand/                    # brand.md + logo SVGs (SP-140-1d)
  icons/                    # icon SVGs + sprite.svg (SP-140-1f)
  wireframes/               # screen wireframes, one SVG per screen (SP-140-1b)
  screens/                  # hi-fi HTML/CSS screen designs (SP-140-1i)
  flows/                    # mermaid flow sources, one .mmd per flow (SP-140-1c)
  feedback/                 # human annotations, JSON per target (SP-140-4d)
```

Presence of `design/` with any of these subdirectories activates design
features (webui DesignView, prompt guidance, tool relevance). There is no
registry to register into — detection is conventional, which is what keeps
external tools equal citizens.

## Phase map

| Phase | Spec | Scope | Depends on |
|-------|------|-------|-----------|
| 1 | SP-140-1 | Format charter: DTCG tokens, SVG wireframe conventions, mermaid flows, brand/icons, README manifest, validator tool | — |
| 2 | SP-140-2 | `designer` persona, `design-system` skill, agent-facing tools (`design_assets`, `design_render`, `design_import_sketch`) | 140-1 |
| 3 | SP-140-3 | WebUI DesignView + canvas (React Flow over mermaid, screen nodes, tokens viewer) | 140-1 (140-2 for end-to-end) |
| 4 | SP-140-4 | Visual loop: render→critique, consistency checks, human annotations | 140-2, 140-3 |
| 5 | SP-140-5 | Design↔code sync (continuous, bidirectional): token export (CSS vars / TS / Tailwind `@theme`), screen scaffold briefs (`design_brief`), `design_sync` code→design import, directional drift reporting | 140-1 (140-4 for full value) |
| 6 | SP-140-6 | Loop surface: agent state in the design space — live inventory, `/api/design/status` + health strip, annotation pins, agent panel in Design mode, critique findings sidecar | 140-3/4/5 |
| 7 | SP-140-7 | Human co-editing: revision-checked writes (409 seam), incoming-change conflict UX, structured token/status editing, classified drag-and-drop gestures, review parity with agent edits | 140-6 (7.1 needs 6.a/6.f; 7.3 needs 6.b) |
| 8 | SP-140-8 | Screen-centric IA: the shell's primary axis is the screen — screen workbench (render/status/feedback/flows/tokens/agent-state facets), kind tabs demoted to a Library group, adaptive chrome | 140-3/6/7 (coordinates with 140-9) |
| 9 | SP-140-9 | Format rework: HTML-first screen tier (wireframe tier retired), structured flow sources, `.mmd` as a provenance-hashed derived export | 140-1 (amends §1b/§1c; ripples into 140-2/4/5) |
| 10 | SP-140-10 | Design-mode chat UX: RTL-aware side-column placement, per-mode conversation pinning, full-height agent panel + live visibility, inline tool details (retire the tool-details sidebar dependency) | 140-6/7 (10d changes the shared chat mechanism; Code-mode regression gates) |

Phases 2 and 3 are independent of each other and can proceed in parallel
after Phase 1. Only Phase 5's token export (SP-140-5 §5a) needs Phase 1
alone — it can start early if sync value is needed before the canvas
exists; the drift reporting (§5c) additionally needs Phase 2's
`design_assets`.

## Relationship to existing work

- **SP-137 (vision first-class, shipped 2026-09-08)** — this spec consumes
  it and adds no vision plumbing. No design-tier code may reference a
  provider by name (SP-137's rule applies to every child spec).
- **`pkg/agent_tools/analyze_ui_screenshot_handler.go`** — existing
  render-and-analyze path for images/URLs/local HTML. SP-140-2's
  `design_render` wraps it for SVG + mermaid sources rather than
  duplicating it.
- **`webui/src/components/LivePreview.tsx`** — existing SVG/HTML split
  preview. SP-140-3's detail pane reuses it; no behavior change to it.
- **Persona catalog** (`pkg/personas/configs/*.json`, `docs/PERSONAS.md`) —
  `designer` is one catalog entry plus a prompt file. Workflow knowledge
  goes in a **skill** (`pkg/skills/library/design-system/`), not the
  catalog, per PERSONAS.md §9.
- **Tool registry** (SP-109) — each new tool is a `ToolHandler` struct +
  one line in `pkg/agent_tools/all.go`.
- **ADR-0008 native seams** — if a future native shell wants the canvas,
  `--native-canvas` fits the existing flag/manifest/ratify protocol
  unchanged. Noted for the future; no action in this spec.

## Non-goals (all phases)

- No Figma (or any proprietary-format) dependency. Exporters from Figma
  *to* `design/` may be documented; Figma is never a runtime dependency.
- No raster/bitmap editing. Sprout produces and edits vector/text assets.
- No WYSIWYG high-fidelity visual editor in v1. The canvas edits structure
  (flows, layout sidecars, annotations), not pixel-level screen art.
- No multi-user realtime collaboration.
- No attempt to replace Penpot/Figma for visual designers. Sprout's design
  surface targets agent-authored and agent-edited assets that humans review
  and refine — in sprout or in any tool that reads the formats.

## Cross-spec invariants

1. Every design asset is a text file in `design/` following SP-140-1. If a
   feature needs a non-text artifact (e.g. a rendered PNG), it is derived
   output: generated, cacheable, never the source of truth.
2. Derived/cached artifacts (rendered PNGs, layout sidecars) must be
   recognizable as derived — sidecars carry a `"derivedFrom"` field or the
   generator writes a provenance header — so tooling can regenerate them.
3. The agent side never depends on the canvas, and the canvas never
   depends on the agent: both read/write the same files.
4. No design-tier code names a provider (SP-137 rule) or a proprietary
   product as a dependency.
5. New webui CSS uses design tokens only (no raw hex/rgba), per
   `docs/internal/design-system.md`.
6. Design assets are versioned by the workspace's git repository like
   any other source file — no external versioning system, no
   out-of-band sync, no side-channel history. Every design change
   reaches the tree through the same write/diff/review/commit surfaces
   as code, and generated artifacts carry provenance hashes so any
   commit's consistency is checkable offline.
7. **Master tool roster**: the design tool set is exactly
   `design_validate` (140-1g), `design_assets`, `design_render`,
   `design_import_sketch` (140-2c), `design_critique` (140-4a),
   `design_export_tokens`, `design_sync`, `design_brief` (140-5a/5b/5g).
   Every one of these that touches workspace paths runs Gate-1
   `PrecheckFileAccess`, including the Phase 4/5 writers (critique
   cache, token export, sync apply) — not just the Phase 2 four.
   Browser- and vision-dependent tools (`design_render`,
   `design_import_sketch`, `design_critique`) are `//go:build !js` with
   WASM stubs mirroring `all_vision.go`; only `design_assets` and
   `design_validate` ship WASM variants.

## Acceptance criteria (umbrella)

- [ ] All five child specs shipped (SP-140-6 extends the shipped surface;
      its own ACs live in its spec).
- [ ] End-to-end: a fresh workspace with no `design/` directory goes from
      one prompt ("design a mobile check-deposit flow for this bank app")
      to a validated `design/` tree (tokens, wireframes, flow) with the
      webui DesignView showing the flow graph and screens — using only
      open formats, with no step requiring an external product.
- [ ] End-to-end loop (both directions): from the produced tree,
      `design_export_tokens` produces CSS variables consumed by a themed
      screen build; a subsequent dev turn that tweaks the implementation
      is imported back via `design_sync`; the next design turn builds on
      the updated tree rather than against it.
- [ ] Every artifact in the produced tree opens correctly in at least one
      non-sprout tool (browser for SVG/HTML, mermaid live editor or GitHub
      rendering for `.mmd`, any JSON tool for tokens).
- [ ] Co-located history: after the loop above, `git log` on the workspace
      shows the dev change and its `design_sync` adoption available for a
      single commit; the PR/diff view renders both; reverting the commit
      reverts both.
