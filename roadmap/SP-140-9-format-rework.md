# SP-140-9 — Format Rework: HTML-First Screens, Derived Flows, Structured Steps

> **Status (2026-09-26):** Shipped (9.1–9.8; 9.7's HTML render path shipped
> with SP-143, flow-critique on the derived graph pinned in 9.3's notes; 9.8's
> determinism/sidecar/drift/migrated-fixture pins landed per item). Parent:
> [SP-140](./SP-140-design-workspace.md). Amends SP-140-1 §1b/§1c (format
> charter); ripples into SP-140-4 (critique), SP-140-5 (sync/brief), and the
> `design-system` skill / `designer` persona (SP-140-2). Coordinates with
> SP-140-8 (screen-centric IA): this spec changes *what a screen is and
> where flow truth lives*; 8 changes *which axis the shell leads with*.
> They land independently — 8 works against either format.

## Problem

Two of the charter's formats are organized around the wrong source of
truth, and the dogfood of the design process keeps producing the same two
complaints:

1. **The primary screen format is a paint layer.** The charter's screen
   tier is one SVG per wireframe (SP-140-1b) — a vector-paint format used
   as a UI-layout format. Authoring a screen in SVG means hand-placing
   boxes and labels; the result reads as "wireframe tool output", not a
   screen. The charter *already has* a better screen format — the
   self-contained HTML screen tier (SP-140-1i, `design/screens/`) — and it
   is demoted to "hi-fi companion" while the SVG wireframe is primary. The
   hierarchy is inverted: HTML is the better screen format; SVG is the
   better icon/brand format (where it already lives, 140-1d/1f).
2. **Flow truth is hand-authored text.** Flows are one `.mmd` per flow,
   written by hand. That inverts the one rule the canvas already got right
   (SP-140-7 §4): *drag writes derived data, never the source*. A `.mmd`
   as the source means: (a) it drifts from the screens it describes
   (renamed screen, moved trigger — the `.mmd` is not updated), (b)
   process steps live as prose edge labels instead of structured data,
   and (c) `design_sync`'s structural deltas (a new route appeared) must
   *write* `.mmd` — a semantic source — which is exactly the shape the
   layout-sidecar rule exists to prevent.

The goal: **each concern gets one source of truth, and the reviewable text
formats become derived artifacts** — the same physics as the §7d layout
sidecar and the token-export provenance hash:

- Screen layout truth → `design/screens/<name>.html` (the primary tier).
- Process/nav truth → a structured flow source (ordered steps + triggers)
  plus per-screen nav edges carried on the screens themselves.
- `.mmd` → a *derived export* (deterministic, provenance-hashed,
  diffable-for-review) — rendered by the existing canvas, never
  hand-edited.
- SVG → icons, brand, and generated artifacts only.

## Premise: one truth per concern; reviewable formats are derived

The design tree already practices this for two artifacts: the layout
sidecar (derived, hash-guarded, regenerable) and `design/generated/`
token exports (deterministic, `source-hash` provenance, recomputable
offline). This spec extends the pattern to the screen and flow tiers:

- A **human-authored** artifact is the thing the designer (or agent)
  actually curates: the HTML screen, the token values, the flow steps, the
  feedback notes.
- A **derived** artifact is regenerated deterministically and carries a
  provenance hash so any checkout can verify it offline: the `.mmd`
  export, the layout sidecar, the token exports.
- The validator enforces the split: derived artifacts that don't match a
  recomputation are *errors* (drift), the same class as a dirty token
  alias graph.

## §9a — Screen format: HTML is the primary tier

- `design/screens/<name>.html` (self-contained, per the existing 140-1i
  convention) becomes *the* screen format. A screen that exists only as an
  SVG wireframe no longer satisfies the "screen" slot of the tree.
- Screens carry a small **data-attribute contract** (machine-readable,
  validator-checked; exact schema settled in item 9.1):
  - `data-screen="<stem>"` — the screen's identity (agrees with the README
    manifest entry).
  - `data-nav` — the screen's outbound edges: one per `<a
    data-nav="to:<stem>;trigger:<label>">` navigation target in the
    markup (the designer wires real anchors/buttons, not a parallel
    graph file).
  - `data-status` is *not* on the screen — status stays in the README
    manifest (one status truth, per 140-1e).
- The **wireframe tier (140-1b) is retired**: the validator reports
  `design/wireframes/*.svg` as `deprecated` (warn) for one release, then
  `error`. Icon SVGs (140-1f) and brand SVGs (140-1d) are unaffected —
  SVG stays the icon format, exactly where it is good.
- Authoring path: an agent turn builds a screen as HTML from the
  `design_brief` output (the brief's "delivered screen file" becomes the
  HTML screen); `design_import_sketch`'s `wireframes` target retargets to
  `screens` (HTML extraction).

## §9b — Structured flow source; `.mmd` becomes a derived export

- **Flow source** (new, human-authored): `design/flows/<name>.json` —
  one per process, the structured steps of the complaint:
  ```json
  {
    "name": "sign-up",
    "steps": [
      { "id": "s1", "label": "Start", "screen": "sign-up" },
      { "id": "s2", "label": "Verify email", "screen": "verify",
        "trigger": "submit code", "next": "s3" }
    ]
  }
  ```
  (schema details — optional `condition`, parallel branches, terminal
  steps — settled in item 9.2; v1 is a linear step list with triggers.)
- **Derived export**: a deterministic generator emits
  `design/flows/<name>.mmd` from the `.json` source + the screens'
  `data-nav` edges (the graph a flow *implies*: its steps, plus every
  screen reachable via `data-nav` from a step's screen, marked as
  off-path). The `.mmd` carries a provenance header
  (`source-hash: fnv1a64:<hex>` over the inputs, the token-export
  convention) so `design_validate` recomputes and flags drift as an error.
  Hand-editing a `.mmd` = editing a derived artifact = invalid; the edit
  goes into the `.json`/screens and the export regenerates.
- **Cross-check**: a `data-nav` edge from a screen that no flow step or
  other screen accounts for is a *warning* (an off-path navigation —
  often intentional, e.g. a global nav), never an error.

## §9c — The canvas renders the derived graph

- The Flows canvas keeps rendering mermaid (the vendored `mermaid.min.js`
  stays) — it now renders the **derived** `.mmd`. No new renderer.
- The §7d layout sidecar (per-node positions, hash-guarded) is unchanged:
  it keys on node ids, and the generator keeps node ids stable
  (step ids / screen stems), so an existing sidecar still applies after a
  regeneration.
- Canvas drags write the sidecar only — the rule from 140-7 §4 now holds
  for the whole flow tier, not just node positions.

## §9d — Ripples (shipped in the same items as the validator, per the
SP-140 "no split source/registration" rule)

- **`design_validate`**: screens as primary tier (140-1i rules + the
  §9a data-attribute schema); flow `.json` source schema (§9b); derived
  `.mmd` drift check (recompute + hash); wireframe-tier deprecation (warn
  → error).
- **`design_sync`**: structural deltas no longer write `.mmd`. A new route
  on the dev side now proposes *a new screen + a step/edge* (a `.json`
  source delta, structural confidence), and an inferred raw-hex styling
  delta is unchanged (proposal only).
- **`design_brief`**: flows IN/OUT read from the `.json` steps + the
  screen's `data-nav` (the edge labels remain the triggers); the "wireframe
  path" field becomes "screen file" (`design/screens/<stem>.html`).
- **`design_assets` / `design_export_tokens`**: inventory tiers updated
  (screens primary; flows as `.json` sources + derived `.mmd`); token
  export is unaffected.
- **`design_critique`**: screen critique renders the HTML screen (the
  primary artifact); flow critique renders the derived graph. The
  rubric vocabulary is unchanged.
- **Prompts** (`design-system` skill, `designer` persona, the SP-140-2
  tool docs): the loop becomes *brief → tokens → screens (HTML) → steps
  (flow `.json`)*; "flows are derived, never hand-authored" is a
  standing rule next to "design/ is the semantic source of truth".

## §9e — Migration and risk

- **Existing tree**: the dogfood workspace on this branch has SVG
  wireframes (the SP-140 fixtures). One-time conversion, agent-authored:
  each wireframe stem becomes an HTML screen (content preserved — layout,
  labels, token refs); flows become `.json` steps + regenerated `.mmd`.
  The feature is pre-release and single-consumer, so a hard cutover (warn
  window = one release, then reject) is acceptable; no long-lived
  compat shim beyond the validator's deprecation window.
- **Risks**: (1) *Prompt/validator drift* — the skill, persona, and
  validator must change in the same items that change the formats (one
  commit per item, per the SP-140-4/5 commit discipline). (2) *Sidecar
  invalidation* — node-id stability in the generator is a test-pinned
  contract, not an assumption. (3) *Test churn* — the design tier's
  fixtures (validator, sync, brief, critique) all touch the formats; the
  items order fixture migration before the cutover so no item ships
  against a half-migrated tree. (4) *`.mmd` consumers* — GitHub/mermaid-
  live-editor rendering of the derived file is unchanged (it is still a
  valid `.mmd`); the umbrella AC "every artifact opens in a non-sprout
  tool" holds.

## Items

- **9.1** Screen data-attribute contract + validator rules (the §9a
  schema; screens primary; wireframe tier → `deprecated` warn).
- **9.2** Flow source `.json` schema + deterministic `.mmd` generator
  (provenance hash, stable node ids) + validator drift check.
  > **Progress (2026-09-25):** Shipped. v1 schema per §9b's example (linear
  > steps with triggers; `condition`/parallel branches deferred). Generator
  > (pkg/design/flowsource*.go): steps render with their step id as the node
  > id, off-path data-nav-reachable screens join keyed by stem (§9c id
  > stability); the derived .mmd carries a `flow-source-hash: fnv1a64` header
  > over the .json + touched screens (token-export fold, documented input
  > order). Validator: drift = error, header-less .mmd = transitional info
  > (`flow_mmd_legacy`), off-path data-nav cross-check = warn scoped to
  > .json-bearing trees; the node-stem and §4b bidirectionality rules accept
  > §9b step ids as legitimate node ids. ParseFlowchart grew |label| edge
  > parsing (design_brief/DesignView can read derived flows). Export target
  > `flows` is explicit-only, never in `all`. The dogfood tree's 4 legacy
  > flows carry the transitional notice (9.4 converts them).
- **9.3** Canvas renders the derived graph (mermaid reuse; sidecar
  contract pinned by test).
- **9.4** Migration: convert the branch's SVG wireframes → HTML screens
  and `.mmd` flows → `.json` steps (agent-authored, content-preserving);
  wireframe tier warn → error.
  > **Progress (2026-09-26):** Shipped. All 11 dogfood wireframes are gone
  > (4 stems kept their existing kit screens; 7 converted as desktop kit
  > copies carrying the wireframes' content and §9a data-nav contracts);
  > all 4 flows carry .json sources with derived .mmd exports (agent-turn
  > as a non-screen flow: screen optional in the v1 schema); the README
  > manifest and generated index regenerated. The wireframe-tier severity
  > flipped info → error (the tier is removed; presence is the error) and
  > every rule that walked the wireframe universe now walks screens —
  > bidirectionality, orphan inventory, README refs (screens only, no
  > "(or wireframes)" hedging). The dogfood tree validates 0 findings.
  > flow_mmd_legacy stays info for external trees' transitional state.
- **9.5** Ripples: `design_sync` structural deltas (screen+step
  proposals, no `.mmd` writes), `design_brief`, `design_assets`.
- **9.6** Prompt updates: `design-system` skill + `designer` persona +
  tool docs (screens-first loop; flows derived).
  > **Progress (2026-09-26):** Shipped. Skill workflow is
  > brief → tokens → screens → flow sources → regenerate exports, with the
  > wireframe charter replaced by the removal statement (convert, never
  > restore) and the flows step teaching the .json schema + targets:flows;
  > designer persona's charter rebuilt the same way; pin tests on both
  > (TestDesignSystemSkillFormatRework, TestReadEmbeddedPromptFile_DesignerPrompt).
- **9.7** `design_critique`: HTML-screen render path; flow critique on
  the derived graph.
- **9.8** Test suite: generator determinism pins, sidecar-stability
  pin, validator drift cases, migrated fixtures; chunk pins hold.

## Acceptance criteria

- [x] Every screen in a produced tree is `design/screens/<stem>.html`;
      no `design/wireframes/` remains (icons/brand SVGs unaffected).
      (Dogfood tree migrated 2026-09-26: 11/11 screens, wireframes/ gone;
      presence of a wireframe is a hard error.)
- [x] No `.mmd` in the tree is hand-authored: each carries a
      `source-hash` provenance header, and `design_validate` recomputes
      it (drift = error). Editing a `.mmd` by hand is detectable and
      rejected the same way a dirty token alias graph is. (flow-source-hash
      + flow_mmd_drift; external trees' header-less .mmd carry the
      transitional flow_mmd_legacy info.)
- [x] A dev turn that adds a route produces, via `design_sync`, a
      proposal for a new screen + step — never a `.mmd` edit. (design_sync's
      structural deltas propose screens/flow sources; the derived .mmd is
      never a sync write target — regenerating it is the export's job.)
- [x] The canvas renders the derived graph; a drag still writes only the
      layout sidecar; an existing sidecar survives a regeneration
      (node-id stability pinned by test). (flowsource_render_test.go:
      derived .mmd parses as ordinary mermaid; node ids stable across a
      regenerating edit; bytes deterministic.)
- [x] The `design-system` skill, the `designer` persona, and the
      validator agree on the formats (one spec as the single source; a
      prompt/validator contradiction is a review blocker). (Pinned:
      TestDesignSystemSkillFormatRework + the designer prompt's
      post-9.4-formats block in TestReadEmbeddedPromptFile_DesignerPrompt.)
- [x] The umbrella end-to-end ACs still hold: a fresh workspace goes
      prompt → validated tree (tokens, screens, flow sources) with the
      DesignView showing the graph and screens, using only open formats.
      (umbrella_e2e_test.go green over the wireframe-free fixtures.)
