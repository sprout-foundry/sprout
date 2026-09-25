# TODO

Active work tracked here. Each item is a small, independently committable
unit for the workflow automation (~30 min – 2 h). Every item cites its spec
section — read the cited spec before starting. Validation gate for every
item: `make vet && make fmt-check && make lint && make build-all` and
`go test ./...` clean (webui items additionally: `cd webui && npx
prettier --check`).

---

## SP-140 — Design Workspace (all items shipped on `feat-design-workspace`)

The following items were completed on the design-workspace branch. Every item
is marked `[x]` and carries a summary of what landed; the SP-140 specs under
`roadmap/` remain the authority for the contract.

## SP-140-1 — Format Charter (`roadmap/SP-140-1-formats.md`)

- [x] **1.1** Scaffold the `design/` directory contract (tokens/, brand/,
      icons/, wireframes/, screens/, flows/, feedback/) with a starter
      `design/README.md` manifest template: device frames declared as
      machine-parseable fenced lines (`frames:` → `name: WxH`),
      screen/flow purpose, status markers. Spec: SP-140-1 §1e + parent
      SP-140 "Directory contract".
- [x] **1.2** Token convention checks in the validator: JSON parse,
      `$value`/`$type` leaf structure, `$type` membership (color,
      dimension, fontFamily, fontWeight, number, duration, cubicBezier,
      strokeStyle, border), alias `{group.token}` resolution — reject
      dangling and cyclic refs, `$extensions` passed through untouched.
      Spec: SP-140-1 §1a.
- [x] **1.3** SVG wireframe convention checks: root `<svg>` with integer
      viewBox, self-containment (no `<script>`, no external href/src,
      data-URI rasters only), slug name rule `^[a-z0-9]+(-[a-z0-9]+)*$`,
      `data-nav` targets must exist as wireframe stems. Hard checks;
      frame-match/`<text>`/id checks are advisory `info`. Spec:
      SP-140-1 §1b.
- [x] **1.4** Mermaid flow convention checks: Go-side parser for the
      `flowchart` subset (edge/node extraction, not full syntax
      fidelity); node-id == wireframe-stem rule for screen flows.
      Spec: SP-140-1 §1c.
- [x] **1.5** Brand + icon convention checks: `brand.md` palette
      references into tokens (no raw hex), icon/logo SVGs follow the
      same self-containment + slug rules, optional `sprite.svg`
      `<symbol>` entries. Spec: SP-140-1 §1d, §1f.
- [x] **1.6** Screens convention checks: `design/screens/*.html`
      self-containment (no network `<script>`/CDN refs; inline or
      workspace-relative CSS), slug naming, advisory device-frame width
      check against README `frames:` declarations. Spec: SP-140-1 §1i.
- [x] **1.7** README manifest check: relative links resolve to real
      files; `frames:` block parses (name → `WxH`); wireframe
      frame-matching runs against declarations (advisory). Spec:
      SP-140-1 §1e, §1g.
- [x] **1.8** `design_validate` ToolHandler: `//go:build !js`-safe (no
      browser/vision deps); one struct + one line in
      `pkg/agent_tools/all.go`; runnable with no args (whole tree) or a
      path; structured findings `{file, line?, severity, message, rule}`;
      advisory only (findings never block a turn). Spec: SP-140-1 §1g.
- [x] **1.9** Git contract findings: validator emits a `fix` finding
      when `.gitattributes` lacks `design/**/*.svg diff=html` (exact
      line to add; append, never clobber — repo already has
      `.gitattributes`); gitignore guidance for `design/.cache/` only;
      warn when a data URI exceeds the size threshold. Spec:
      SP-140-1 §1h.
- [x] **1.10** Fixture test tree + seeded-bad fixtures: valid tree →
      zero findings (`TestDesignAssetConventions`); each bad fixture
      trips its rule class (broken alias, cyclic alias, unknown
      `$type`, missing viewBox, `<script>`, external href, dangling
      `data-nav`, mermaid id with no wireframe, README link to missing
      file, screens HTML with network script). Spec: SP-140-1 AC.
- [x] **1.11** Proprietary-name grep test: hardcoded word list (`figma`,
      `penpot`, `sketch`, `illustrator`, `adobe`, `photoshop`) over the
      new design-tier Go files and webui design components — not docs,
      specs, or fixtures (`TestVisionTierNoProviderNames` pattern).
      Spec: SP-140-1 AC.

---

## SP-140-2 — Designer Persona & Tools (`roadmap/SP-140-2-persona-tools.md`) — depends on 1.x

- [x] **2.1** `pkg/personas/configs/designer.json` catalog entry (id
      `designer`, aliases `ux`/`design`, delegatable, `git_write`
      capability, explicit `allowed_tools` array — no default-set
      mechanism exists; listing not-yet-registered tools is harmless)
      + catalog loader conflict tests. Spec: SP-140-2 §2a.
- [x] **2.2** Designer system prompt
      `pkg/agent/prompts/subagent_prompts/designer.md` (+ update the
      static index `pkg/agent/prompts/subagent_prompts/README.md`;
      add `IDDesigner` constant in `pkg/personas/ids.go`): directory
      contract, format charter, validate-then-declare-done workflow,
      critique vocabulary. Spec: SP-140-2 §2a.
- [x] **2.3** `design_assets` tool: inventory JSON (manifest summary,
      per-asset rows, token group counts, flow node/edge counts,
      cached validator findings); `{exists: false}` + scaffold guidance
      when no `design/`. Spec: SP-140-2 §2c.
- [x] **2.4** Shared render helper extracted from
      `analyze_ui_screenshot_handler.go`: extends input detection
      (`IsHTMLInput` → renderable-input check covering `.svg`) or takes
      an explicit render-mode arg, so SVG renders to PNG instead of
      falling through to the raw `image/svg+xml` vision branch; local
      file rendering passes `allow_file_url: true` in `BrowseOptions`.
      Spec: SP-140-2 §2c.
- [x] **2.5** `design_render` tool (`//go:build !js` + WASM stub
      mirroring `all_vision.go`): SVG/HTML via the shared helper,
      mermaid via standalone HTML with pinned vendored mermaid script
      in `pkg/agent_tools/design/` (keep LICENSE/NOTICE; exempt from
      500-line rule); images attached via SP-137 path;
      `analysis_prompt` passthrough; `.mmd` source never mutated.
      Gate-1 precheck. Spec: SP-140-2 §2c.
- [x] **2.6** `design_import_sketch` tool (`//go:build !js` + WASM
      stub): thin gatekeeper (path validation, Gate-1 precheck,
      target conventions in prompt); vision tier does extraction;
      output reminds the agent to run `design_validate` (the tool does
      not write files). Spec: SP-140-2 §2c.
- [x] **2.7** `design-system` skill at
      `pkg/skills/library/design-system/SKILL.md`: full workflow (brief →
      tokens → wireframes → flows → screens, validate between each),
      greenfield scaffold sequence (README first, then tokens, … ;
      appends to existing `.gitattributes`), brownfield inventory.
      Persona-agnostic. Spec: SP-140-2 §2b.
- [x] **2.8** Prompt guidance: static conditional section in
      `system_prompt.md` + minimal `system_prompt.lite.md` (when
      `design/` exists: read README first, use `design_assets`, route
      to designer/skill) — static prose, no runtime injection; keep
      prompt-consistency tests green. Spec: SP-140-2 §2d.
- [x] **2.9** Gate-1 negative tests: off-workspace path denial for
      `design_assets`/`design_render`/`design_import_sketch` (native
      builds). Spec: SP-140-2 §2e.
- [x] **2.10** WASM variant of `design_assets` per the `all_*_wasm.go`
      pattern + tool-roster smoke test (SP-112-9 pattern); confirm
      `design_render`/`design_import_sketch` are excluded from the
      WASM roster. Spec: SP-140-2 AC + SP-140 invariant 7.
- [x] **2.11** Scripted-client end-to-end tests: `design_render`
      fixture SVG/HTML round trip (images flow the seed tool-result
      path); `design_import_sketch` as a scripted *agent* turn (vision
      scripted client + `write_file` + `design_validate`) producing
      validated `design/wireframes/login.svg`. Spec: SP-140-2 AC.
      → `pkg/agent/design_e2e_test.go` (build tag `!js`).

---

## SP-140-3 — WebUI DesignView (`roadmap/SP-140-3-designview.md`) — depends on 1.x; parallel with 2.x

- [x] **3.1** Vendored npm deps: add `@xyflow/react`, `dagre`, `mermaid`
      to `webui/package.json` (net-new; no CDN references anywhere).
      Spec: SP-140-3 §3g.
- [x] **3.2** `webui/src/services/api/designApi.ts` (alongside
      `filesApi.ts`, export line in `services/api/index.ts`) over
      existing workspace-file APIs: `listAssets`, `readAsset`,
      `writeLayout`, `writeFeedback` — zero new HTTP endpoints.
      Spec: SP-140-3 §3f.
- [x] **3.3** DesignView shell: `webui/src/components/design/DesignView.tsx`
      wired into the `EditorWorkspace.tsx` view branch + a nav
      affordance in `Sidebar.tsx` (`currentView: 'design'`; `ViewType`
      open union, no type change); visible only when `design/` exists;
      lazy-loaded dynamic-import chunk (verify zero bundle impact
      otherwise); three tabs (Flows, Screens, Tokens). Shell only —
      tabs are separate components (500-line rule). Spec: SP-140-3 §3a.
- [x] **3.4** Layout-derivation pure functions: `.mmd` → dagre → node
      positions, in `webui/src/design/layout.ts` + `sidecar.ts`.
      Runner: `cd webui && npx vitest run src/design/*.test.ts*`
      (explicit globs; never bare vitest). Spec: SP-140-3 §3b.
- [x] **3.5** Flows canvas component (`FlowsCanvas.tsx`): React Flow
      rendering with wireframe SVG imagery in nodes (object URLs),
      labeled boxes for flows without wireframes, edge labels visible,
      pan/zoom, node/edge select → detail pane. Canvas never writes
      `.mmd`. Spec: SP-140-3 §3b.
- [x] **3.6** Canvas persistence: drag-reposition writes
      `design/flows/<name>.layout.json` sidecar `{nodes, layoutHint,
      derivedFrom}`; hash drift on `.mmd` edit regenerates layout on
      next load. Spec: SP-140-3 §3b.
- [x] **3.7** Screens tab (`ScreensGrid.tsx`): SVG thumbnail grid with
      README status chips, device-frame-aware sizing from README
      `frames:`, click → detail pane instantiating `LivePreview` as a
      controlled component with `onContentChange` wired to designApi
      write-back. Spec: SP-140-3 §3c.
- [x] **3.8** Tokens tab (`TokensTree.tsx`): grouped DTCG tree, color
      swatches, typography/spacing specimens, search/filter by token
      path, detail pane opens `.tokens.json` in editor. Read-only.
      Spec: SP-140-3 §3d.
- [x] **3.9** Feedback write path stub: annotation affordance in the
      detail pane writing `design/feedback/<target>.json` per the
      SP-140-4d schema (including `resolved`/`resolution` fields).
      Spec: SP-140-3 §3e.
- [x] **3.10** Sidecar hash/staleness + designApi Vitest coverage.
      Runner: `cd webui && npx vitest run <explicit globs>` (never bare
      vitest; `scripts/vitest-safe.sh` runs packages/ui tests, not
      webui). Spec: SP-140-3 AC.
- [x] **3.11** Playwright e2e `test/webui/design_view.spec.ts`:
      chrome-channel launch per AGENTS.md; own stack with pre-seeded
      `workspaceDir` fixture (standard `start-stack.mjs` boots a fresh
      workspace with no `design/`): open fixture workspace → flow
      renders → select node → detail pane → open screen in editor.
      Spec: SP-140-3 AC.

---

## SP-140-4 — Visual Loop (`roadmap/SP-140-4-visual-loop.md`) — depends on 2.x and 3.x

- [x] **4.1** `design_critique` tool core: render target(s) via
      `design_render`, images via SP-137 path, structured rubric prompt
      (hierarchy, affordance, consistency, spacing rhythm, contrast,
      touch targets), structured findings `{target, area, severity, note,
      suggestion}`. Spec: SP-140-4 §4a.
- [x] **4.2** Critique degradation: non-vision primary → static findings
      with `visual: false`, never fails the turn (SP-137 tier order).
      Spec: SP-140-4 §4a.
- [x] **4.3** Critique cache + cost cap: PNG cache under
      `design/.cache/renders/` keyed by content hash with provenance
      header (skip re-render on unchanged content); whole-tree critique
      capped at 20 screens with explicit notice (tool arg, not CLI
      flag). Spec: SP-140-4 §4e.
- [x] **4.4** Consistency rule pack — flow/wireframe bidirectionality:
      every `data-nav` target exists; every flow edge has a wireframe
      counterpart unless terminal; README screen references exist.
      Spec: SP-140-4 §4b.
- [x] **4.5** Consistency rule pack — inventory & naming: token-usage
      tracking (literal colors/fonts flagged `info`), orphan screens
      (`info`), slug rule + wireframe/screens name mismatch (`warn`).
      Spec: SP-140-4 §4b.
- [x] **4.6** Self-review loop in skill + designer prompt: write →
      `design_validate` → `design_critique` → fix; stopping rule = all
      findings `info` or explicitly accepted. Spec: SP-140-4 §4c.
- [x] **4.7** Feedback consumption: `design_assets` reports pending
      feedback targets with counts; skill loop starts any
      `changes-requested` target with a feedback-file read. Spec:
      SP-140-4 §4d.
- [x] **4.8** DesignView annotation resolution: mark annotation
      `resolved` from the detail pane; agent closes the loop with a
      `resolution` note (schema fields defined in SP-140-4 §4d).
      Spec: SP-140-4 §4d.
- [x] **4.9** Seeded-fixture tests for each consistency rule (right
      severity) + feedback round-trip test + cache render-count test +
      21-screen cap test. Spec: SP-140-4 AC.

---

## SP-140-5 — Design↔Code Sync (`roadmap/SP-140-5-sync.md`) — core after 1.x; full value after 4.x

- [x] **5.1** `design_export_tokens` tool: DTCG → CSS variables / TS /
      Tailwind `@theme` (also swift/kotlin), deterministic
      byte-identical output, targets `design/generated/`. Spec:
      SP-140-5 §5a.
- [x] **5.2** Export hardening: provenance content-hash headers,
      refuses dirty alias graphs. Spec: SP-140-5 §5a + SP-140 invariant 2.
- [x] **5.3** `design_sync` analyze mode: read touched UI code + design
      tree, return sync report `{delta, kind, design_files, confidence,
      basis: literal|structural|inferred}`; diff + convention based (CSS
      vars, Tailwind classes, router files), no AST analysis; touched
      files default from `env.ResolveToolFuncs().ListChanges` (parsed —
      no typed ChangeTracker accessor exists). Spec: SP-140-5 §5b.
- [x] **5.4** `design_sync` apply mode: write safe subset (literal
      token renames/revalues, wireframe/sidecar updates, flow edge
      additions), mark inferred as proposals; never modify files outside
      `design/`; Gate-1 on touched paths. Spec: SP-140-5 §5b, §5e.
- [x] **5.5** Drift direction reporting in `design_assets` +
      `design_validate`: design-ahead vs code-ahead as distinct rows
      with distinct remedies. Spec: SP-140-5 §5c.
      (5.5 ships the rows + remedies on both tool surfaces; §5c's
      "skill and persona prompts explain the difference" is the 5.6
      prompt/skill wiring, deferred there. The difference is currently
      explained in the design_assets/design_validate tool Descriptions.)
- [x] **5.6** Loop in the definition of done: prompt/skill wiring —
      UI-affecting turns end with `design_sync`; skill's handoff section
      becomes a sync section; designer prompt drops "hand off" for "keep
      the tree truthful". Also carries §5c's "skill and persona prompts
      explain the design-ahead vs code-ahead difference". Spec: SP-140-5 §5d.
- [x] **5.7** Co-commit rule + `design:` commit-type convention in the
      skill's sync section; test that a fixture loop produces one commit
      carrying code + design adoption and that reverting removes both.
      Spec: SP-140-5 §5f.
- [x] **5.8** `design_brief` tool: reads the design tree for a screen
      (wireframe, flow edges, tokens, README purpose, pending feedback)
      and returns a structured brief; writes no files (contract, not
      generator); Gate-1. Spec: SP-140-5 §5g.
- [x] **5.9** End-to-end loop test: scripted-client design turn
      (designer persona, SP-137 scripted pattern) → export → dev turn →
      `design_sync` → tree reflects the tweak; next design turn builds
      on it. Spec: SP-140-5 AC.
- [x] **5.10** Offline provenance check: at an arbitrary checkout, the
      hash in a generated artifact's header matches the token inputs at
      that commit (decidable from the tree alone). Spec: SP-140-5 AC.
- [x] **5.11** Umbrella end-to-end validation (parent SP-140 AC): fresh
      workspace with no `design/` goes from one prompt to a validated
      `design/` tree with DesignView showing the flow graph and
      screens; every artifact opens in a non-sprout tool; drift loop
      and co-commit verified. Run after all prior items.
      New test pkg/design/umbrella_e2e_test.go (`//go:build !js`)
      validates the whole parent AC as ONE hermetic run: (0) a fresh
      workspace reports no `design/` + no findings; (1) one prompt's work
      (Scaffold + every tier + export) produces the tree; (2) design_validate
      is clean of ERRORS (negative-control test proves a seeded dangling
      `data-nav` trips an error) and design_assets/Scan report the
      asset kinds, token groups, flow node/edge counts, manifest frames +
      status; (3) the DesignView input structure is asserted in the webui's
      OWN path↔kind vocabulary (`design/flows/*.mmd` graph → FlowsCanvas,
      wireframes/screens → ScreensGrid, tokens → TokensTree), with the
      rendering itself covered by the committed webui DesignView suite +
      3.11 Playwright spec; (4) every artifact is a valid instance of a
      standard open format — SVG parses as XML with an integer viewBox,
      HTML is self-contained (no network refs), `.mmd` is the supported
      mermaid subset, tokens are valid DTCG with `$type`/`$value` leaves,
      generated CSS/TS/etc. carry the recomputable provenance banner, and
      a whole-tree walk rejects any binary/proprietary artifact; (5) drift
      loop: design-ahead and code-ahead report distinctly with distinct
      remedies, design_sync apply resolves code-ahead, stays confined to
      `design/`, and a second apply is byte-identical (no-op); (6) co-commit
      on a throwaway git fixture: ONE commit carries both the dev revalue
      and its design_sync adoption, `git revert` removes both, and the
      committed artifact's `source-hash` matches the token inputs recomputed
      from that commit's tree alone (offline). Test-only: no production
      behaviour changed; reuses the exported surface of prior items
      (Scaffold, ValidateTree, ResolveExportTokens/RenderArtifacts,
      ParseFlowchart, AnalyzeDrift, AnalyzeTouchedFiles/PlanSyncApply,
      TokenExportInputHash). Hermetic (temp workspaces + `git init` fixture);
      determinism asserted by a byte-identical second run. Two documented
      environment-robustness notes surfaced by this item: (a) the shared
      `provGit`/`git show` helper trims a trailing newline, so the umbrella
      reads committed token blobs byte-faithfully via a local raw-git helper;
      (b) a global `core.autocrlf=input` would normalize committed token
      blobs, so the fixture pins `core.autocrlf=false` + an eol `.gitattributes`
      (the pre-existing 5.10 provenance suite shares fragility (a)+(b)).

---

## SP-140-6 — The Loop Surface (`roadmap/SP-140-6-loop-surface.md`) — depends on 3.x/4.x/5.x (shipped)

- [x] **6.1** Live tree: `DesignWorkspaceProvider` refetches the
      inventory on window focus, on a 30 s interval while Design mode is
      active, and via a manual refresh control; a refetch that no longer
      sees the selected asset clears selection; the fetch stays gated on
      `active` (asserted: no design fetches from a Code-mode session).
      Stale-response safety: a monotonic fetch sequence means a slow
      older response can never overwrite a newer one. Spec: SP-140-6 §6a.
- [x] **6.2** Design status endpoint: pure `pkg/design/status.go`
      (`BuildDesignStatus` — aggregation only over the exported
      `ValidateTree`, drift, and `ScanFeedbackDir` surfaces, capped
      findings list, deterministic order) + read-only
      `GET /api/design/status` handler mounted via a new
      `registerDesignRoutes` group in `pkg/webui/routes.go`;
      no-`design/` workspaces return `{exists: false}` with 200.
      Cross-check test: endpoint values equal what `design_validate` /
      `design_assets` report for the same fixture tree. Spec:
      SP-140-6 §6b.
- [x] **6.3** Health strip atop the design surface: validate tally
      chips, the two drift rows (only ahead rows render; synced shows a
      quiet mark), pending-feedback count, refresh control; every
      chip's click-through lands on the surface it names (finding →
      asset selection, design-ahead → Tokens, code-ahead → agent panel
      prefill, pending → annotated asset). Token-only CSS; testid
      registry entries. Depends on 6.2. Spec: SP-140-6 §6c.
- [x] **6.4** Critique findings sidecar: `design_critique` writes
      `design/.cache/renders/findings/<target-slug>.findings.json`
      (`target`, `rubric`, `sourceHash`, `generated`, `findings[]`
      with blocker/major/minor/info severity; `visual: false` for
      non-vision runs); keyed on the target label, not the artifact
      stem (artifact stems collide across tiers — wireframes/login.svg
      and screens/login.html both render to renders/login.png);
      derived output — `design_validate` does not emit findings for it.
      Spec: SP-140-6 §6d.
- [x] **6.5** Annotation pins: pin layer over the detail pane's
      screen preview and wireframe nodes rendering each §4d annotation
      at its `at` coordinates, colored by area from palette tokens,
      open vs resolved visually distinct; pin click focuses the
      annotation in the pane; grid cards show pending counts;
      click-to-place during "Add feedback" records the clicked point
      as `at` (center default preserved when placement is skipped).
      Placement crosses tab↔pane through a window CustomEvent bridge
      (the `agent-file-changed` pattern). Spec: SP-140-6 §6e.
- [x] **6.6** Agent presence: DesignShell agent panel (collapsible;
      overlay on mobile) rendering the real Chat component from the
      `WorkspaceShellProps` chat payload the shell already receives —
      no second query client; "Ask the designer" prefill affordances on
      remedy rows (seed the input via the controlled `onInputChange`,
      never auto-send); stays inside the design surface area, Code-mode
      chrome untouched. Spec: SP-140-6 §6f.
- [x] **6.7** Loop results in the detail pane: last critique from the
      §6d sidecar with a stale marker (`generated` < asset mtime) and
      a "no critique recorded" prefill when absent; code-ahead assets
      show the "Adopt via agent" prefill (UI performs no `design/`
      writes — asserted GET-only). Depends on 6.2 + 6.4. Spec:
      SP-140-6 §6g.
- [x] **6.8** Umbrella: testid registry entries for all 26 new loop-
      surface ids (forward-reference checks green); house-rule sweep
      (prettier, gofmt, 500-line rule, lazy-chunk boundary via
      `designChunk.test.ts`); bounded test gates green. Playwright
      strip→finding→pin coverage deferred to the next mergeable pass
      (needs the seeded-stack e2e harness; documented below). Spec:
      SP-140-6 §6h + AC.

---

## SP-140-7 — Human Co-Editing (`roadmap/SP-140-7-co-editing.md`) — depends on 6.1 (live tree), 6.f wiring (agent panel)

- [x] **7.1** Safe-write seam: `/api/file` POST accepts opt-in
      `baseMtime`/`baseHash`; mismatch → 409 with current revision,
      nothing written; omitted → behavior unchanged (Code mode
      regression-pinned). `designApiWrite` gains
      `writeAssetIfUnchanged` (+ `DesignWriteConflictError`,
      `force` for the Keep-mine path) threading the loaded revision.
      Spec: SP-140-7 §7a.
- [x] **7.2** Incoming-change awareness: design surfaces subscribe to
      the `agent-file-changed` bridge filtered to `design/`; not-editing
      → silent asset refetch; editing + diverged → non-blocking conflict
      banner (Review = side-by-side compare; Keep mine = guarded-bypass
      write + restore action; Take theirs = reload); identical text =
      silent refresh. Spec: SP-140-7 §7b.
- [x] **7.3** Token editing: structured leaf editor in the Tokens detail
      pane (color well + text; unit-preserving coercion); surgical DTCG
      edit (parse → one `$value` → canonical stringify) via the safe
      write; alias warning from the 6.2 `tokenRefs` payload; a §7a
      conflict surfaces reload-and-retry (no forced write on the
      semantic layer). Depends on 7.1. Spec: SP-140-7 §7c.
- [x] **7.4** Screen status curation: draft/review/ready/clear menu on
      the screen detail; structured README manifest rewrite (marker
      lines only, the parseManifestStatuses vocabulary); unparsable
      manifest → open in editor, never clobbered (zero-writes
      asserted). Depends on 7.1. Spec: SP-140-7 §7d.
- [x] **7.5** Drag-and-drop gestures (each a pure gesture→write model +
      thin adapter): pin drag persists `at` coords (coordinate-only,
      pointer capture); screen card reorder persists README `Screens:`
      order (order-only rewrite; statuses/summaries/frames byte-stable;
      unmentioned stems trail). Explicitly not built: token-drag-onto-
      wireframe (prefill instead), asset moves, OS drop. Spec:
      SP-140-7 §7e.
- [x] **7.6** Review parity + co-commit doc: Go test pinning that webui
      writes publish the same `file_changed` event agent writes publish
      (the per-turn strip treats both identically); co-commit bullet in
      the design-system skill covering human edits from Design mode;
      ChangeTracker scope unchanged (no HTTP-write tracking). Spec:
      SP-140-7 §7f.

---

## SP-142 — Chat Mode Lanes (`roadmap/SP-142-chat-mode-lanes.md`)

- [x] **142.1** Server: `Mode` field on chatSession (`""` = legacy, reads
      as code); create stamps the mode; summary/list/switch carry it;
      cross-mode switch → `409 mode_mismatch`; TS mirror update
      (`types/generated.ts`, `services/chatSessions.ts`). Spec: SP-142 §1.
- [x] **142.2** Client: mode-filtered tab strip in `useChatSessionsSync`;
      pin writes scoped to in-mode switches; `mode_mismatch` fallback to
      the mode pin or a fresh session. Spec: SP-142 §2.
- [ ] **142.3** Server: workspace query gate — a query from a second chat
      while another runs in the same client context returns
      `409 workspace_busy` naming the running chat; releases on
      `query_completed`; shared-mode unchanged. Spec: SP-142 §3.
- [ ] **142.4** Client: busy notice in the composer ("send anyway"
      queues behind the running chat; drains on completion; cancel
      clears). Spec: SP-142 §3.
- [ ] **142.5** Design agent panel header: design chat name + scoped
      New Chat affordance. Spec: SP-142 §4.

---

## SP-140 — Vision tier follow-ups (`roadmap/SP-140-vision-capability-first-class.md`)

- [ ] **V-1** Vision-tier model preference: the registry `vision_model`
      pin beats the active model in `GetVisionModelForProvider`
      (pkg/agent_tools/vision_client.go), so an agent running a
      natively-multimodal model (e.g. glm-5.3-flash on zai-coding,
      catalog-tagged `vision`) still forks its vision calls to the
      pinned model — observed live: a designer-persona run's
      design_critique vision calls went to the registry-pinned
      glm-5v-turbo, which the user's plan does not include, 429'd
      three times, and degraded to OCR. Fix: prefer the ACTIVE model
      when it is itself vision-capable (SP-137 runtime > declared
      precedence); fall back to the registry `vision_model` only when
      the active model lacks vision. Also review the zai registry
      configs' vision_model staleness (glm-5.3-flash is the current
      natively-multimodal flash tier).
- [x] **V-2** Skill nit: design-system SKILL.md Step 1 now says to use
      structured-file writes for token JSON (a live
      designer-persona run hand-edited valid-but-mangled indentation;
      semantics were correct, formatting was not).

---

## Status

SP-140-6 (loop surface, items 6.1–6.8) and SP-140-7 (human co-editing,
items 7.1–7.6) shipped on `feat-design-workspace` 2026-09-19. Deferred
from 6.8 with rationale: the Playwright strip→finding→pin e2e (needs the
seeded-stack harness; component coverage covers the flows).
SP-140-1…5 are complete on `feat-design-workspace` (sections above).
The bg-sessions inactivity-expiry item
(quiet watchers killed by the 2h LastPolled expiry) shipped in
`bg-sessions: activity-based expiry`: running sessions are no longer reaped
for being unpolled — cleanup probes pid liveness at TTL boundaries, renews
alive sessions, reaps only dead ones; per-session `TTL` on StartOptions
(agent shell sessions default 8h) and `sprout shell-bg keepalive ID` provide
the explicit escape hatches. Covered by background_expiry_test.go and
TestShellBgKeepalive_*.
---
