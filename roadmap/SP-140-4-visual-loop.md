# SP-140-4 — Visual Loop: Render→Critique, Consistency Checks, Human Feedback

> **Status (2026-09-22):** Shipped — merged to `main` via `fe93ae98a`
> (released in v0.18.12).
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-2 and
> SP-140-3 (annotations consume the 140-3 write path; the checks need 140-2's
> render path).

## Problem

With the persona, tools, and canvas in place, the agent can *produce*
designs but cannot *see* them. There is no closed loop: no automated
critique of rendered output, no consistency checking across screens and
flows (a `data-nav` that leads nowhere, a button on screen A with no
route back, spacing values that ignore the token file), and no channel
for human feedback to reach the agent. Designs drift and nobody notices.

## Design

### 4a. Critique loop — `design_critique`

New agent tool wrapping `design_render` + the vision tier:

- Args: `target` (screen, flow, or whole tree), optional
  `rubric` (`consistency` | `accessibility` | `hierarchy` | `all`,
  default `all`), optional `compare_to` (a second screen for delta
  review).
- Renders the target(s), attaches images via the SP-137 path, and
  prompts with a structured critique rubric derived from the designer
  prompt's vocabulary (hierarchy, affordance, consistency, spacing
  rhythm, contrast, touch-target sizes).
- Output: structured findings `{target, area, severity, note,
  suggestion}` plus a derived-artifact PNG path (cache under
  `design/.cache/renders/`, gitignore-able — provenance header per
  SP-140 invariant 2).
- Non-vision primaries: degrade to `design_validate`-style static
  findings with an explicit `visual: false` marker; never fail the turn
  (SP-137 tier order applies when any vision path exists).

### 4b. Consistency checker — extends `design_validate`

New rule packs in the SP-140-1g validator (same findings schema — not a
new tool):

- **Flow/wireframe bidirectionality:** every `data-nav` target exists;
  for flows, every edge has a wireframe counterpart unless the node is
  a terminal state; screens referenced in README exist.
- **Token usage:** SVG `fill`/`stroke`/`font-family` values that are
  literal colors/font names instead of comments referencing
  `{token.paths}` are flagged `info` (wireframes are sketches — literal
  values are allowed but tracked so SP-140-5's sync reporting can quantify
  them).
- **Screen inventory:** every wireframe stem appears in either a flow
  or the README (orphan screens surface as `info`).
- **Naming:** slug rule, duplicate screen names across
  `wireframes/` and `screens/` mismatch (`warn`).

### 4c. Self-review workflow step

The `design-system` skill's revise step (SP-140-2b) gains the concrete
loop: after writing artifacts → `design_validate` (static) →
`design_critique` (visual) → fix → repeat, with a stopping rule
(critique findings all `info` or explicitly accepted). The designer
prompt references the same loop. This is prompt/skill content plus the
4a/4b tools — no new runtime machinery.

### 4d. Human feedback channel

Schema for `design/feedback/<target>.json` (write path shipped in
SP-140-3e):

```json
{
  "target": "wireframes/login.svg",
  "status": "changes-requested",
  "resolution": "",
  "annotations": [
    {
      "id": "a1",
      "at": {"x": 0.42, "y": 0.18},
      "area": "hierarchy",
      "note": "Primary CTA reads as secondary; swap emphasis with the link below",
      "resolved": false,
      "created": "2026-09-15T10:36:47Z"
    }
  ]
}
```

- `at` coordinates are normalized 0–1 (resolution-independent).
- `resolved` (per annotation, set from the DesignView detail pane) and
  the top-level `resolution` note (written by the agent when it closes
  the loop with a summary of changes) are the resolution fields the
  140-3e/4d flow depends on.
- Agent side: `design_assets` output includes pending feedback targets
  with counts; the skill's loop starts any `changes-requested` target
  with a read of its feedback file. No new tool needed — this is
  prompt/skill wiring over existing file tools.
- History: feedback files are workspace files like everything else —
  the annotation that requested a change and the design edit that
  resolved it land in the same repository, giving every design
  decision a durable *why* reachable via `git log -p design/feedback/`.
- The DesignView feedback affordance (SP-140-3e) gains: resolution
  flow (mark annotation `resolved` from the detail pane; agent can
  close the loop by summarizing changes into a `resolution` note).

### 4e. Critique caching and cost control

- Rendered PNG cache keyed by content hash (same hash as layout
  sidecars). Repeat critiques of unchanged screens skip re-render.
- Whole-tree critique is capped: `--sample` or implicit cap at 20
  screens per run with explicit notice, so one turn cannot fire 60
  vision calls. Aligns with SP-125's cost-consciousness.

## Non-goals

- No automated design scoring/ranking against external benchmarks.
- No automatic *application* of critique — the loop proposes; the
  agent (and human, via feedback) decides.
- No pixel-diff regression tooling (Chromatic-style) — the webui's own
  visual regression exists for `@sprout/ui`; project-level visual
  regression is out of scope.
- No realtime co-editing of feedback.

## Acceptance criteria

- [ ] `design_critique` on a fixture screen with a vision-scripted
      client returns structured findings; images flow the SP-137 path;
      PNG lands in `design/.cache/renders/` with provenance header.
- [ ] Non-vision scripted client: `design_critique` returns
      `visual:false` static findings, no error.
- [ ] Consistency rule packs: seeded fixtures each trip their rule
      (orphan screen, dangling data-nav, README screen reference to
      missing file, literal-color tracking) with the right severity.
- [ ] Feedback round trip: annotation written via the webui write path
      → `design_assets` reports pending feedback → skill-loop test
      (scripted agent) reads feedback and addresses the annotated
      screen → annotation resolvable in DesignView.
- [ ] Cache: second critique of unchanged content skips re-render
      (test asserts render-count).
- [ ] Whole-tree cap enforced with explicit notice at 21+ screens.
- [ ] `go test ./...`, `make vet && make lint && make build-all` clean.
