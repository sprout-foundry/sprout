# Roadmap

Roadmap specifications for the sprout project. Each spec describes a major
architectural area, its current state, and open work.

## Implemented

| Spec | Title | Status |
|------|-------|--------|
| SP-001 | [Agent Core Architecture](./SP-001-agent-core.md) | ✅ Active |
| SP-002 | [Configuration, Credentials & Providers](./SP-002-configuration.md) | ✅ Active |
| SP-003 | [Webui & Frontend Architecture](./SP-003-webui.md) | ✅ Active |
| SP-004 | [Security, Validation & MCP](./SP-004-security.md) | ✅ Active |
| SP-005 | [Supporting Systems & Infrastructure](./SP-005-infrastructure.md) | ✅ Active |
| SP-014 | [Agent Terminal Sessions — Hidden PTY Routing](./SP-014-agent-terminal-sessions.md) | ✅ Implemented |
| SP-016 | [Embedding Index — Duplicate Detection & Semantic Search](./SP-016-embedding-duplicate-detection.md) | ✅ Implemented |
| SP-018 | [Memory System](./SP-018-memory-system.md) | ✅ Implemented |
| SP-019 | [Multi-Chat Sessions](./SP-019-multi-chat-sessions.md) | ✅ Implemented |
| SP-020 | [Trace/Dataset Mode](./SP-020-trace-dataset-mode.md) | ✅ Implemented |
| SP-021 | [Self-Review Tool](./SP-021-self-review-tool.md) | ✅ Implemented |
| SP-022 | [Remote Provider Registry](./SP-022-remote-provider-registry.md) | ✅ Implemented |
| SP-024 | [Context Management — File Read Optimization](./SP-024-context-management.md) | ✅ Implemented |
| SP-055 | [CLI Pinned Input — Always-On Steering Panel](./SP-055-cli-pinned-input.md) | ✅ Shipped |
| SP-057 | [CLI Output Consistency — Glyph Migration & Unified Picker](./SP-057-cli-output-consistency.md) | ✅ Shipped |
| SP-050 | [Orchestrator Persona Collapse — One Persona, Configurable Git-Write](./SP-050-orchestrator-persona-collapse.md) | ✅ Implemented |
| SP-051 | [Depth-Aware Subagent UI — Visible Nesting in the CLI](./SP-051-depth-aware-subagent-ui.md) | ✅ Implemented |
| SP-053 | [WebUI CLI Parity — Persona/Depth, Live Tools, Cost Footer](./SP-053-webui-cli-parity.md) | ✅ Implemented |
| SP-049 | [Shell Permission Overhaul — Tiered Allow-Lists & User Policy](./SP-049-shell-permission-overhaul.md) | ✅ Implemented |
| SP-059 | [Subagent ↔ Primary Interaction Overhaul](./SP-059-subagent-interaction.md) | ✅ Implemented |
| SP-069 | [Pull Request Creation](./SP-069-pull-request-creation.md) | ✅ Implemented |
| SP-070 | [Agent Completion Notifications](./SP-070-completion-notifications.md) | ✅ Implemented |
| SP-071 | [Conversation Rewind & Edit-and-Resend](./SP-071-conversation-rewind.md) | ✅ Implemented |
| SP-072 | [Per-Hunk Diff Approval — Approve-Before-Apply](./SP-072-diff-approval.md) | ✅ Implemented |
| SP-074 | [Finish the Tool-Registry Migration](./SP-074-tool-registry-migration.md) | ✅ Implemented (Phases 1–4 complete) |
| SP-060 | [Desktop App — Per-Workspace Server Mode](./SP-060-desktop-serve.md) | ✅ Implemented |
| SP-026 | [Coordinator Persona (formerly Executive Assistant)](./SP-026-executive-assistant.md) | ✅ Implemented (renamed 2026-06-03; legacy aliases preserved) |
| SP-067 | [Automate Workflow Completion Injection](./SP-067-automate-completion-injection.md) | ✅ Implemented |
| SP-076 | [WebUI Streaming Fix + Verbosity Modes](./SP-076-webui-streaming-verbosity.md) | ✅ Implemented |
| SP-015 | [Cloud Platform Integration](./SP-015-cloud-platform.md) | ✅ Implemented (sprout-side R1–R7 complete; cross-repo evolution in [`../sprout-foundry`](../sprout-foundry/AGENTS.md)) |
| SP-063 | [Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent](./SP-063-computer-use-persona.md) | ✅ Implemented (off-by-default, 9-layer safety stack, WebUI settings; panic key + destructive denylist explicitly deferred as design questions, not remaining work) |
| SP-006 | Delegate Tool — In-Process Agent Delegation | ⚰️ Superseded by [SP-059](./SP-059-subagent-interaction.md) (2026-05-31) |
| SP-009 | [Component Library Maturation — Publish & Storybook](./SP-009-component-library-maturation.md) | ✅ Implemented (Storybook + MDX docs + Chromatic; webui imports `@sprout/ui`) |
| SP-010 | [Editor Modernization](./SP-010-editor-modernization.md) | ✅ Implemented (EditorPane 2604→513 lines; EditorCore extracted; React.memo + 18 bug fixes) |
| SP-013 | [Agent Settings Management Tool](./SP-013-agent-settings-skill.md) | ✅ Implemented (`manage_settings` tool registered; `pkg/agent/settings_handler.go`) |
| SP-022 | [Workspace Management & Project Detection](./SP-022-workspace-management.md) | ✅ Implemented (WorkspacePicker + WorkspacePane + LocationSwitcher + WorkspaceBar) |
| SP-023 | [In-Process Subagent Execution](./SP-023-in-process-subagents.md) | ✅ Implemented (`pkg/agent/subagent_runner*.go`; consumed by SP-059 + orchestrator) |
| SP-062 | [CLI-Native Background Shell Execution](./SP-062-cli-background-shell.md) | ✅ Implemented (`BackgroundProcessManager` wired into shell dispatch) |
| SP-064 | [Automate CLI — Status, Stop, Logs](./SP-064-automate-cli-monitoring.md) | ✅ Implemented (`sprout automate status/stop/logs`) |
| SP-065 | [WebUI Automations Panel](./SP-065-automate-webui-panel.md) | ✅ Implemented (live WS event stream; commit 4f0a81c5) |
| SP-068 | [Security Check Consolidation — One Risk Scale, One Resolver, One Broker](./SP-068-security-check-consolidation.md) | ✅ Implemented (Phases 1–3 shipped: single resolver, single broker, `sprout explain`) |
| SP-073 | [Cooperative Cancellation — Stop Actually Aborts](./SP-073-cooperative-cancellation.md) | ✅ Implemented (zero `TODO(SP-034-1c)` markers remain; all 10 sites threaded) |
| SP-066 | [Never-Ending Context — Substitution-First Compaction, Hierarchical Rollups, Embedded Recall](./SP-066-never-ending-context.md) | ✅ Substantially Shipped (Phases 1-3 + 3b/3c shipped 2026-06-08; Phase 3d deferred → see SP-082 for related work) |

## In Progress

_(none)_

## Proposed

| Spec | Title | Status |
|------|-------|--------|
| SP-008 | [Reliability Engineering — Concurrency & Observability](./SP-008-reliability-engineering.md) | ⚠️ Partially Shipped (concurrency tests, semantic-recall instrumentation, vision metrics shipped 2026-06; structured-log context propagation and typed error hierarchy pending → see SP-094) |
| SP-011 | [Terminal Parity & Bug Fixes](./SP-011-terminal-parity.md) | 📋 Proposed |
| SP-012 | [UX Polish](./SP-012-ux-polish.md) | ⚠️ Partially Shipped (status footer, glyph vocabulary, notification center, reduced-motion shipped 2026-06; mobile, scroll-detection pending) |
| SP-016b | [Expanded Embedding Index — Full Workspace Semantic Search](./SP-016b-expanded-embedding-index.md) | 📋 Proposed |
| SP-017 | [Settings Panel Rework — Scoped Collapsible Sections](./SP-017-settings-panel-rework.md) | ✅ Partially Implemented (scoped labels + collapsible Advanced section shipped; performance/OCR/commit-review tabs merged → see SP-101) |
| SP-025 | [Tree-Sitter Integration — Real AST](./SP-025-tree-sitter-integration.md) | 📋 Spec (no code) |
| SP-027 | [Persistent Context & Conversational Memory](./SP-027-persistent-context.md) | 📋 Proposed (compaction shipped; persistent recall deferred) |
| SP-045 | [WASM Build Feature Parity](./SP-045-wasm-feature-parity.md) | ⚠️ Partially Shipped (wasmshell, embedding mode switching, onnx lazy-load, mcp_refresh tool shipped 2026-06; Tier 2b/2c tooling pending → see SP-100) |
| SP-046 | [Browser-Primary Workspace Sync Model](./SP-046-workspace-sync-model.md) | 📋 Proposed |
| SP-048 | [CLI Delight — Terminal UX Polish](./SP-048-cli-delight.md) | ✅ Partially Implemented (status footer, glyph vocabulary, tool-execution timeline shipped; silence-fill pending → see SP-101) |
| SP-054 | [LSP Language Coverage Expansion](./SP-054-lsp-language-coverage.md) | 📋 Proposed |
| SP-058 | [Selective Grammar Embedding for WASM and Daemon](./SP-058-selective-grammar-embed.md) | ✅ Implemented (grammar subset pruned via SP-091-1; 22 MB saved off the daemon binary) |
| SP-061 | [Remove Static Embedding Provider, Consolidate on ONNX](./SP-061-remove-static-embeddings.md) | ✅ Implemented (static provider removed via SP-091-2; ONNX is the sole path) |
| SP-075 | [Large-File Decomposition](./SP-075-large-file-decomposition.md) | 📋 Proposed (config.go 2833→388, EditorPane 2604→513 shipped; several webui components + `pkg/agent/tool_handlers_subagent_spawn.go` 1208 lines still over 500) |

## Future / On Hold

Parked pending real demand — not scheduled. See [`future/`](./future/).

| Spec | Title | Reason |
|------|-------|--------|
| SP-007 | [Extend Configuration — Role-Based Configs](./future/SP-007-extend-config.md) | Speculative; `subagent_types` + the shipped EA personas (SP-026) already cover most of the need. Revisit if users ask for per-project custom roles. |

## Decisions

| Spec | Title |
|------|-------|
| SP-039 | [UI Consolidation Decision](./SP-039-DECISION.md) |
| SP-039 | [Component Categorization](./SP-039-component-categorization.md) |
