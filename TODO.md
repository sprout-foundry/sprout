# TODO

## Hosted chat in `SproutWorkspace` — the chat view has no data

A host that mounts `SproutWorkspace` with a `layout` (the views path) gets a
chat view with no messages and no send handler: the chat state lives only in
the standalone app (`App.tsx` / `AppContent.tsx`), `SproutWorkspace` renders
`ViewsLayout` with no per-view props, and nothing reads `host.transport`
beyond `authMode`. ChatView now renders an empty, disabled chat instead of
crashing (92d8883db); these items make it work. Spec: SP-160 §160a/§160b
(composition API and host transport) — update the spec and
`docs/integration/host-contract.md` as each item lands.

Decided by the owner: the agent behind a hosted chat is a workspace daemon
when the host has one for the project (full tools; the daemon is the source
of truth), else the in-browser WASM agent; chat sessions are persisted by the
host when it offers a session store. Order matters — each item builds on the
one above. Validation gate as for the host-contract lane (package build +
`docs/__tests__/workspace-package.test.js`, type-check, prettier on changed
files, the vitest files the item touched; any `.go` change → the full gate).

- [x] **hc.1** Extract the chat state into the package. Move the chat slice
      of `AppStore`, `useWebSocketEventHandler` (+ `hooks/wsHandlers/*`),
      `useChatSessionManager`, the queue ops and the `chatProps` /
      `reviewProps` / `diffState` assembly (`AppContent.tsx` around the
      `chatProps` useMemo) into one reusable unit — e.g.
      `WorkspaceChatProvider` + `useWorkspaceChat()` — that needs only a
      fetch function and an events provider. The standalone app uses it
      (no behaviour change: existing chat vitest and webui e2e chat specs
      stay green). Export it from `./views` with declarations. Tests: the
      provider driven by a fake fetch + fake events provider produces
      messages from delivered events and calls `/api/query` on send.
- [x] **hc.2** `SproutWorkspace` mounts the chat. In `"own"` mode it mounts
      the hc.1 provider and passes `ViewsLayout` `props` for `chat` (and
      `changes`), so a host's layout gets a working chat with no extra
      wiring. Add an optional `viewProps` prop (per-kind overrides merged
      over the defaults) for hosts that compose their own. Retire the
      `EMPTY_SHELL_PROPS` chat stub where the provider replaces it. Test:
      an own-mode `SproutWorkspace` with `DEFAULT_VIEWS_ARRANGEMENT` renders
      an ENABLED chat and a send reaches the fake backend.
- [x] **hc.3** Host-driven transport. In own mode, register the host for the
      module-level services (the `setActiveHost` the standalone entry does
      in `index.tsx`), and make `clientFetch` and the WebSocket URL use
      `host.transport.apiBaseURL` / `wsURL` when set (today only
      `SPROUT_PROXY_BASE` / `VITE_WS_URL`); connect the events provider and
      subscribe the hc.1 handler (today only `useAppInitialization` does).
      Document "one workspace per page" (module singletons). Tests: fetch and
      WS go to the host's URLs; the local build is unchanged.
- [x] **hc.4** Agent backend selection in the host contract. Extend
      `HostTransport` with an explicit agent backend: `{ kind: 'daemon',
      apiBaseURL, wsURL }` (a sprout daemon reachable through the host —
      preferred) or `{ kind: 'wasm', modelEndpoint }` (the in-browser agent).
      For `wasm`, install the cloud adapter from the host (model endpoint
      from `transport.modelEndpoint`, not the hard-coded value in
      `services/platformProvider.ts`), route agent events through
      `setAgentEventDispatcher` → `deliverLocal` (as `useAppInitialization`
      does for the hosted build), and import the repository from
      `project.repoUrl` instead of the `?repo=` URL parameter. A host may
      switch backends when a daemon becomes available (re-mount is fine).
      Contract doc + SP-160 §160b updated. Tests for both kinds.
- [x] **hc.5** Host session store. When the host advertises a chat session
      store (a capability, e.g. `capabilities.chatSessions`), the chat
      session list / create / rename / delete / switch / messages calls go
      to the host (same request shapes as the daemon's `/api/chat-sessions*`
      endpoints, at `transport.apiBaseURL`), and finished turns are appended
      to it — for both the daemon and WASM backends — instead of browser
      storage. Without the capability, behaviour is unchanged
      (`cloudChatSessions` / daemon). Document the endpoint shapes in
      `docs/integration/host-contract.md` so a host can implement them.
      Tests: sessions round-trip through a fake host store; a reload
      restores the transcript.
- [x] **hc.6** Release prep: bump `packages/workspace` to 1.1.0, changelog
      entry listing hc.1–hc.5 and the contract additions, and confirm the
      consumer test (`docs/__tests__/workspace-consumer.test.mjs`) passes
      with the new exports. Do NOT tag — tagging `workspace-v1.1.0`
      publishes and is the owner's step (see Not automatable).

## Report a bug

- [x] **bug.1** A visible "Report a bug" button that opens a new issue on
      `github.com/sprout-foundry/sprout`:
      - **Where:** always reachable — the help/user menu and the status bar
        (or the equivalent spot in the layered/builder UIs where the menu bar
        is hidden), plus the existing menu command `report_issue` in
        `webui/src/components/MenuBar.tsx`, which today opens the old
        `github.com/alantheprice/sprout` repo with a blank issue — fix it.
      - **Prefill** (`/issues/new?title=…&body=…&labels=bug`): a short
        template — what happened / what you expected / steps — plus
        environment: sprout version (from the build/bootstrap, not a
        literal), mode (local daemon, in-browser, desktop/studio, hosted),
        OS and browser. Never include file paths, workspace names, session
        ids, provider keys, prompts or model output (public repo hygiene);
        keep the URL under GitHub's length limit.
      - **One source of truth:** the repo URL lives in one constant (also
        used by `sprout bug` below and any docs links); grep for and fix
        every other `alantheprice/sprout` URL in the web UI, CLI and docs.
      - **Hosts:** expose it through the host contract — a `reportBug`
        navigation intent; `localHost` opens the GitHub issue; a host may
        resolve it to its own support flow (the platform keeps its support
        tickets), and the button uses the intent.
      - **CLI:** `sprout bug` opens the same prefilled URL in the browser (and
        prints it), with version/OS filled in.
      - Fix the "About" dialog to show the real version.
      Vitest for the URL builder (prefill, no sensitive fields, length cap)
      and the button; Go test for `sprout bug`'s URL.

## Workers deploys with data bindings

- [x] **prov.1** `sprout deploy` to Cloudflare Workers must bring the
      project's declared bindings with it. Today the Workers adapter
      (`pkg/deploy/cloudflare_workers.go`) uploads the script and ignores
      `wrangler.toml`, so the `web-app-data` starter deploys without its D1
      database and its API fails at runtime — and the starter's
      `database_id` is a placeholder (`00000000-…`) that points at nothing.
      - **Create what is declared, reuse what exists:** for each binding the
        project's `wrangler.toml` declares (`[[d1_databases]]`, and
        `[[kv_namespaces]]` / `[[r2_buckets]]` only when present), find or
        create the resource in the user's account and attach it to the
        uploaded script. A placeholder D1 id means "create it"; write the
        real id back into `wrangler.toml` so local and deployed config agree.
      - **Safe to rerun:** a second deploy reuses the resources the first one
        created instead of making duplicates, and two different projects
        never share one by accident (name resources from the worker name
        plus a stable suffix recorded in the project, not the display name
        alone).
      - **All or nothing:** if any declared binding cannot be created or
        attached, the deploy fails with a plain message naming it; it never
        goes live with some bindings missing. Apply pending D1 migrations
        (`drizzle/migrations`) before switching traffic, or say clearly that
        they are not applied.
      - Uses the existing deploy credential (`CLOUDFLARE_API_TOKEN` and the
        account id); the token is never passed as an argument or printed.
      - Tests against the fake Cloudflare API in `pkg/deploy/fake.go`; update
        `docs/CLI_REFERENCE.md` and the `web-app-data` README's deploy section.

## CI flake

- [x] **flake.1** `TestDrainAndWait` (`pkg/utils/token_bucket_test.go:391`)
      failed on the macOS runner with "Expected wait ~200ms, got 351ms": the
      150–300ms window bounds scheduler latency, not the contract. Keep the
      lower bound (it proves the wait happened) and replace the tight upper
      bound with a generous one (e.g. 2s) or loop per
      `docs/internal/test-flakiness.md`; scan `pkg/utils` and `pkg/agent`
      for other single-shot wall-clock upper bounds under ~1s and fix the
      same way. `go test -count=20 -run TestDrainAndWait ./pkg/utils/`
      passes under `-p 4` load.

## Design tokens (for the platform look-and-feel work)

- [x] **design.1** Fix and extend `@sprout-foundry/design`
      (`packages/design/tokens.css`): `--brand-frost` means a light cyan in
      dark mode but a green (#1ba03d) in light mode — give each brand token
      one meaning in both themes (rename or add a token for the green, and
      update every use in `webui/` and `packages/`); add motion tokens
      (`--duration-fast` ~120ms, `--duration-base` ~180ms,
      `--duration-slow` ~320ms alongside the existing `--ease-*`) and a
      `prefers-reduced-motion` block that zeroes them; document them in the
      package README. Bump the package version (1.1.0) — publishing is the
      owner's `design-v1.1.0` tag. Tests: a token check that each brand
      token resolves in both themes and the web UI builds.

## Starter dependency security (before builders use the starters)

GitHub Dependabot reports ~100 open alerts in the starter lockfiles
(`pkg/starters/data/{static-site,web-app,web-app-data}/package-lock.json`),
13 critical: vitest (RCE via the UI server / malicious site), tinypool
(prototype pollution → RCE), astro (RCE via AVIF image optimization). Every
starter is copied into each new builder project, so these ship to users.

- [x] **deps.1** Patch the starters: bump every starter's direct
      dependencies to current releases that resolve the open Dependabot
      alerts (`gh api repos/sprout-foundry/sprout/dependabot/alerts` filtered
      to `pkg/starters/data/`), regenerate each lockfile with `npm install`,
      keep exact pins where the starter pins exactly (web-app-data's caret
      ranges become exact pins too, so a fresh copy is reproducible), and
      run `npm audit --audit-level=high` clean in each starter (document any
      advisory with no fix and why it does not apply). Then run
      `scripts/ci-check-starters.sh` with the built binary: every starter
      builds, passes its tests and serves every manifest route. Update the
      stack skills if a major bump changes conventions. Bump each starter's
      version in its manifest.
- [x] **deps.2** Keep them patched: the starters CI workflow
      (`.github/workflows/starters.yml`) runs `npm audit --audit-level=high`
      per starter and fails on high/critical; add a `.github/dependabot.yml`
      entry per starter directory (npm, weekly, grouped) so updates arrive
      as PRs. actionlint clean.

## Host-contract lane — publish blockers for `@sprout-foundry/workspace`

Branch `feat/host-contract` (worktree `../sprout-host`). Read
`roadmap/SP-160-integration-api.md` §160a/§160e and
`docs/integration/workspace-package.md`. Every `[ ]` item in this section is
in scope for this lane. Validation gate: no `.go` change → `make lint`,
`cd webui && npm run type-check`, `make build-workspace-package` (package
build + `docs/__tests__/workspace-package.test.js`), prettier on changed
files, and only the vitest files the item touched; any `.go` change → the
full gate `make vet && make fmt-check && make lint && make lint-go-new &&
make build-all`.

- [x] **ws.9** Expose the composition API: the package's `exports` map has
      only `.` (host contract), `./styles.css` and `./wasm/`, so a host
      cannot import `SproutWorkspace`, `SproutProviders` or the space
      registry (they exist only in `dist/views.js` / `dist/providers.js`
      with no declarations). Add `./views` and `./providers` subpath
      exports (keep `.` small — lazy loading, ws.4) with self-contained
      declarations rolled up across the views and providers graphs (the
      API Extractor approach ws.7c uses for the host entry), update
      `docs/integration/workspace-package.md` imports, and add a
      consumer test to `docs/__tests__/workspace-package.test.js` (or a
      script it runs): `npm pack`, install the tarball into a scratch
      TypeScript + Vite React 18 app outside the repo, import
      `SproutWorkspace`, `SproutProviders` and the host types from their
      public paths, and require `tsc --noEmit` and `vite build` to pass
      and the built app to contain a single React copy.
      Spec: SP-160 §160a, Acceptance criteria 3 and 5.
      Fixed: the package exposes `./views` and `./providers` subpath
      exports (each `types` + ESM `import`, no `require`), and the
      declaration rollup (`scripts/bundle-dts.mjs`) now rolls one
      API Extractor pass per entry (`.`, `./views`, `./providers`),
      inlining the bundled workspace and non-peer third-party type
      graphs so `dist/views.d.ts` and `dist/providers.d.ts` reference
      only `react`. `.` stays small — the entry's lazy-loading graph is
      unchanged. `docs/integration/workspace-package.md` documents the
      three entry points and their imports, and
      `docs/__tests__/workspace-consumer.test.mjs` (run from the artifact
      test) packs the tarball, installs it into a scratch Vite React 18
      app outside the repo, and requires `tsc --noEmit` and `vite build`
      to pass with a single React copy.
- [x] **ws.10** Package size: the 62 MB WASM ships twice (hashed and a
      fixed-name fallback, `emit-wasm-assets.mjs`), 132 MB unpacked. Ship
      only the content-hashed WASM and `wasm_exec.js` in the package
      (referenced through the manifest); keep the fixed-name copies only
      where the local embed (`pkg/webui/static`) needs them. Artifact test
      asserts one WASM file in the package and a size budget.
      Spec: SP-160 §160e.
      Fixed: `packages/workspace/scripts/emit-wasm-assets.mjs` now emits
      only the content-hashed `sprout.<hash>.wasm`, `wasm_exec.<hash>.js`
      and `wasm-manifest.json` into `dist/wasm/` — the fixed-name
      duplicates are gone (dist/wasm/ 118 MB → 59 MB). The
      cloud/standalone build path is untouched, so the local embed
      (`pkg/webui/static`, `webui/public/wasm`) still ships the fixed-name
      copies, and the loader already resolves the hashed name from the
      manifest. Pinned by the package emit test and the artifact test
      (exactly one WASM binary and one `wasm_exec`, plus a single-copy
      size budget derived from the emitted binary).


Active work tracked here. Each item is a small, independently committable
unit for the workflow automation (~30 min – 2 h). Every item cites its spec
section — read the cited spec before starting. Completed work lives in git
history; finished sections are removed.

Validation gate for every item: `make vet && make fmt-check && make lint &&
make lint-go-new && make build-all`, plus bounded tests for the packages the
item touched (`go test -p 2 ./<pkg>/...`, never a bare `go test ./...`).
Webui items additionally: `cd webui && npx prettier --check` on changed files
and `make test-webui-vitest`.

Items are ordered so nothing precedes what it depends on. Work that needs a
human decision or external accounts is listed under **Not automatable** at
the end, without checkboxes.

---

## Agent audit trail

A host that runs the agent for its users (a workspace daemon, `sprout
runner`, the in-browser agent) needs a complete record of what the agent
did and what it sent to which model provider, and the CLI's own security
audit log should actually exist. Facts only — provider, model, digests,
tool calls — never prompt content in these records. Validation gate as in
the header (Go changes → full gate).

- [ ] **au.1** Instantiate the security audit log. `NewAuditLogger` /
      `SetAuditLogger` are only called from tests, so every `LogJSON` site
      is a no-op and `sprout audit tail` reads a file nothing writes. Create
      the logger at agent start (path from config, default under the state
      dir, mode 0600), and test that a denied shell command lands in the
      file.
- [ ] **au.2** Trace mode hygiene. `--trace-dataset-dir` writes full prompts
      and raw responses unredacted with mode 0644 (`pkg/trace/jsonl.go`).
      Write owner-only (0600 files, 0700 dirs) and run the same redaction
      as the egress backstop (`secretdetect`) over what it writes, unless
      the user passes an explicit `--trace-unredacted`. Tests.
- [ ] **au.3** Per-call audit events. For every model call the agent makes,
      emit an event: time, chat/session id, provider, model, endpoint host,
      request and response SHA-256 and byte counts, tokens, outcome, and
      the trigger (user turn, tool-call follow-up, subagent). Tool calls get
      the same treatment (tool, args digest, result status, files touched).
      Record locally in the audit log (au.1); when a host configures an
      audit endpoint (daemon / runner / in-browser agent), also send them
      there in batches, retrying without blocking the turn. Document the
      event shape in `docs/integration/host-contract.md`. Tests: one event
      per call including failovers; no prompt text in any event.

## Benchmark and CLI fixes (found in the first real benchmark run)

A one-task run (static-site/add-about-section, ai-worker/qwen3.8-27b)
recorded "fail" with `Result: null`, `Err: null` and an empty failure
section: the turn-end verification never ran. Reproduced by hand, the same
model made the correct edit to `src/pages/about.astro`, so the harness, not
the model, lost the result.

- [x] **bench.8** Benchmark runs always verify: find why the run's turn
      reported no changed application paths (the verification gate stayed
      closed) — check whether edits made through shell commands, or by a
      subagent, are missing from the turn's change window
      (`Agent.TurnChangedPaths`) and whether `ProcessQueryWithContinuityAs`
      opens the window in the benchmark path. Fix the root cause; as a
      backstop the runner initializes a git baseline in each fresh copy
      and, when the tracker reports no changes but `git status` shows
      changed application paths, runs verification anyway. Tests: a
      scripted run editing through the edit tool, through a shell
      command, and through a subagent all produce a verification result.
      Spec: SP-154 §154b, SP-149.
- [x] **bench.9** Report the reason, never a silent fail: a run without a
      verification result records why (the hook's not-verified reason:
      no code changes, verification disabled, setup error, timeout) and
      the report lists it under failure categories. Test: the empty
      "Failure categories: No failures" next to a failed run can no
      longer happen. Spec: SP-154 §154c.
- [x] **bench.10** Keep evidence for failed runs: save each failed run's
      working copy diff (`git diff` against the baseline), the agent
      transcript and the verification output under
      `<output>/runs/<task>-<model>-<n>/` (`--keep-runs=failed|all|none`,
      default `failed`). Spec: SP-154 §154c.
- [x] **cli.1** `sprout agent` must not silently hand a turn to a daemon of
      a different binary or config: today, when a daemon is already
      running (e.g. the web UI's backend), the CLI forwards the turn to it
      ("Running via daemon at …/agent.sock") even when the invoked binary
      is a different version and `--isolated-config` names another
      config. Run in-process when the daemon's version or config differs
      from the invoking binary's (or refuse with a clear message and a
      `--no-daemon` flag), and always print which binary and config ran
      the turn. Tests for version mismatch and isolated config.

## Tool robustness

- [x] **tool.1** `read_file` range requests that miss: a coordinator called
      `read_file` on a ~1000-line `TODO.md` 14 times in a row, each time
      getting the same middle-truncated output while asking for "lines
      615-680", until it fell back to `sed -n`. The tool takes the range as
      `view_range: [start, end]` and silently ignores any other argument
      name. Fix: reject unknown arguments with an error that names the
      valid ones (`path`, `view_range`), or accept the common aliases
      (`start_line`/`end_line`, `offset`/`limit`, `line_start`/`line_end`)
      and map them; when output is truncated, the notice states the total
      line count and the exact `view_range` to read the omitted part. Add a
      repeat guard: the same `read_file` call with identical arguments a
      third time in one turn returns a short note ("identical to your last
      read — use view_range [a, b] for lines a-b") instead of the content
      again. Tests for each.

## Not automatable

- Publish `@sprout-foundry/workspace` 1.1.0 (hosted chat, hc.1–hc.6): push
  `main`, then `git tag workspace-v1.1.0 -m "workspace v1.1.0"` and push the
  tag; the publish workflow does the rest.

- Resolved 2026-10-07: starter frameworks, local storage emulation and the
  Pages/Workers rule (see "SP-153 reference starters" above).
- **Decision + accounts:** running the benchmark on real models and
  publishing results — needs API keys and spend; also decide where
  results live and the run cadence (SP-154 open questions).
- **Accounts:** live Cloudflare end-to-end deploy of each reference
  starter (preview, then confirmed production) — needs a real Cloudflare
  account and token (SP-156 acceptance).
