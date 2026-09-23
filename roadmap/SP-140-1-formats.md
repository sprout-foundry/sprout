# SP-140-1 — Design Format Charter: DTCG Tokens, SVG Wireframes, Mermaid Flows

> **Status (2026-09-22):** Shipped — merged to `main` via `fe93ae98a`
> (feat-design-workspace, released in v0.18.12).
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on nothing.

## Problem

Nothing in sprout defines where design assets live or what formats they
take. A model asked for "design tokens" improvises a path and schema; two
runs produce incompatible trees; nothing validates any of it. Before any
persona, canvas, or sync work can be reliable, the formats must be
fixed, open, and validated.

Format selection criteria (from SP-140's charter): text, git-diffable,
readable/writable by common tools, standard or de-facto standard, no
vendor lock-in — and per SP-140's git premise, chosen so a PR diff is a
meaningful design review. The selections:

| Asset | Format | Standard body / ecosystem |
|-------|--------|--------------------------|
| Design tokens | **W3C Design Tokens Community Group format** (`.tokens.json`, `$value`/`$type`) | W3C DTCG; read by Style Dictionary, Tokens Studio |
| Wireframes | **SVG** (with sprout naming/attribute conventions) | W3C; renders in browsers, Figma, Illustrator, Penpot |
| Screen designs | **HTML + CSS** (self-contained) | W3C; universally renderable |
| Flows (nav / user / process) | **Mermaid** (`flowchart`, `.mmd`) | Mermaid; renders natively on GitHub, in Markdown |
| Brand | **Markdown + SVG** (`brand.md`, logos) | — |
| Icons | **SVG** + optional `sprite.svg` symbol sheet | W3C |

## Design

### 1a. Tokens — W3C DTCG

Location `design/tokens/*.tokens.json`. Requirements:

- Top-level structure is groups; leaf nodes carry `$value` and `$type`
  (`color`, `dimension`, `fontFamily`, `fontWeight`, `number`,
  `duration`, `cubicBezier`, `strokeStyle`, `border`). Unknown `$type`
  values are a validator error, not a silent pass.
- Alias references use `{group.token}` dot paths; the validator must
  resolve every reference and reject cycles and dangling paths.
- One file per tier (`color`, `typography`, `spacing`, `sizing`,
  `motion`) plus any project tiers. No `index.json` aggregation —
  consumers glob.
- `$extensions` is allowed and passed through untouched (projects may
  carry tool-specific metadata there; sprout never requires any).

The `webui` design-token rule (no raw hex in CSS) is the in-repo precedent
that DTCG maps to real constraints; SP-140-5 generates CSS variables from
these files.

### 1b. Wireframes — SVG conventions

Location `design/wireframes/<screen-name>.svg`. Conventions (validator-
enforced; they exist so SP-140-3 can render nodes and SP-140-4 can check
consistency):

- Root element: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 W H">`
  with integer `W H` matching a device frame declared in
  `design/README.md` (see §1e's machine-parseable `frame:` lines).
- Self-contained: no `<script>`; no external resource references —
  embedded rasters must be data URIs. External `href`/`src` is an error.
- Text stays `<text>` (not outlined paths) so it remains greppable,
  diffable, and accessible.
- Interactive elements (buttons, links, inputs) carry stable `id`
  attributes; click targets that navigate carry
  `data-nav="<screen-name>"`, where the value is another wireframe's file
  stem. This is the machine-checkable screen→screen edge.
- One screen per file; the file stem is the canonical screen name
  (`^[a-z0-9]+(-[a-z0-9]+)*$`).

Validator enforcement levels: well-formedness, viewBox presence,
self-containment, slug rule, and `data-nav` targets are hard checks;
frame matching, `<text>` usage, and stable ids on interactive elements
are advisory (`info`) — the validator cannot reliably parse intent from
SVG structure.

### 1c. Flows — mermaid

Location `design/flows/<flow-name>.mmd`. One `flowchart` per file.

- **Screen flows:** node ids are wireframe file stems. The flow and the
  wireframes are two views of one system; `design_validate` (1g) and
  SP-140-4's consistency checks rely on the id == stem rule.
- **Process/user flows** not tied to screens use free-form ids; nothing
  cross-checks them.
- Edge labels carry trigger semantics (`-- "tap Submit" -->`).
- Mermaid is the source of truth. SVG/PNG exports are derived artifacts
  (SP-140 invariant 2). The webui renders mermaid client-side (SP-140-3);
  no CLI renderer is required.

### 1d. Brand

`design/brand/brand.md` — name, voice, palette **references into tokens**
(`{color.brand.primary}`, never raw hex), logo usage rules, do/don't
list. Logo files alongside as SVG. The no-raw-hex rule in brand.md is
what keeps the token file authoritative.

### 1e. README manifest

`design/README.md` — human- and agent-readable inventory: device frames
in use, per-screen one-line purpose, per-flow purpose, status markers
(`draft` / `review` / `ready`), and links. This is the file the agent
reads first when `design/` exists (SP-140-2d wires the prompt guidance).
Keep it plain Markdown with a light suggested shape; the validator checks
links resolve, not prose. Device frames are declared with
machine-parseable fenced lines so the validator and SP-140-3's
device-frame-aware sizing can read them:

```
frames:
  desktop: 1440x900
  mobile: 390x844
```

### 1f. Icons

`design/icons/*.svg`, plus optional `design/icons/sprite.svg` composed of
`<symbol id="icon-name">` entries. Same self-containment rules as
wireframes. Icon names share the screen-name slug rule.

### 1g. Validator tool — `design_validate`

New `ToolHandler` in `pkg/agent_tools` (one struct + one line in
`all.go`), runnable with no args (whole tree) or a path. Checks:

- Tokens: JSON parse, `$type` membership, alias resolution (dangling,
  cyclic), file-tier glob sanity.
- SVGs (wireframes + icons + logos): XML well-formedness, viewBox
  present, self-containment (no script/external refs), name slug rule,
  `data-nav` targets exist as wireframe stems.
- Mermaid: parse (`mermaid.parse` equivalent — Go-side parser for the
  `flowchart` subset; full syntax fidelity is not required, edge/node
  extraction is), node-id == wireframe-stem rule for screen flows.
- Screens (`design/screens/*.html`): self-containment (no `<script>`
  pulling network resources; inline `<style>` or workspace-relative
  CSS only), slug naming shared with wireframes, advisory check that
  the root container width matches a declared device frame.
- README: relative links resolve to real files; `frames:` block parses
  (name → `WxH`); wireframe viewBox frame-matching runs against these
  declarations (advisory `info`).

Output: structured per-file findings `{file, line?, severity, message,
rule}` — not prose. Exit semantics match other agent tools (`ToolResult`
with findings; `IsError` only on tool failure, not on findings).

Deliberately advisory: findings never block an agent turn; the skill and
persona prompt direct the agent to run the validator and fix findings
before declaring a design done.

### 1h. Git contract for the design tree

The tree is versioned by the workspace repository — no side channel
(SP-140 invariant 6). Concrete consequences this spec owns:

- **`.gitattributes`** (created by the scaffold flow): `design/**/*.svg
  diff=html` for readable markup diffs; `.tokens.json` and `.mmd` are
  plain text (default diff is already line-based — good).
- **Ignore policy:** `design/.cache/` (render PNGs, critique scratch) is
  gitignored; `design/generated/` is *not* auto-ignored — committing
  generated artifacts is the project's choice, and the provenance-hash
  headers make either choice consistent (SP-140-5c). The scaffold
  writes the `.cache/` line only.
- **Binary hygiene:** the self-containment rules (1b, 1f) double as
  git hygiene — data-URI rasters inside otherwise-text SVGs keep
  diffs reviewable; a validator `warn` fires when a data URI exceeds a
  size threshold, pointing at the "move it to `brand/` and reference
  it" escape hatch.

Ownership: the validator's no-args run on a workspace lacking
`.gitattributes` (or lacking the `diff=html` line) emits a `fix`
finding with the exact line to add; the agent (via the design-system
skill, SP-140-2b) applies it. The scaffold **appends** to an existing
`.gitattributes` — user workspaces already carry text/binary rules
that must not be clobbered.

## 1i. Screens — HTML + CSS

Location `design/screens/<screen-name>.html`, one self-contained file
per screen (file stem shares the wireframe slug rule). Conventions:

- Inline `<style>` or workspace-relative `<link>` CSS only; no
  `<script>` pulling network resources (vendored/local scripts allowed
  for derived artifacts only). No external font/CDN references.
- The root container's width should match a declared device frame
  (advisory check).
- Semantic HTML; text stays text (same greppability rationale as
  wireframes).
- These are the sources SP-140-2's `design_render` renders and
  SP-140-3's Screens tab previews via `LivePreview`.

## Non-goals

- No token *transformation* here (CSS/TS generation is SP-140-5).
- No mermaid rendering in Go (client-side rendering is SP-140-3).
- No theme/multi-brand token resolution (one active brand per workspace
  in v1; `$extensions` carries anything fancier).
- No WebUI surface (DesignView is SP-140-3).

## Acceptance criteria

- [ ] `TestDesignAssetConventions` covers a full fixture `design/` tree:
      valid tree passes with zero findings.
- [ ] Seeded-bad fixtures each produce the right finding class: broken
      alias reference, cyclic aliases, unknown `$type`, SVG missing
      viewBox, SVG with `<script>`, SVG with external href, `data-nav`
      to a nonexistent screen, mermaid node id with no matching
      wireframe, README link to a missing file.
- [ ] `design_validate` registered in `pkg/agent_tools/all.go`; callable
      with no args on a workspace with `design/`; returns structured
      findings JSON.
- [ ] `make vet && make fmt-check && make lint && make build-all` and
      `go test ./...` clean.
- [ ] No proprietary product names appear in the design-tier code:
      grep-enforced test (mirroring SP-137's
      `TestVisionTierNoProviderNames` pattern) with a hardcoded word
      list (`figma`, `penpot`, `sketch`, `illustrator`, `adobe`,
      `photoshop`) scanned over the new design-tier Go files and webui
      design components — not docs, specs, or fixtures.
