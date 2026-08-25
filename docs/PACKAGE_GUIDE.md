# Package Guide

Quick-reference map of the 60+ packages under `pkg/`. Grouped by concern,
with dependency direction notes.

## Agent Core

The central orchestration engine. Everything else either feeds data to the
agent or renders its output.

| Package | Purpose |
|---------|---------|
| `agent` | Core agent: tool registry, conversation management, subagent runner, context operations, change tracking, seed integration. **Largest package** (203 source files). |
| `agent_api` | Shared API types used across all providers (client types, vision capabilities). |
| `agent_commands` | Command definitions and slash-command system. |
| `agent_providers` | LLM provider implementations (OpenAI, Anthropic, generic, etc.), model definitions, streaming. |
| `agent_tools` | Tool system: interface-based tool definitions, tool handlers, workspace sync. |

**Dependency direction:** `agent` imports from `agent_api`, `agent_providers`, `agent_tools`, `configuration`, `console`, `events`, and others. No package imports `agent` except `cmd/` and `webui/`.

## Configuration & Credentials

| Package | Purpose |
|---------|---------|
| `configuration` | Config loading/saving, credential resolution, test isolation helpers. |
| `credentials` | Credential storage and key management. |

## Terminal & Display

| Package | Purpose |
|---------|---------|
| `console` | Terminal UI: activity indicators, tool timeline, assistant turn renderer, reasoning fold, glyph vocabulary. |
| `webui` | React web UI server with embedded assets. WebSocket handler, session management, API endpoints. **Second-largest package** (133 source files). |
| `wasmshell` | WASM-compiled shell module for browser-based terminal. |

## Code Intelligence

Semantic code analysis — powers repo mapping, dead code detection, and embedding search.

| Package | Purpose |
|---------|---------|
| `ast` | Tree-sitter AST integration for multi-language symbol extraction. |
| `codegraph` | Code intelligence graph (callers, callees, dead code). |
| `codereview` | Automated code review logic. |
| `embedding` | ONNX-based embedding infrastructure, HNSW vector store, semantic search. |
| `index` | Embedding index management for duplicate detection. |

## Infrastructure & Operations

| Package | Purpose |
|---------|---------|
| `automate` | Workflow discovery, PID file management, process identity, session tracking. |
| `events` | Event bus for inter-component communication. |
| `git` | Git operations wrapper. |
| `history` | Conversation history persistence. |
| `mcp` | MCP (Model Context Protocol) server management and secret handling. |
| `security` | Shell command risk classification and security model. |
| `service` | Daemon service management (launchd/systemd). Platform-specific build tags. |
| `workflow` | Workflow engine: JSON loading, step execution, TODO-loop iteration, budget tracking, checkpoint persistence. |
| `lsp` | Language Server Protocol integration. |

## Providers & Models

| Package | Purpose |
|---------|---------|
| `modelcontract` | Model capability contracts. |
| `modelprobe` | Model probing and detection. |
| `modelregistry` | Model registry server. |
| `model_settings` | Per-model configuration. |
| `providercatalog` | Provider catalog data. |
| `providerregistry` | Remote provider registry. |
| `llmproxy` | LLM proxy for routing. |

## Utilities

Small, focused packages with narrow responsibilities.

| Package | Purpose |
|---------|---------|
| `errors` | Typed error definitions. |
| `envutil` | Environment variable utilities. |
| `factory` | Object factory patterns. |
| `filediscovery` | File discovery utilities. |
| `filesystem` | Workspace-aware filesystem operations. |
| `notify` | OS notification system (osascript, notify-send, etc.). |
| `prompts` | System prompt definitions. |
| `personas` | Agent persona definitions and risk cascades. |
| `redact` | Secret redaction for tool output. |
| `secretdetect` | Secret detection scanner. |
| `skills` | Skill system (activate/list). |
| `search` | Search utilities. |
| `spec` | Spec file parsing. |
| `testutil` | Test helpers shared across packages. |
| `text` | Text processing utilities. |
| `tools` | Core tool interfaces. |
| `trace` | Tracing/dataset mode. |
| `training` | Training data export. |
| `types` | Shared type definitions. |
| `ui` | Shared UI components (@sprout/ui). |
| `utils` | General utilities. |
| `validation` | Input validation. |
| `zsh` | ZSH-specific command detection. |
| `logging` | Structured logging. |
| `export` | Export functionality. |
| `commands` | Command abstractions. |
| `clihooks` | CLI lifecycle hooks. |
| `noninteractive` | Non-interactive mode helpers. |
| `webcontent` | Web content fetching and browser interaction. |
| `pythonruntime` | Python runtime management (PDF processing). |
| `interfaces` | Shared interface definitions. |

## internal/

Packages that should not be imported externally.

| Package | Purpose |
|---------|---------|
| `internal/hnsw` | HNSW (Hierarchical Navigable Small World) graph for approximate nearest neighbor search. Used by `embedding`. |

## Cross-Cutting Notes

- **No package imports `cmd/`.** The `cmd/` package is the composition root — it wires dependencies and registers cobra commands. All business logic lives in `pkg/`.
- **`pkg/agent` is the hub.** It imports from most infrastructure packages but nothing imports it except `cmd/` and `webui/`.
- **`internal/` is underutilized.** Currently only `internal/hnsw`. Candidates for `internal/` migration: packages only consumed by `cmd/` and `pkg/agent` with no external consumers (e.g., `clihooks`, `noninteractive`, `factory`).
- **Platform-specific code** uses Go build tags (`//go:build darwin`, `//go:build linux`, etc.). See `pkg/service/` for the pattern.
