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

## Status

No active items as of 2026-09-15. The bg-sessions inactivity-expiry item
(quiet watchers killed by the 2h LastPolled expiry) shipped in
`bg-sessions: activity-based expiry`: running sessions are no longer reaped
for being unpolled — cleanup probes pid liveness at TTL boundaries, renews
alive sessions, reaps only dead ones; per-session `TTL` on StartOptions
(agent shell sessions default 8h) and `sprout shell-bg keepalive ID` provide
the explicit escape hatches. Covered by background_expiry_test.go and
TestShellBgKeepalive_*.
---
