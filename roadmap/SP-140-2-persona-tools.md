# SP-140-2 — Designer Persona, Design Skill, and Agent Design Tools

> **Status (2026-09-15):** Draft — not started.
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-1.

## Problem

The formats exist (SP-140-1) but nothing makes the agent good at them. The
base prompt steers toward code; there is no design workflow knowledge, no
visibility into an existing `design/` tree, no way to render the agent's
own output for critique, and no way to import a sketch or whiteboard photo
as a starting point. Asked to "design a flow," the agent produces whatever
its base instincts suggest, in whatever location it guesses.

## Design

### 2a. `designer` persona — one catalog entry

`pkg/personas/configs/designer.json` following the existing catalog
schema (see `docs/PERSONAS.md` §1, §3):

- **ID:** `designer`. Aliases: `ux`, `design`.
- **Delegatable:** `true` (orchestrator/coordinator can spawn it like
  `coder`/`tester`).
- **System prompt:** `pkg/agent/prompts/subagent_prompts/designer.md` —
  design literacy in sprout's idiom: the directory contract, the
  format charter (DTCG semantics, SVG conventions, mermaid flow
  vocabulary, screen-flow id==stem rule), the validate-then-declare-done
  workflow, and the critique vocabulary (hierarchy, affordance,
  consistency, spacing rhythm, contrast) used by SP-140-4's loop.
- **AllowedTools:** explicit array (the catalog has no default-set
  mechanism — every persona lists its tools): the standard file/shell/
  edit/subagent set plus the design tools (`design_assets`,
  `design_render`, `design_import_sketch`, `design_validate`) and
  `analyze_ui_screenshot`. Listing tools that register later is
  harmless (the allowlist filters the registered roster), so the
  persona can land before the tools. **Not** a minimal allowlist: the
  designer also needs shell (to run renders), edit/write, and
  subagents. The persona specializes behavior; it does not sandbox.
- **Capabilities:** `git_write` — **deliberate**, and required by
  SP-140's git premise: the design↔code loop commits design and code
  together, so the persona who finishes a design iteration must be
  able to commit it like any other contributor. Same two-gate risk
  model as `orchestrator` applies.
- **Provider/Model:** unset (inherits user default). Vision is a runtime
  capability question resolved by SP-137's registry-driven resolver, not
  a persona field.

Conflicts/aliases load-checked by the existing catalog loader
(`pkg/personas/catalog.go`). No new persona machinery — if this spec
needs new persona machinery, the design is wrong.

### 2b. `design-system` skill — the workflow knowledge

`pkg/skills/library/design-system/SKILL.md` (+ supporting files), the
PERSONAS.md §9 sanctioned way to add behavior. Contents:

- The full design workflow: brief → tokens → wireframes → flows →
  screens, with the validate step between each.
- Greenfield scaffold: the exact sequence for bootstrapping a `design/`
  tree from a prompt (README first, then tokens, then wireframes, then
  flows, then screens).
- **Brownfield:** how to inventory an existing tree (`design_assets`), respect
  what exists, and extend it.
- Critique-and-revise loop usage (with SP-140-4 tools).
- Loop awareness: keep tokens authoritative so SP-140-5 export and
  sync stay clean.

The skill is persona-agnostic: any persona can activate it. The designer
persona's prompt tells it to activate the skill on design work.

### 2c. Agent tools

Four `ToolHandler` structs, each one line in `pkg/agent_tools/all.go`.

**`design_assets`** — inventory + context for the `design/` tree.
- Args: optional `path` (subtree), optional `format` filter.
- Output (structured JSON): manifest summary, per-asset rows
  `{path, kind, name, status?, summary?}` parsed from SVG
  titles/README, token group counts, flow node/edge counts, findings
  from `design_validate` (cached, non-blocking).
- Workspace with no `design/` → explicit `{exists: false}` result plus
  scaffold guidance text, so the model never hallucinates a tree.
- WASM/JS variant per the `all_*_wasm.go` pattern (reads via the VFS
  bridge).

**`design_render`** — render a design source to an image for critique.
- Args: `source` (wireframe SVG, screen HTML, flow `.mmd`), optional
  `viewport_width`/`viewport_height`, optional `flow_layout`
  (`dagre` orientation hints applied to a derived SVG — the `.mmd`
  source is never mutated).
- Implementation: for SVG/HTML reuse the browser-adapter render path
  inside `analyze_ui_screenshot_handler.go` (extract a shared helper;
  no duplication). The existing branch is gated on `IsHTMLInput`,
  which matches `.html`/`.htm` only — the shared helper must extend
  input detection (e.g. an `IsRenderableInput` covering `.svg`) or
  take an explicit render-mode argument, so SVG sources render to PNG
  instead of falling through to the raw `image/svg+xml` vision branch.
  Local file rendering passes `allow_file_url: true` in `BrowseOptions`
  (see `browser_adapter.go`'s `buildBrowseOptions`).
- For mermaid: generate a standalone HTML page embedding the mermaid
  script + source, render via the same browser path (this is why
  `flow_layout` lives here — layout is a render-time concern). Mermaid
  script sourcing: pinned vendored copy in `pkg/agent_tools/design/`
  (offline-safe; matches the vendored copy SP-140-3 uses in the webui;
  keep the upstream LICENSE/NOTICE and exempt the vendored bundle from
  the 500-line rule).
- Build tags: `design_render` and `design_import_sketch` depend on the
  host browser/vision tiers — they are `//go:build !js` files with WASM
  stubs mirroring `all_vision.go`/`all_browse_url_wasm.go`, not
  unconditional registrations in `all.go`.
- Output: `ToolResult` with images attached via the SP-137
  images-preserving path, plus a text summary. Critique prompting
  (`analysis_prompt`) passes through to the vision tier.
- Non-vision primary → tool returns the artifact path + guidance to run
  OCR/native fallback per SP-137 tier order; it must not fail.

**`design_import_sketch`** — whiteboard/paper/photo/Figma-export →
`design/` starting point.
- Args: `image_path` (workspace-relative), `target` (`wireframes` |
  `tokens` | `flows`), optional `screen_name`.
- Implementation: pure prompt orchestration — image attaches to the
  vision tier via SP-137; the model extracts structure and writes
  convention-compliant SVG/token/mermaid files with the normal file
  tools. The tool itself is a thin gatekeeper (path validation, Gate-1
  precheck mirroring `analyze_ui_screenshot`'s `PrecheckFileAccess`
  pattern, target conventions in the prompt), not an image pipeline;
  the post-write `design_validate` run is the agent's job, directed by
  the skill (2b) — the tool's output reminds the agent to run it.
- Gate 1 precheck on `image_path` like every file-touching tool.

**`design_validate`** — defined in SP-140-1g; listed here because the
persona prompt and skill direct its use.

### 2d. Prompt guidance

Both embedded prompts (`pkg/agent/prompts/system_prompt.md`,
`system_prompt.lite.md`) get a short static section (not runtime
injection — embedded prompts are fixed; the LLM evaluates the
condition, same shape as the SP-137 image-guidance block at
`system_prompt.md:126`): when the workspace contains `design/`, read
`design/README.md` first, use `design_assets` for inventory, and route
design work to the designer persona or design-system skill. Lite
version kept minimal.

### 2e. Security notes

- All four tools run Gate-1 `PrecheckFileAccess` on workspace paths
  (mirror `analyze_ui_screenshot_handler.go`); the same requirement
  extends to every later design tool that touches paths (SP-140
  invariant 7: critique cache, token export, sync apply, brief).
- `design_render` renders workspace-local files in a headless browser —
  the same trust level as the existing `analyze_ui_screenshot` HTML
  path (already accepted); no network in render beyond mermaid's
  vendored script (none — vendored).
- No new risk classes; nothing here widens the classifier surface beyond
  file reads/writes the agent already does with `write`/`edit`.

## Non-goals

- No Figma-import automation (users export SVG from any tool and use
  `design_import_sketch` / plain file drop).
- No vision plumbing (SP-137's resolver is consumed as-is).
- No WebUI surfaces (SP-140-3).

## Acceptance criteria

- [ ] `designer` persona appears in `/persona list`; `GetSubagentType`
      resolves it and its aliases; catalog conflict tests pass.
- [ ] `orchestrator` can spawn `designer` as a subagent (delegatable
      path exercised in a test).
- [ ] `design_assets` on a fixture tree returns correct inventory JSON;
      on an empty workspace returns `{exists:false}` + scaffold
      guidance.
- [ ] `design_render` on fixture SVG and HTML returns attached images
      through the seed tool-result path (scripted-client round trip,
      the SP-137 Phase-1 pattern); on `.mmd` produces a rendered flow
      image without mutating the source.
- [ ] `design_import_sketch` end-to-end with a scripted vision client:
      photo of a hand-drawn login screen produces a validated
      `design/wireframes/login.svg`.
- [ ] All four tools carry Gate-1 prechecks; negative test each for
      off-workspace denial.
- [ ] WASM variant of `design_assets` registered and covered by the
      tool-roster smoke test (SP-112-9 pattern).
- [ ] `go test ./...`, `make vet && make lint && make build-all` clean;
      prompt files pass the existing prompt-consistency tests.
