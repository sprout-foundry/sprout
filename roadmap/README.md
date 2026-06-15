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

| SP-060 | [Desktop App — Per-Workspace Server Mode](./SP-060-desktop-serve.md) | ✅ Implemented |

## In Progress

| Spec | Title | Status |
|------|-------|--------|
| SP-015 | [Cloud Platform Integration](./SP-015-cloud-platform.md) | 🚧 Partially implemented |
| SP-063 | [Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent](./SP-063-computer-use-persona.md) | 🚧 Core implemented (backend + tools + persona + safety); WebUI settings + interactive opt-in remain |

## Proposed

| Spec | Title | Status |
|------|-------|--------|
| SP-006 | [Delegate Tool — In-Process Agent Delegation](./SP-006-delegate-tool.md) | 📋 Proposed |
| SP-008 | [Reliability Engineering — Concurrency & Observability](./SP-008-reliability-engineering.md) | 📋 Proposed |
| SP-009 | [Component Library Maturation — Publish & Storybook](./SP-009-component-library-maturation.md) | 📋 Proposed |
| SP-010 | [Editor Modernization](./SP-010-editor-modernization.md) | 📋 Proposed |
| SP-011 | [Terminal Parity & Bug Fixes](./SP-011-terminal-parity.md) | 📋 Proposed |
| SP-012 | [UX Polish](./SP-012-ux-polish.md) | 📋 Proposed |
| SP-013 | [Agent Settings Management Tool](./SP-013-agent-settings-skill.md) | 📋 Proposed |
| SP-016b | [Expanded Embedding Index — Full Workspace Semantic Search](./SP-016b-expanded-embedding-index.md) | 📋 Proposed |
| SP-017 | [Settings Panel Rework — Scoped Collapsible Sections](./SP-017-settings-panel-rework.md) | 📋 Proposed |
| SP-022 | [Workspace Management & Project Detection](./SP-022-workspace-management.md) | 📋 Proposed |
| SP-023 | [In-Process Subagent Execution](./SP-023-in-process-subagents.md) | 📋 Proposed |
| SP-025 | [Tree-Sitter Integration — Real AST](./SP-025-tree-sitter-integration.md) | 📋 Proposed |
| SP-026 | [Executive Assistant Persona](./SP-026-executive-assistant.md) | 📋 Proposed |
| SP-027 | [Persistent Context & Conversational Memory](./SP-027-persistent-context.md) | 📋 Proposed |
| SP-045 | [WASM Build Feature Parity](./SP-045-wasm-feature-parity.md) | 📋 Proposed |
| SP-046 | [Browser-Primary Workspace Sync Model](./SP-046-workspace-sync-model.md) | 📋 Proposed |
| SP-048 | [CLI Delight — Terminal UX Polish](./SP-048-cli-delight.md) | 📋 Proposed |
| SP-049 | [Shell Permission Overhaul — Tiered Allow-Lists & User Policy](./SP-049-shell-permission-overhaul.md) | ✅ Implemented |
| SP-054 | [LSP Language Coverage Expansion](./SP-054-lsp-language-coverage.md) | 📋 Proposed |
| SP-056 | [CLI Reasoning Fold — Collapsed Thinking Indicator](./SP-056-cli-reasoning-fold.md) | 📋 Proposed |
| SP-058 | [Selective Grammar Embedding for WASM and Daemon](./SP-058-selective-grammar-embed.md) | 📋 Proposed |
| SP-059 | [Subagent ↔ Primary Interaction Overhaul](./SP-059-subagent-interaction.md) | ✅ Implemented |
| SP-061 | [Remove Static Embedding Provider, Consolidate on ONNX](./SP-061-remove-static-embeddings.md) | 📋 Proposed |
| SP-064 | [Automate CLI — Status, Stop, Logs](./SP-064-automate-cli-monitoring.md) | 📋 Proposed |
| SP-065 | [WebUI Automations Panel](./SP-065-automate-webui-panel.md) | 📋 Proposed |
| SP-068 | [Security Check Consolidation — One Risk Scale, One Resolver, One Broker](./SP-068-security-check-consolidation.md) | 📋 Proposed |
| SP-069 | [Pull Request Creation](./SP-069-pull-request-creation.md) | 📋 Proposed (gap analysis) |
| SP-070 | [Agent Completion Notifications](./SP-070-completion-notifications.md) | 📋 Proposed (gap analysis) |
| SP-071 | [Conversation Rewind & Edit-and-Resend](./SP-071-conversation-rewind.md) | 📋 Proposed (gap analysis) |
| SP-072 | [Per-Hunk Diff Approval — Approve-Before-Apply](./SP-072-diff-approval.md) | 📋 Proposed (gap analysis) |

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
