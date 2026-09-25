# SP-143 — Screen Kit: Interactive, Token-Driven Screens

Status: Shipped v1 (2026-09-25) — items 143.1–143.7 landed; acceptance criteria verified by test/webui/design_kit.spec.ts. v1 scoped a bare runtime; v2 widens to the
full kit (generated utilities, device chrome, states, base templates,
derived index) after scoping discussion. Depends on SP-140-9 §9a (the
screen `data-nav` contract); independent of its migration items.

## Problem

A `design/screens/*.html` today is a static, themeless, deviceless picture:

1. **No theme reach.** `design_export_tokens` emits a deterministic
   `tokens.css` and nothing consumes it. Screens hand-repeat values, so a
   token edit never reaches a screen.
2. **No utilities.** Every screen re-derives its button/spacing/type CSS
   from scratch — big writes (LLM time), drifting results, no shared
   vocabulary.
3. **No device.** A phone screen renders as an unbounded document. Nothing
   frames it at 393×852 with iPhone chrome; desktop is indistinguishable
   from phone.
4. **No flow, no states.** `data-nav` edges do nothing when clicked; a
   screen shows one frozen moment (never its empty/loading/error states).
5. **Dishonest previews.** The webui renders screens as
   `<iframe srcDoc>` (`LivePreview.tsx`) — srcDoc has no base URL, so every
   workspace-relative reference silently breaks in the tool while resolving
   fine in the agent's `file://` render/critique path.

## Premise — the kit: tool-owned assets, one authored artifact per screen

The model authors exactly one thing per screen. Everything else is
generated, derived, or a fixed checked-in asset.

### 1. Generated from project tokens (deterministic, provenance-hashed, never hand-edited)

- `design/generated/tokens.css` — `:root` custom properties **plus a fixed
  vocabulary of utility classes** derived from token groups:
  `color.*` → `.bg-*`/`.text-*`/`.border-*`, `space.*` → `.p-*`/`.m-*`/
  `.gap-*`, `font.*` → `.text-*-{size,weight}`/`.font-*`, `radius.*` →
  `.rounded-*`, `shadow.*` → `.shadow-*`. Known group names map to
  utilities; unknown groups pass through as vars only. Emitted by
  `design_export_tokens` (css target grows the utility layer).
- `design/generated/tokens.json` — resolved token values for JS (new
  `json` export target; same generator, same hash convention).

### 2. Fixed assets, versioned, copied into trees by the skill scaffold

- `design/runtime/sprout-screens.js` — the runtime. Classic script (file://
  CORS null-origin rules out ES modules), no dependencies:
  - `[data-nav]` interception → in-place screen swap (fetch + body/title
    replace, `history.pushState`, back works, no full reload); standalone
    fallback to plain `../screens/<stem>.html` navigation when fetch is
    blocked;
  - states: a screen declares `data-states="a,b,c"` on `<html>`; sections
    carry `data-state="a"`; the runtime toggles via `#state=a` hash and
    renders a state switcher **only in preview mode** (marker injected by
    the webui preview wrapper — static renders stay clean);
  - `window.SproutScreens.nav(stem)` / `.setState(name)` API;
  - `data-sprout-screens` version attribute on `<html>`; `source-hash`
    header comment (hand-edits detectable).
- `design/runtime/chrome.css` — device chrome. `data-device="phone"` on
  `<html>` → iPhone chrome by default (rounded bezel, dynamic island,
  status bar, home indicator) sized by the README `frames:` entry; absent/
  `desktop` → no chrome (desktop is the default layout). Chrome is fixed
  hardware realism, not project-themed (light/dark via `color-scheme`
  only).
- `design/runtime/base/phone.html`, `base/desktop.html` — base documents
  with the reference lines (runtime, tokens.css, chrome.css), the device
  attribute, and a body slot. New screens start from a copy.

### 3. Model-authored, one per screen: `design/screens/<stem>.html`

A full HTML document from a base template. Layout via utilities +
`var(--…)`; navigation as real `<a data-nav="to:<stem>;trigger:<label>">`
elements; states as declared `data-states` + `data-state` sections.
Dynamic by construction (HTML/CSS is unbounded); locked by the validator
contract: slug stems, self-containment with workspace-relative-only refs
(existing §1i), `data-nav` targets exist as stems, states declared before
use, utilities/vars over raw values where a token exists (advisory),
container width matches a declared frame (advisory, existing).

**Format decision — HTML, not JSON screens.** JSON layout needs a bespoke
renderer and a layout schema — the combination where LLM output is
weakest — while HTML+CSS is the densest-trained layout medium there is and
renders with zero new code. Machine-readability does not come from
authoring JSON; it comes from the derived index (below). A JSON screen
format stays out until dogfooding demands it.

### 4. Derived, never hand-edited: `design/generated/screens.json`

The screen graph — per stem: device/frame, declared states, nav edges with
triggers — generated from the screens' data-attributes, provenance-hashed
over the inputs; drift = validator error (the `.mmd`/token convention).
This is the machine-readable index other tooling consumes (`design_brief`,
`design_sync` structural proposals, cross-agent context) without parsing
HTML at read time.

## Items

- **143.1 Export:** utilities in the `css` target + the `json` target
  (`design_export_tokens`; group→utility vocabulary mapping; determinism
  pins; hash headers).
- **143.2 Chrome + base templates** as scaffold assets (`chrome.css`,
  `base/phone.html`, `base/desktop.html`; `data-device` contract; skill
  scaffold copies them).
- **143.3 Runtime v1** (nav/swap/history, states + preview-gated switcher,
  device behavior, API, version/hash stamps).
- **143.4 Preview integration** (`rewriteScreenRefs` → `/api/file` for all
  workspace-relative refs including `../generated/*` and `../runtime/*`;
  preview marker injection; `/api/file` MIME check for `.js`/`.css`;
  vitest on the rewriter).
- **143.5 `screens.json` generator + validator rules** (data-nav target
  existence, states-declared-before-use, runtime hash when present,
  screens.json drift; `design/runtime/*` tree registration).
- **143.6 Generator surface:** SKILL.md + designer persona + tool docs —
  screens start from base templates, utilities-first styling, data-nav
  anchors, declared states; runtime/tokens referenced, never authored.
- **143.7 E2E:** a phone-framed screen pair — themed from tokens, iPhone
  chrome at the declared frame, in-place navigation with back, state
  switching, and a token edit restyling every screen with zero screen
  writes.

## Non-goals

- No component framework, no templating language, no bundler, no ESM.
- No JSON screen renderer in v1.
- No state persistence beyond the URL hash; no form state.
- Device chrome is fixed realism (an iPhone is an iPhone); not themed from
  project tokens.
- The runtime never writes files, never talks to the agent, never leaves
  the document it runs in.
- No changes to the flows canvas or SP-140-9's `.json` flow sources; the
  runtime consumes screen `data-nav` edges only.

## Acceptance criteria

- [x] A phone screen previewed in the workbench shows iPhone chrome at the
      declared frame size, themed entirely from `generated/tokens.css`
      (vars + utilities), with in-place `data-nav` navigation (back works)
      and a working state switcher; no full reloads.
- [x] The same file opened from disk (`file://`) navigates between screens
      via plain links, still themed.
- [x] `design_render`/`design_critique` on a runtime-present screen render
      it identically to a runtime-absent one (runtime inert in static
      renders; states pinnable via URL hash for render/critique targets).
- [x] Editing one token + re-exporting restyles every screen with zero
      screen-file writes.
- [x] `screens.json` regenerates deterministically; a hand-edit (hash
      mismatch) is a validator error; trees without the runtime validate
      clean (missing runtime = info).
- [x] A fresh scaffolded tree includes the runtime assets, base templates,
      and a starter screen wired to tokens with working nav.
