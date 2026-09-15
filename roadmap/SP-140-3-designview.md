# SP-140-3 — WebUI DesignView: Flow Canvas and Screen Browser

> **Status (2026-09-15):** Draft — not started.
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-1;
> SP-140-2 makes it end-to-end but is not a blocker.

## Problem

The agent will produce design trees (SP-140-1/2), but the webui has no
surface for them. `LivePreview.tsx` renders one open SVG/HTML file;
nothing shows the system: no flow graph, no screen inventory, no spatial
overview, no way for a human to see what the agent designed or mark it
up. Design assets would be invisible text files.

## Design

### 3a. DesignView — one new top-level view

`webui/src/components/design/DesignView.tsx`, added to the existing
view routing (`AppContent.tsx` currentView pattern). Activates when
`design/` exists in the workspace (same detection the agent uses —
presence of the directory); hidden otherwise. Layout: canvas as the
primary surface, with a left rail (assets browser) and a right detail
pane.

Three tabs inside DesignView: **Flows**, **Screens**, **Tokens**.
Mirrors the asset classes; keeps the view shallow (SP-139's
de-necessitation lesson: do not build a six-tab panel again).

### 3b. Flows canvas — React Flow over mermaid + wireframes

- **Library:** React Flow (`@xyflow/react`, MIT). Chosen for plain
  serializable JSON nodes/edges and headless positioning. Layout
  computed by `dagre` (MIT) — same layout engine mermaid itself uses,
  so canvas positions match the agent's `design_render` output.
- **Sources of truth:** `design/flows/*.mmd` (edges, labels, groups)
  and `design/wireframes/*.svg` (node imagery). The canvas holds
  **derived** position data only: `design/flows/<name>.layout.json`
  sidecars `{nodes: {id: {x, y}}, derivedFrom: <mmd content hash>}` —
  SP-140 invariant 2; regenerated when the hash drifts.
- **Node imagery:** wireframe SVGs render inside nodes (object URL
  from file text). Flows without wireframes render labeled boxes.
- **Interactions v1:** pan/zoom, node select → detail pane, edge
  select → label + source line, drag to reposition (persists to
  sidecar), click-through: open the wireframe in the editor.
- **Round-trip rule:** the canvas never writes `.mmd`. Adding/editing
  flows is the agent's job (chat or edit in the flow source file shown
  in the detail pane). This keeps one writer for semantics and avoids
  building a mermaid serializer.

### 3c. Screens tab

- Grid of wireframe/screen cards (SVG thumbnails, screen name, README
  status chip). Click → detail pane = **LivePreview reuse**: the
  existing split editor + renderer component, instantiated with the
  screen file (behavior unchanged; it is already exactly this surface).
- Device-frame-aware sizing from `design/README.md` frames.

### 3d. Tokens tab

- Grouped tree of the DTCG files with swatches for `color`,
  rendered specimens for typography/spacing. Read-only in v1 (editing
  tokens is file editing; the detail pane opens the `.tokens.json` in
  the editor with a JSON schema for validation).
- Search/filter across token paths.

### 3e. Feedback annotations (stub for SP-140-4d)

The detail pane reserves an annotation affordance (pin + note on
screens) that writes `design/feedback/<target>.json` per the SP-140-4d
schema. In this spec only the write path and file schema land; the
agent-side consumption (reading feedback) is SP-140-4.

### 3f. Data access

New thin `webui/src/services/designApi.ts` over the existing
workspace-file APIs (same fetch patterns as `filesApi`):
`listAssets()`, `readAsset(path)`, `writeLayout(name, sidecar)`,
`writeFeedback(target, json)`. No new HTTP endpoints unless the
existing file APIs cannot express these — prefer zero backend changes;
the workspace is already served to the webui.

### 3g. Vendored mermaid for client rendering

Pin mermaid (and dagre) into the webui bundle for the Flows tab and
Screen-flow diagrams. Offline-safe, consistent with SP-140-2c's
agent-side vendored copy. No CDN references (offline/workspace-isolation
rules).

### 3h. Design-system compliance

DesignView's own UI uses the existing `@sprout/ui` tokens — no raw
hex/rgba (house rule). Fittingly, SP-140-5 will later let this rule be
enforced *from* a project's own tokens; out of scope here.

## Non-goals

- No freehand drawing/pixel editing.
- No canvas-side `.mmd` authoring (chat is the authoring surface).
- No multi-flow mega-canvas composition in v1 (one flow per canvas
  tab-state).
- No backend HTTP additions if the existing file APIs suffice.

## Acceptance criteria

- [ ] Workspace with fixture `design/` tree: Flows tab renders the
      flow graph with wireframe imagery in nodes; edge labels visible;
      dagre layout matches `design_render`'s orientation hints.
- [ ] Drag persistence: reposition writes the sidecar with
      `derivedFrom` hash; editing the `.mmd` (hash drift) regenerates
      layout on next load.
- [ ] Screens tab: thumbnails render; click opens LivePreview split
      view on the file.
- [ ] Tokens tab: fixture DTCG files render grouped with swatches;
      search filters by token path.
- [ ] Workspace without `design/`: DesignView hidden; zero bundle
      impact verified by lazy-load (dynamic import chunk).
- [ ] Feedback write path produces `design/feedback/<target>.json` in
      SP-140-4d's schema.
- [ ] Vitest coverage: layout-derivation pure functions (mmd → dagre →
      nodes), sidecar hash/staleness logic, designApi.
- [ ] Playwright e2e (`test/webui/design_view.spec.ts`, chrome-channel
      launch per AGENTS.md): open fixture workspace → flow renders →
      select node → detail pane → open screen in editor.
- [ ] `cd webui && npx prettier --check` and `make lint` clean.
