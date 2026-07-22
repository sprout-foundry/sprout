# Roadmap

Roadmap specifications for the sprout project. Each spec describes
a major architectural area, its current state, and open work.

Specs land at the root until core work ships; once shipped, the spec body
lives in git history (no per-spec archive file). The root directory contains
only specs still receiving active changes or retained as living reference.

**Counts (as of 2026-07-21):** 87 shipped · 1 pending · 2 parked in `future/`.

## Shipped

Spec bodies preserved in git history; no per-spec archive (83 historical specs).

| Spec | Title | Status |
|------|-------|--------|
| SP-001 | Agent Core Architecture | ✅ Active (recently refactored) |
| SP-002 | Configuration, Credentials & Providers | ✅ Active |
| SP-003 | Webui & Frontend Architecture | ✅ Active (under active development) |
| SP-004 | Security, Validation & MCP | ✅ Active |
| SP-005 | Supporting Systems & Infrastructure | ✅ Active |
| SP-008 | Reliability Engineering — Concurrency & Observability | ✅ Shipped (Tracks A + B complete 2026-06) |
| SP-009 | Component Library Maturation — Storybook + @sprout/ui | ✅ Implemented (Storybook + MDX docs + Chromatic visual regression; webui imports @sprout/ui as monorepo sibling) |
| SP-010 | Editor Modernization | ✅ Implemented (EditorPane 2604→513 lines; EditorCore extracted; React.memo + 18 bug fixes) |
| SP-011 | Terminal Parity & Bug Fixes | ✅ Shipped (all 3 phases complete 2026-06) |
| SP-012 | UX Polish | ✅ Implemented (a11y gap-closure shipped 2026-07-01: `role="treeitem"`/`aria-expanded` on FileTree rows, `aria-live="polite"` on ChatPanel log region, global `:focus-visible` styles in `webui/src/index.css`, `notificationBus.markAllRead()` + NotificationCenter "Mark all read" button. Broader SP-012 surface shipped 2026-06-23 → 2026-06-30 per the spec history.) |
| SP-013 | Agent Settings Management Tool | ✅ Implemented (manage_settings tool registered; pkg/agent/settings_handler.go) |
| SP-014 | Agent Terminal Sessions — Hidden PTY Routing + Background Mode | ✅ Implemented (Hidden PTY routing + background mode shipped) |
| SP-015 | Cloud Platform Integration | ✅ Implemented (sprout-side; 2026-06-26) — R1–R7 complete in this repo. Cross-repo evolution lives in `../sprout-foundry` |
| SP-016 | Embedding Index — Duplicate Detection & Semantic Search | ✅ Implemented (core infrastructure complete; expanded scope in SP-016b) |
| SP-016b | Expanded Embedding Index — Full Workspace Semantic Search | ✅ Shipped (backend complete 2026-06; minor SearchView.tsx UI gap) |
| SP-017 | Settings Panel Rework — Scoped Collapsible Sections | ✅ Partially Implemented (scoped labels shipped; collapsible sections pending → see SP-101) |
| SP-018 | Memory System | ✅ Implemented |
| SP-019 | Multi-Chat Sessions | ✅ Implemented |
| SP-020 | Trace/Dataset Mode | ✅ Implemented |
| SP-022 | Remote Provider Registry | ✅ Implemented |
| SP-022 | Workspace Management & Project Detection | ✅ Implemented (WorkspacePicker + WorkspacePane + LocationSwitcher + WorkspaceBar) |
| SP-023 | In-Process Subagent Execution | ✅ Active |
| SP-024 | Context Management — File Read Optimization | ✅ Phase 1-3, Phase 4 complete (Phase 2 deferred; tree-sitter in SP-025) |
| SP-025 | Tree-Sitter Integration — Real AST for Multi-Language Symbol Extraction | ✅ Shipped (all 5 phases complete 2026-06) |
| SP-026 | Coordinator Persona (formerly "Executive Assistant") | ✅ Implemented (renamed 2026-06-03, see commit `516a9d41`) |
| SP-027 | Persistent Context & Conversational Memory | ✅ Shipped (all 4 phases complete 2026-06) |
| SP-039 | SP-039 — UI Consolidation Decision | ✅ Decision made |
| SP-039 | Component Categorization | (supporting doc — see linked spec) |
| SP-045 | WASM Build Feature Parity | ✅ Shipped (Tiers 1-3 complete 2026-06) |
| SP-046 | Browser-Primary Workspace Sync Model | ✅ Shipped (all 5 numbered items complete 2026-06) |
| SP-048 | CLI Delight — Terminal UX Polish | ✅ Partially Implemented (status footer + glyph vocabulary shipped; tool timeline + silence-fill pending → see SP-101) |
| SP-049 | Shell Permission Overhaul — User-Configurable Policy & Headless Hardening | ✅ Implemented (Phases 3a–3d complete) |
| SP-050 | Orchestrator Persona Collapse — One Persona, Configurable Git-Write | ✅ Implemented |
| SP-051 | Depth-Aware Subagent UI — Visible Nesting in the CLI | ✅ Implemented |
| SP-053 | WebUI CLI Parity — Persona/Depth, Live Tools, Cost Footer | ✅ Implemented |
| SP-054 | LSP Language Coverage Expansion | ✅ Shipped (all 3 phases complete 2026-06) |
| SP-055 | CLI Pinned Input — Always-On Steering Panel | ✅ Shipped — Phases 1/2/3 + 3b (done-queue mode) + 3c (UTF-8) + OPOST fix. |
| SP-056 | CLI Reasoning Fold — Collapsed Thinking Indicator | ✅ Implemented (2026-06-30) |
| SP-057 | CLI Output Consistency — Glyph Migration & Unified Picker | ✅ Shipped (all 5 phases, 2026-05-25) |
| SP-058 | Selective Grammar Embedding for WASM and Daemon | ✅ Implemented (Daemon binary 149 MB per 899d667f; 22 MB below 171 MB target) |
| SP-059 | Subagent ↔ Primary Interaction Overhaul + Delegate Retirement | ✅ Implemented (Phases 1–6 complete; delegate tool retired; audited 2026-06-27) |
| SP-059 | SP-059-6a — Delegate Feature Porting Review | (supporting doc — see linked spec) |
| SP-060 | Desktop App — Per-Workspace Server Mode | ✅ Implemented (Phase A + Phase B shipped and verified) |
| SP-061 | Remove Static Embedding Provider, Consolidate on ONNX | ✅ Implemented (Static embedding provider removed via SP-091-2) |
| SP-062 | CLI-Native Background Shell Execution | ✅ Implemented (BackgroundProcessManager wired into shell dispatch) |
| SP-063 | Destructive-App Denylist — Pre-Click Gate for Computer-Use Actions | (supporting doc — see linked spec) |
| SP-063 | Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent | ✅ Implemented — all safety gates shipped as of 2026-06-30 (including gate 4h destructive-app denylist) |
| SP-063 | Panic Key — Emergency Stop for Computer-Use Action Loops | (supporting doc — see linked spec) |
| SP-064 | Automate CLI — Status, Stop, Logs | ✅ Implemented (sprout automate status/stop/logs) |
| SP-065 | WebUI Automations Panel | ✅ Implemented (live WS event stream; commit 4f0a81c5) |
| SP-066 | Never-Ending Context — Substitution-First Context Management, Hierarchical Rollups, and Embedded Memory Recall | ✅ Shipped — Phase 1 (model-aware reservation, `pkg/agent/context_budget.go`), Phase 2 (hierarchical rollup, `pkg/agent/rollup*.go` + `embedded_prompts.go`), Phase 3 (semantic recall, `pkg/agent/semantic_recall.go` + `turn_embedding.go`), and Phase 3d (embedding-clustered rollup boundaries, `rollup_boundary.go`) all landed. `d6094ec5` closed the dormant-wire regression on Phase 3d; 4 integration tests cover the chain. |
| SP-067 | Automate Workflow Completion Injection | ✅ Implemented (2026-06-06) |
| SP-068 | Security Check Consolidation — One Risk Scale, One Resolver, One Broker | ✅ Implemented (Phases 1–3 shipped: single resolver, single broker, sprout explain) |
| SP-069 | Pull Request Creation — Close the "agent did the work, now what?" Gap | ✅ Implemented |
| SP-070 | Agent Completion Notifications — Tell the User When It's Their Turn | ✅ Implemented |
| SP-071 | Conversation Rewind & Edit-and-Resend — Undo a Wrong Turn | ✅ Implemented |
| SP-072 | Per-Hunk Diff Approval — Optional Approve-Before-Apply for Agent Edits | ✅ Implemented |
| SP-073 | Cooperative Cancellation — Thread Context So Stop Actually Aborts | ✅ Implemented (zero TODO(SP-034-1c) markers remain; all 10 sites threaded) |
| SP-074 | Finish the Tool-Registry Migration — Retire the Dual-Dispatch Shim | ✅ Shipped (Phases 1–4 complete; 2026-06-26) |
| SP-076 | WebUI Streaming Fix + Verbosity Modes | ✅ Implemented (2026-06-26) |
| SP-077 | ChangeTracker Reverts Committed Work During Git Operations | ✅ Implemented (Phase 1 + Phase 2) |
| SP-078 | Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion | ✅ Implemented (2026-06-30; Phases 1–4 complete) |
| SP-079 | Migrate Stub Tool Handlers off the Legacy `*Agent` Path | ✅ Implemented (2026-06-30) |
| SP-080 | Type the Unknown-Tool Error in ToolRegistry | ✅ Implemented (2026-06-30) |
| SP-081 | Delete the Dead `pkg/tools/global.go` Executor | ✅ Implemented (2026-06-30) |
| SP-082 | Preserve Key Insertion Order in Structured File Tools | ✅ Implemented (2026-06-30) — supersedes the original SP-066 key-order proposal |
| SP-083 | Cross-Session Search — Find Past Conversations by Content | ✅ Implemented (2026-06-30) |
| SP-084 | Export Sessions to Shareable Markdown / HTML | ✅ Implemented (2026-06-30) |
| SP-085 | Cost Analytics Dashboard — Model / Provider / Day Breakdown | ✅ Implemented (2026-06-30) |
| SP-086 | Skill Install — Pull Skills from Git, URLs, and Registries | ✅ Implemented (2026-06-30) |
| SP-087 | SP-087 Acceptance Report | (supporting doc — see linked spec) |
| SP-087b | Full Playwright Coverage of the WebUI | ✅ Implemented (2026-06-30; acceptance criterion 3 partial — trace/video/screenshot config deferred, see SP-087-acceptanc |
| SP-105 | CLI Interactive Panels — Settings Browser & Usage Dashboard | ✅ Implemented — `/settings` interactive AskUser-driven panel + `/usage` Unicode bar-chart dashboard + `--json` flag; `/stats` aliased to `/usage`. `pkg/agent_commands/settings_cmd.go` + `usage_cmd.go`; 23 unit tests pass. |
| SP-106 | CLI Output Polish + SelectList Touch Scroll | ✅ Implemented (all 3 features: markdown table rendering, nested list indentation + indented code blocks, SelectList mouse wheel scroll) |
| SP-107 | Code Intelligence Graph | ✅ Implemented — auto-build on first query (`codegraph_handler.go:60`), embedding_index integration (`embedding_index_handler.go:267`), qualified-name edge fix (`repo_map.go:ToCodegraphSymbols`). 41 codegraph + 29 edge-extraction tests pass; `find_dead_code`/`get_callers`/`get_callees` produce real results. Spec reconciliation at `55c997e1`; primary wiring at `7ea9061d`, `ce0e6b48`, `82d40fa1`. |
| SP-109 | Single-Source Tool Definitions — Eliminate Dual Maintenance | ✅ Implemented (all 4 phases complete; legacy `ToolConfig` registry deleted; `ToolHandler.Definition()` is the single source of truth) |
| SP-110 | Background Completion Injection & Auto-Resume | ✅ Implemented — All 3 phases shipped at `6d31e17a` (`pkg/agent/notifications.go`, `pkg/webui/wakeup_poller.go` with 2s ticker + all-gates-checked polling loop, Settings → Agent → General → "Enable auto-resume" toggle, per-session tokens/resumes budgets, interrupt-safety via `DisableWakeup`). Off by default; opt-in. |
| SP-115 | CLI UX — Footer Keyboard Hint Row | ✅ Implemented — `KeymapHintRow()` formatter (`pkg/console/input_keymap.go:188`), `SetShowKeymapHint()` field + setter (`status_footer.go:240`), scroll-region-aware rendering at `status_footer.go:731`, hint-toggle plumbing wired into REPL bootstrap. Footer hint row shows accurate, useful shortcuts per commit `d33db212`. |
| SP-116 | Multi-Instance Isolation | ✅ Implemented — git-repo auto-detection in `cmd/root.go` makes `.sprout/` isolation the default for repo-backed directories; bg processes scoped to config dir; layered config merges workspace overrides with global providers. Phases 1–4 shipped 2026-07-15 (`ac4d72e6`, `ef47144d`, `c7c4047b`, `99991ba2`, `c0602add`). |
| SP-118 | Daemon Multi-Window Session Isolation | ✅ Implemented (Phases 1–5 shipped 2026-07-15; Phase 6 partial — TODO.md sync landed, README + Settings UI deferred per AGENTS.md "no documentation" rule). Mode 2 (daemon) supports N parallel browser windows per user via `agentEnforceSingleSession` dispatch + `UserConnections` registry; Mode 1 (`sprout agent`) keeps single-active semantics. `daemon_multi_session` feature flag defaulted ON; rollback via `sprout config set daemon_multi_session=false`. `active_ws_count_by_user` metric exposed at `/api/ws-metrics`. |
| SP-119 | Workspace-aware Directory Resolution | ✅ Implemented — `automate.DirIn(workspaceDir)` helper threads workspace context through agent-tool and interface-handler paths so daemon-served workspaces find `<workspace>/automate/` instead of the daemon root. 3 phases shipped 2026-07-15 (`6608ecf3`, `aa2d05a9`). Out-of-scope follow-ups (~25 callsites across `pkg/agent/persistence.go`, `pkg/agent/skills.go`, `pkg/agent_tools/shell_native.go`, etc.) tracked under SP-091. |
| SP-120 | Codebase Organization & Test Infrastructure Cleanup | ✅ Implemented — Phase 1 + 2a/2b/2c + Phase 3 all shipped 2026-07-15. The 199-file cmd/ god package lost another ~2000 lines to a new pkg/cliui/ (terminal subscriber, tool/subagent display, turn stats). Tests/builds all clean. |
| SP-123 | User-Level Command Policies | ✅ Shipped — Phases 1–3 (2026-07-16). Unified command-policy layer with `Always Allow` / `Always Prompt` / `Always Deny` actions across the five fragmented pre-existing config surfaces; overrides `permissive`-mode auto-approval. |

## Pending

_Specs whose core work has shipped but whose bodies remain at the root as
living reference (per the policy established in `212044d8`). When a
retained spec's body is no longer needed, it can be deleted from the
root and the historical record is the git log. As of 2026-07-21, every
spec at the root has shipped — there is no actively in-flight work in
the root directory._

| Spec | Title | Status |
|------|-------|--------|
| SP-075 | [Large-File Decomposition](./SP-075-large-file-decomposition.md) | ✅ Phase 3 fully shipped 2026-07-16 — All 12 top-tier offenders (890-1500 lines) decomposed into 4-7 sibling files each (anchor + 3-6 siblings), all under 730 lines. Original `config.go` reduced to ~396 lines; `agent_workflow.go` 1519→3 lines; `tool_handlers_subagent.go` 1568→41 lines; plus 12 new splits in 2026-07. **Next-tier status (as of 2026-07-18):** All 5 next-tier candidates touched — `wasmshell/commands.go` (now split into 5 files, max 450); `generic_provider.go` (8 sibling files, max 602); `browser_rod.go` (5 files, max 668); `change_tracking_shell.go` (residual 464 + persist sibling + tests); `webui/src/components/Terminal.tsx` (683 → 576 lines via three new hooks: `useAvailableShells`, `useAttachableSessions`, `useVerticalDragResize` — each with dedicated test files, 28 new tests all green). 2026-07-18 SP-075-extension complete on Terminal.tsx; broader change-tracking split remains optional. |
| SP-112 | [Platform Parity — Resolve Stubbed Feature Gaps](./SP-112-platform-parity.md) | 🟢 Tier 1 + Tier 2 + SP-112-9 shipped (2026-07-20, `3ecd290a` + `3ab5c751` + `c7be8b5`). Tier 1: Windows Job Objects + process-group unification in `pkg/agent_tools/background_process_signal_windows.go`. Tier 2: WASM tool exclusion at registration time (`all_browse_url_wasm.go`, `all_codegraph_wasm.go` — SP-112-7 verified already shipping prior to this work). SP-112-3: `pid_alive` consolidated into `pkg/utils/pidalive` (eliminated triplicated `_windows.go` copies in webui/automate/service). SP-112-9: cross-platform CI matrix (windows-latest + macos-latest) + WASM tool-roster smoke test. Tier 4 (permanent WASM limitation docs) deferred — see spec body. |
| SP-113 | [Multi-Billing-Model Cost Tracking](./SP-113-multi-billing-cost-tracking.md) | 🟢 Implemented (Phases 1–4 shipped `4552363c` 2026-07-02 as SP-080, then renumbered 2026-07-05). `bab487da` post-merge cleanup: subagent double-debit fix, fleet budget isolation, CLI footer "included"/"free" annotations, ProviderTable billing column. Spec kept at root as living reference for future scope (subscription quota tracking, per-billing-type cost alerts, Ollama Cloud credits). |
| SP-114 | [Unify CLI and Steer Panel Command Execution](./SP-114-unify-command-execution.md) | 🟢 Phase 1 + Phase 2 shipped (`ab6c975e` + 2026-07-17). Phase 2: `POST /api/command/execute` (dedicated command surface, separate from `/api/query/steer`); WebUI `onSendCommand` handler in `ChatView.tsx` routes safe commands to the new endpoint with notification-based output. Destructive commands (`/commit`, `/clear`, `/exit`, `/init`, etc.) remain CLI-only. Long-output WS streaming deferred. |
| SP-124 | [LLM-Augmented Security Analysis](./SP-124-llm-security-analysis.md) | 🟢 Phase 1, 2, 3 shipped (2026-07-19) — backend `AnalyzeShellCommand` + cache + broker plumbing; WebUI dialog renders analysis panel with risk-tone badge (`SecurityApprovalDialog.tsx`); CLI picker renders analysis + Elevate option via `pkg/utils.SecurityAnalysisView` shared helper. |
| SP-124b | [Batch Security Analysis for Chained Commands](./SP-124b-batch-analysis.md) | 🟢 Phase 1 + Phase 2 shipped (2026-07-19 / 2026-07-20, `ad0c20d0` + `bb2464c6`) — `Chain` type + `ParseChain` (delegates to SP-122 `SplitChainedCommand`), `ChainedClassification` + `ClassifyChainedCommand`, `AnalyzeChain` with chain-aware prompt when `len(Subcommands) > 1`, normalized cache key under `sp-124b:v1:` namespace (collapses whitespace, preserves operators, separates `&&`/`||`/`;`/`|`). Phase 2: 10-subcommand cap → per-subcommand fallback, chain stepper in `SecurityApprovalDialog.tsx` with per-subcommand risk dots, CLI per-subcommand badge rendering. 1300-line test footprint across `pkg/agent/security_analyzer_sp124b_*_test.go`, `pkg/console/security_prompt_test.go`, `pkg/utils/security_analysis_view_test.go`, `webui/src/components/SecurityApprovalDialog.test.tsx`. |
| SP-125 | [Low-Context Mode (32K context support)](./SP-125-low-context-mode.md) | 🟢 Shipped (2026-07-20 / 2026-07-21, 14 sub-item commits). Core abstraction + 6 levers wired (`f43ffb07`, `344f2c8b`, `cbc031ee`, `a5cb2ef3`, `102ff7cb`, `a7fb45fb`, `751b81a4`, `fc927d28`, `6da4d466`, `a6663af0`, `4a4d34a4`, `0c9d1f53`). `/context` slash command ships at `0c9d1f53` (show / set full / set low / clear, aliases, tab-completion). Activation notice + model eligibility + integration tests + AGENTS.md size warning all included. Open non-goals: R3 (lite capability probe variant) and R4 (subagent LCM inheritance) — both deferred per TODO. |
| SP-126 | [Effective Context Cap (Honor `Config.MaxContextTokens` End-to-End)](./SP-126-effective-context-cap.md) | 🟢 Shipped (2026-07-20, `35c66b24`) — `ResolveEffectiveContextCap(cfg, nativeContextWindow)` in `pkg/configuration/context_profile.go:291` + `EffectiveContextCapErrorf` helper. `seed_provider.Info()` now applies the cap so seed's internal budget math receives the capped value; `seed_query.OnIteration` re-applies the cap defensively from `a.effectiveContextCap`. The `OnIteration` callback no longer clobbers the user's cap on turn 1+ (Bug 2 root cause). Activation notice printed to stderr when the user explicitly set a cap lower than the native window. Min cap = 1024 (mirrors existing `/max-context` validator). Regression tests at `pkg/agent/seed_provider_info_cap_test.go` + `pkg/agent/seed_query_oniteration_cap_test.go`. |
| SP-127 | [Promote Filesystem Gate to Gate-1](./SP-127-filesystem-gate-promotion.md) | 🔵 Scoping — fold the handler-side `FilesystemGate` (added in `fix/file-tools-filesystem-gate`) into `staticGateAutoApprove` so a single call site decides file access policy. Closes the `patch_structured_file` retrofit gap, removes the dual-mental-model wart, and centralizes path-tier classification. Migration plan M1–M4 covers the gate consolidation. **Phase 2 (Workflow `allowed_paths` runtime extensions)** adds cd-target validation, per-step allowed_paths overrides, effectiveCwd-aware resolver scope, symlink re-validation under allowed folders, and audit events for `allowed_path` hits. | |

## Future / On Hold

Parked or suspended — not scheduled. See [`future/`](./future/).

| Spec | Title | Reason |
|------|-------|--------|
| SP-007 | [Extend Configuration — Role-Based Configs](./future/SP-007-extend-config.md) | 🧊 On hold (parked 2026-06-14) — speculative; revisit only with evidence of user demand. |
| SP-080-desktop | [Desktop Release — Security, Compliance, Distribution Readiness](./future/SP-080-desktop-release-security.md) | 🔴 Suspended (2026-07-07). Desktop builds and CI disabled; electron deps removed from `package.json`. Re-enable only after explicit re-prioritization. |