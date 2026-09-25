# SP-143 — Screen Runtime: Interactive Screens with Tokens and Flows

Status: Draft (2026-09-25). Depends on SP-140-9 §9a (screens-first tier, screen
`data-nav`); the preview-fix item (1.1) is independently shippable today.

## Problem

A `design/screens/*.html` today is a static picture. It cannot:

1. **Consume the theme.** `design_export_tokens` deterministically emits
   `design/generated/tokens.css` (CSS custom properties, provenance-hashed) —
   and nothing references it. Screens hand-repeat hex values, so a token edit
   never reaches a screen and the "one theme" premise leaks at the last mile.
2. **Flow.** The only interactive contract is `data-nav` (on SVG wireframes
   today; screens per SP-140-9 §9a). Clicking one in a rendered screen does
   nothing — multiscreen "does this flow feel right" checks mean opening files
   one at a time.
3. **Preview honestly.** The webui renders screens as
   `<iframe srcDoc={content}>` (`webui/src/components/LivePreview.tsx`). srcDoc
   has **no base URL**, so every workspace-relative reference in a screen —
   `<link href="../generated/tokens.css">`, `<script src="../runtime/…">` —
   silently breaks in the tool that is supposed to show the design. (The
   agent-side render/critique path loads the file via `file://`
   (`render_input.go`), where relative refs DO resolve — the two paths
   disagree about what a screen looks like.)

The result: the LLM pays for big self-contained screen writes (every screen
inlines everything), the previews can't show theme or flow, and "does it feel
real" is unanswerable without hand-assembly. The difference between this
tooling being usable or not is a small shared runtime plus honest previews —
screens stay one small write each.

## Premise: one small runtime, referenced — never authored — per screen

- **The runtime is an asset, not an output.** One checked-in,
  provenance-hashed `design/runtime/sprout-screens.js` per tree. Screens
  reference it; no screen embeds it. The LLM never writes runtime code.
- **Tokens by reference.** Screens declare
  `<link rel="stylesheet" href="../generated/tokens.css">` and style with
  `var(--…)`. One token write restyles every screen (the export's
  determinism makes this safe to regenerate).
- **Flows are anchors.** Screen-to-screen edges are real
  `<a data-nav="to:<stem>;trigger:<label>">` elements (the §9a contract).
  The runtime intercepts them; standalone file opens degrade to plain
  navigation (`../screens/<stem>.html` — works under `file://`).
- **Preview composes; files stay self-contained-ish.** The screens
  convention (SP-140-1 §1i) already permits workspace-relative refs and bans
  network refs — that rule is exactly right. The **preview path** rewrites
  relative refs to absolute `/api/file?path=…` URLs before srcDoc (the
  iframe sandbox is `allow-scripts allow-same-origin`, so runtime fetches of
  sibling screens resolve same-origin). The file stays valid on disk and
  under `file://`; only the in-memory preview copy is rewritten.
- **No framework, no build step.** One classic-script file (~a few hundred
  lines), no dependencies, no bundler. ES modules are blocked under
  `file://` (CORS null-origin), so classic script it is.

## §1 — Items

- **1.1 Preview ref rewriting (webui, independently shippable).** A pure
  `rewriteScreenRefs(html, screenPath)` helper: workspace-relative
  `href`/`src` (and CSS `url(...)`) → absolute `/api/file?path=<workspace
  path>`; absolute/data: refs untouched. Applied in `ScreensTabContainer`
  and the `ScreenWorkbenchContainer` render facet before `LivePreview`.
  Verify `/api/file` serves `.js` with an executable MIME (nosniff would
  block the runtime tag — if so, teach the file endpoint content-types, not
  the screens). Vitest: relative link/script/style/img rewritten, external
  and data: untouched, `design/`-rooted and workspace-rooted path forms both
  handled.
- **1.2 The runtime asset.** `design/runtime/sprout-screens.js`, committed
  to the skill's scaffold and copied into trees by the design-system skill
  (like README scaffolding today). v1 surface:
  - auto-boot on DOMContentLoaded;
  - `[data-nav]` click interception → swap to the target screen in place:
    fetch (preview/same-origin) with standalone fallback
    (`location.href = ../screens/<stem>.html`) when fetch is blocked;
  - swap = replace `<body>` children + `<title>`, `history.pushState`,
    back button works, no full reload (that is the "feels real" part);
  - `window.SproutScreens.nav(stem)` programmatic API;
  - a version banner in a `data-sprout-screens` attribute on `<html>`
    (debuggable, zero UI).
  Provenance: `source-hash` header comment (the `.mmd`/token-export
  convention) so hand-edits are detectable.
- **1.3 Validator + tree registration.** `pkg/design/tree.go` recognizes
  `design/runtime/*.js`; `design_validate` checks the provenance hash when
  the runtime is present (missing runtime = info, not error — old trees keep
  validating). `design_assets` inventory lists the tier.
- **1.4 Token wiring convention.** Skill + validator advisory: a screen
  styling with raw hex where a token exists → advisory `info` finding
  (the brand.md rule, extended to screens, advisory-only per the
  findings-never-block rule). Screens reference
  `../generated/tokens.css`; `design_export_tokens`'s css target becomes
  part of the screens loop (regenerate on token change — the design-ahead
  drift row already says this).
- **1.5 Generator-surface updates (the fix-where-it's-generated rule).**
  `design-system` SKILL.md screens section + `designer` persona + tool docs:
  screens carry the two reference lines (runtime + tokens), `var(--…)`
  styling, real `data-nav` anchors; the runtime is referenced, never
  written; flows across screens are wired in markup, not prose.
- **1.6 E2E.** A screen pair with `data-nav` edges: in the workbench
  preview, a click swaps screens in place (no navigation), back returns;
  token edit → export → screens restyle without a screen write.

## §2 — Non-goals

- No component framework, no templating language, no build tooling. If a
  screen needs more than markup + CSS vars + data-nav, that is a signal the
  design is specifying an app, not a screen.
- No state management beyond history (v1 has no form state, no variants; a
  `data-variant` toggle is a later addition if dogfooding asks for it).
- No changes to the flows canvas, `design_brief`, or SP-140-9's `.json`
  flow sources — the runtime consumes §9a `data-nav` edges only.
- The runtime never writes files, never talks to the agent, never leaves the
  preview/document it is loaded in.

## §3 — Acceptance criteria

- [ ] The webui workbench preview of a screen shows themed styling from
      `generated/tokens.css` and clicking a `data-nav` anchor swaps to the
      target screen in place, with working back navigation.
- [ ] The same screen file opened directly from disk (file://) navigates
      between screens via plain links, styled by the same tokens.
- [ ] `design_render`/`design_critique` on a screen are unchanged (file://
      path already resolves relative refs) and a runtime-present screen
      renders identically with the runtime inert (no boot side effects in a
      static render).
- [ ] Editing one token + re-exporting restyles every screen with zero
      screen-file writes.
- [ ] `design_validate` flags a hand-edited runtime (hash mismatch) as an
      error and a missing runtime as info; old trees validate clean.
- [ ] A fresh scaffolded tree (design-system skill) includes the runtime
      asset and its two reference lines in the starter screen.
