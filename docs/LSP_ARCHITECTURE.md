# LSP Architecture Plan

This document scopes a reusable Language Server Protocol (LSP) architecture for the web editor so TypeScript/JavaScript can be implemented first, then other languages can plug in without bespoke code paths.

## Goals

1. Provide IDE-grade language features (diagnostics, hover, completion, definition, references, rename, code actions, formatting) in the CodeMirror editor.
2. Keep one consistent frontend request/response model across all languages.
3. Keep one consistent backend session/process model across all language servers.
4. Support both LSP servers and non-LSP adapters behind a common interface.
5. Make capability rollout incremental and safe.

## Non-goals

1. Replacing existing CodeMirror language highlighting/parsing.
2. Rewriting editor architecture unrelated to semantic language features.
3. Shipping every language in one release.

## Current Baseline

1. Editor uses CodeMirror and custom extensions in [webui/src/components/EditorPane.tsx](../webui/src/components/EditorPane.tsx).
2. Language selection/detection is centralized in [webui/src/extensions/languageRegistry.ts](../webui/src/extensions/languageRegistry.ts).
3. Diagnostics pipeline exists via [webui/src/extensions/lintDiagnostics.ts](../webui/src/extensions/lintDiagnostics.ts) and [webui/src/services/api.ts](../webui/src/services/api.ts).
4. Backend diagnostics endpoint is [pkg/webui/api_diagnostics.go](../pkg/webui/api_diagnostics.go).
5. Validation backend currently targets Go-focused tooling in [pkg/validation/validation.go](../pkg/validation/validation.go).

## Reusable Architecture

### 1. Backend Layering

Create three backend layers under pkg/webui and pkg/lsp.

1. Transport/API layer
- HTTP and WebSocket endpoints that are language-agnostic.
- Stateless request validation plus routing to the language service manager.

2. Session/manager layer
- Workspace-scoped language sessions.
- Document lifecycle tracking (open/change/save/close).
- Capability checks and request fan-out.
- Cancellation/token management.

3. Provider adapter layer
- One adapter per language-server family.
- Examples:
  - TypeScript adapter (tsserver or typescript-language-server).
  - Go adapter (gopls).
  - Python adapter (pyright or pylsp).
- Adapter translates generic requests into protocol/tool-specific calls.

### 2. Frontend Layering

Create three frontend layers under webui/src.

1. Editor integration layer
- CodeMirror plugins and commands.
- Cursor/selection hooks and keybinding triggers.

2. Language client layer
- A language-agnostic client service that handles:
  - Request dispatch.
  - In-flight cancellation.
  - Capability checks.
  - Position conversion (offset <-> line/character).

3. Feature presenters
- UI components for hover cards, references panel, rename modal, code actions list, diagnostics panel.
- No language-specific logic in presenters.

### 3. Shared Protocol Contracts

Use one internal schema for frontend/backend contracts, regardless of underlying language server.

Core concepts:

1. Session identity
- workspaceId
- languageId
- sessionId

2. Document identity
- uri (workspace-relative URI or file URI)
- version
- content

3. Position/range
- line/character (LSP standard)
- optional byte offset for CodeMirror convenience

4. Feature request envelope
- requestId
- method (hover, completion, definition, references, rename, codeAction, formatting, diagnostics)
- params
- cancellationToken

5. Feature response envelope
- requestId
- result
- error
- capabilityUnavailable flag

### 4. Capability-Driven Behavior

Do not assume all servers support all features.

1. During session init, store server capabilities.
2. Frontend queries capabilities before enabling controls.
3. Unsupported features degrade gracefully:
- Hidden menu entries.
- Disabled commands with tooltip.
- No-op fallback with notification.

### 5. Session Lifecycle Pattern

Use the same lifecycle for every language.

1. Session start
- Resolve workspace root and project config files.
- Start or acquire language session for languageId.
- Initialize server and capabilities.

2. Document open
- Send didOpen with full text and version 1.

3. Document change
- Send didChange with incremental or full sync based on server preference.
- Track monotonically increasing version.

4. Document save
- Send didSave.

5. Document close
- Send didClose.

6. Session teardown
- Idle timeout and process cleanup.
- Explicit shutdown on workspace close.

### 6. Concurrency and Reliability Pattern

1. Request cancellation
- New hover/completion/definition request cancels stale in-flight request for same editor and feature.

2. Debounce policy
- Diagnostics debounce longer than hover/completion.
- Keep feature-specific debounce constants in one place.

3. Crash recovery
- If LS process exits, restart session once and surface a warning.
- On repeated failures, mark session unhealthy and stop auto-retrying until user action.

4. Timeout policy
- Feature-specific timeout defaults.
- Unified error taxonomy exposed to UI.

### 7. Security and Workspace Boundaries

1. Strict workspace-root path validation for all document URIs.
2. No arbitrary process launch from frontend-provided command.
3. Whitelist server command templates per language.
4. Sanitize and bound payload sizes.
5. Log server stderr in diagnostics bundle paths.

## Suggested Package and File Layout

Backend (new):

1. pkg/lsp/types.go
- Shared request/response structs.

2. pkg/lsp/manager.go
- Session manager interface and implementation.

3. pkg/lsp/session.go
- Generic session state machine.

4. pkg/lsp/adapters/adapter.go
- Adapter interface.

5. pkg/lsp/adapters/typescript_adapter.go
- TS/JS implementation.

6. pkg/lsp/adapters/gopls_adapter.go
- Go implementation.

7. pkg/webui/api_lsp.go
- HTTP/WebSocket handlers for language features.

Frontend (new):

1. webui/src/languageClient/types.ts
- Shared contract types mirroring backend.

2. webui/src/languageClient/client.ts
- Generic request dispatcher and cancellation.

3. webui/src/languageClient/capabilities.ts
- Capability cache/query helpers.

4. webui/src/languageClient/position.ts
- Position conversion helpers.

5. webui/src/features/language/*
- Feature modules: hover, completion, definition, references, rename, codeActions, formatting.

6. webui/src/extensions/languageFeatures.ts
- CodeMirror extension bridge to language client.

## API Shape Proposal

Use these endpoints/events as stable language-agnostic contracts.

1. POST /api/lsp/session/start
- Input: workspaceId, languageId
- Output: sessionId, capabilities

2. POST /api/lsp/session/stop
- Input: sessionId

3. POST /api/lsp/document/open
- Input: sessionId, uri, languageId, version, content

4. POST /api/lsp/document/change
- Input: sessionId, uri, version, changes

5. POST /api/lsp/document/save
- Input: sessionId, uri

6. POST /api/lsp/document/close
- Input: sessionId, uri

7. POST /api/lsp/request
- Input: sessionId, requestId, method, params
- Output: requestId, result, error

8. WebSocket optional path for push events
- diagnostics/update
- server/status
- session/restarted

## Feature Rollout Plan

### Phase 0: Infrastructure and Contracts

1. Introduce shared types and manager interfaces.
2. Add no-op adapter and test harness.
3. Add capability plumbing from backend to frontend.

### Phase 1: TypeScript/JavaScript Diagnostics

1. Add TS adapter and session lifecycle.
2. Route diagnostics through existing lint extension path.
3. Keep existing non-LSP diagnostics as fallback.

### Phase 2: Navigation and Hover

1. Hover.
2. Go to definition.
3. Find references.
4. Peek panel UI.

### Phase 3: Editing Semantics

1. Rename symbol (with preview).
2. Code actions/quick fixes.
3. Organize imports.
4. Signature help.

### Phase 4: Formatting and Expansion

1. Document/range formatting.
2. Add Go and Python adapters via same contracts.
3. Add adapter conformance tests shared across languages.

## Testing Strategy

### Backend

1. Unit tests for manager/session state machine.
2. Contract tests for adapters against a fake server transport.
3. Integration tests that spin up real language servers in CI where feasible.
4. Crash-recovery and timeout tests.

### Frontend

1. Unit tests for position conversion and capability gating.
2. Integration tests for feature modules with mocked language client.
3. Editor interaction tests for key flows: hover, definition, rename, diagnostics.

### End-to-end

1. Smoke test: TS file gets semantic diagnostics.
2. Navigation test: definition and references across files.
3. Rename test: updates all references in workspace scope with preview.

## Migration and Compatibility

1. Keep existing [webui/src/components/GoToSymbolOverlay.tsx](../webui/src/components/GoToSymbolOverlay.tsx) as fallback for in-file symbol navigation when no semantic provider is available.
2. Keep existing diagnostics API while introducing /api/lsp paths.
3. Add a feature flag for LSP integration in settings for staged rollout.
4. Emit structured logs so support bundles can diagnose LS startup and request failures.

## Decisions to Make Before Implementation

1. TS backend engine choice:
- tsserver directly.
- typescript-language-server.

2. Transport choice:
- HTTP-only request model.
- Hybrid HTTP + WebSocket push model.

3. Sync model:
- Full-text didChange only initially.
- Incremental didChange from day one.

4. Process model:
- One LS process per workspace per language.
- Shared LS process with multiple workspaces.

5. Capability baseline for GA:
- diagnostics + definition + hover.
- or include references + rename in first milestone.

## Recommended First Implementation Slice

1. Build manager/session contracts and TS adapter behind a feature flag.
2. Integrate diagnostics and hover first.
3. Add definition and references next.
4. Add rename only after reference accuracy and workspace resolution are proven.

This sequence gives meaningful user value early while preserving a reusable architecture that scales to additional languages and language servers.
