# Roadmap

Roadmap specifications for the sprout project. Each spec describes
a major architectural area, its current state, and open work.

Specs land at the root until core work ships; once shipped, the spec body
lives in git history (no per-spec archive file). The root directory contains
only specs still receiving active changes or retained as living reference.

**Counts (as of 2026-08-17):** 101 shipped · 15 pending · 2 parked in `future/`.

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
| SP-129 | Provider Pricing Automation — Enrichment Fixes + Agent-Driven Audit | ✅ Implemented (2026-07-26) — enrichment pipeline fixes (ZAI $0 pricing, missing DeepSeek models, negative sentinels, OpenAI date matching, meta-model filtering); DeepSeek pricing corrected to official docs; `cmd/audit_pricing` verification tool with verified manifest; `pricing-audit-agent.yml` daily agent-driven workflow (drift-gated fast path); agent successfully created and merged PR #27 with MiniMax/Mistral/OpenAI corrections. 41 models verified, 0 drift. |

## Pending

_Specs whose core work has shipped but whose bodies remain at the root as
living reference (per the policy established in `212044d8`). When a
retained spec's body is no longer needed, it can be deleted from the
root and the historical record is the git log. As of 2026-07-21, every
spec at the root had shipped; SP-133 (added 2026-08-04) is the first
specified-but-unstarted entry since._

| Spec | Title | Status |
|------|-------|--------|
| SP-138 | [Crash-Safe Session Persistence & Interrupted-Turn Recovery](./SP-138-crash-safe-session-recovery.md) | 📋 Specified (not started) — 5-phase plan: atomic session writes + `.bak` generation; per-session append-only turn journal (WAL) with iteration-boundary appends; load-time journal replay + dangling-tool-call repair + interrupted-state marking; crash-detection UX in CLI/WebUI session lists; SIGTERM/SIGHUP graceful-save hardening. Root causes verified in code: `autoSaveState` only runs in the `ProcessQueryWithContinuity` defer (`conversation.go:52`), `SaveStateScoped` writes non-atomically (`persistence_message.go:141`), no orphan-repair exists, no interrupted-session notion anywhere. |
| SP-075 | [Large-File Decomposition](./SP-075-large-file-decomposition.md) | ✅ Phase 3 fully shipped 2026-07-16 — All 12 top-tier offenders (890-1500 lines) decomposed into 4-7 sibling files each (anchor + 3-6 siblings), all under 730 lines. Original `config.go` reduced to ~396 lines; `agent_workflow.go` 1519→3 lines; `tool_handlers_subagent.go` 1568→41 lines; plus 12 new splits in 2026-07. **Next-tier status (as of 2026-07-18):** All 5 next-tier candidates touched — `wasmshell/commands.go` (now split into 5 files, max 450); `generic_provider.go` (8 sibling files, max 602); `browser_rod.go` (5 files, max 668); `change_tracking_shell.go` (residual 464 + persist sibling + tests); `webui/src/components/Terminal.tsx` (683 → 576 lines via three new hooks: `useAvailableShells`, `useAttachableSessions`, `useVerticalDragResize` — each with dedicated test files, 28 new tests all green). 2026-07-18 SP-075-extension complete on Terminal.tsx; broader change-tracking split remains optional. |
| SP-112 | [Platform Parity — Resolve Stubbed Feature Gaps](./SP-112-platform-parity.md) | 🟢 Tier 1 + Tier 2 + SP-112-9 shipped (2026-07-20, `3ecd290a` + `3ab5c751` + `c7be8b5`). Tier 1: Windows Job Objects + process-group unification in `pkg/agent_tools/background_process_signal_windows.go`. Tier 2: WASM tool exclusion at registration time (`all_browse_url_wasm.go`, `all_codegraph_wasm.go` — SP-112-7 verified already shipping prior to this work). SP-112-3: `pid_alive` consolidated into `pkg/utils/pidalive` (eliminated triplicated `_windows.go` copies in webui/automate/service). SP-112-9: cross-platform CI matrix (windows-latest + macos-latest) + WASM tool-roster smoke test. Tier 4 (permanent WASM limitation docs) deferred — see spec body. |
| SP-113 | [Multi-Billing-Model Cost Tracking](./SP-113-multi-billing-cost-tracking.md) | 🟢 Implemented (Phases 1–4 shipped `4552363c` 2026-07-02 as SP-080, then renumbered 2026-07-05). `bab487da` post-merge cleanup: subagent double-debit fix, fleet budget isolation, CLI footer "included"/"free" annotations, ProviderTable billing column. Spec kept at root as living reference for future scope (subscription quota tracking, per-billing-type cost alerts, Ollama Cloud credits). |
| SP-114 | [Unify CLI and Steer Panel Command Execution](./SP-114-unify-command-execution.md) | 🟢 Phase 1 + Phase 2 + Phase 2c + Phase 2d shipped (`ab6c975e` 2026-07-17, `d0f2ee56` 2026-07-21 server streaming, `44e4fe4f` 2026-07-22 web UI streaming). `POST /api/command/execute` dedicated command surface; stdout streams over the chat WebSocket via `EventTypeCommandOutput` + UTF-8-safe `streamPipeChunks`; `useCommandOutput` hook + `CommandOutputPanel` (`webui/src/components/CommandOutputPanel.tsx`) integrate into `ChatView.tsx`. Destructive commands (`/commit`, `/clear`, `/exit`, `/init`, etc.) remain CLI-only. |
| SP-124 | [LLM-Augmented Security Analysis](./SP-124-llm-security-analysis.md) | 🟢 Phase 1, 2, 3 shipped (2026-07-19) — backend `AnalyzeShellCommand` + cache + broker plumbing; WebUI dialog renders analysis panel with risk-tone badge (`SecurityApprovalDialog.tsx`); CLI picker renders analysis + Elevate option via `pkg/utils.SecurityAnalysisView` shared helper. |
| SP-121 | Unified Product Experience | ✅ All 6 phases shipped — editor is now the unified product surface. (Spec body deleted; see git log.) |
| SP-121-7 | [Repo Click → GitHub Content Flow](./_active/SP-121-7-repo-content-flow.md) | ✅ Browser-side GitHub clone via isomorphic-git + lightning-fs. Multi-repo gitClient service (clone/status/log/branch/checkout/read/diff/add/commit/push). File tree with VFS bridge to WASM shell. Phase 7 (agent integration) shipped — agentGitTools.ts + agentGitToolBridge.ts (Go↔JS bridge for 13 git tools). Playwright E2E tests deferred. |
| SP-121-8 | [Git UI Polish](./SP-121-8-git-ui-polish.md) | ✅ Implemented (audited 2026-08-02) — All 5 items shipped: README preview (ReactMarkdown + `isReadmeFile`), Push/Pull buttons with git-op feedback, branch chip checkout with VFS re-bridge, file/folder creation (`handleCreateFile`/`handleCreateFolder` wired to `RepoFileTree`), ZIP download (`downloadRepoAsZip` + button). 12 tests in `RepoFileTree.test.tsx`, 16 tests in `RepoDetailPage.test.tsx`. Only remaining spec item ("Open in File Browser" desktop) blocked by SP-080-desktop suspension. |
| SP-121-9 | [First-Run UX + Onboarding](./SP-121-9-first-run-ux.md) | ✅ Implemented (audited 2026-08-02) — All 3 items shipped: Onboarding screen with 3 tabs (Import URL, Select Repo, New Repo), GitHub URL import (`parseGitHubUrl` handles https/SSH/shorthand), Create new repo dialog (local git init + optional README). `RepoOnboarding.tsx` renders when `currentView === 'repodetail' && !selectedRepo`. 21 tests in `RepoOnboarding.test.tsx`, 10 tests in `repoDownload.test.ts`. |
| SP-121-11 | [Git Provider OAuth + Multi-Repo](./SP-121-11-github-oauth-and-multi-repo.md) | 🟡 Draft — not implemented. Index previously listed as shipped; audit (2026-07-27) found zero OAuth, multi-repo, or Integrations UI components. |
| SP-CLOUD | Cloud-First WebUI | ✅ All 8 sub-specs verified. (Spec body deleted; see git log.) |
| SP-124b | [Batch Security Analysis for Chained Commands](./SP-124b-batch-analysis.md) | 🟢 Phase 1 + Phase 2 shipped (2026-07-19 / 2026-07-20, `ad0c20d0` + `bb2464c6`) — `Chain` type + `ParseChain` (delegates to SP-122 `SplitChainedCommand`), `ChainedClassification` + `ClassifyChainedCommand`, `AnalyzeChain` with chain-aware prompt when `len(Subcommands) > 1`, normalized cache key under `sp-124b:v1:` namespace (collapses whitespace, preserves operators, separates `&&`/`||`/`;`/`|`). Phase 2: 10-subcommand cap → per-subcommand fallback, chain stepper in `SecurityApprovalDialog.tsx` with per-subcommand risk dots, CLI per-subcommand badge rendering. 1300-line test footprint across `pkg/agent/security_analyzer_sp124b_*_test.go`, `pkg/console/security_prompt_test.go`, `pkg/utils/security_analysis_view_test.go`, `webui/src/components/SecurityApprovalDialog.test.tsx`. |
| SP-125 | [Low-Context Mode (32K context support)](./SP-125-low-context-mode.md) | 🟢 Shipped (2026-07-20 / 2026-07-21, 14 sub-item commits). Core abstraction + 6 levers wired (`f43ffb07`, `344f2c8b`, `cbc031ee`, `a5cb2ef3`, `102ff7cb`, `a7fb45fb`, `751b81a4`, `fc927d28`, `6da4d466`, `a6663af0`, `4a4d34a4`, `0c9d1f53`). `/context` slash command ships at `0c9d1f53` (show / set full / set low / clear, aliases, tab-completion). Activation notice + model eligibility + integration tests + AGENTS.md size warning all included. Open non-goals: R3 (lite capability probe variant) and R4 (subagent LCM inheritance) — both deferred per TODO. |
| SP-126 | [Effective Context Cap (Honor `Config.MaxContextTokens` End-to-End)](./SP-126-effective-context-cap.md) | 🟢 Shipped (2026-07-20, `35c66b24`) — `ResolveEffectiveContextCap(cfg, nativeContextWindow)` in `pkg/configuration/context_profile.go:291` + `EffectiveContextCapErrorf` helper. `seed_provider.Info()` now applies the cap so seed's internal budget math receives the capped value; `seed_query.OnIteration` re-applies the cap defensively from `a.effectiveContextCap`. The `OnIteration` callback no longer clobbers the user's cap on turn 1+ (Bug 2 root cause). Activation notice printed to stderr when the user explicitly set a cap lower than the native window. Min cap = 1024 (mirrors existing `/max-context` validator). Regression tests at `pkg/agent/seed_provider_info_cap_test.go` + `pkg/agent/seed_query_oniteration_cap_test.go`. |
| SP-127 | [Promote Filesystem Gate to Gate-1](./SP-127-filesystem-gate-promotion.md) | 🟢 Shipped M1–M4 (`a06a3f8a` M1, `1acff1cb`–`ac969831` M2, `91853ec0` M3 audit/docs, `779999e1` M4 cleanup). Path-tier classification unified into Gate 1 (`classifyFileAccess`); all 6 file-touching handlers migrated to `PrecheckFileAccess`; old `withFilesystemApproval`/`FilesystemGate` machinery removed. Audit trail, docs, conformance tests, and integration tests all updated. Follow-on: SP-068 path-tier `RiskCategory` exposed for UnifiedRiskResolver (`868822e5`); Phase 2.7 `AllowedPathHit` audit event (`7869287d`). |

| SP-130 | [Home-Directory Workspace Gate](./SP-130-home-workspace-gate.md) | 🟢 Implemented (2026-07-31) — Phases 1–4 shipped. Backend: `workspace_gate.go` (home detection + consent store), `handleAPIWorkspaceGet` gates `needs_workspace_selection` on home-without-consent, `FindProjectsInDirectory` removed from the hot path (kills the recursive home walk that triggered macOS TCC "access Apple Music" prompts), `setClientWorkspaceRoot` defense-in-depth, server startup restores most-recent workspace. Frontend: `WorkspaceGateModal` blocking overlay with explicit home-consent flow, broken `extractHomeDir` heuristic replaced with backend `home_dir`. Service plist/unit comments updated. CLI interactive mode warns when CWD is home. |
| SP-131 | [Agent Creation & Event Publishing Dedup](./SP-131-agent-creation-dedup.md) | 🟢 Phases 0–5, 7 shipped — `setupWebUIAgent`/`rearmWebUIAgent` helpers (P1, P7), `PublishEvent` dedup (P2), regression tests (P3), `SafeGo` helper (P4), `captureCommandOutput` helper (P5). Phase 6 (webui lock boilerplate) investigated and deferred — patterns too varied for clean abstraction. |
| SP-132 | [WebUI API Consistency & Boilerplate Elimination](./SP-132-webui-api-consistency.md) | 🟢 Phases 1–4, 6–7 shipped — `requireMethod` (P1, 105 sites), `writeJSONError` consolidated (P2), bare `json.NewEncoder` migrated (P3, 165 sites), `http.Error`→`writeJSONErr` (P4, 259 sites), `cs.Agent` locking audit clean (P6), 9 goroutines recovered (P7). Phase 5 (file splitting) deferred. |
| SP-133 | [Config / State / Cache / Secret Separation](./SP-133-config-state-separation.md) | 📋 Specified (2026-08-04), not started — splits `~/.sprout` into four category roots (config / state / data+cache / credentials). Closes the `$HOME`-is-a-workspace aliasing class that made a global `embedding_index.enabled` read as a per-workspace opt-in and index the whole home directory at daemon startup. Builds on the shipped `workspace.json` filename split and the `.sprout` project-marker down-weight. 6 phases; credentials move last, isolated, behind a marker file. |
| SP-134 | [GPU Acceleration for Native macOS Embeddings (CoreML EP)](./SP-134-gpu-macos-embeddings.md) | 📋 Specified (2026-08-06), not started — native Go binary embedding is CPU-only (~6.5 units/s q4, ~30 min cold build). Browser WebGPU path exists and default flipped to `webgpu` (2026-08-06). Local graph analysis is decisive: the ONNX exports (q4/fp16) contain MatMulNBits / MultiHeadAttention / RotaryEmbedding / SimplifiedLayerNormalization ops with no CoreML EP builders, so even fixed-shape bucketing partitions at every transformer block (the prior 291-partition failure). Path is a Phase-0 CoreML spike with go/no-go gates, then a CoreML-friendly re-export (unfused attention, plain LayerNorm) if it fails as expected. |
| SP-135 | [Code-Specific Embedding Model (smaller than Gemma, ~100k repos)](./SP-135-code-embedding-model.md) | 📋 Specified (2026-08-06), not started — verdict: 100k repos (~0.9B tokens) is NOT enough to beat Gemma-300M from scratch, but GO on adopt-then-fine-tune of a 137M code model (CodeRankEmbed / CodeSage-small-v2) for code retrieval + duplicate detection, with Gemma kept for conversation/NL semantics. Dual-model routing, mandatory threshold re-measurement, 3 phases. |
| SP-136 | [Daemon-First Architecture — Shared Process Model](./SP-136-daemon-first-architecture.md) | 📋 Specified (2026-08-07), not started — consolidates four native execution paths (interactive CLI, non-interactive CLI, daemon, service) into one daemon-mediated model. Phased: P0 file-lock index writes (urgent corruption fix), P1 daemon multi-workspace hardening, P2 lazy daemon auto-start, P3 embedding service via daemon socket, P4 full CLI-on-daemon. Each phase ships independently and is revertable; in-process fallback retained as safety net. |
| SP-137 | [Embedding Index Resource Bounds & Gate Consistency](./SP-137-embedding-resource-bounds.md) | ✅ Implemented (2026-08-16) — enforces the experimental opt-in gate on the daemon embedding socket and the embedding_index tool's manager acquisition; adds a MemAvailable floor that stops index builds cleanly when host memory runs low; auto-started daemons raise their own oom_score_adj to be preferred OOM victims over user processes. |

## Future / On Hold

Parked or suspended — not scheduled. See [`future/`](./future/).

| Spec | Title | Reason |
|------|-------|--------|
| SP-007 | [Extend Configuration — Role-Based Configs](./future/SP-007-extend-config.md) | 🧊 On hold (parked 2026-06-14) — speculative; revisit only with evidence of user demand. |
| SP-080-desktop | [Desktop Release — Security, Compliance, Distribution Readiness](./future/SP-080-desktop-release-security.md) | 🔴 Suspended (2026-07-07). Desktop builds and CI disabled; electron deps removed from `package.json`. Re-enable only after explicit re-prioritization. |