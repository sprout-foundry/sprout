# TODO

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

## Commit message generation (bug fixes)

- [x] **commit.1** Commit tool: generate a real message when `message` is
      omitted. Today `pkg/agent_tools/commit_handler.go` commits `notes`
      verbatim (or the literal "Auto-commit"), while the parameter
      descriptions promise an auto-generated message. When `message` is
      empty, generate a Conventional Commit message from the staged diff,
      with `notes` as context, reusing the `sprout commit` generator
      (`pkg/agent_commands/commit_flow.go`, `commit_helpers.go`). If
      generation fails, return an error and commit nothing; never fall back
      to "Auto-commit". Update both parameter descriptions to match.
      Tests (stubbed generator): notes-only produces a conventional subject
      of at most 72 characters; `message` wins over `notes`; generator
      failure commits nothing.
- [x] **commit.2** Commit provider fallback: `GetCommitProvider()`
      (`pkg/configuration/config_commit_review.go`) returns "" when
      `commit_provider` is unset, contradicting the `CommitProvider` field
      comment in `pkg/configuration/config.go` ("defaults to
      LastUsedProvider"). Under `--skip-prompt` (no interactive selection)
      `sprout commit` then logs an empty provider and model and falls back to
      asking for a message, which aborts. Fall back to the last-used provider
      and its model when unset; keep interactive selection only where a
      prompt is possible. Tests: unset commit provider resolves to the
      last-used provider; explicit `commit_provider` still wins;
      `--skip-prompt` with no provider configured at all returns a clear
      error instead of an empty-message abort.

## SP-148 — Structured Plans (`roadmap/SP-148-structured-plans.md`)

- [x] **148.1** New package `pkg/plancontract`: Go types for the plan
      schema (`version`, `revision`, `created`, `updated`, `goal`,
      `scope[]`, `steps[]`, `design`, `starter`, `acceptance[]` with
      `kind` = build | test | page | interaction | manual,
      `out_of_scope[]`) and a validator. Table tests with fixtures: a
      valid plan passes; duplicate IDs, an acceptance item without a
      kind, an unknown kind, and a scope item with no acceptance item each
      fail with a clear message. Spec: SP-148 §148a.
- [x] **148.2** Plan store: read/write `.sprout/plan.json` through the
      validator; every write bumps `revision` and `updated` and
      regenerates `.sprout/plan.md` from the JSON. Tests: round trip,
      revision bump, markdown regenerated on edit, invalid write rejected.
      Spec: SP-148 §148a, §148b.
- [x] **148.3** Interaction acceptance items reuse the browse step format
      (`webcontent.BrowseStep`; parsing compatible with `parseBrowseSteps`
      in `pkg/agent/tool_handlers_browse.go`). Validator rejects malformed
      steps. Tests with valid and invalid step fixtures. Spec: SP-148
      §148d.
- [x] **148.4** `sprout plan --structured` (`cmd/plan.go`) writes
      `.sprout/plan.json` plus the rendered markdown; add a schema section
      to `pkg/agent/prompts/planning_prompt.md` requiring at least one
      acceptance item per scope item. Test with a scripted model response
      producing a valid plan file. Spec: SP-148 §148b.
- [x] **148.5** When `.sprout/plan.json` exists, the agent reads it at the
      start of a turn and gets a compact plan summary (goal, scope items
      with status) in context. Test: fixture plan → summary present in the
      turn context; no plan → no change. Spec: SP-148 §148c.
- [x] **148.6** Todo items carry an optional plan scope ID
      (`pkg/agent/tool_handlers_todo.go`), persisted across sessions.
      Test: scripted run over a fixture plan emits todos with scope IDs.
      Spec: SP-148 §148c.
- [x] **148.7** Scope write-back: when scope changes during execution, the
      agent updates the plan through the store (new revision) instead of
      diverging. Test: scripted scope addition → plan revision increments
      and markdown updates. Spec: SP-148 §148c.

---

## SP-153 — Starters and Stack Skills (`roadmap/SP-153-starters-and-stack-skills.md`)

- [x] **153.1** `.sprout/starter.json` schema and validator (starter ID and
      version, `build`, `test`, `dev`, `preview` commands, dev port,
      routes, build output directory). Table tests for valid and invalid
      manifests. Spec: SP-153 §153a.
- [x] **153.2** Manifest loader API (`LoadStarterManifest(projectRoot)`)
      as the single source of build/test/dev/preview commands; missing
      file → none, never guessed. Tests. Spec: SP-153 §153a.
- [x] **153.3** Starter embedding and instantiation: embedded, versioned
      starter tree layout; instantiate into an empty directory (refuse a
      non-empty one); a minimal test-only fixture starter proves the
      mechanism. Tests. Spec: SP-153 §153b.
- [x] **153.4** `sprout new --starter <id>` CLI command over 153.3, listing
      available starters on unknown ID. Tests with the fixture starter.
      Spec: SP-153 §153b.
- [x] **153.5** Stack skill auto-activation: when `.sprout/starter.json`
      names a starter, its skill under `pkg/skills/library/<starter>/`
      activates automatically. Test with a fixture skill. Spec: SP-153
      §153c.
- [x] **153.6** Web UI API: `GET /api/starters` (embedded list) and an
      instantiate endpoint behind the existing new-project path. Go tests.
      Spec: SP-153 §153b.
- [x] **153.7** Web UI new-project dialog
      (`webui/src/components/layered/NewProjectDialog.tsx`) offers a
      starter choice from 153.6; empty project stays the default. Vitest.
      Spec: SP-153 §153b.
      Note: the chooser lives in the local new-project flow
      (`WorkspaceGateModal` → `createWorkspaceNative`), not the hosted
      `NewProjectDialog` the item names — the hosted dialog creates a
      GitHub repo (no local directory to instantiate into), while a starter
      is a local template and `POST /api/starters/instantiate` writes a
      local path. Chooser defaults to "Blank" (empty project).
- [x] **153.8** Upgrade proposals: when a project's manifest names an
      older starter version than the embedded one, the agent is told and
      may propose the upgrade using the skill's upgrade note; it never
      applies one silently. Test: version mismatch → proposal notice, no
      file changes. Spec: SP-153 §153d.

---

## SP-152 — Outbound Language Guard (`roadmap/SP-152-language-guard.md`)

- [x] **152.1** New package `pkg/langguard`: prose extraction (strip code
      blocks, inline code, URLs, file paths, quoted user text), Unicode
      script pass, minimum-length threshold. Table tests. Spec: SP-152
      §152a, §152d.
- [x] **152.2** Same-script pass: add the trigram detector (pure Go,
      WASM-compatible; `whatlanggo` unless measurement says otherwise).
      Measure native and WASM binary size before/after and include the
      numbers in the commit notes. Tests across a broad sample of
      languages and scripts. Spec: SP-152 §152d.
- [x] **152.3** User-language resolution: majority language over the
      user's recent messages; short or mixed input falls back to a
      configured language setting; no fixed language list. Tests. Spec:
      SP-152 §152a.
- [x] **152.4** Config: guard on by default everywhere, including the CLI;
      a config setting turns it off. Tests for default and off. Spec:
      SP-152 §152f.
- [x] **152.5** Final-message guard: on mismatch the message is not
      displayed; regenerate once with an explicit language instruction;
      a second mismatch shows a templated notice in the user's language
      with "view original". Scripted-model tests. Spec: SP-152 §152b.
- [x] **152.6** Streaming hold-back: buffer the start of each streamed
      reply until enough prose to judge (code excluded), check, then
      release and stream live. Test: a wrong-language stream never
      reaches the client. Spec: SP-152 §152c.
- [x] **152.7** Completion re-check: a reply that switches language
      mid-stream is checked at completion and replaced (server event +
      web UI handling of the replacement). Go test + vitest. Spec: SP-152
      §152c.
- [x] **152.8** WASM: the guard runs in the browser build's outbound path
      (`cmd/wasm`); WASM build passes and size impact is noted
      (guard adds ≈545 KB / 0.97 % to `sprout.wasm`, 54.7 → 55.2 MB;
      recorded in the spec). Spec: SP-152 §152d.
- [x] **152.9** Metrics: log each mismatch with model ID; per-model
      mismatch rate in diagnostics (role is added in 150.5). Test. The
      guard records a check (and a mismatch) per model in a process-wide
      `LanguageGuardMetrics` recorder; the per-model mismatch rate is
      exposed via `Agent.LanguageGuardStats()` / `GlobalLanguageGuardMetrics()`.
      Spec: SP-152 §152e.

---

## SP-149 — Verified Done (`roadmap/SP-149-verified-done.md`)

- [x] **149.1** Config: verification enabled flag (off by default in the
      CLI; settable per project, globally, or by an embedding
      environment) and repair-attempt limit N with a small default.
      Tests. Spec: SP-149 §149e, §149c.
- [x] **149.2** Check runner for `build` and `test` checks using commands
      only from the starter manifest (153.2) or explicit project config;
      baseline build+test when no plan exists; structured result type
      (checks, pass/fail, output excerpts). Test: a model-proposed
      command has no effect. Spec: SP-149 §149a, §149b.
- [x] **149.3** `page` checks: start the app from the manifest's `dev`
      command and port, open each listed route headless, fail on console
      errors, record screenshot references. Test with a fixture app that
      logs a console error. Spec: SP-149 §149a.
- [x] **149.4** `interaction` checks: run the plan's scripted browser steps
      (148.3) and confirm the expected outcome; `manual` items listed,
      never gated. Tests. Spec: SP-149 §149a.
- [x] **149.5** Turn-end hook: when enabled, after a turn that changed
      application code and before the final reply, run verification;
      a failing gated check is fed back as a structured report and the
      turn continues; stop after N repair attempts on the same check.
      Test: fixture broken build → loop runs → stops at N. Spec: SP-149
      §149c.
- [ ] **149.6** Final-reply contract: success may be reported only with a
      passing result attached; when the stopping rule fires, the reply
      states what passes, what fails and what was tried. Tests. Spec:
      SP-149 §149c, §149d.
- [ ] **149.7** Non-interactive `sprout agent` runs exit non-zero when
      verification is enabled and fails; disabled verification changes no
      behavior. Tests. Spec: SP-149 §149e.

---

## SP-154 — Agent Task Benchmark (`roadmap/SP-154-agent-benchmark.md`)

- [ ] **154.1** Benchmark harness skeleton: task fixture format
      (plain-language request, frozen SP-148 plan, starter reference) and
      loader. Tests with a fixture task on the 153.3 fixture starter.
      Spec: SP-154 §154a.
- [ ] **154.2** Runner: headless non-interactive run per task in a fresh
      starter copy, 3 runs per model; pass/fail comes only from the
      SP-149 result. Test: a scripted model claims success but fails a
      check → recorded as fail. Spec: SP-154 §154a, §154b.
- [ ] **154.3** Per-task metrics: repair attempts, turns, wall time,
      tokens and cost (existing cost tracking), language-guard
      mismatches. Test. Spec: SP-154 §154b.
- [ ] **154.4** Default model list from the provider catalog's
      `recommended_model` entries; configurable override. Test. Spec:
      SP-154 §154b.
- [ ] **154.5** Reports: markdown + JSON comparing models and starters,
      pass rate per starter per model over 3 runs, failure categories.
      Golden-file test. Not part of `go test ./...` (network and cost).
      Spec: SP-154 §154c.

---

## SP-150 — Model Roles (`roadmap/SP-150-model-roles.md`)

- [ ] **150.1** `roles` config section (`planner`, `coder`, `summarizer`,
      `reviewer`, `commit`), parsed and merged across global and project
      config; unset roles fall back to the conversation model. Tests.
      Spec: SP-150 §150a.
- [ ] **150.2** Existing settings (`subagent_model`, `commit_model`,
      review and completion models) read as aliases for their roles. A
      test per alias. Spec: SP-150 §150a.
- [ ] **150.3** `ResolveRole(name)` replaces the per-setting getters
      internally; context-profile resolution (SP-125) runs on the
      resolved model; the provider-name grep test still passes. Tests.
      Spec: SP-150 §150b.
- [ ] **150.4** Plan mode uses the `planner` role, the main loop `coder`;
      `summarizer` is resolvable for SP-151/SP-157. Tests. Spec: SP-150
      §150a.
- [ ] **150.5** Metering: every model call carries its role; usage ledger
      (`pkg/agent/usage_ledger.go`) and cost model record per-role
      tokens and cost; per-role totals in `/cost` views and usage events;
      add role to the 152.9 mismatch metric and the 154.3 metrics.
      Tests. Spec: SP-150 §150c.
- [ ] **150.6** CLI: `/model --role <role> <model>` and `sprout config`
      support for roles. Tests. Spec: SP-150 §150d.
- [ ] **150.7** Web UI settings: role models section, collapsed by
      default. Vitest. Spec: SP-150 §150d.

---

## SP-151 — Progress Events (`roadmap/SP-151-progress-events.md`)

- [ ] **151.1** Event types `progress_milestone`, `progress_question`,
      `progress_verification`, `progress_complete` with run, plan
      revision and scope IDs in `events_types.go`; regenerate the
      `@sprout/events` TypeScript union (`packages/events`). Tests. Spec:
      SP-151 §151a, §151d.
- [ ] **151.2** Emit milestone events as plan scope items start and finish
      (from 148.6 scope IDs), with files-touched count and elapsed time.
      Test with a fixture plan. Spec: SP-151 §151a.
- [ ] **151.3** Emit `progress_verification` from SP-149 results (evidence
      references) and `progress_complete` with the final result or "not
      verified". Test: event order and correlating IDs. Spec: SP-151
      §151a; SP-149 §149c.
- [ ] **151.4** Emit `progress_question` alongside `ask_user_request` with
      plan context. Test. Spec: SP-151 §151a.
- [ ] **151.5** Coalesce milestones only (`pkg/webui/stream_coalesce.go`);
      question, verification and completion events never coalesced.
      Test. Spec: SP-151 §151b.
- [ ] **151.6** Deterministic template summaries in Go and CLI rendering
      in the status footer (`pkg/cliui/terminal_subscriber_events.go`).
      Tests. Spec: SP-151 §151c.
- [ ] **151.7** Web UI compact progress strip in the chat rendering the
      summaries. Vitest. Spec: SP-151 §151c.
- [ ] **151.8** Optional `summarizer`-role summaries built only from event
      fields, template fallback on error; never a success summary without
      a passing verification event. Tests. Spec: SP-151 §151c.

---

## SP-155 — Live Preview and Extension Points (`roadmap/SP-155-live-preview-and-extension-points.md`)

- [ ] **155.1** Public mode registration API (id, label, icon, shell
      component, availability predicate) in
      `webui/src/workspaces/registry.ts`; built-in Code and Design
      register through it. Vitest: a test mode appears in the switcher;
      Code/Design unchanged. Spec: SP-155 §155b.
- [ ] **155.2** Default mode from configuration (built-in default stays
      `code`). Vitest. Spec: SP-155 §155b.
- [ ] **155.3** Preview pane component with starting / running / stopped /
      failed states, restart action, reload on file changes. Vitest.
      Spec: SP-155 §155a.
- [ ] **155.4** Backend: start or detect the dev server from the starter
      manifest's `dev` command and port (153.2) and expose its URL to the
      pane. Go tests. Spec: SP-155 §155a.
- [ ] **155.5** Hosted: the pane embeds the URL from a registered preview
      port (`register_preview_port`) instead of only printing it. Test
      with a stub. Spec: SP-155 §155a.
- [ ] **155.6** Pane placement: Code-mode panel and the SP-143 screen
      preview slot where it applies; e2e with a fixture starter project
      and local dev server. Spec: SP-155 §155a.
- [ ] **155.7** Export chat, changes, file and preview views from
      `@sprout/ui` (or a documented entry point) with typed props; run
      `cd packages/ui && npm run build` before webui type-check. Spec:
      SP-155 §155c.
- [ ] **155.8** Layout configuration accepts an embedding-supplied
      arrangement; an example embedding composes chat + preview from the
      exported views. Vitest. Spec: SP-155 §155c.
- [ ] **155.9** Move primary UI strings to copy keys with today's text as
      defaults; visual regression shows no change. Spec: SP-155 §155d.

---

## SP-156 — Deploy Targets and Ship Mode (`roadmap/SP-156-deploy-targets.md`)

- [ ] **156.1** Deploy target interface (`Deploy`, `Status`, `List`,
      `Rollback`, `PreviewURL`) and a fake adapter. Round-trip tests.
      Spec: SP-156 §156a.
- [ ] **156.2** `.sprout/deploy.json` config and validator; build output
      taken from the starter manifest. Tests. Spec: SP-156 §156a.
- [ ] **156.3** Credentials from the existing credential store or the
      embedding environment; never in model context, tool arguments or
      logs. Test asserting no token in model requests or logs. Spec:
      SP-156 §156b.
- [ ] **156.4** Build and upload: build in the workspace with the
      manifest's `build` command after verification passed on the same
      tree; refuse if the tree changed since verification. Tests. Spec:
      SP-156 §156a-2.
- [ ] **156.5** Preview vs production: preview deploys may run
      automatically after verification passes; production always needs
      explicit user confirmation. Test: unconfirmed production deploy is
      refused. Spec: SP-156 §156a-3.
- [ ] **156.6** CLI: `sprout deploy`, `deploy status`, `deploy history`,
      `deploy rollback <id>` over the fake adapter. Tests. Spec: SP-156
      §156c.
- [ ] **156.7** Agent tools `deploy_status` and `deploy` with the
      verification and confirmation gates; deploy outcomes emit progress
      events (SP-151). Tests. Spec: SP-156 §156c.
- [ ] **156.8** Cloudflare adapter (Pages for static output, Workers where
      needed) against the user's own account token; unit tests against
      an `httptest` fake of the Cloudflare API, no live account. Spec:
      SP-156 §156a-1.
- [ ] **156.9** Ship mode through the 155.1 registry: live URL and
      version, deploy action, history with change summaries, roll back;
      Code/Design unaffected. Vitest. Spec: SP-156 §156d.

---

## SP-157 — Checkpoints and Project Health (`roadmap/SP-157-checkpoints-and-project-health.md`)

- [ ] **157.1** Timeline model on `pkg/history`: change sets with template
      summaries (diff + plan scope IDs) and deploy entries. Go tests with
      a fixture history. Spec: SP-157 §157a.
- [ ] **157.2** Checkpoints: created automatically on passing verification
      and on deploy, and on demand; restoring is one action and is itself
      a timeline entry. Tests. Spec: SP-157 §157a.
- [ ] **157.3** Timeline UI with restore action in the changes surface
      (the SP-145 Changes surface if it has landed, otherwise the current
      Changes panel); file-level views remain. Vitest. Spec: SP-157 §157a.
- [ ] **157.4** Optional `summarizer`-role change summaries with template
      fallback. Test. Spec: SP-157 §157a.
- [ ] **157.5** Quality after edits (off by default in the CLI): run the
      manifest's or project's formatter and linter after a code-changing
      turn and fix findings in the same turn. Scripted test with a seeded
      lint violation. Spec: SP-157 §157b.
- [ ] **157.6** Require a test for new behavior (a `test` acceptance item
      or a test added in the turn); enforced through verification when
      enabled. Test. Spec: SP-157 §157b.
- [ ] **157.7** `sprout health`: size and complexity signals and failing
      checks; findings proposed as small, separately approvable fixes.
      Fixture test. Spec: SP-157 §157c.
- [ ] **157.8** `sprout health`: duplicated code (SP-016 embedding index)
      and outdated dependencies. Fixture test. Spec: SP-157 §157c.
- [ ] **157.9** Build/runtime error classifier (missing dependency, syntax
      error, type error, failing test, app crashed on start) with short
      explanation templates shown alongside the raw output. Fixture
      tests. Spec: SP-157 §157d.
- [ ] **157.10** Agent attempts a fix for a classified error before
      reporting it. Scripted test. Spec: SP-157 §157d.

---

## Not automatable

- **Decision:** framework for each reference starter (static site, web
  app, web app with data) — SP-153 open question. Unblocks the items
  below.
- **Decision:** local development emulation for starter 3's Cloudflare
  storage (D1/KV/R2) so its tests run without an account — SP-153.
- After the framework decision, add as `[ ]` items under SP-153: one item
  per starter scaffold (minimal app, one test, formatter/linter/test
  config, README, manifest, design scaffold), one per stack skill
  (`pkg/skills/library/<starter>/`), and the CI job that instantiates,
  builds, tests and serves each starter (SP-153 §153b, §153c).
- After starters exist, add as `[ ]` items under SP-154: at least five
  tasks per starter with frozen plans (SP-154 §154a).
- **Decision + accounts:** running the benchmark on real models and
  publishing results — needs API keys and spend; also decide where
  results live and the run cadence (SP-154 open questions).
- **Accounts:** live Cloudflare end-to-end deploy of each reference
  starter (preview, then confirmed production) — needs a real Cloudflare
  account and token (SP-156 acceptance).
- **Decision:** Pages vs Workers selection rule per starter (SP-156 open
  question); 156.8 can proceed with Pages for static output meanwhile.
