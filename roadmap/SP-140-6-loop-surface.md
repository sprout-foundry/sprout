# SP-140-6 — The Loop Surface: Agent State in the Design Space

> **Status (2026-09-22):** Shipped — `849f41282` (PR #85), merged to
> `main` via `fe93ae98a` (released in v0.18.12).
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-3/4/5
> (all shipped on `feat-design-workspace`). Co-editing of design assets
> by humans — edits that interleave safely with agent edits — is scoped
> separately in [SP-140-7](./SP-140-7-co-editing.md), which builds on
> this spec's live tree (§6a), status endpoint (§6b), and agent panel
> (§6f).

## Problem

SP-140-1…5 made the agent design-literate and shipped both loops: the
visual loop (render→critique, consistency rules, human feedback) and the
design↔code sync loop (drift direction, `design_sync`, co-commits,
offline provenance). The webui DesignView (SP-140-3 + the workspace-modes
rework) renders the tree. But the two halves do not touch: **the loop the
agent runs is invisible in the space where the human reviews its
output.** Concretely, at the current HEAD:

1. **No agent presence.** `DesignShell` renders no chat and no activity
   feed — `WorkspaceShellProps` hands the chat payload only to
   `CodeShell`. The specs' answer to "how do flows get authored?" is
   "chat is the authoring surface" (SP-140-3 §3b), yet Design mode has no
   chat reachable from it. Directing the agent means abandoning the mode.
2. **No loop-state visibility.** `design_assets` and `design_validate`
   report validation findings, drift direction with remedies, and pending
   feedback counts. `webui/src` contains zero references to any of it. A
   user opening Design mode cannot see whether the tree is valid,
   drifting, or carrying unanswered feedback.
3. **Annotations are written but never rendered.** The §3e affordance
   writes `at: {x, y}` (defaulting to the asset's center —
   `DEFAULT_ANNOTATION_POINT` — because it has no drawing surface) and
   item 4.8 ships the resolution flow, but no pin is ever drawn on the
   render the note is about.
4. **Stale truth.** `DesignWorkspaceProvider` fetches the inventory once
   per mode activation (effect deps `[active, fetchFn]`). After an agent
   turn edits `design/`, the surface shows the previous tree until the
   user leaves and re-enters the mode.
5. **Critique findings evaporate.** `design_critique` returns findings as
   tool output; only the PNG + a cache-key sidecar persist under
   `design/.cache/renders/`. The last critique of a screen is not
   reviewable anywhere.
6. **Human editing stops at raw text.** A screen's HTML is editable in
   the detail pane's LivePreview split (SP-140-3 §3c), but that is the
   whole editing story: the Tokens tab is read-only — tweaking a value
   means opening the raw `.tokens.json` in the code editor — and the
   README status markers (`draft`/`review`/`ready`) have no affordance
   at all. The user's curation loop — adjust a value, mark a screen
   ready — has no surface in the design space.

The next phase makes the **loop itself the interface**. The user in
Design mode is in a review-and-direct posture: they should see the
tree's health, see what the agent did, mark up renders in place, make
the small edits a human owns (token values, screen status), and
dispatch the rest to the agent without leaving the mode.

## Premise: reads come from the agent's own scanners; writes stay one-sided

The health data is computed by the same `pkg/design` machinery the agent
tools use (`ValidateTree`, the drift analyzers, `ScanFeedbackDir`) — the
webui never re-implements a validator, so there is one truth. The write boundary is by *author*, not silence: the webui writes only
what a human directly authors — screen HTML (the §3c LivePreview
write-back), feedback files (§3e/§4d), canvas layout sidecars
(derived), and, new in this phase, token `$value` edits and README
status markers (§6h/§6i). Everything that authors *semantics* — flow
sources, wireframes, new screens, sync adoption, critique runs — is
**agent-mediated**: the UI prefills a prompt; the agent's tools do the
writing. This keepsSP-140-5 §5e intact (the semantic layer is invited to adopt, never
auto-enforced) and the canvas's round-trip rule intact (no `.mmd`
writes).

## Design

### 6a. Live tree — inventory refresh

`DesignWorkspaceProvider` refetches the inventory when:

- the window regains focus,
- on a slow interval (30 s) while Design mode is active,
- on an explicit refresh control (the health strip's refresh button,
  §6c).

A refetch that no longer sees a previously selected asset clears the
selection; otherwise selection is preserved. The fetch stays gated on
`active` (a Code-mode session never pays for it). No WebSocket push in
this phase — polling + focus is enough for tree-sized payloads and
keeps the provider's contract simple.

### 6b. Design status endpoint — `GET /api/design/status`

One new read-only HTTP endpoint, mounted by a `registerDesignRoutes`
group in `pkg/webui/routes.go` (the file's existing grouped-registration
pattern). This deliberately amends SP-140-3 §3f's "zero new endpoints"
preference: that clause excepted endpoints the file APIs cannot express,
and findings/drift/feedback aggregates cannot be expressed by
`/api/files` + `/api/file` without shipping a second validator to the
browser — which would fork the truth.

- **Pure core** in `pkg/design/status.go`: `BuildDesignStatus` composes
  the already-exported scanners — `ValidateTree` (findings with
  severity tallies), the drift analyzer behind `design_assets` (the two
  directional rows, design-ahead and code-ahead, each with its remedy —
  same vocabulary pinned by
  `TestDesignAssets_DriftBothDirectionsDistinctRows`), and
  `ScanFeedbackDir` (pending targets with unresolved-annotation
  counts). No new analysis logic; aggregation only.
- **Handler** `handleAPIDesignStatus`: read-only, resolves `design/`
  inside the workspace through the same filesystem-safe resolver the
  other file handlers use; no writes; no Gate-1 (that is the agent-tool
  surface, not HTTP). A workspace with no `design/` returns
  `{exists: false}` with HTTP 200 — the health strip renders "no design
  tree", not an error.
- **Payload shape** (v1):

  ```json
  {
    "exists": true,
    "validation": {
      "errors": 1, "warnings": 2, "infos": 5,
      "findings": [{"file": "...", "line": 12, "severity": "warn",
                     "rule": "svg_data_nav_dangling", "message": "..."}]
    },
    "drift": {
      "designAhead": {"count": 2, "remedy": "run design_export_tokens ..."},
      "codeAhead":   {"count": 3, "remedy": "run design_sync to import ..."}
    },
    "feedback": {"pendingCount": 1,
                  "pending": [{"target": "design/screens/login.html",
                                "unresolved": 2, "status": "changes-requested"}]}
  }
  ```

- Findings in the payload are **capped** (first 200 in the validator's
  canonical order); the tallies stay authoritative so the strip is
  never wrong about counts.

### 6c. Health strip

A slim status bar across the top of the design surface (inside
`DesignSurface`, above the canvas/detail split): validate tally chips
(errors/warnings), the two drift rows exactly as the endpoint words them
(only ahead rows render; a synced tree shows one quiet "in sync" mark),
and the pending-feedback count. A click on a chip or row:

- validation finding → selects that asset in the sidebar/detail pane
  and shows the finding;
- design-ahead → opens the Tokens section (the export remedy's surface);
- code-ahead → opens the agent panel (§6f) with the remedy prefilled as
  a prompt;
- pending feedback → selects the annotated asset (pins visible, §6e).

Includes the manual refresh control (§6a). House rules apply: token-only
CSS, `data-testid` registry entries, aria labels.

### 6d. Critique findings sidecar

`design_critique` additionally writes its structured findings to
`design/.cache/renders/<artifact-stem>.findings.json` next to the
existing cache sidecar:

```json
{
  "target": "design/screens/login.html",
  "rubric": "all",
  "sourceHash": "<sha256 of render source>",
  "generated": "2026-09-19T12:00:00Z",
  "findings": [{"area": "hierarchy", "severity": "major",
                 "note": "...", "suggestion": "..."}]
}
```

This is derived output per SP-140 invariant 2: provenance lives in-band
(`sourceHash` + `generated`, the same convention as the layout
sidecar's `derivedFrom`), the file is regenerable by re-running the
tool, and `.cache/` is already gitignore-guided (SP-140-1 §1h). Severity
vocabulary is the tool's own (`blocker`/`major`/`minor`/`info`). A
non-vision run (`visual: false`) writes the static findings with the
same schema plus a `"visual": false` field. Writes go through the
tool's existing artifact path (already Gate-1 scoped to `design/.cache/`).

### 6e. Annotation pins on renders

The detail pane's screen preview (LivePreview) and wireframe nodes gain
a **pin layer**: every annotation in the target's feedback file renders
as a pin at its normalized `at` coordinates, colored by `area` (the
§4a rubric vocabulary — the affordance's area picker already offers
exactly these). Pin click opens that annotation in the pane (note,
resolved state, resolution note); open annotations are visually
distinct from resolved ones. Screens-grid cards show a pending count
badge. Data path is the existing `readFeedback` — no backend change.

### 6f. Agent presence in Design mode

`DesignShell` grows a collapsible **agent panel** (right side, or
bottom on mobile) so Design mode can direct the agent without a mode
switch. Two disciplined reuse rules:

- The panel renders from the `chat`/`messages`/`toolExecutions`/
  `isProcessing` payload `WorkspaceShellProps` **already passes to every
  shell** (DesignShell currently ignores it). No second query client, no
  new plumbing — the same send path the Code shell uses.
- Context prefill: an **"Ask the designer"** affordance on every asset
  (detail pane) and on the health strip's remedy rows seeds the input
  box with the asset path / remedy text (selected brief via
  `design_brief` vocabulary). Seeding fills the input; it never
  auto-sends.

The panel stays within the Design lazy chunk; the mode keeps its
no-menubar/no-statusbar shell identity.

### 6g. Loop results in the detail pane

With a screen selected, the pane shows, beneath the existing feedback
surfaces:

- **Last critique** — the §6d sidecar's findings (severity chip, area,
  note, suggestion), with an explicit stale marker when
  `generated` < the asset's inventory `modified` time (mtime
  comparison; no client-side hashing in v1). A missing sidecar renders
  "no critique recorded" with a prefill prompt to run one.
- **Drift context for this asset** — when the tree is code-ahead and
  the asset has pending sync relevance (it is a wireframe/screen a
  structural delta would touch), surface the drift row's remedy as an
  "Adopt via agent" prefill. The UI never applies; the agent runs
  `design_sync --apply`.

### 6h. House rules

Token-only CSS (`docs/internal/design-system.md`), files under 500
lines (new components split per the SP-140-3 file-split pattern),
everything inside the lazy design chunk (`designChunk.test.ts` extended
to the new modules), testid registry entries for every new interactive
element, and no raw hex anywhere including pin colors (area colors come
from the `@sprout/ui` palette tokens).

## Non-goals

- No WYSIWYG pixel editing; no canvas-side `.mmd` authoring (parent
  non-goals, unchanged).
- No webui writes to `design/` beyond feedback files — sync adoption
  and critique runs stay agent-mediated (§ premise).
- No WebSocket/realtime push of agent state (polling + focus suffices;
  a push channel is future work if the interval proves noisy).
- No per-file sync-proposal computation in the browser — code-ahead
  detail comes from the status endpoint's drift row, not from a
  client-side re-analysis.
- No new agent tools. The roster is closed (SP-140 invariant 7); this
  phase adds one HTTP read endpoint and one derived-artifact write to
  an existing tool.

## Acceptance criteria

- [ ] Inventory refetches on focus, on the 30 s interval while active,
      and via the manual control; a deleted selected asset clears
      selection; a Code-mode session issues no design fetches.
- [ ] `GET /api/design/status` on a fixture tree returns validation
      tallies + capped findings, both drift rows with remedies, and
      pending feedback — the same values `design_validate` /
      `design_assets` / `design_assets` report for the same tree
      (cross-checked by test).
- [ ] No-`design/` workspace: endpoint returns `exists: false`; health
      strip renders its empty state; zero design fetch loops.
- [ ] Health strip: chips render tallies; ahead rows render remedies;
      synced tree shows the quiet mark; every chip's click-through
      lands the user on the surface it names.
- [ ] `design_critique` writes `.findings.json` beside the PNG with
      `sourceHash`/`generated`/severity vocabulary; non-vision runs
      write `visual: false`; the file is absent from `design_validate`
      findings (derived, not source).
- [ ] Pins render at their `at` coordinates on the screen preview,
      colored by area, distinct open vs resolved; pin click focuses the
      annotation in the pane; grid cards show pending counts.
- [ ] Click-to-place: starting "Add feedback" then clicking the preview
      records that point as `at`; skipping placement keeps the center
      default.
- [ ] Design mode agent panel sends through the existing chat path;
      "Ask the designer" prefills without sending; no second query
      client is introduced (test asserts single transport).
- [ ] Detail pane shows the last critique with the stale marker rule;
      missing sidecar shows the prefill prompt; code-ahead assets show
      the adopt prefill and the UI performs no `design/` writes.
- [ ] `cd webui && npx prettier --check`, `make lint`, `go test ./...`
      clean; new components covered by vitest; Playwright spec covers
      strip → finding → detail pane → pin.
