# SP-160 — Integration API: Composition, Host Contract, Backend Contract

> **Status (2026-10-05):** Proposed.
> Related: SP-155 (extension points; this spec completes them), SP-147
> (spaces of one project), SP-112 (platform parity), SP-151 (progress
> events), SP-149 (verified done).

## Problem

Sprout's web UI is the core of what Sprout offers: the full product a user
gets by running `sprout` and opening its web UI. It must stay complete,
standalone and open. Other applications also want to host Sprout's spaces
(design, code, and the rest) inside their own product. Today that only works
by building the whole web UI in a special "cloud" mode, and the seams show:

- **Two switches for one question.** "Am I hosted?" is answered at build time
  (`VITE_SPROUT_MODE`, `webui/src/config/mode.ts`) and again at runtime
  (`appMode` from `/api/bootstrap`, `webui/src/bootstrapAdapter.ts`).
  Capability flags are module-level bindings patched after the adapter loads.
- **Host-specific product code inside Sprout.** About 30 modules branch on
  `isCloud`, and some components know about one particular host's billing,
  team and runner pages, its usage endpoint, and its GitHub account card
  (`CreditsChip.tsx`, `layered/HomeNav.tsx`, `services/platformGitHub.ts`,
  `utils/platformUrl.ts`).
- **No way to compose.** `webui/package.json` is private with no library
  build or `exports`, so a host cannot mount individual spaces or views.
  It can only take the whole app, and then nests its own pages inside it
  (`layered/PlatformHome.tsx` renders host pages in an iframe).
- **An undocumented backend contract.** In hosted mode the UI's `/api/*`
  calls are split between the in-browser WASM agent, browser-local stores,
  canned responses and the host (`services/cloudAdapter.ts`,
  `services/cloudEndpointRegistry/`). A host that serves part of that API
  has to imitate the daemon's behavior by hand, with nothing to check it
  against.
- **Build drift.** Hosted builds are produced several ways with different
  base paths and modes (`scripts/build-webui-dist.mjs` builds the release
  bundle at base `/`; hosts build at their mount path), and the WASM files
  are served with long-lived caching but are not content-hashed.

## Goal

Sprout exposes a clean, documented **integration API** so any host can build
around Sprout's spaces cleanly, visually and in data, without reaching into
internals. Sprout's own local web UI is built on that same API, which keeps
the API complete and keeps the local product first-class.

Three parts: the **composition API** (what a host mounts), the **host
contract** (what a host provides), and the **backend contract** (the API the
spaces talk to, versioned and conformance-tested).

## Design

### 160a. Composition API — `@sprout/workspace`

A versioned package that exports the spaces and the pieces of the workspace:

- `SproutWorkspace` — mounts one project's workspace: `project`, `space`
  (one of the registered spaces), `host` (160b), optional `layout`
  (SP-155 arrangement) and `onSpaceChange`.
- The registered spaces (design, code, and later ones) through the SP-155
  registry, plus the individual views (`ChatView`, changes, files, editor,
  preview, design views) for hosts that want their own arrangement.
- Providers and hooks the views need, behind one `SproutProviders` wrapper,
  so a host does not assemble the provider stack by hand.
- Styles as a separate stylesheet that consumes the design tokens (160d).

Packaging:

- Library build (ESM, code-split, type declarations), published to GitHub
  Packages alongside `@sprout-foundry/design`.
- Heavy parts load lazily: the editor, the WASM agent and space-specific
  code load when a space opens, not when the package is imported.
- Sprout's own local web UI becomes a thin app that renders its shell around
  `SproutWorkspace` with the local host. Anything the local UI needs that the
  package does not export is a gap in the API, not a private shortcut.

### 160b. Host contract — `SproutHost`

The only channel between Sprout and whatever hosts it. A typed interface the
host passes in; Sprout never infers its host from build flags or URLs.

| Area | The host provides |
|---|---|
| Identity | Current user (id, display name, avatar) or none |
| Entitlements | A usage summary to display (generic: remaining share, label, link target) and what to do when usage runs out |
| Transport | Where backend calls go (160c), WebSocket URL, auth mode, and the model endpoint for the in-browser agent |
| Navigation | Outward links Sprout can request by intent (`account`, `usage`, `help`, `signOut`), each resolved by the host; deep links back into a project/space |
| Notifications | A sink Sprout posts to, and an optional count the host displays |
| Chrome slots | Optional React nodes the host supplies for Sprout's own header areas (e.g. account menu), or "host renders chrome" to hide them |
| Theme | Token values or a theme name; Sprout follows them live |
| Capabilities | Explicit flags for what this host supports (terminal, server git, MCP, local models, verification, automations, …) |

Rules:

- Sprout contains no host-specific product logic. Billing, team, runner and
  account concepts arrive only as host-provided entitlements, navigation
  intents and chrome slots.
- `isCloud` and `appMode` are replaced by capabilities. Every current
  `isCloud` branch maps to a capability or moves to the host.
- The local web UI ships `localHost`: no account, local files, local
  backend, all local capabilities on.

### 160c. Backend contract

The API the spaces talk to, written down and versioned:

- An OpenAPI description of the HTTP API, generated from code: handlers are
  registered as Huma operations (`github.com/danielgtaylor/huma/v2`, the
  `humago` adapter on the existing `ServeMux`) with typed input and output,
  and `cmd/genapi` writes the document. Never hand-written, so it cannot
  drift from the handlers. Streaming and WebSocket endpoints that do not fit
  are listed explicitly with a reason.
- A schema for WebSocket events (the `@sprout/events` types become the
  source for the event schema).
- Each endpoint family is marked by where it may be served from: a Sprout
  daemon, the in-browser WASM agent, browser-local storage, or the host.
- A conformance test suite, published with the package, that any
  implementation runs: the local daemon, the WASM agent, and any host that
  serves part of the API. Drift fails the suite instead of failing users.
- Version negotiation in bootstrap: the host reports the contract version it
  implements; the workspace refuses to start on an incompatible major
  version with a clear message.

### 160d. Design tokens

- One token package (`@sprout-foundry/design`, `packages/design`) is the
  source for the web UI and for hosts. The web UI's tokens in
  `webui/src/App.css` move into it; theme packs layer on top.
- Hosts theme Sprout through 160b's theme field, never by copying CSS or
  observing DOM changes.

### 160e. Build and delivery

- One hosted artifact: the `@sprout/workspace` package, built once per
  release in production mode. Hosts consume the package; they do not build
  the web UI from source.
- WASM and other large assets are content-hashed and referenced through the
  package, so immutable caching is safe and an upgrade never mixes old WASM
  with new code.
- The standalone local build keeps its current embed path
  (`pkg/webui/static`).

## Phases

1. **Host contract and capabilities** (160b): define `SproutHost`, add
   `localHost`, convert `isCloud` branches to capabilities, move
   host-specific UI behind entitlements, navigation intents and chrome
   slots. No packaging change yet.
2. **Backend contract** (160c): document the API and events, write the
   conformance suite, run it against the daemon and the WASM agent.
3. **Composition package** (160a, 160e): library build with lazy loading and
   content-hashed assets; the local web UI consumes it; publish.
4. **Tokens** (160d): one token package for the web UI and hosts.

Phases 1 and 2 can proceed in parallel; 3 needs 1.

## Acceptance criteria

- [ ] No module in the web UI branches on `isCloud`, `appMode` or a host
      name; a test fails if one is added.
- [ ] The local web UI runs entirely through `SproutWorkspace` + `localHost`
      with no loss of features.
- [ ] A minimal example host (in-repo, test only) mounts two spaces with
      its own chrome and theme, receives a notification and a navigation
      intent, and never imports a web UI internal.
- [ ] The conformance suite passes against the daemon and the WASM agent; a
      deliberately broken endpoint fails it.
- [ ] Importing `@sprout/workspace` loads no editor or WASM code until a
      space opens.
- [ ] Upgrading the package changes the WASM asset URLs.

## Non-goals

- No change to what the local Sprout product offers; it stays complete.
- No host-specific code in Sprout, including for any first-party host.
- No iframe-based embedding.

## Open questions

- Contract version policy: semver on the package, or a separate contract
  version reported in bootstrap (or both).
- Which views are public API at 1.0 versus internal.
- Whether the conformance suite also runs in the browser against the WASM
  agent, or through a Go harness.
