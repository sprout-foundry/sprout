# Roadmap

Roadmap specifications for the sprout project. Each spec describes
a major architectural area, its current state, and open work.

Specs ship to [`./_completed/`](./_completed/) once their core work lands.
The root directory only contains specs still receiving active changes.

**Counts (as of 2026-07-02):** 72 shipped · 3 pending · 1 on hold.

## Shipped

Full spec bodies in [`_completed/`](./_completed/) (72 files).

| Spec | Title | Status |
|------|-------|--------|
| SP-001 | [Agent Core Architecture](./_completed/SP-001-agent-core.md) | ✅ Active (recently refactored) |
| SP-002 | [Configuration, Credentials & Providers](./_completed/SP-002-configuration.md) | ✅ Active |
| SP-003 | [Webui & Frontend Architecture](./_completed/SP-003-webui.md) | ✅ Active (under active development) |
| SP-004 | [Security, Validation & MCP](./_completed/SP-004-security.md) | ✅ Active |
| SP-005 | [Supporting Systems & Infrastructure](./_completed/SP-005-infrastructure.md) | ✅ Active |
| SP-008 | [Reliability Engineering — Concurrency & Observability](./_completed/SP-008-reliability-engineering.md) | ✅ Shipped (Tracks A + B complete 2026-06) |
| SP-009 | [Component Library Maturation — Storybook + @sprout/ui](./_completed/SP-009-component-library-maturation.md) | ✅ Implemented (Storybook + MDX docs + Chromatic visual regression; webui imports @sprout/ui as monorepo sibling) |
| SP-010 | [Editor Modernization](./_completed/SP-010-editor-modernization.md) | ✅ Implemented (EditorPane 2604→513 lines; EditorCore extracted; React.memo + 18 bug fixes) |
| SP-011 | [Terminal Parity & Bug Fixes](./_completed/SP-011-terminal-parity.md) | ✅ Shipped (all 3 phases complete 2026-06) |
| SP-013 | [Agent Settings Management Tool](./_completed/SP-013-agent-settings-skill.md) | ✅ Implemented (manage_settings tool registered; pkg/agent/settings_handler.go) |
| SP-014 | [Agent Terminal Sessions — Hidden PTY Routing + Background Mode](./_completed/SP-014-agent-terminal-sessions.md) | ✅ Implemented (Hidden PTY routing + background mode shipped) |
| SP-015 | [Cloud Platform Integration](./_completed/SP-015-cloud-platform.md) | ✅ Implemented (sprout-side; 2026-06-26) — R1–R7 complete in this repo. Cross-repo evolution lives in `../sprout-foundry` |
| SP-016 | [Embedding Index — Duplicate Detection & Semantic Search](./_completed/SP-016-embedding-duplicate-detection.md) | ✅ Implemented (core infrastructure complete; expanded scope in SP-016b) |
| SP-016b | [Expanded Embedding Index — Full Workspace Semantic Search](./_completed/SP-016b-expanded-embedding-index.md) | ✅ Shipped (backend complete 2026-06; minor SearchView.tsx UI gap) |
| SP-017 | [Settings Panel Rework — Scoped Collapsible Sections](./_completed/SP-017-settings-panel-rework.md) | ✅ Partially Implemented (scoped labels shipped; collapsible sections pending → see SP-101) |
| SP-018 | [Memory System](./_completed/SP-018-memory-system.md) | ✅ Implemented |
| SP-019 | [Multi-Chat Sessions](./_completed/SP-019-multi-chat-sessions.md) | ✅ Implemented |
| SP-020 | [Trace/Dataset Mode](./_completed/SP-020-trace-dataset-mode.md) | ✅ Implemented |
| SP-021 | [Self-Review Tool](./_completed/SP-021-self-review-tool.md) | ✅ Implemented |
| SP-022 | [Remote Provider Registry](./_completed/SP-022-remote-provider-registry.md) | ✅ Implemented |
| SP-022 | [Workspace Management & Project Detection](./_completed/SP-022-workspace-management.md) | ✅ Implemented (WorkspacePicker + WorkspacePane + LocationSwitcher + WorkspaceBar) |
| SP-023 | [In-Process Subagent Execution](./_completed/SP-023-in-process-subagents.md) | ✅ Active |
| SP-024 | [Context Management — File Read Optimization](./_completed/SP-024-context-management.md) | ✅ Phase 1-3, Phase 4 complete (Phase 2 deferred; tree-sitter in SP-025) |
| SP-025 | [Tree-Sitter Integration — Real AST for Multi-Language Symbol Extraction](./_completed/SP-025-tree-sitter-integration.md) | ✅ Shipped (all 5 phases complete 2026-06) |
| SP-026 | [Coordinator Persona (formerly "Executive Assistant")](./_completed/SP-026-executive-assistant.md) | ✅ Implemented (renamed 2026-06-03, see commit `516a9d41`) |
| SP-027 | [Persistent Context & Conversational Memory](./_completed/SP-027-persistent-context.md) | ✅ Shipped (all 4 phases complete 2026-06) |
| SP-039 | [SP-039 — UI Consolidation Decision](./_completed/SP-039-DECISION.md) | ✅ Decision made |
| SP-039 | [Component Categorization](./_completed/SP-039-component-categorization.md) | (supporting doc — see linked spec) |
| SP-045 | [WASM Build Feature Parity](./_completed/SP-045-wasm-feature-parity.md) | ✅ Shipped (Tiers 1-3 complete 2026-06) |
| SP-046 | [Browser-Primary Workspace Sync Model](./_completed/SP-046-workspace-sync-model.md) | ✅ Shipped (all 5 numbered items complete 2026-06) |
| SP-048 | [CLI Delight — Terminal UX Polish](./_completed/SP-048-cli-delight.md) | ✅ Partially Implemented (status footer + glyph vocabulary shipped; tool timeline + silence-fill pending → see SP-101) |
| SP-049 | [Shell Permission Overhaul — User-Configurable Policy & Headless Hardening](./_completed/SP-049-shell-permission-overhaul.md) | ✅ Implemented (Phases 3a–3d complete) |
| SP-050 | [Orchestrator Persona Collapse — One Persona, Configurable Git-Write](./_completed/SP-050-orchestrator-persona-collapse.md) | ✅ Implemented |
| SP-051 | [Depth-Aware Subagent UI — Visible Nesting in the CLI](./_completed/SP-051-depth-aware-subagent-ui.md) | ✅ Implemented |
| SP-053 | [WebUI CLI Parity — Persona/Depth, Live Tools, Cost Footer](./_completed/SP-053-webui-cli-parity.md) | ✅ Implemented |
| SP-054 | [LSP Language Coverage Expansion](./_completed/SP-054-lsp-language-coverage.md) | ✅ Shipped (all 3 phases complete 2026-06) |
| SP-055 | [CLI Pinned Input — Always-On Steering Panel](./_completed/SP-055-cli-pinned-input.md) | ✅ Shipped — Phases 1/2/3 + 3b (done-queue mode) + 3c (UTF-8) + OPOST fix. |
| SP-056 | [CLI Reasoning Fold — Collapsed Thinking Indicator](./_completed/SP-056-cli-reasoning-fold.md) | ✅ Implemented (2026-06-30) |
| SP-057 | [CLI Output Consistency — Glyph Migration & Unified Picker](./_completed/SP-057-cli-output-consistency.md) | ✅ Shipped (all 5 phases, 2026-05-25) |
| SP-058 | [Selective Grammar Embedding for WASM and Daemon](./_completed/SP-058-selective-grammar-embed.md) | ✅ Implemented (Daemon binary 149 MB per 899d667f; 22 MB below 171 MB target) |
| SP-059 | [Subagent ↔ Primary Interaction Overhaul + Delegate Retirement](./_completed/SP-059-subagent-interaction.md) | ✅ Implemented (Phases 1–6 complete; delegate tool retired; audited 2026-06-27) |
| SP-059 | [SP-059-6a — Delegate Feature Porting Review](./_completed/SP-059-6a-review.md) | (supporting doc — see linked spec) |
| SP-060 | [Desktop App — Per-Workspace Server Mode](./_completed/SP-060-desktop-serve.md) | ✅ Implemented (Phase A + Phase B shipped and verified) |
| SP-061 | [Remove Static Embedding Provider, Consolidate on ONNX](./_completed/SP-061-remove-static-embeddings.md) | ✅ Implemented (Static embedding provider removed via SP-091-2) |
| SP-062 | [CLI-Native Background Shell Execution](./_completed/SP-062-cli-background-shell.md) | ✅ Implemented (BackgroundProcessManager wired into shell dispatch) |
| SP-063 | [Destructive-App Denylist — Pre-Click Gate for Computer-Use Actions](./_completed/SP-063-destructive-denylist-design.md) | (supporting doc — see linked spec) |
| SP-063 | [Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent](./_completed/SP-063-computer-use-persona.md) | ✅ Implemented — all safety gates shipped as of 2026-06-30 (including gate 4h destructive-app denylist) |
| SP-063 | [Panic Key — Emergency Stop for Computer-Use Action Loops](./_completed/SP-063-panic-key-design.md) | (supporting doc — see linked spec) |
| SP-064 | [Automate CLI — Status, Stop, Logs](./_completed/SP-064-automate-cli-monitoring.md) | ✅ Implemented (sprout automate status/stop/logs) |
| SP-065 | [WebUI Automations Panel](./_completed/SP-065-automate-webui-panel.md) | ✅ Implemented (live WS event stream; commit 4f0a81c5) |
| SP-066 | [Never-Ending Context — Substitution-First Context Management, Hierarchical Rollups, and Embedded Memory Recall](./_completed/SP-066-never-ending-context.md) | ✅ Substantially Shipped (Phase 3d deferred) |
| SP-067 | [Automate Workflow Completion Injection](./_completed/SP-067-automate-completion-injection.md) | ✅ Implemented (2026-06-06) |
| SP-068 | [Security Check Consolidation — One Risk Scale, One Resolver, One Broker](./_completed/SP-068-security-check-consolidation.md) | ✅ Implemented (Phases 1–3 shipped: single resolver, single broker, sprout explain) |
| SP-069 | [Pull Request Creation — Close the "agent did the work, now what?" Gap](./_completed/SP-069-pull-request-creation.md) | ✅ Implemented |
| SP-070 | [Agent Completion Notifications — Tell the User When It's Their Turn](./_completed/SP-070-completion-notifications.md) | ✅ Implemented |
| SP-071 | [Conversation Rewind & Edit-and-Resend — Undo a Wrong Turn](./_completed/SP-071-conversation-rewind.md) | ✅ Implemented |
| SP-072 | [Per-Hunk Diff Approval — Optional Approve-Before-Apply for Agent Edits](./_completed/SP-072-diff-approval.md) | ✅ Implemented |
| SP-073 | [Cooperative Cancellation — Thread Context So Stop Actually Aborts](./_completed/SP-073-cooperative-cancellation.md) | ✅ Implemented (zero TODO(SP-034-1c) markers remain; all 10 sites threaded) |
| SP-074 | [Finish the Tool-Registry Migration — Retire the Dual-Dispatch Shim](./_completed/SP-074-tool-registry-migration.md) | ✅ Shipped (Phases 1–4 complete; 2026-06-26) |
| SP-076 | [WebUI Streaming Fix + Verbosity Modes](./_completed/SP-076-webui-streaming-verbosity.md) | ✅ Implemented (2026-06-26) |
| SP-077 | [ChangeTracker Reverts Committed Work During Git Operations](./_completed/SP-077-changetracker-reverts-committed-work.md) | ✅ Implemented (Phase 1 + Phase 2) |
| SP-078 | [Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion](./_completed/SP-078-steer-panel-ux-parity.md) | ✅ Implemented (2026-06-30; Phases 1–4 complete) |
| SP-079 | [Migrate Stub Tool Handlers off the Legacy `*Agent` Path](./_completed/SP-079-migrate-stub-tool-handlers.md) | ✅ Implemented (2026-06-30) |
| SP-080 | [Type the Unknown-Tool Error in ToolRegistry](./_completed/SP-080-type-unknown-tool-error.md) | ✅ Implemented (2026-06-30) |
| SP-081 | [Delete the Dead `pkg/tools/global.go` Executor](./_completed/SP-081-delete-dead-global-executor.md) | ✅ Implemented (2026-06-30) |
| SP-082 | [Preserve Key Insertion Order in Structured File Tools](./_completed/SP-082-preserve-structured-file-key-order.md) | ✅ Implemented (2026-06-30) — supersedes the original SP-066 key-order proposal |
| SP-083 | [Cross-Session Search — Find Past Conversations by Content](./_completed/SP-083-cross-session-search.md) | ✅ Implemented (2026-06-30) |
| SP-084 | [Export Sessions to Shareable Markdown / HTML](./_completed/SP-084-export-session-markdown.md) | ✅ Implemented (2026-06-30) |
| SP-085 | [Cost Analytics Dashboard — Model / Provider / Day Breakdown](./_completed/SP-085-cost-analytics-dashboard.md) | ✅ Implemented (2026-06-30) |
| SP-086 | [Skill Install — Pull Skills from Git, URLs, and Registries](./_completed/SP-086-skill-install-command.md) | ✅ Implemented (2026-06-30) |
| SP-087 | [SP-087 Acceptance Report](./_completed/SP-087-acceptance.md) | (supporting doc — see linked spec) |
| SP-087b | [Full Playwright Coverage of the WebUI](./_completed/SP-087b-full-playwright-webui-coverage.md) | ✅ Implemented (2026-06-30; acceptance criterion 3 partial — trace/video/screenshot config deferred, see SP-087-acceptanc |

## Pending

Specs still in flight (3 files). When a spec's core work
ships, it moves to [`_completed/`](./_completed/).

| Spec | Title | Status |
|------|-------|--------|
| SP-012 | [UX Polish](./SP-012-ux-polish.md) | ⚠️ ~90% Shipped — Notification center, reduced-motion, inline tool badges, MAX_PANES, sidebar persistence, mobile responsive, loading skeletons, panel animations shipped; remaining: ARIA gaps in FileTree (role="treeitem"/aria-expanded) and ChatPanel (role="log"), global `:focus-visible` styles, `notificationBus.markAllRead()` |
| SP-075 | [Large-File Decomposition](./SP-075-large-file-decomposition.md) | ⚠️ In Progress — Phase 1 (config + cmd) and Phase 2 (agent core) substantially shipped 2026-06; Phase 3 (providers + web) shipped for several files. Original 2833-line `config.go` reduced to ~396 lines; `agent_workflow.go` 1519→3 lines; `tool_handlers_subagent.go` 1568→41 lines. **Remaining files over 600-line target:** `steer_input.go` 1313, `go_adapter.go` 1188, `models.go` 1121, `settings_api_put.go` 1094, `client.go` 1060, `client_context.go` 1011, `tool_handlers_subagent_spawn.go` 999, `Terminal.tsx` 780, `agent_modes.go` 732, `input_core.go` 715, `generic_provider.go` 669. |
| SP-080 | [Multi-Billing-Model Cost Tracking](./SP-080-multi-billing-cost-tracking.md) | 📋 Planned — Design complete. Three billing models (pay-per-token, subscription, free) with dual-cost tracking (charged vs token value). Fleet budget isolation, per-billing-type dashboard breakdown. 4 phases. |
| SP-105 | [CLI Interactive Panels — Settings Browser & Usage Dashboard](./SP-105-cli-interactive-panels.md) | 🔵 Proposed — `/settings` interactive browser + `/usage` visual dashboard |

## Future / On Hold

Parked pending real demand — not scheduled. See [`future/`](./future/).

| Spec | Title | Reason |
|------|-------|--------|
| SP-007 | [Extend Configuration — Role-Based Configs](./future/SP-007-extend-config.md) | 🧊 On hold (parked 2026-06-14) — speculative; revisit only with evidence of user demand. See roadmap/00-INDEX.md "Future  |