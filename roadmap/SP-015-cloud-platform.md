# SP-015: Cloud Platform Integration

**Status:** 📋 Proposed (partially implemented)
**Depends on:** SP-003 (WebUI), SP-014 (Terminal Sessions)
**Priority:** High
**Effort Estimate:** ~2-3 weeks (polish existing infrastructure, add missing components)

## Problem

Sprout Foundry's cloud platform needs to serve the sprout webui as a static bundle in the browser IDE (Mode C — free-tier client-side experience). The webui React app must run without a local Go backend, with API calls routed through a cloud adapter that bridges to the Foundry platform and the WASM shell.

Most of the foundational infrastructure already exists: the `CloudAdapter` class, the `CloudEndpointRegistry` with 103 classified endpoints, build-time mode flags, dist bundle tooling, and configurable WASM paths. However, there are remaining gaps in dist build completeness, component-level feature flag adoption, testing coverage, and clear documentation of the adapter contract.

## Current State

### Cloud Adapter Infrastructure (implemented)

| Component | File | Lines | Status |
|-----------|------|-------|--------|
| API Adapter interface | `webui/src/services/apiAdapter.ts` | ~80 | ✅ Complete |
| CloudAdapter class | `webui/src/services/cloudAdapter.ts` | ~500 | ✅ Complete |
| CloudEndpointRegistry | `webui/src/services/cloudEndpointRegistry.ts` | ~103 endpoints | ✅ Complete |
| Feature flag module | `webui/src/config/mode.ts` | ~40 | ✅ Complete |
| Bootstrap adapter | `webui/src/bootstrapAdapter.ts` | ~25 | ✅ Complete |
| WASM shell (configurable paths) | `webui/src/services/wasmShell.ts` | configurable `wasmUrl`/`wasmExecUrl` | ✅ Complete |

**`CloudAdapter`** implements `APIAdapter` and routes webui fetch calls:
- Chat endpoints (`/api/query`, `/api/query/steer`, `/api/query/stop`) → body translation + Foundry proxy
- Git endpoints (`/api/git/*`) → URL rewrite to `/api/proxy/git/*`
- Settings endpoints (`/api/settings/*`) → URL rewrite to `/api/proxy/settings/*`
- Stats (`/api/stats`) → URL rewrite to `/api/proxy/stats`
- WASM-local endpoints (`/api/files`, `/api/file`, `/api/create`, etc.) → handled by WASM shell
- Synthetic endpoints → return pre-defined JSON responses (instances, SSH, onboarding, config)
- All other `/api/*` → forwarded to Foundry backend

**`CloudEndpointRegistry`** defines 103 endpoints across 4 categories:
- **wasm-local** (15): File operations, search, terminal stubs — handled by WASM shell
- **foundry-backend** (~70): Chat, git, settings, providers, sessions, LSP, history — proxied to Foundry
- **synthetic** (~14): Instances, SSH, onboarding, config — return static responses
- **no-op** (1): OS file browser open — silent success

**`mode.ts`** exports feature flags driven by `REACT_APP_SPROUT_MODE` build-time env var and the installed adapter:
- `isCloud` — true when `REACT_APP_SPROUT_MODE=cloud`
- `supportsSSH` — consults adapter, defaults to `true` (local) or adapter flag
- `supportsInstances` — consults adapter, defaults to `isCloud`
- `supportsLocalTerminal` — consults adapter, defaults to `!isCloud`
- `supportsSettings` — consults adapter, defaults to `!isCloud`

**`CloudAdapter` capability flags:**
- `supportsSSH = false`
- `supportsInstances = true`
- `supportsLocalTerminal = false`
- `supportsSettings = false`
- `fileOpsViaAPI = false` (WASM handles files locally)
- `showOnboarding = false` (cloud is pre-configured)

### Build Tooling (implemented)

| Target | File | Status |
|--------|------|--------|
| `scripts/build-wasm.sh --dist` | `scripts/build-wasm.sh` | ✅ `--dist` flag implemented |
| `scripts/build-webui-dist.mjs` | `scripts/build-webui-dist.mjs` | ✅ `--mode cloud\|local` with `VITE_SPROUT_MODE` |
| `make build-webui-dist` | `Makefile` | ✅ Calls `build-webui-dist.mjs --mode cloud` |
| `make build-webui-dist-local` | `Makefile` | ✅ Calls `build-webui-dist.mjs --mode local` |
| `make verify-dist` | `scripts/verify-dist-bundle.sh` | ✅ Validates dist bundle on static HTTP server |

### Client Session Integration (implemented)

- `webui/src/services/clientSession.ts` delegates all fetch calls through the installed adapter
- `WEBUI_CLIENT_ID_HEADER` injected into all adapter requests for session correlation

### Test Coverage (partial)

| Test File | Lines | Status |
|-----------|-------|--------|
| `cloudAdapter.test.ts` | ~250 | ✅ Unit tests for adapter behavior |
| `cloudAdapter.integration.test.ts` | ~1,300 | ✅ Integration tests across all CLOUD_ENDPOINTS |
| `mode.test.ts` | ~300 | ✅ Feature flag tests (local, cloud, with adapter) |

## Remaining Work

### R1: Ensure Cloud Build Flag Consistency

**Problem:** The build tooling uses `VITE_SPROUT_MODE` (Vite-style) while `mode.ts` reads `REACT_APP_SPROUT_MODE` (CRA-style). The `build-webui-dist.mjs` script sets `VITE_SPROUT_MODE=cloud` but `mode.ts` checks `process.env.REACT_APP_SPROUT_MODE`. The `bootstrapAdapter.ts` also reads `process.env.REACT_APP_SPROUT_MODE`.

The dist build script (`build-webui-dist.mjs`) may be setting the wrong env var name. Need to verify that the variable name in the build script matches what `mode.ts` and `bootstrapAdapter.ts` read.

**Action:**
- Audit `build-webui-dist.mjs` env var setting against `mode.ts` and `bootstrapAdapter.ts` env var reads
- Ensure consistency: either all use `REACT_APP_SPROUT_MODE` or all use `VITE_SPROUT_MODE`
- Update the build script or the source files to match

### R2: Component-Level Feature Flag Adoption

**Problem:** The feature flags exist in `mode.ts` but need to be consistently adopted across all components that render local-only features (SSH panels, instance management, local settings, onboarding).

**Action:**
- Audit all components that reference SSH, instances, local terminal, or settings to ensure they use `supportsSSH`, `supportsInstances`, `supportsLocalTerminal`, and `supportsSettings` from `mode.ts`
- Components to check: `Sidebar.tsx`, `LocationSwitcher.tsx`, `SettingsPanel.tsx`, `App.tsx`, `TerminalPane.tsx`, any onboarding components
- Ensure cloud-mode builds never render these panels (not just hide them — the feature flag should gate rendering at the component tree level)

### R3: Endpoint Registry Maintenance and Completeness

**Problem:** The endpoint registry (103 entries) may have drifted from actual webui API calls. When new endpoints are added to the Go backend or webui, they need to be classified in the registry.

**Action:**
- Add a lint/test step that scans `webui/src/services/api.ts` (and other fetch call sites) for API paths and cross-references against `CLOUD_ENDPOINTS` to catch unclassified endpoints
- Document the classification process: new endpoints default to `foundry-backend` unless explicitly marked otherwise

### R4: WebSocket Adapter Contract

**Problem:** The `CloudAdapter.getWebSocketURL()` returns the configured Foundry WS URL, but the webui's WebSocket client (`websocket.ts`) may not go through the adapter pattern. In cloud mode, the WebSocket connection needs to reach the Foundry backend, not the local Go process.

**Action:**
- Audit `webui/src/services/websocket.ts` to verify it reads the WS URL from the adapter (via `getAdapter()?.getWebSocketURL()`) in cloud mode
- If not, modify the WebSocket initialization to consult the adapter

### R5: Dist Bundle Structure and Serving

**Problem:** The dist bundle output structure needs to be clearly defined and verifiable for Foundry consumption. The `build-wasm.sh --dist` output structure (webui/, wasm/, version.json) may differ from what `build-webui-dist.mjs` produces.

**Action:**
- Define the canonical dist bundle layout
- Ensure `build-webui-dist.mjs` produces the same output structure as `build-wasm.sh --dist` (or document the difference and why)
- Ensure `version.json` is included in the dist bundle
- Verify the bundle serves correctly from a plain static HTTP server (covered by `make verify-dist`)

### R6: Chat Body Translation Robustness

**Problem:** The chat body translation in `CloudAdapter` (converting `{ query, chat_id }` to `{ messages, stream }`) needs thorough testing for edge cases: empty queries, missing optional fields, steer continuation, stop signals, and session state preservation.

**Action:**
- Add edge case tests for body translation: empty query, missing chat_id, steer without prior conversation, concurrent requests
- Verify that the Foundry backend contract (expected fields, optional fields) is documented and matches the translation

### R7: Error Handling and Fallback

**Problem:** When the Foundry backend is unreachable or returns errors, the cloud adapter should provide clear error messages and graceful degradation.

**Action:**
- Add error handling in the adapter for network failures, timeout, and unexpected response formats
- Consider retry logic for transient errors
- Ensure the webui displays meaningful errors to users (not raw fetch errors)

## API Route Classification

This table documents the complete endpoint classification for the cloud adapter. It is the authoritative reference for what Foundry's Service Worker shim (if used instead of the adapter pattern) would need to implement.

| Category | Routes | Adapter Behavior |
|----------|--------|-----------------|
| **WASM-local** | `/api/files`, `/api/file`, `/api/create`, `/api/delete`, `/api/rename`, `/api/browse`, `/api/workspace/browse`, `/api/search`, `/api/search/replace`, `/api/file/check-modified`, `/api/file/consent`, `/api/files/prettier-config`, `/api/terminal/sessions`, `/api/terminal/shells`, `/api/terminal/history` | Handled by WASM shell (no network call) |
| **LLM redirect** | `/api/query`, `/api/query/steer` | Body translation + proxy to `/api/proxy/chat` |
| **LLM control** | `/api/query/stop`, `/api/query/status` | Proxy to `/api/proxy/chat/stop` and `/api/proxy/chat/status` |
| **Git proxy** | `/api/git/*` (~20 endpoints) | URL rewrite to `/api/proxy/git/*` |
| **Settings proxy** | `/api/settings`, `/api/settings/*` (credentials, providers, MCP, skills, subagent-types) | URL rewrite to `/api/proxy/settings/*` |
| **Stats proxy** | `/api/stats` | URL rewrite to `/api/proxy/stats` |
| **Sessions & history** | `/api/sessions`, `/api/sessions/restore`, `/api/chat-sessions`, `/api/chat-sessions/*`, `/api/history/*` | Proxied to Foundry backend |
| **LSP & diagnostics** | `/api/diagnostics`, `/api/semantic`, `/api/lsp/status`, `/api/lsp/ws`, `/api/workspace/symbols` | Proxied to Foundry backend |
| **Synthetic** | `/api/instances`, `/api/instances/ssh-*`, `/api/onboarding/*`, `/api/config`, `/api/workspace`, `/api/support-bundle` | Return pre-defined JSON responses |
| **No-op** | `/api/open-in-file-browser` | Silent success response |
| **Hidden by flags** | (SSH panel, instance management UI) | UI components not rendered in cloud mode — no API calls made |
| **Static** | `/`, `/static/*`, `/manifest.json`, icons | Served directly from static bundle |

## Implementation Phases

### Phase 1: Audit and Fix (Week 1)

- R1: Fix env var name consistency (`VITE_SPROUT_MODE` vs `REACT_APP_SPROUT_MODE`)
- R4: Audit WebSocket adapter integration
- R5: Define and verify dist bundle output structure
- Verify `make build-webui-dist` produces a working cloud bundle

### Phase 2: Component Coverage (Week 1-2)

- R2: Audit and fix component-level feature flag adoption
- R7: Add error handling and fallback in the adapter

### Phase 3: Testing and Maintenance (Week 2-3)

- R3: Add endpoint registry completeness check (lint/test)
- R6: Add edge case tests for chat body translation
- Verify `make verify-dist` passes with real static serving
- Document the adapter pattern for Foundry integration team

## Success Criteria

| Metric | Target |
|--------|--------|
| `REACT_APP_SPROUT_MODE=cloud npm run build` | Produces working cloud-mode bundle |
| `make build-webui-dist` | Produces versioned bundle in `dist/cloud/` |
| `make verify-dist` | All assets load from static server (no 404s) |
| Cloud-mode webui in browser | Renders editor, file browser, terminal (WASM), chat; no SSH/instance panels |
| CloudAdapter fetch routing | All 103 CLOUD_ENDPOINTS produce correct behavior |
| Feature flags | All local-only components gated by `supports*` flags |
| Endpoint registry | No unclassified API paths (lint catches new endpoints) |
| Chat translation | Body translation handles all edge cases (tested) |
| WebSocket | Cloud mode connects to Foundry WS URL via adapter |

## Files Reference

| File | Action |
|------|--------|
| `webui/src/services/apiAdapter.ts` | Review: adapter interface (stable) |
| `webui/src/services/cloudAdapter.ts` | Modify: env var consistency, error handling, WebSocket |
| `webui/src/services/cloudEndpointRegistry.ts` | Maintain: add new endpoints as they appear |
| `webui/src/services/clientSession.ts` | Review: adapter delegation |
| `webui/src/services/websocket.ts` | Audit: adapter integration |
| `webui/src/config/mode.ts` | Review: flag consistency |
| `webui/src/bootstrapAdapter.ts` | Modify: env var name if needed |
| `webui/src/services/wasmShell.ts` | Review: configurable paths (done) |
| `webui/src/components/Sidebar.tsx` | Audit: feature flag gating |
| `webui/src/components/LocationSwitcher.tsx` | Audit: feature flag gating |
| `webui/src/components/App.tsx` | Audit: feature flag gating |
| `scripts/build-webui-dist.mjs` | Modify: env var name consistency |
| `scripts/build-wasm.sh` | Review: dist output structure |
| `scripts/verify-dist-bundle.sh` | Review: coverage |
| `Makefile` | Review: dist targets |

## Open Questions

1. Should the adapter pattern replace the Service Worker shim entirely (for Foundry's browser IDE)? Currently both patterns are described. The adapter is a simpler pattern that handles routing in the main thread rather than in a service worker.
2. Should cloud mode support offline/cached operation via service worker caching? Or is the cloud adapter sufficient without SW caching?
3. Does Foundry need the dist bundle as a flat directory, or as a tarball/npm package for version pinning?
4. Should the chat body translation live in the adapter (current) or be co-located with the API types for easier maintenance?
