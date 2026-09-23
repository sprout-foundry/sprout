# SP-140-3 — WebUI DesignView: Flow Canvas and Screen Browser

> **Status (2026-09-22):** Shipped — merged to `main` via `fe93ae98a`
> (released in v0.18.12).
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
view routing (`currentView` on `AppState`, `webui/src/types/app.ts:92`;
the render branch lives in `EditorWorkspace.tsx`'s view switch, with a
nav affordance added to `Sidebar.tsx`). `ViewType` is an open union, so
`'design'` needs no type change. Activates when `design/` exists in the
workspace (same detection the agent uses — presence of the directory);
hidden otherwise. Layout: canvas as the primary surface, with a left rail
(assets browser) and a right detail pane.

File-split plan (AGENTS.md <500-line rule): `DesignView.tsx` is a shell
only — routing, tab state, layout. Each tab is its own component
(`FlowsCanvas.tsx`, `ScreensGrid.tsx`, `TokensTree.tsx`), the canvas
layout derivation lives in pure modules (`webui/src/design/layout.ts`,
`sidecar.ts`), and the feedback affordance is its own component.

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
  sidecars `{nodes: {id: {x, y}}, layoutHint: <hint or "">, derivedFrom:
  <mmd content hash>}` — SP-140 invariant 2; regenerated when the hash
  drifts. `layoutHint` records the `design_render` orientation hint so
  the canvas layout and the agent's render agree (the AC "dagre layout
  matches `design_render`'s orientation hints" is checked against this
  field).
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
  existing split editor + renderer component, instantiated directly as
  the controlled component (`content`/`language: 'html'`/`fileName`
  props; the synthetic `__workspace/` preview-buffer path is not
  used). `onContentChange` wires to a file write through `designApi`
  (the detail pane's write-back path).
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

New thin `webui/src/services/api/designApi.ts` (alongside
`filesApi.ts`, with an export line in `webui/src/services/api/index.ts`)
over the existing workspace-file APIs (same fetch patterns as
`filesApi.ts`): `listAssets()`, `readAsset(path)`, `writeLayout(name,
sidecar)`, `writeFeedback(target, json)`. No new HTTP endpoints unless
the existing file APIs cannot express these — prefer zero backend
changes; the workspace is already served to the webui.

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
      launch per AGENTS.md): the standard `start-stack.mjs` webServer
      boots a fresh temp workspace with no `design/`, so this spec must
      start its own stack with a pre-seeded `workspaceDir` fixture
      (`test/webui/fixtures/sprout.ts` `StartSproutOptions`): open
      fixture workspace → flow renders → select node → detail pane →
      open screen in editor.
- [ ] `cd webui && npx prettier --check` and `make lint` clean.
