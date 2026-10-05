# SP-158 — The Design Loop in the Browser Build

> **Status (2026-10-05):** Proposed.
> Amends SP-140 invariant 7. Related: SP-147 (mode shapes the agent per
> request), SP-143 (screen kit), SP-155 (preview pane, mode registry,
> composable views).

## Problem

The in-browser build (the Go WASM shell behind `cloudWasmHandlers.ts`)
runs the same agent, prompts and `design-system` skill as the daemon, but
the design loop the skill describes cannot run there:

- **Tools.** Only `design_assets` and `design_validate` ship to WASM
  (SP-140 invariant 7). The skill's core loop also needs
  `design_export_tokens` (derived screen CSS and flow `.mmd` are never
  hand-edited), `design_sync` (end every UI-affecting turn) and
  `design_brief`. All three are pure Go — `pkg/design` builds for
  `js/wasm` — and are excluded by policy only.
- **Rendering.** `design_render` and `design_critique` rasterize with
  rod/Chromium, which does not exist in the browser. The browser itself
  can render the screens: they are self-contained HTML + CSS with an
  optional runtime script (SP-143).
- **Images.** The model never sees an image in the browser build:
  - `POST /api/upload/image` is a synthetic error in browser mode.
  - The `platform` provider config sets no `supports_vision`, so the
    agent treats the model as text-only and strips tool-result images.
  - WASM file reads are text only (`pkg/wasmshell` returns
    `string(data)`), so binary files arrive mangled: `read_file` on an
    image, the editor's image viewer, and binary screen references all
    break.
  - `analyze_image_content` and `design_import_sketch` are stubbed out on
    the grounds that they need the separate vision pipeline. Neither
    does: `design_import_sketch` only attaches the image, and the primary
    model is multimodal.
- **Design status.** `handleWasmLocal` has a `/api/design/status` case,
  but the path is not in the cloud endpoint registry, so the request goes
  to the platform and the health strip stays empty.

## Design

### 158a. Ship the pure-Go design tools to WASM

Drop the `!js` build tag and the `_js.go` stubs for `design_brief`,
`design_export_tokens` and `design_sync`. Update the roster test
(`pkg/agent_tools/design_wasm_roster_test.go`) and
`scripts/wasm-tool-roster-smoke.sh`. Writes go through the same VFS as
every other WASM writer; verify revision-checked writes (SP-140-7) with
`baseHash`.

### 158b. Images reach the model

- Binary-safe file reads in the WASM shell (bytes, not `string`), with
  `/api/file` returning the real MIME type in browser mode.
- `platformProviderConfig` declares image support for platform models
  that accept images.
- Chat image upload writes into the VFS (for example
  `.sprout/uploads/`) and returns the path, instead of a synthetic error.
- `analyze_image_content` in WASM attaches the image inline (the
  `buildImageAttachment` path is pure Go) rather than erroring.
- Register `/api/design/status` as a wasm-local endpoint.

### 158c. In-browser rasterization

`design_render` gets a `js` implementation that calls a host bridge,
following the existing `__sproutGitTools` / `__sproutShellGit` pattern:

1. Go (WASM) calls `__sproutRender({source, frame, state})` and awaits
   the promise.
2. The host builds the screen document with the inliner the preview
   already uses (`design/screenRefs.ts`), extended to binary assets as
   `data:` URLs.
3. It loads the document in an offscreen same-origin iframe sized to the
   README `frames:` entry, lets the runtime script run (states, nav), and
   waits for fonts and layout to settle.
4. It rasterizes the settled DOM to a canvas (SVG `foreignObject` or a
   small library such as `html-to-image`) and returns PNG bytes.
5. Go writes the render artifact under `design/.cache/renders/` exactly as
   the native tool does, so caching and findings sidecars are unchanged.

Flows render from the layout the flow canvas already computes (dagre)
to SVG, then to PNG. `mermaid` is already a dependency if a closer match
to the native renderer is wanted.

### 158d. Critique and sketch import on the primary model

- `design_critique` in WASM: render (158c) and attach the PNG to the tool
  result; the primary model writes the critique. The `visual: false`
  static fallback stays for models that do not accept images.
- `design_import_sketch` in WASM: identical to native (attach the image),
  enabled once 158b lands.

### 158e. Skill text matches the host

When a skill is folded into the prompt, append a short note listing any
tool the skill names that is not registered on this host, with the
instruction to skip that step and say so. This keeps every skill honest
on every build without per-host skill copies.

## Acceptance criteria

- [ ] WASM roster: `design_brief`, `design_export_tokens`, `design_sync`
      registered; roster test and smoke script updated.
- [ ] A chat image in browser mode reaches the model as an image (e2e,
      cloud mode).
- [ ] `read_file` and the image viewer show a PNG correctly in browser
      mode.
- [ ] `design_render` in browser mode produces a PNG of a phone-frame
      screen with its states applied (e2e).
- [ ] `design_critique` in browser mode returns findings with
      `visual: true` from the primary model.
- [ ] Design health strip populated in browser mode.
- [ ] Skill fold notes unavailable tools (unit test with a stub
      registry).
- [ ] A Design-mode query in browser mode starts with the design-system
      skill folded (e2e; the per-request mode plumbing already exists).

## Non-goals

- Pixel parity with Chromium renders. The critique judges layout,
  hierarchy and token use; small rasterization differences are fine.
- Network fonts or remote assets in screens (screens are self-contained
  by contract).
- Rendering in a Web Worker. The WASM shell runs on the main thread and
  the bridge needs the DOM.

## Open questions

- Should native builds also critique on the primary model when it
  accepts images, retiring the separate vision-tier call?
- Does the hosted page's CSP allow the inlined `data:` runtime script in
  the preview iframe? Verify before 158c depends on it.
