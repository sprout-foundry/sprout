# TODO

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

- [ ] **ws.9** Expose the composition API: the package's `exports` map has
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
- [ ] **ws.10** Package size: the 62 MB WASM ships twice (hashed and a
      fixed-name fallback, `emit-wasm-assets.mjs`), 132 MB unpacked. Ship
      only the content-hashed WASM and `wasm_exec.js` in the package
      (referenced through the manifest); keep the fixed-name copies only
      where the local embed (`pkg/webui/static`) needs them. Artifact test
      asserts one WASM file in the package and a size budget.
      Spec: SP-160 §160e.


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

## Automation reliability

- [x] **auto.1** Coordinator sessions stop early: runs of
      `automate/workflow.json` (`initial` mode, one coordinator session)
      end with status `success` after one or a few items while runnable
      `[ ]` items remain (Oct 5: sessions ended at 18:13, 20:34 and 23:33
      with 30+ items open; the platform runner did the same). The
      coordinator treats "finished an item and summarized" as done. Keep the
      coordinator approach (the `loop` mode is not used); fix it in the
      runtime: after the coordinator's final answer, re-read the todo file;
      while runnable `[ ]` items remain and the session made progress (a new
      commit or a newly ticked item), continue with a new turn using a short
      continuation prompt; stop when nothing runnable is left or a session
      makes no progress, so permanently skipped items cannot loop forever.
      Record the stop reason in the run record (`.sprout/automate/*.json`).
      Tests: a scripted coordinator that stops after each item still
      completes three items; a run whose only remaining items are skipped
      stops after one no-progress turn.
      Fixed: `pkg/workflow/continuation.go` adds `RunInitialContinuation`,
      invoked after the initial coordinator turn; it re-reads the TODO file
      and issues continuation turns while runnable `[ ]` items remain and the
      turn made progress (new commit or newly ticked item), stopping on
      no-runnable-items / no-progress / budget / cancel / cap and recording
      the reason in the run record. Opt-in via a `continuation` block in the
      workflow config. Pinned by scripted loop tests.
- [x] **auto.2** Workflow runs never wait on approvals, and a blocked
      command does not end the run. Three fixes in the security path
      (`pkg/agent/seed_tool_security.go`,
      `pkg/agent_tools/security_classifier_workspace.go`,
      `pkg/configuration/config_subagent_type.go`):
      (a) A workflow/automate run is non-interactive for approvals even
      when launched from a terminal: today `canPrompt` follows the
      console's interactivity, so Caution commands prompt and wait up to
      `utils.ApprovalPromptTimeout` (30 min) with nobody to answer. In a
      workflow run, Caution results follow the configured risk profile
      without prompting; hard blocks still block.
      (b) A hard block in a non-interactive run rejects that one command
      with a clear tool error the agent can act on, instead of the
      "fatal security block … The run will exit" path that ends the whole
      session. The critical tier itself (IsCriticalOperation) is
      unchanged and stays absolute.
      (c) `offWorkspacePathInCommand` treats any `/…` token as a file path,
      including quoted search patterns such as `grep -n "/api/git/"`;
      ignore tokens whose top-level directory does not exist on this
      machine (or that are clearly pattern arguments), so route strings
      do not trigger "outside the workspace root" prompts.
      Tests: a workflow run started with a TTY never waits on a Caution
      approval; a hard-blocked command returns an error and the next tool
      call still runs; `grep -n "/api/git/" file` in the workspace is not
      flagged while `cat /etc/hosts` still is.
      Fixed: a workflow run marks the agent non-interactive for approvals
      (`SetWorkflowRun`), so Caution results follow the risk profile without
      prompting while hard blocks still block; a non-interactive hard block
      rejects the single command with an actionable error instead of the
      run-exiting path; `offWorkspacePathInCommand` ignores rooted tokens
      whose top-level directory does not exist (and that carry no `..`), so
      route/search patterns are not flagged. `IsCriticalOperation` unchanged.
      Pinned by Go tests.

- [x] **auto.3** Cache savings show $0: `calculateCachedTokenSavings`
      (`pkg/agent/metrics.go`) returns 0 whenever the model has no cached
      price in the catalog, which is the case for the recommended models (a
      session with 99% cache reuse reported "Cost savings: $0.000000").
      (a) Populate `cached_input_cost` per model in
      `pkg/providercatalog/providers.json` from the providers' published
      cache-read prices (`cmd/refresh_provider_catalog`; e.g. DeepInfra
      DeepSeek-V4.1-Flash $0.006/M vs $0.20/M input), keeping it on refresh.
      (b) When the provider reports the request's actual cost (OpenRouter
      `cost`), compute savings as the uncached cost of the prompt minus the
      actual cost instead of from catalog rates. Tests for both paths and
      for the unknown-price case (shown as "unknown", not $0).
      Fixed: `cached_input_cost` is populated per model in
      `pkg/providercatalog/providers.json` and kept by the refresh tool;
      `ResolveModelPricing` falls back to the curated catalog for a missing
      cached rate; `calculateCachedTokenSavings` prefers the provider's
      reported actual cost (inverting the cache discount to reconstruct the
      uncached prompt cost) and otherwise uses the catalog rate, returning an
      explicit unknown (rendered as "unknown", never $0) when neither applies.
      Pinned by Go tests.
- [x] **auto.4** Continuation stops on a single verification-only turn:
      `RunInitialContinuation` (`pkg/workflow/continuation.go`) stops with
      `no_progress` the first time a turn ends without a new commit or a
      newly ticked item, even when that turn left verified, uncommitted
      work in the tree (it happened after a subagent hit its iteration cap
      and the coordinator spent its turn re-verifying). Allow a configurable
      number of consecutive no-progress turns before stopping
      (`continuation.max_idle_turns`, default 2), and when the previous turn
      made no progress, send a continuation prompt that names the state
      ("the last turn did not commit; finish and commit the in-progress
      item, or leave a note under it and move on"). Count a turn as
      progress only for commits or ticks, never for working-tree edits
      alone. Tests: one idle turn then a commit continues; idle turns up to
      the limit stop with `no_progress`; the nudge prompt is sent after an
      idle turn.


## Review fixes — correctness and spec promises
Found in the code review of the automated work. Fix these before new feature
items, in order. Each fix adds a test that tries to break the rule it
protects.

- [x] **fix.1** Verification commands cannot change mid-turn: the runner
      re-reads `.sprout/starter.json` and `.sprout/plan.json` every repair
      round (`pkg/verify/verify.go`), and the model can edit both. Snapshot
      the manifest commands and plan acceptance at turn start, always run the
      baseline build/test, and refuse model writes to `.sprout/starter.json`
      during a turn. Test: a model that rewrites the test command to `true`
      still fails verification. Spec: SP-149 §149b.
- [x] **fix.2** Deliver the verification result to the user: the block is
      appended only to the returned string (`seed_query_result.go`), which
      the streaming CLI and the web UI never show. Write it into the last
      assistant message in state and emit it as a stream chunk. Test: a
      failing turn shows "Verification: FAILED" in CLI and web output.
      Spec: SP-149 §149d.
- [x] **fix.3** No "not verified" notice when verification is off:
      `progress_complete` renders "Run complete — not verified (verification
      disabled)" after every turn by default. Do not emit or render it when
      verification is disabled. Spec: SP-151, SP-155 default UI unchanged.
- [x] **fix.4** Language guard ignores runtime-injected user messages: the
      English `<verification-report>` messages count in the user-language
      vote (`final_message_guard.go` `recentUserMessages`). Exclude injected
      messages or reuse the language resolved at turn start. Test: a Spanish
      user with two repair rounds is still judged Spanish. Spec: SP-152.
- [x] **fix.5** Model roles keep legacy behavior: inline completions resolve
      the coder role, which reads `subagent_*` before `completion_*`
      (`api_completion.go`); restore completion-first. The reviewer gate
      `ResolveRole(reviewer) != ""` always fires; gate on an explicit
      reviewer selection. `roleSelection` must merge field-wise with the
      same precedence as the legacy getters (remove the test that pins the
      inversion). Test: existing configs with only legacy fields resolve the
      same models as before roles. Spec: SP-150 §150a.
- [x] **fix.6** Coalesced milestones keep their route: `mergeMilestones`
      (`pkg/webui/stream_coalesce.go`) drops `client_id`/`chat_id`/`user_id`,
      so every batch is filtered out; merge only same-route events and copy
      the route keys onto the batch. Spec: SP-151.
- [x] **fix.7** Role attribution: subagent and reviewer spend is rolled into
      the parent's role with zero prompt/completion; the plan agent is
      stamped `coder`; ledger bookings carry no role
      (`TakeUnbookedUsageByRole` has no caller); role usage is not restored
      with state. Fix all four so per-role totals equal the overall total.
      Fixed: `SubagentResult` now carries the subagent's role + prompt/
      completion split, and the single/parallel/reviewer rollups use
      `Agent.RollupSubagentUsage` so spend lands in the right role with real
      tokens; `createPlanningAgent` stamps the planner role (`SetRole`); the
      cost ledger books per role (`RecordCostWithRole` +
      `TakeUnbookedUsageByRole`); `AgentState`/`ConversationState` persist
      and restore `role_usage`. Spec: SP-150 §150c.
- [x] **fix.8** Starter instantiate endpoint stays inside the daemon root:
      `pkg/webui/api_starters.go` has no containment check; return 403
      outside `GetDaemonRoot()` as `handleAPIWorkspaceBrowse` does.
      Fixed: `handleAPIStartersInstantiate` now resolves the daemon root's
      symlinks and rejects a canonical target that is neither the root nor
      strictly under it (`target_outside_daemon_root`, 403), mirroring the
      `handleAPIWorkspaceBrowse` containment convention. Pinned by
      `TestHandleAPIStartersInstantiate` "target outside daemon root returns
      403" + "daemon root itself is an allowed target".
- [x] **fix.9** Hide the test fixture starter from `sprout new` and the web
      UI chooser (its npm commands cannot run); keep it for tests only.
      Fixed: `pkg/starters` gains `testOnlyStarters` (the fixture) and
      `ListForUsers()` (full catalogue minus test-only trees); `sprout new`
      (`cmd/new.go`) and `GET /api/starters` (`api_starters.go`) now list via
      `ListForUsers()`, while `List`, `Instantiate`, `Version`, `Manifest` and
      `FileCount` still address the fixture by id (tests + benchmark). Pinned
      by `TestListForUsers`, the updated CLI/web-UI list tests, and the CLI
      flag help no longer naming the hidden fixture.
- [x] **fix.10** Language guard in streaming mode: emit a replacement event
      with the regenerated text (CLI and web UI) instead of relying on the
      length heuristic; show the notice only for the final (no-tool-call)
      response, not mid-turn preambles. Spec: SP-152 acceptance.
- [x] **fix.11** "View original": render the held original in the web UI and
      offer it in the CLI, or remove the claim from the notice text.
      Spec: SP-152 §152b.
      Fixed: the notice's promise is now real on both surfaces. The web UI
      renders a collapsed "Original reply" disclosure under a guarded
      assistant message (`MessageItem`, fed by the existing
      `languageGuardOriginal` the WS handler stores); the CLI gains
      `/original` (alias `orig`, `pkg/agent_commands/original.go`) reading
      the newest held payload via the exported
      `agent.LastLanguageGuardOriginal`, and the terminal replacement
      notice now points at it. Pinned by Go command/CLI tests and vitest.
- [x] **fix.12** Cap total repair rounds per turn (not only per check key),
      so alternating failures or new interaction IDs cannot loop.
      Spec: SP-149.
      Fixed: a `total_repair_rounds` cap (config, default derived as 2×N)
      bounds the loop in addition to the per-check rule; the pure
      `verificationLoopShouldStop` helper ends the turn when either fires,
      and the §149d failure report still attaches. Pinned by pure stop-rule
      tests and an alternating-failure integration fixture.
- [x] **fix.13** Verification runs only for application-code changes: skip
      docs and `.sprout/` paths in `TurnChangedPaths`. Spec: SP-149 §149a.
      Fixed: a pure `IsApplicationCodePath` predicate plus
      `Agent.TurnChangedApplicationPaths`; the turn-end gate and the
      not-verified reason read the filtered window, so docs-only or
      bookkeeping-only turns never start a build while a code+docs turn
      still runs. Pinned by a predicate table and full-turn tests.
- [x] **fix.14** Plan revision never decreases: `planstore` `Save` bumps from
      `max(stored, given)`.
      Fixed: `Save` now bumps from the higher of the caller's revision and
      the revision on disk (unreadable stored file contributes no floor),
      so a stale caller can never move the revision backwards. Pinned by
      stale-caller, monotonic, first-write, corrupt-file, and
      never-decreases-sequence tests.
- [x] **fix.15** Mode registry: reject re-registering built-in mode ids and
      never leave zero modes (`webui/src/workspaces/registry.ts`).
      Fixed: `code`/`design` are protected in both directions (registration
      rejects their ids as a warned no-op, disposal refuses built-in
      entries); the disposer keeps its identity check for non-built-ins; and
      `resolveWorkspaceMode` never returns undefined (layered fallback to a
      registered built-in). Pinned by vitest.
- [x] **fix.16** Plan snapshot for milestones refreshes on plan revision, and
      a scope that goes pending→completed in one write emits both events
      (`scope_milestones.go`).
      Fixed: the milestone path re-reads the plan and refreshes the cached
      revision/titles when it changes (a vanished plan refreshes to rev 0);
      the extracted pure transition helper emits started-then-finished for a
      scope completed in a single write. Pinned by milestone tests.
- [x] **fix.17** Progress strip filters by the active chat (`ProgressStrip.tsx`);
      TS event types match the Go payloads (optional `plan_revision`,
      `elapsed_ms`, batched `milestones`).
      Fixed: the strip takes the chat id from `ChatProps`, accepts
      chatless or matching-chat events, drops foreign-chat events, and
      clears on chat switch (batches filter on the top-level envelope).
      `ProgressMilestoneData` now models both the flat payload and the
      coalesced batch (optional `plan_revision`/`elapsed_ms`/`phase`, route
      keys). Pinned by vitest.
- [x] **fix.18** Role models settings: re-sync the draft when config changes
      and keep custom role names on save (`RoleModelsSection.tsx`); reject
      unknown role names in `/model --role`.
      Fixed: the draft re-syncs from `settings.roles` on a value change only
      (canonical-JSON compare, so unrelated refreshes never clobber typing);
      saves seed from the persisted map so custom roles survive; `/model
      --role` rejects names outside `configuration.BuiltInRoles()` with the
      valid set in the message. Pinned by vitest and Go command tests.

## Review fixes — finish ticked items that are not wired
- [x] **wire.1** Benchmark: `sprout benchmark` CLI entry and a per-task
      timeout that stops a hung turn. (The ≥5 tasks per starter wait for the
      starter frameworks — see Not automatable.) Spec: SP-154.
      Fixed: `cmd/benchmark.go` registers `sprout benchmark` (suite/model/
      output/runs/timeout flags, report.md+report.json, partial report on
      early stop); `Runner.Timeout` + `runTurnWithTimeout` stop a hung turn
      through the agent's real interrupt and record it wrapping
      `ErrRunTimeout`, never Passed. Pinned by timeout + CLI tests.
- [x] **wire.2** Summarizer role: call the progress summarizer where the spec
      says, through role metering and with a timeout, or remove it.
      Spec: SP-151 §151.8.
      Fixed: the progress-event handler now calls the optional model
      summarizer (bounded by a timeout with template fallback) and books its
      usage under the `summarizer` role via `BookRoleUsage`; a success
      summary still requires a passing verification. Pinned by CLI tests.

## Review fixes — repository rules
- [x] **rules.1** Remove item tags from code comments and user-visible
      strings added by the automated work ("TODO 153.6", "item 149.5",
      "(152.7)", "SP-149 §149a" in the manual-check reason). Spec-level
      references in docs are fine.
      Fixed: removed the automated-work spec tags from comments and
      user-visible strings across `pkg/`, `cmd/`, `webui/src/` and `test/`
      in seven verified comment/string-only batches — the SP-148..157
      series, the bare `§149`/`§152` forms, `TODO 15x.y`, `item 14x/15x`
      and `(15x.y)` shapes — rewriting each sentence so it reads cleanly.
      Every batch was proven comment/string-only (non-comment tokens
      unchanged) and the touched suites pass; the item's named examples are
      cleaned. The only remaining matches are three roadmap *filenames*
      (`roadmap/SP-148-structured-plans.md`, `roadmap/SP-153-starters-and-
      stack-skills.md`, `roadmap/SP-152-language-guard.md`), which are
      spec-level doc references the item allows.
- [x] **rules.2** Split files over 500 lines introduced or grown by the
      automated work (`pkg/agent/seed_provider_chat.go`,
      `pkg/benchmark/runner_test.go`, `config_roles_test.go`,
      `verification_hook_test.go`, and the others the review listed), no
      behavior change.
      Fixed: split the named files (and their test counterparts) by
      responsibility into cohesive files all under 500 lines —
      `seed_provider_chat.go` 682→252 plus a stream file (234) and a
      language-guard file (228); `runner_test.go` 1128→323 plus contract
      (387), metrics (289) and suite (188) files; `verification_hook_test.go`
      600→433 plus a guards file (187); `config_roles_test.go` 651→182 plus
      a resolve file (481). Move-only (assertion counts unchanged; the agent,
      benchmark and configuration suites pass). Large pre-existing files not
      grown by the automated work (e.g. `pkg/webui/chat_sessions.go`,
      `pkg/design/*`) were deliberately left untouched.

## Web UI delivery hygiene (before SP-160)
Small fixes to how the hosted web UI bundle is built and cached; they are
needed whatever the integration work does. Spec context: SP-160 §160e.

- [x] **hyg.1** Release cloud bundle base path and mode: `.github/workflows/release.yml`
      runs `scripts/build-webui-dist.mjs --mode cloud`, which builds in
      production mode at base `/`, while hosts serve the bundle under
      `/webui/` (root-absolute `/assets/*` URLs break there; see the comment
      in `webui/vite.config.ts`). Build the released cloud bundle at
      `/webui/` in production mode, and add a test or CI check that the
      released `index.html` references only `/webui/` asset URLs.
      Fixed: the cloud branch passes `-- --mode cloud` to `npm run build`, so
      Vite selects base `/webui/` while staying a production build
      (`isProd` now keys on the build command, not the mode label). A CI
      check (`scripts/verify-webui-dist-base.mjs`) fails when the released
      `index.html` carries non-`/webui/` asset URLs; pinned by vitest.
- [x] **hyg.2** Content-hash the WASM assets: `sprout.wasm` and
      `wasm_exec.js` keep fixed names in the bundle (`build-webui-dist.mjs`,
      `services/wasmShell.ts` probes `/webui/wasm` and `/wasm`), so a host
      that caches them as immutable can mix old WASM with new code after an
      upgrade. Emit hashed file names plus a small manifest the loader reads;
      test that changing the WASM changes its URL.
      Fixed: the dist build emits `sprout.<hash>.wasm` / `wasm_exec.<hash>.js`
      plus `wasm-manifest.json`; `wasmShell.ts` reads the manifest and uses
      the hashed URLs, falling back to the fixed names when it is absent and
      honoring explicit URL overrides. Pinned by vitest.
- [x] **hyg.3** Single `services/api` import style: `OnboardingDialog.tsx`
      and `ErrorBoundary.tsx` import it dynamically while about 70 modules
      import it statically, which only produces a Vite warning and no chunk
      split. Use static imports everywhere (or split deliberately).
      Fixed: both files now import `ApiService` statically; no dynamic
      `services/api` import remains in `webui/src`.

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
- [x] **149.6** Final-reply contract: success may be reported only with a
      passing result attached; when the stopping rule fires, the reply
      states what passes, what fails and what was tried. Tests. Spec:
      SP-149 §149c, §149d.
- [x] **149.7** Non-interactive `sprout agent` runs exit non-zero when
      verification is enabled and fails; disabled verification changes no
      behavior. Tests. Spec: SP-149 §149e.

---

## SP-154 — Agent Task Benchmark (`roadmap/SP-154-agent-benchmark.md`)

- [x] **154.1** Benchmark harness skeleton: task fixture format
      (plain-language request, frozen SP-148 plan, starter reference) and
      loader. Tests with a fixture task on the 153.3 fixture starter.
      Spec: SP-154 §154a.
- [x] **154.2** Runner: headless non-interactive run per task in a fresh
      starter copy, 3 runs per model; pass/fail comes only from the
      SP-149 result. Test: a scripted model claims success but fails a
      check → recorded as fail. Spec: SP-154 §154a, §154b.
- [x] **154.3** Per-task metrics: repair attempts, turns, wall time,
      tokens and cost (existing cost tracking), language-guard
      mismatches. Test. Spec: SP-154 §154b.
- [x] **154.4** Default model list from the provider catalog's
      `recommended_model` entries; configurable override. Test. Spec:
      SP-154 §154b.
- [x] **154.5** Reports: markdown + JSON comparing models and starters,
      pass rate per starter per model over 3 runs, failure categories.
      Golden-file test. Not part of `go test ./...` (network and cost).
      Spec: SP-154 §154c.

---

## SP-150 — Model Roles (`roadmap/SP-150-model-roles.md`)

- [x] **150.1** `roles` config section (`planner`, `coder`, `summarizer`,
      `reviewer`, `commit`), parsed and merged across global and project
      config; unset roles fall back to the conversation model. Tests.
      Spec: SP-150 §150a.
- [x] **150.2** Existing settings (`subagent_model`, `commit_model`,
      review and completion models) read as aliases for their roles. A
      test per alias. Spec: SP-150 §150a.
- [x] **150.3** `ResolveRole(name)` replaces the per-setting getters
      internally; context-profile resolution (SP-125) runs on the
      resolved model; the provider-name grep test still passes. Tests.
      Spec: SP-150 §150b.
- [x] **150.4** Plan mode uses the `planner` role, the main loop `coder`;
      `summarizer` is resolvable for SP-151/SP-157. Tests. Spec: SP-150
      §150a.
- [x] **150.5** Metering: every model call carries its role; usage ledger (`pkg/agent/usage_ledger.go`) and cost model record per-role tokens and cost; per-role totals in `/cost` views and usage events; add role to the 152.9 mismatch metric and the 154.3 metrics. Tests. Spec: SP-150 §150c.
- [x] **150.6** CLI: `/model --role <role> <model>` and `sprout config`
      support for roles. Tests. Spec: SP-150 §150d.
- [x] **150.7** Web UI settings: role models section, collapsed by
      default. Vitest. Spec: SP-150 §150d.

---

## SP-151 — Progress Events (`roadmap/SP-151-progress-events.md`)

- [x] **151.1** Event types `progress_milestone`, `progress_question`,
      `progress_verification`, `progress_complete` with run, plan
      revision and scope IDs in `events_types.go`; regenerate the
      `@sprout/events` TypeScript union (`packages/events`). Tests. Spec:
      SP-151 §151a, §151d.
- [x] **151.2** Emit milestone events as plan scope items start and finish
      (from 148.6 scope IDs), with files-touched count and elapsed time.
      Test with a fixture plan. Spec: SP-151 §151a.
- [x] **151.3** Emit `progress_verification` from SP-149 results (evidence
      references) and `progress_complete` with the final result or "not
      verified". Test: event order and correlating IDs. Spec: SP-151
      §151a; SP-149 §149c.
- [x] **151.4** Emit `progress_question` alongside `ask_user_request` with
      plan context. Test. Spec: SP-151 §151a.
- [x] **151.5** Coalesce milestones only (`pkg/webui/stream_coalesce.go`);
      question, verification and completion events never coalesced.
      Test. Spec: SP-151 §151b.
- [x] **151.6** Deterministic template summaries in Go and CLI rendering
      in the status footer (`pkg/cliui/terminal_subscriber_events.go`).
      Tests. Spec: SP-151 §151c.
- [x] **151.7** Web UI compact progress strip in the chat rendering the
      summaries. Vitest. Spec: SP-151 §151c.
- [x] **151.8** Optional `summarizer`-role summaries built only from event
      fields, template fallback on error; never a success summary without
      a passing verification event. Tests. Spec: SP-151 §151c.

---

## SP-155 — Live Preview and Extension Points (`roadmap/SP-155-live-preview-and-extension-points.md`)

- [x] **155.1** Public mode registration API (id, label, icon, shell
      component, availability predicate) in
      `webui/src/workspaces/registry.ts`; built-in Code and Design
      register through it. Vitest: a test mode appears in the switcher;
      Code/Design unchanged. Spec: SP-155 §155b.
- [x] **155.2** Default mode from configuration (built-in default stays
      `code`). Vitest. Spec: SP-155 §155b.
- [x] **155.3** Preview pane component with starting / running / stopped /
      failed states, restart action, reload on file changes. Vitest.
      Spec: SP-155 §155a.
- [x] **155.4** Backend: start or detect the dev server from the starter
      manifest's `dev` command and port (153.2) and expose its URL to the
      pane. Go tests. Spec: SP-155 §155a.
- [x] **155.5** Hosted: the pane embeds the URL from a registered preview
      port (`register_preview_port`) instead of only printing it. Test
      with a stub. Spec: SP-155 §155a.
- [x] **155.6** Pane placement: Code-mode panel and the SP-143 screen
      preview slot where it applies; e2e with a fixture starter project
      and local dev server. Spec: SP-155 §155a.
- [x] **155.7** Export chat, changes, file and preview views from
      `@sprout/ui` (or a documented entry point) with typed props; run
      `cd packages/ui && npm run build` before webui type-check. Spec:
      SP-155 §155c.
- [x] **155.8** Layout configuration accepts an embedding-supplied
      arrangement; an example embedding composes chat + preview from the
      exported views. Vitest. Spec: SP-155 §155c.
      Fixed: `ViewsLayout` + `resolveViewsArrangement` merge an
      embedding-supplied `ViewsArrangement` per slot onto the built-in
      composition (unknown kinds throw); `ExampleEmbedding` composes chat +
      preview from the views entry exports. Pinned by vitest.
- [x] **155.9** Move primary UI strings to copy keys with today's text as
      defaults; visual regression shows no change. Spec: SP-155 §155d.
      Fixed: a typed copy registry (`webui/src/config/copy.ts`) maps 39
      primary chrome keys (product name, mode switcher, sidebar tabs,
      home nav, welcome/empty states, primary actions) to today's exact
      text, with an installable override seam and a safe fallback; the
      migrated components resolve strings through it at render time. The
      defaults are pinned literally in tests so a future edit cannot
      silently change the UI. Pinned by vitest.

---

## SP-156 — Deploy Targets and Ship Mode (`roadmap/SP-156-deploy-targets.md`)

- [x] **156.1** Deploy target interface (`Deploy`, `Status`, `List`,
      `Rollback`, `PreviewURL`) and a fake adapter. Round-trip tests.
      Spec: SP-156 §156a.
      Fixed: new `pkg/deploy` with `DeployTarget`, `Deployment`,
      `DeployRequest`, deployment kinds/states, sentinel errors, and an
      in-memory `FakeTarget` adapter. Pinned by round-trip Go tests.
- [x] **156.2** `.sprout/deploy.json` config and validator; build output
      taken from the starter manifest. Tests. Spec: SP-156 §156a.
      Fixed: new `pkg/deployconfig` loads/validates `.sprout/deploy.json`
      (target/project required, `build_output` must stay under the project
      root; missing file is a sentinel) and `Resolve` merges it with the
      starter manifest into a `deploy.DeployRequest`, defaulting the build
      output to the manifest's. Pinned by Go tests.
- [x] **156.3** Credentials from the existing credential store or the
      embedding environment; never in model context, tool arguments or
      logs. Test asserting no token in model requests or logs. Spec:
      SP-156 §156b.
      Fixed: `pkg/deploy.ResolveCredential` prefers the credential store and
      falls back to an embedding-supplied env var, with a typed missing
      error; `Credential` masks on `%v`/`%#v` and marshals without its
      secret, so it cannot ride into model context or logs. Pinned by tests
      using a sentinel token across serialized types and redacted payloads.
- [x] **156.4** Build and upload: build in the workspace with the
      manifest's `build` command after verification passed on the same
      tree; refuse if the tree changed since verification. Tests. Spec:
      SP-156 §156a-2.
      Fixed: `pkg/deploy.Deployer.BuildAndDeploy` gates on a passing-
      verification tree fingerprint, builds in the workspace via the
      manifest command, re-checks the tree, then uploads through the target;
      any refusal path returns before an upload. Pinned by stub-based Go
      tests.
- [x] **156.5** Preview vs production: preview deploys may run
      automatically after verification passes; production always needs
      explicit user confirmation. Test: unconfirmed production deploy is
      refused. Spec: SP-156 §156a-3.
      Fixed: `pkg/deploy.Confirmation` (fail-closed) is checked as the first
      gate in `BuildAndDeploy`; an unconfirmed production request returns
      `ErrProductionNeedsConfirmation` before any build or upload, while
      preview proceeds automatically. Pinned by Go tests.
- [x] **156.6** CLI: `sprout deploy`, `deploy status`, `deploy history`,
      `deploy rollback <id>` over the fake adapter. Tests. Spec: SP-156
      §156c.
      Fixed: `cmd/deploy.go` registers the command + subcommands, resolves
      `.sprout/deploy.json`, and drives `deploy.Deployer.BuildAndDeploy`
      against a persisted fake target; `--production` is gated on `--yes`.
      Verified end to end and pinned by Go tests.
- [x] **156.7** Agent tools `deploy_status` and `deploy` with the
      verification and confirmation gates; deploy outcomes emit progress
      events (SP-151). Tests. Spec: SP-156 §156c.
      Fixed: `pkg/agent_tools` registers `deploy_status` (read-only) and
      `deploy` (preview by default); `deploy` refuses unless verification
      passed and routes production through the approval gate (no model-
      supplied confirmation), emitting a progress event on the outcome.
      Pinned by Go tests.
- [x] **156.8** Cloudflare adapter (Pages for static output, Workers where
      needed) against the user's own account token; unit tests against
      an `httptest` fake of the Cloudflare API, no live account. Spec:
      SP-156 §156a-1.
      Fixed: `pkg/deploy` gains a Cloudflare adapter implementing
      `DeployTarget`: Pages is the fully implemented path (deploy, list,
      status, per-deployment preview URL, rollback) and Workers covers the
      single-script shape. It takes an injected base URL/`http.Client` and
      the resolved `deploy.Credential` (never reads the environment); the
      token reaches only the auth header and is scrubbed from errors.
      Pinned by `httptest`-backed tests (round trip, typed non-2xx errors,
      token absence, project binding, concurrency).
- [x] **156.9** Ship mode through the 155.1 registry: live URL and
      version, deploy action, history with change summaries, roll back;
      Code/Design unaffected. Vitest. Spec: SP-156 §156d.
      Fixed: a `ship` workspace mode registers through the public mode API
      and renders a pure `ShipSurface` (live URL/version/last deploy, deploy
      action, linkable history with a change-summary slot, rollback) driven
      entirely by a `ship` slice on `WorkspaceShellProps`. Code and Design
      are untouched and still register/switch. Pinned by vitest.

---

## SP-157 — Checkpoints and Project Health (`roadmap/SP-157-checkpoints-and-project-health.md`)

- [x] **157.1** Timeline model on `pkg/history`: change sets with template
      summaries (diff + plan scope IDs) and deploy entries. Go tests with
      a fixture history. Spec: SP-157 §157a.
      Added `pkg/history/timeline`: change sets (history revisions with
      deterministic template summaries of files/insertions/deletions and
      optional plan scope IDs) plus deploy entries carrying
      `deploy.Deployment`. Ordered oldest-first or newest-first, with a
      stable tie-break. A subpackage (not the history package itself) so
      the history data layer stays free of the deploy/plancontract
      dependencies. Pinned by Go tests over a seeded temp history.
- [x] **157.2** Checkpoints: created automatically on passing verification
      and on deploy, and on demand; restoring is one action and is itself
      a timeline entry. Tests. Spec: SP-157 §157a.
      Fixed: `pkg/history` gains a checkpoint store (create/list/get/restore,
      workspace-scoped) that reuses the revision revert for restore and
      records the restore as a checkpoint; the timeline gains a checkpoint
      entry kind. Automatic capture is wired at the passing-verification
      seam (the turn's revision) and the completed-deploy seam, and the
      on-demand `checkpoint` agent tool lists/creates/restores. Pinned by
      Go tests (including that capture uses the given revision, not the
      store head, and that reads never create the store).
- [x] **157.3** Timeline UI with restore action in the changes surface
      (the SP-145 Changes surface if it has landed, otherwise the current
      Changes panel); file-level views remain. Vitest. Spec: SP-157 §157a.
      Fixed: `ProjectTimeline` renders change sets, deploys and checkpoints
      from a typed prop (mirroring the backend timeline entry), with a
      two-step restore confirm (a single click never restores) and pure
      sort/restorability helpers. It is an additive tab in
      `AgentChangesPanel`; the existing per-file views are unchanged.
      Pinned by vitest.
- [x] **157.4** Optional `summarizer`-role change summaries with template
      fallback. Test. Spec: SP-157 §157a.
      Fixed: `pkg/history/timeline` gains an opt-in model summary
      (`SummarizeEntry`/`SummarizeEntryWithUsageFn`) that builds the prompt
      from the entry's own fields only, bounds the call, falls back to the
      deterministic template on a nil client, error, timeout, empty output,
      or an entry with nothing to summarize, and books usage under the
      `summarizer` role only when the model actually ran. Pinned by tests
      including the fallback and no-fabrication rules. Consumers (a timeline
      rendering surface that books the usage) land with that surface.
- [x] **157.5** Quality after edits (off by default in the CLI): run the
      manifest's or project's formatter and linter after a code-changing
      turn and fix findings in the same turn. Scripted test with a seeded
      lint violation. Spec: SP-157 §157b.
      Fixed: the starter manifest gains `format`/`lint` commands and config
      an off-by-default `quality` section; `pkg/verify.QualityRunner` runs
      formatter then linter under the same trusted-command discipline, and
      the turn-end hook (`runTurnEndQuality`) runs it only for a
      code-changing, non-subagent turn and feeds linter findings back
      through the shared repair loop. Pinned by a scripted seeded-lint
      test, off-by-default, and formatter-failure cases.
- [x] **157.6** Require a test for new behavior (a `test` acceptance item
      or a test added in the turn); enforced through verification when
      enabled. Test. Spec: SP-157 §157b.
      Fixed: an off-by-default `verification.require_test` flag makes the
      verification runner append a failing check when a code-changing turn
      neither had a plan `test` acceptance item nor added/changed a test
      file; disabled it changes nothing, and a docs-only turn is not
      flagged. Pinned by five end-to-end tests (one per rule).
- [x] **157.7** `sprout health`: size and complexity signals and failing
      checks; findings proposed as small, separately approvable fixes.
      Fixture test. Spec: SP-157 §157c.
      Fixed: new `pkg/health` scans the tree for oversized files and
      high-complexity functions and adapts `pkg/verify` checks into
      findings, each carrying a proposed fix; `cmd/health.go` registers
      `sprout health` (text + `--json`) in the Diagnostics group. Pinned by
      a fixture-project test.
- [x] **157.8** `sprout health`: duplicated code (SP-016 embedding index)
      and outdated dependencies. Fixture test. Spec: SP-157 §157c.
      Fixed: `pkg/health` gains `DuplicateFinder` (default: token-cosine
      over the existing AST extraction; the SP-016 embedding index was
      removed from the repo, so a seam stands in its place) and
      `DependencyChecker` (network-free `go.mod` parse + an injectable
      latest-version seam; the live checker shells `go list -m -u`), with
      `--no-duplicates`/`--no-deps`/`--duplicate-threshold`. Pinned by
      fixture tests.
- [x] **157.9** Build/runtime error classifier (missing dependency, syntax
      error, type error, failing test, app crashed on start) with short
      explanation templates shown alongside the raw output. Fixture
      tests. Spec: SP-157 §157d.
      Fixed: new stdlib-only `pkg/errclass` with a closed `Category` set
      (plus a conservative `unknown`), a deterministic first-match text
      table covering Go, JS/TS and Python shapes, and `Classify` returning
      the category, a short explanation template and the raw output
      preserved verbatim. Pinned by fixture tests.
- [x] **157.10** Agent attempts a fix for a classified error before
      reporting it. Scripted test. Spec: SP-157 §157d.
      Fixed: the turn-end report builders classify a failed check's excerpt
      through `pkg/errclass` and attach a short `Classification:` line
      above the raw excerpt (never replacing it), so the existing repair
      loop feeds a classified failure back with its explanation and stays
      bounded by the current caps; an unknown failure keeps the prior
      behavior. Pinned by scripted end-to-end tests (one per rule).

---

## SP-160 §160c — Backend contract follow-up

- [x] **contract.18** Route inventory after the Huma migration:
      `cmd/api_inventory` parses `pkg/webui/routes.go` and now finds 18 of
      159 routes, because the families moved to `huma.Register` operations
      (`pkg/webui/huma_*.go`); `TestRouteCountIsStable` and the
      `docs/api/endpoints.md` freshness test fail. Build the inventory from
      the registered Huma operations plus the remaining plain routes (from
      the API object in-process, not by parsing source), keep the "served
      by" column, regenerate `docs/api/endpoints.md`, and make both tests
      pass. Spec: SP-160 §160c.
      Fixed: the Huma set is now read in-process from the live registration
      set (`cmd/api_inventory/huma_routes.go` `humaOpsInProcess` reads the
      in-process Huma API object's OpenAPI document, not source), merged with
      the plain `mux.HandleFunc` routes parsed from the `registerXxxRoutes`
      functions (all four files, not just `routes.go`). `Route` gains an
      `ExplicitMethod` so Huma rows keep their registered method from the
      OpenAPI spec instead of collapsing to "any". The "Served by" column is
      unchanged. `docs/api/endpoints.md` regenerated at 195 rows (31 plain +
      164 Huma); `TestRouteCountIsStable` constant updated 159 → 195 with an
      accurate explanation; both the count and the freshness test pass.

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
