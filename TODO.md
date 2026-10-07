# TODO

Active work for the host-contract lane (branch `feat/host-contract`): SP-160
phases 1, 3 and 4 — the host contract (§160b), then the composition package
(§160a, §160e), then design tokens (§160d). Every `[ ]` item below is in
scope for this lane. Each item is a small, independently committable unit
for the workflow automation (~30 min – 2 h) and cites its spec section —
read `roadmap/SP-160-integration-api.md` (the cited section and Phases)
before starting.
Completed work lives in git history; finished sections are removed.

Validation gate for every item: `make vet && make fmt-check && make lint &&
make lint-go-new && make build-all`, plus `cd webui && npm run typecheck`,
`npx prettier --check` on changed files and only the specific vitest files
the item touched (`npx vitest run <file>`).

**This machine runs other automation in parallel; keep tests light.** Never
run the full Go or vitest suites, never start browsers or containers, and
run one test command at a time.

**The hosted build must keep working.** The platform still ships today's
cloud build of this web UI. Every item preserves current behavior in both
builds: the local build runs on `localHost`, the cloud build on a
`cloudHost` that reproduces what `isCloud`/`appMode` do today. No platform
code changes here, and no platform-specific logic is added to sprout.

Items are ordered so nothing precedes what it depends on. Work that needs a
human decision is listed under **Not automatable** at the end, without
checkboxes.

---

## SP-160 §160b — Host contract (`roadmap/SP-160-integration-api.md`)

- [x] **host.1** `SproutHost` interface in `webui/src/host/` (`types.ts`):
      the areas in §160b's table — identity, entitlements (generic usage
      summary: remaining share, label, link target, out-of-usage action),
      transport (backend base URL, WebSocket URL, auth mode, model endpoint
      for the in-browser agent), navigation (intents `account`, `usage`,
      `help`, `signOut`, plus deep links into a project/space), notifications
      (sink and optional count), chrome slots, theme, and capabilities
      (explicit flags: terminal, server git, MCP, local models, verification,
      automations, agent changes, and every flag `config/mode.ts` derives
      today). Add `HostProvider` and `useHost()`, and mount the provider at
      the app root. Doc comments on every field. Vitest for the provider.
      Spec: SP-160 §160b.
- [x] **host.2** `localHost` and `cloudHost` (`webui/src/host/`):
      `localHost` = no account, local backend at the current origin, all
      local capabilities on. `cloudHost` reproduces today's hosted behavior
      from the existing bootstrap and adapters (`bootstrapAdapter.ts`,
      `services/cloudAdapter.ts`, `config/runtimeConfig.ts`). The entry
      point picks the host once at startup (the cloud entry passes
      `cloudHost` explicitly); nothing else reads the build flag. Vitest for
      both hosts' capability sets matching today's `config/mode.ts` values.
      Spec: SP-160 §160b.
- [x] **host.3** Capabilities replace the module-level flags: every
      `supports*` binding in `config/mode.ts` and its call sites read
      `useHost().capabilities` (or a non-React accessor for services). No
      behavior change in either build. Vitest for the touched components.
      Spec: SP-160 §160b.
- [x] **host.4** Convert `isCloud`/`appMode` branches in the workspace UI
      (sidebar sections, `SettingsPanel.tsx`, `StatusBar.tsx`,
      `WorkspaceBar.tsx`, `Terminal.tsx`, security and escalation hooks) to
      capabilities. List the full set first with
      `grep -rlE '\bisCloud\b|appMode' webui/src` (37 modules today) and
      record in the commit which capability each branch became. Split into
      a second commit if the diff grows past ~600 lines.
      Spec: SP-160 §160b.
- [x] **host.5** Convert the remaining `isCloud`/`appMode` branches in
      startup and transport (`useAppInitialization.ts`, `websocket.ts`,
      `WasmLoadingOverlay.tsx`, `serviceWorkerRegistration.ts`,
      `useCloudSessionPersistence.ts`, `runtimeConfig.ts`) to the host's
      transport and capabilities. Spec: SP-160 §160b.
- [x] **host.6** Move host-specific product UI behind the contract:
      `CreditsChip.tsx` renders the host's entitlements summary;
      platform links in `layered/HomeNav.tsx`, `ProjectRail.tsx`,
      `LayeredSidebar.tsx` and `UserMenu.tsx` become navigation intents or
      chrome slots; `services/platformGitHub.ts`,
      `PlatformGitHubAccountCard.tsx` and `utils/platformUrl.ts` move into
      `cloudHost` (supplied as a chrome slot or entitlement), so no module
      outside `webui/src/host/` names the platform's billing, team, runner
      or account pages. `layered/PlatformHome.tsx` (the iframe) stays for
      now but is reached only through `cloudHost`. Vitest.
      Spec: SP-160 §160b (Rules).
- [x] **host.7** Notifications and theme: the web UI posts its
      notifications to `host.notifications` (`localHost` keeps today's
      in-app toasts and center) and shows the host's count where a host
      provides one; the theme follows `host.theme` live (token values or a
      theme name), replacing any DOM observation. Vitest.
      Spec: SP-160 §160b, §160d.
- [x] **host.8** Guard: remove the `isCloud` and `appMode` exports, and add
      a check (ESLint `no-restricted-imports`/`no-restricted-syntax` or a
      vitest that scans `webui/src`) that fails if any module outside
      `webui/src/host/` reads the build mode, `appMode`, or names a host.
      Spec: SP-160 Acceptance criteria 1.
- [x] **host.9** Example host (test only): extend
      `webui/src/views/ExampleEmbedding.tsx` (or a sibling under
      `webui/src/host/example/`) into a minimal host with its own chrome and
      theme that mounts two spaces, receives a notification and a
      navigation intent, and imports only the public entry points
      (`webui/src/views/index.ts` and `webui/src/host/index.ts`), enforced by
      the test. Spec: SP-160 Acceptance criteria 3.
- [x] **host.10** Documentation: `docs/integration/host-contract.md` — the
      interface field by field, `localHost`, how a host supplies entitlements,
      navigation intents, chrome slots, theme and capabilities, and a table
      of the former `isCloud` branches and what each became. Link it from
      SP-160. Spec: SP-160 §160b.

## SP-160 §160a, §160e — Composition package (`roadmap/SP-160-integration-api.md`)

Starts after host.9. The package is the only hosted artifact (§160e); the
standalone local build keeps embedding into `pkg/webui/static`.

- [ ] **ws.1** Package scaffold `packages/workspace` (`@sprout-foundry/workspace`):
      Vite library build (ESM, code-split, type declarations), `exports`
      for the entry points, `publishConfig` for GitHub Packages like
      `packages/design`, version kept equal to the sprout release. Wired
      into the npm workspaces and `make build-all`. A test checks the build
      emits the entry, types and no unexpected files. Spec: SP-160 §160a.
- [ ] **ws.2** `SproutProviders`: one wrapper for the provider stack the
      views need (extracted from the app root), exported from the package;
      the local app uses it. Vitest. Spec: SP-160 §160a.
- [ ] **ws.3** `SproutWorkspace`: mounts one project's workspace with props
      `project`, `space` (from the SP-155 registry), `host` (§160b),
      optional `layout` (SP-155 arrangement) and `onSpaceChange`; exported
      with the registered spaces and the individual views
      (`webui/src/views/index.ts`). Vitest. Spec: SP-160 §160a.
- [ ] **ws.4** Lazy loading: the editor, the WASM agent and space-specific
      code load when a space opens, not on import. A test inspects the
      build manifest and fails if the entry chunk pulls in the editor or
      WASM loader. Spec: SP-160 Acceptance criteria 5.
- [ ] **ws.5** Styles as a separate stylesheet in the package that consumes
      the design tokens; no global CSS leaks onto the host page outside the
      workspace root. Spec: SP-160 §160a.
- [ ] **ws.6** Content-hashed WASM (§160e): `sprout.wasm` and
      `wasm_exec.js` are emitted with hashed names and referenced through
      the package, in both the package and the cloud build; the local embed
      keeps working. A test fails if the WASM URL does not change when its
      content changes. Spec: SP-160 §160e, Acceptance criteria 6.
- [ ] **ws.7** The local web UI becomes a thin app: its shell renders around
      `SproutWorkspace` with `localHost` and `SproutProviders`, importing
      only the package's public entry points; anything missing is added to
      the package exports, not imported privately. No feature loss.
      Spec: SP-160 Acceptance criteria 2.
- [ ] **ws.8** Publish workflow: a GitHub Actions job publishes
      `@sprout-foundry/workspace` and `@sprout-foundry/design` to GitHub Packages on
      release tags (production build). Workflow file and
      `docs/integration/workspace-package.md` (install, `.npmrc`, mounting
      with a host) only; nothing is published from this lane.
      Spec: SP-160 §160e.

## SP-160 §160d — Design tokens

- [ ] **tok.1** Move the web UI's tokens from `webui/src/App.css` into
      `packages/design` (`@sprout-foundry/design`); theme packs layer on
      top; the web UI imports them from the package. No visual change: a
      test checks every token the UI uses is defined by the package.
      Spec: SP-160 §160d.
- [ ] **tok.2** `host.theme` token values map onto the package's token
      names (documented in `docs/integration/host-contract.md`); a host
      theme overrides tokens only inside the workspace root. Vitest.
      Spec: SP-160 §160d.

## Not automatable

- First publish of `@sprout-foundry/workspace`: owner sets up the GitHub Packages
  token and cuts the release that runs the ws.8 workflow.
- The platform's `SproutHost` implementation (platform `SP-SHELL.md` stage
  1) starts once this branch is merged and the package is published.
- Merge order with the main sprout lane (`wip/local-preview`), which also
  edits the web UI: merge this branch after the current main-lane web UI
  items commit; expect conflicts in sidebar and settings components.
