# SP-140-7 — Human Co-Editing: Design Edits That Work With Agent Edits

> **Status (2026-09-20):** Shipped — items 7.1–7.6 landed on
> `feat-design-workspace` (`d2eea8146`…`d59ae36f2`, rolled up in
> `1ebdc7673`); the §7g Playwright conflict-flow pass is the remaining
> follow-up.
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-3/4/5
> (shipped) and coordinates with SP-140-6 (loop surface; §6a's live tree
> and §6f's agent panel are prerequisites for the awareness items here).

## Problem

The design space is one writer short. The agent authors `design/`
through its tools; the human can only annotate around the edges or drop
to raw text — and when both sides touch the same file, nothing in the
design surfaces notices. Concretely, at the current HEAD:

1. **Writes are blind overwrites.** Every design write path
   (`writeAsset`, `writeFeedback`, `writeLayout` in
   `designApiWrite.ts`) POSTs full content to `/api/file` with no
   revision guard, and `handleFileWrite` unconditionally writes. A user
   editing `screens/login.html` while an agent turn rewrites it loses
   one side silently. Code mode has the same physics but a softer
   landing: editor buffers, the `/api/file/check-modified` polling
   endpoint, and the Agent Changes panel. Design mode has none of it.
2. **Incoming agent changes are invisible in Design mode.** The
   infrastructure exists — `file_changed` WS events feed the per-turn
   `fileEdits` strip and dispatch an `agent-file-changed` window event —
   but no design surface subscribes. The canvas, grid, and token tree
   only update via §6a's refetch; an open editor never learns the file
   moved under it.
3. **Human editing stops at raw text.** Tokens are read-only (tweaking a
   value means opening the raw `.tokens.json` in the code editor);
   README status markers (`draft`/`review`/`ready`) have no affordance;
   the annotation pin's position cannot be adjusted after placement.
   The user's curation loop — adjust a value, mark a screen ready,
   nudge a pin — has no surface.
4. **The safe gestures are underused.** The flows canvas already
   demonstrates the right pattern: drag writes *derived* data (the
   layout sidecar, hash-guarded, never the `.mmd`). Only that one
   gesture exists.

The goal is not a second editing product. It is that **the user's edits
meet the agent's edits the way code edits do**: same files, same git
history, same review machinery, with explicit conflict handling instead
of silent loss — plus drag-and-drop wherever a gesture can be mapped to
a write that is derived, human-authored, or manifest-level.

## Premise: one writer per gesture, one truth per file

Every gesture in this spec is classified before it is built, by what it
writes:

- **Derived writes** (layout sidecars) — always safe, hash-guarded,
  regenerable.
- **Human-authored writes** (screen HTML, feedback files, token values,
  README status markers) — allowed from the UI, guarded by the
  safe-write seam (§7a), authoritative validation stays server-side
  (`design_validate` via SP-140-6 §6b).
- **Semantic writes** (`.mmd` flow sources, wireframes, asset
  moves/renames, new screens) — never written by drag or form; they go
  through the agent (SP-140-6 §6f prefill) because their consistency
  rules (flow node ids, feedback targets, README links) are exactly
  what blind edits break.

This is the same posture as Code mode: the user edits buffers, the
agent edits files, git is the arbiter, and the review surfaces make the
interleaving visible. Nothing here forks that — no parallel edit
history, no design-only version store.

## Design

### 7a. Safe-write seam — revision-checked writes

`POST /api/file` gains an **opt-in** conditional-write contract: the
request body may carry `baseMtime` (unix seconds, from the write
response's `modTime` or the inventory's `modified`) and optionally
`baseHash` (sha256 of the last-read content, for mtime-coarse
filesystems). When either is supplied and does not match current disk
state, the handler returns **409** `{path, currentMtime, currentHash}`
and writes nothing. Omitting the fields preserves today's behavior —
Code mode is untouched, migration is per-caller.

`designApiWrite` gains `writeAssetIfUnchanged` (and safe variants for
feedback/layout/token writes) that threads the loaded revision through.
Every design surface that loads-then-edits a file uses the safe variant.
This seam is the foundation for everything below; it is also the only
piece that touches shared backend code.

### 7b. Incoming-change awareness — the co-editing loop

Design surfaces subscribe to the existing `agent-file-changed` window
bridge (and `file_changed` via the shared handler), filtered to
`design/` paths, and react by role:

- **Not being edited** → silent refetch of the affected asset (merging
  with §6a's focus/interval refetch — one fetch path, event-triggered).
  The canvas, grid, and token tree track the tree without user action.
- **Being edited** (an editor buffer or form holds that asset) → a
  non-blocking conflict banner in the pane:
  **"The agent changed this file while you were editing — Review / Keep
  mine / Take theirs."**
  - *Review* → split compare in the existing editor component: your
    buffer vs current disk (the client retains the loaded base text, so
    the pane can show base→mine and base→theirs side by side; no new
    diff engine — two read-only editors beside the live one).
  - *Keep mine* → deliberate overwrite via the §7a seam with the guard
    explicitly bypassed (force flag); the previous disk text stays in
    the pane's session state behind a "Restore agent's version" action
    for the life of the selection. Git remains the durable safety net.
  - *Take theirs* → drop the buffer, reload from disk.

While an agent turn is in flight (`isProcessing` from the chat payload
the shell already carries — §6f), the health strip shows an "agent
working" indicator. Awareness, never a write lock: the user can keep
editing, and the §7a seam catches the collision if one lands.

### 7c. Token editing — structured form on the DTCG file

Token leaf rows in `TokensTree` become editable: color tokens get a
swatch + hex field, dimensions a number + unit, everything else a text
input. Saving performs a **surgical document edit** — JSON.parse the
`.tokens.json`, set the one leaf's `$value` (structure, aliases,
`$extensions` untouched), canonical `JSON.stringify(2)` — written
through the §7a safe variant. The file stays the only artifact; no
sidecars, no shadow state.

- Light client validation only (parses as JSON; leaf keeps
  `$value`/`$type`). The authoritative check is `design_validate`: the
  health strip (SP-140-6 §6c) reflects any breakage and the edited row
  surfaces its finding inline.
- **Alias warning:** editing a token that others reference shows "N
  tokens reference this" before save. Requires alias reference counts —
  a small addition to the §6b status payload (`tokenRefs` per token
  path, computed by the existing alias-resolution machinery).
- After a save on a design-ahead tree, the strip's design-ahead row
  already names the remedy (`design_export_tokens`) — one click prefills
  it via §6f. The export loop closes through existing pieces.

### 7d. Screen status curation

The status chips on screen cards and the detail pane become a menu
(`draft` / `review` / `ready` / clear). Saving rewrites the README
manifest's status markers — structured, not textual: parse with the
existing `parseManifestStatuses`, rewrite only those lines through the
§7a safe variant. An unparsable manifest never gets clobbered: the menu
falls back to opening `README.md` in the editor with a note. Status is
human curation by definition (it is the review verdict), which is why
this write is allowed where wireframe edits are not.

### 7e. Drag-and-drop — every gesture, classified

| Gesture | Writes | Verdict |
|---|---|---|
| Flow node drag | layout sidecar (derived, `derivedFrom` hash) | **exists** — keep as the model |
| Pin drag on a render | `at` coords in the feedback file (human-authored, coordinate-only) | **build** — drag-to-adjust after §6e's click-to-place |
| Screen card reorder in grid | README `Screens:` listing order (manifest-level) | **build** — requires `listAssets` to expose manifest order; grid order becomes manifest order |
| Token drag onto a wireframe element | wireframe semantics | **no** — prefill "apply `{token}` to X" via §6f instead |
| Asset drag across sections (screens↔wireframes) | move/rename | **no** in v1 — breaks feedback targets, flow node ids, README links; agent-mediated ("move X and update references") |
| OS file drop onto the surface | arbitrary files | **no** in v1 — `design_import_sketch` already owns imports; revisiting later |

The rule the table enforces: a drag may write **derived**, **human-
authored (coordinate/status/list-order)**, or nothing. Each build
gesture ships as a pure gesture→write model function (unit-testable
without DOM) plus a thin pointer-events adapter.

### 7f. Review and history parity

- **Parity guarantee (test-pinned, mostly already true):** writes through
  the webui publish `file_changed` (the HTTP handler already does), so
  user edits appear in the per-turn change strip beside agent edits.
  Pin this with a test so it survives refactors — the "like code"
  promise is that both writers' edits are equally visible.
- **ChangeTracker scope decision:** agent tools track writes via
  `TrackFileWrite`; HTTP writes do not enter the session buffer. This
  phase deliberately does not change that — git is the durable history,
  and adding tracker plumbing to the shared write path is out of scope.
- **Co-commit:** the SP-140-5 §5f rule already covers mixed authorship —
  user edits and agent adoption land in one commit through the existing
  git surface. Documentation in the design-system skill's sync section
  (one paragraph: human edits from Design mode follow the same
  co-commit rule), no new machinery.

### 7g. House rules and testing

Token-only CSS; 500-line rule (gesture models and conflict flow split
per the SP-140-3 pattern); everything inside the lazy design chunk
(`designChunk.test.ts` extended); testid registry entries for the form
controls, banners, and menus. Test posture: concurrency (two racing
safe-writes, exactly one 200), conflict-flow component tests (banner,
review, keep-mine, take-theirs, restore), surgical DTCG edit
determinism (byte-identical re-serialization, `$extensions` preserved),
gesture-model unit tests, README structured-rewrite tests (unparsable
manifest never written), and a Playwright pass: edit a token → strip
updates; edit a screen while a scripted agent writes it → banner →
keep mine; drag a pin; reorder the grid.

## Non-goals

- No realtime multiplayer, OT, or CRDT. Last-writer-wins with explicit
  conflict UX — the same concurrency model Code mode has, made visible.
- No human authoring of `.mmd`, wireframes, or new screens (agent-
  mediated per the premise; unchanged from SP-140-3's round-trip rule).
- No asset moves/renames or OS drag-drop import in v1.
- No ChangeTracker tracking of HTTP writes (§7f decision).
- No pixel/raster editing (parent non-goal).

## Acceptance criteria

- [ ] A safe write against a changed file returns 409 with current
      revision info and writes nothing; an unconditional write behaves
      exactly as today (Code mode regression-pinned).
- [ ] Agent writes to a `design/` asset while a design editor holds it
      produce the banner; Review shows base→mine and base→theirs; Keep
      mine forces through with a restore action; Take theirs reloads.
- [ ] Agent writes to a non-edited asset update the canvas/grid/tree
      without user action (event-triggered refetch, merged with §6a).
- [ ] Token form edits save byte-minimal DTCG changes (parse → one
      leaf → canonical stringify), trigger the alias warning when
      referenced, and health-strip findings appear for a broken save.
- [ ] Status menu writes only the marker lines of an parsable README
      and never clobbers an unparsable one.
- [ ] Pin drag persists new `at` coords; grid reorder persists manifest
      order and the grid follows it; no gesture in this spec can write
      `.mmd` or move assets (asserted at the model layer).
- [ ] User edits made in Design mode appear in the per-turn change
      strip (test-pinned parity).
- [ ] `cd webui && npx prettier --check`, `make lint`, `go test ./...`
      clean; vitest coverage for every new pure model; Playwright
      covers the conflict flow end to end.
