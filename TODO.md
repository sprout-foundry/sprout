# TODO

Active work for the host-contract lane (branch `feat/host-contract`). Each
item is a small, independently committable unit for the workflow automation
(~30 min – 2 h) and cites its spec section — read
`roadmap/SP-160-integration-api.md` §160b (and Phases, 1) before starting.
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

- [ ] **host.1** `SproutHost` interface in `webui/src/host/` (`types.ts`):
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
- [ ] **host.2** `localHost` and `cloudHost` (`webui/src/host/`):
      `localHost` = no account, local backend at the current origin, all
      local capabilities on. `cloudHost` reproduces today's hosted behavior
      from the existing bootstrap and adapters (`bootstrapAdapter.ts`,
      `services/cloudAdapter.ts`, `config/runtimeConfig.ts`). The entry
      point picks the host once at startup (the cloud entry passes
      `cloudHost` explicitly); nothing else reads the build flag. Vitest for
      both hosts' capability sets matching today's `config/mode.ts` values.
      Spec: SP-160 §160b.
- [ ] **host.3** Capabilities replace the module-level flags: every
      `supports*` binding in `config/mode.ts` and its call sites read
      `useHost().capabilities` (or a non-React accessor for services). No
      behavior change in either build. Vitest for the touched components.
      Spec: SP-160 §160b.
- [ ] **host.4** Convert `isCloud`/`appMode` branches in the workspace UI
      (sidebar sections, `SettingsPanel.tsx`, `StatusBar.tsx`,
      `WorkspaceBar.tsx`, `Terminal.tsx`, security and escalation hooks) to
      capabilities. List the full set first with
      `grep -rlE '\bisCloud\b|appMode' webui/src` (37 modules today) and
      record in the commit which capability each branch became. Split into
      a second commit if the diff grows past ~600 lines.
      Spec: SP-160 §160b.
- [ ] **host.5** Convert the remaining `isCloud`/`appMode` branches in
      startup and transport (`useAppInitialization.ts`, `websocket.ts`,
      `WasmLoadingOverlay.tsx`, `serviceWorkerRegistration.ts`,
      `useCloudSessionPersistence.ts`, `runtimeConfig.ts`) to the host's
      transport and capabilities. Spec: SP-160 §160b.
- [ ] **host.6** Move host-specific product UI behind the contract:
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
- [ ] **host.7** Notifications and theme: the web UI posts its
      notifications to `host.notifications` (`localHost` keeps today's
      in-app toasts and center) and shows the host's count where a host
      provides one; the theme follows `host.theme` live (token values or a
      theme name), replacing any DOM observation. Vitest.
      Spec: SP-160 §160b, §160d.
- [ ] **host.8** Guard: remove the `isCloud` and `appMode` exports, and add
      a check (ESLint `no-restricted-imports`/`no-restricted-syntax` or a
      vitest that scans `webui/src`) that fails if any module outside
      `webui/src/host/` reads the build mode, `appMode`, or names a host.
      Spec: SP-160 Acceptance criteria 1.
- [ ] **host.9** Example host (test only): extend
      `webui/src/views/ExampleEmbedding.tsx` (or a sibling under
      `webui/src/host/example/`) into a minimal host with its own chrome and
      theme that mounts two spaces, receives a notification and a
      navigation intent, and imports only the public entry points
      (`webui/src/views/index.ts` and `webui/src/host/index.ts`), enforced by
      the test. Spec: SP-160 Acceptance criteria 3.
- [ ] **host.10** Documentation: `docs/integration/host-contract.md` — the
      interface field by field, `localHost`, how a host supplies entitlements,
      navigation intents, chrome slots, theme and capabilities, and a table
      of the former `isCloud` branches and what each became. Link it from
      SP-160. Spec: SP-160 §160b.

## Not automatable

- Phase 3 (§160a composition package, §160e build and delivery) follows
  this lane: library build, lazy loading, content-hashed WASM, publishing
  `@sprout/workspace` to GitHub Packages. Queue after host.9 lands.
- The platform's `SproutHost` implementation (platform `SP-SHELL.md` stage
  1) starts once host.1–host.6 are merged and published.
- Merge order with the main sprout lane (`wip/local-preview`), which also
  edits the web UI: merge this branch after the current main-lane web UI
  items commit; expect conflicts in sidebar and settings components.
