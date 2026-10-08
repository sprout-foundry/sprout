# TODO

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
- [ ] **deps.2** Keep them patched: the starters CI workflow
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

- Resolved 2026-10-07: starter frameworks, local storage emulation and the
  Pages/Workers rule (see "SP-153 reference starters" above).
- **Decision + accounts:** running the benchmark on real models and
  publishing results — needs API keys and spend; also decide where
  results live and the run cadence (SP-154 open questions).
- **Accounts:** live Cloudflare end-to-end deploy of each reference
  starter (preview, then confirmed production) — needs a real Cloudflare
  account and token (SP-156 acceptance).
