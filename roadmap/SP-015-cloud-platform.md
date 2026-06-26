# SP-015: Cloud Platform Integration

**Status:** ✅ Implemented (sprout-side; 2026-06-26) — R1–R7 complete in this repo. Cross-repo evolution lives in [`../sprout-foundry`](../sprout-foundry/AGENTS.md).
**Depends on:** SP-003 (WebUI), SP-014 (Terminal Sessions)
**Priority:** High
**Effort Estimate:** ~2-3 weeks (polish existing infrastructure, add missing components) — closed

## Status snapshot (2026-06-26)

All seven R-items tracked in `TODO.md` are now ✅ shipped:

| R | Description | Status | Where |
|---|---|---|---|
| R1 | WASM interception in `CloudAdapter.fetch()` | ✅ shipped | `webui/src/services/cloudAdapter.ts::fetch` (~line 117) — checks `isWasmLocalEndpoint()`, calls `handleWasmLocal(shell, ...)`, falls through to server safety-net on WASM-init failure |
| R2 | Env-var name consistency (`VITE_SPROUT_MODE`) | ✅ shipped | `scripts/build-webui-dist.mjs`, `webui/src/config/mode.ts` use the same VITE-prefixed name |
| R3 | Component-level feature-flag adoption | ✅ shipped | `supportsSSH` / `supportsInstances` / `supportsLocalTerminal` / `supportsSettings` flags on `CloudAdapter`; consumers gate via `mode.supports*` |
| R4 | Endpoint registry sync via manifest | ✅ shipped | `scripts/export-endpoint-manifest.mjs` → `dist/endpoint-manifest.json`; `make export-endpoint-manifest` regenerates; foundry imports manifest at build time |
| R5 | WebSocket routing verification | ✅ shipped | Three patterns verified: reverse-proxy (workspace), JSON-over-WS tunnel (runners), SSE-only (browser IDE via `MessageChannel`) |
| R6 | Canonical dist-bundle layout | ✅ shipped | `scripts/build-webui-dist.mjs` produces `dist/sprout-webui/` with `index.html`, `assets/`, `wasm/` |
| R7 | Chat-translation robustness | ✅ shipped | Edge-case tests (empty query, missing `chat_id`, steer, stop); Foundry chat contract documented; shared-module extraction deferred — both repos still translate independently, kept in sync via review |

**Test coverage:** 491 cloud tests pass (`cloudAdapter.test.ts`, `cloudAdapter.integration.test.ts`, `cloudEndpointRegistry.test.ts`).

## What this spec doesn't cover anymore

When SP-015 was first drafted (early 2026), the foundry repo housed
the **same sprout webui** served as a static bundle, with a Service
Worker (`sprout-sw.ts`) intercepting `/api/*` requests in the
browser. The spec body still describes that architecture (see
"Integration Layer 2: Service Worker" below).

Since then the foundry repo pivoted: it now ships its **own
purpose-built webui** (a thin client for workspace / admin / billing
flows — `platform/webui/src/pages/`) that talks directly to foundry
APIs. It does **not** consume sprout's `CloudAdapter`, does **not**
register a Service Worker, and does **not** reuse sprout's endpoint
registry. The two webuis serve different audiences (the IDE vs. the
platform console) and share only `@sprout/ui` and `@sprout/events`
NPM packages.

**Implication for this spec:** the "Service Worker path" sections
below are kept as historical reference for the architectural intent,
but the Service Worker implementation does not exist in either repo
today. Any cross-repo routing concerns belong in the sister repo's
own spec (`../sprout-foundry/`).

## Problem

Sprout Foundry serves the sprout webui in multiple cloud contexts. The webui must run without a local Go backend, with API calls routed through adapters that bridge to the Foundry platform and the WASM shell.

There are **two independent integration paths** that must both work:

1. **CloudAdapter path** — The sprout webui's built-in `CloudAdapter` class intercepts `clientFetch()` calls and routes them. Used when the webui is served by Foundry's Go server (workspace mode, Docker containers).

2. **Service Worker path** — Foundry's `sprout-sw.ts` intercepts `/api/*` fetch requests from the webui in the browser. Used when the webui is served as a static bundle from Cloudflare Pages (browser IDE, free-tier mode).

Both paths must handle the same set of ~100 API endpoints. The CloudAdapter lives in the sprout repo; the Service Worker lives in the foundry repo. They need to stay in sync.

## Current Architecture

### Three Execution Environments

| Environment | Served from | File System | Shell | Agent/Chat | Git |
|---|---|---|---|---|---|
| **A. Local Desktop** | `localhost` Go server | Go server | Go PTY (WebSocket) | Go server | Go server |
| **B. Cloud Static** | Foundry / Cloudflare Pages | WASM shell | WASM shell | Foundry API (SSE) | Foundry API |
| **C. Cloud + Docker** | Foundry (reverse proxy) | Docker FS | Docker shell | Foundry → sprout | Docker git |

### Integration Layer 1: CloudAdapter (sprout repo)

| Component | File | Status |
|-----------|------|--------|
| API Adapter interface | `webui/src/services/apiAdapter.ts` | ✅ Complete |
| CloudAdapter class | `webui/src/services/cloudAdapter.ts` | ✅ Complete |
| CloudEndpointRegistry | `webui/src/services/cloudEndpointRegistry/` | ✅ Complete |
| Feature flag module | `webui/src/config/mode.ts` | ✅ Complete |
| Bootstrap adapter | `webui/src/bootstrapAdapter.ts` | ✅ Complete |

`CloudAdapter` intercepts `clientFetch()` calls:
- Chat → body translation + proxy to Foundry `/api/proxy/chat`
- Git → URL rewrite to `/api/proxy/git/*`
- Settings/Stats → URL rewrite to `/api/proxy/*`
- WASM-local endpoints → classified but NOT yet intercepted (known gap)
- Synthetic endpoints → return static JSON (instances, SSH, onboarding)

### Integration Layer 2: Service Worker (foundry repo)

| Component | File | Status |
|-----------|------|--------|
| Service Worker | `browser-ide/src/sprout-sw.ts` | ✅ ~500 lines |
| Chat bridge | `browser-ide/src/chat-bridge.ts` | ✅ ~80 lines |
| WASM VFS bridge | `browser-ide/src/sw-vfs.ts` | ✅ ~150 lines |
| Editor loader | `browser-ide/src/editor.ts` | ✅ ~300 lines |

The Service Worker intercepts `/api/*` requests from the sprout webui:
- File ops → WASM shell via `MessageChannel`
- Chat → Foundry CORS proxy via `chat-bridge.ts` (translates sprout `{query}` → Foundry `{messages, stream}`)
- Git → Foundry git proxy
- Settings → Foundry API
- Terminal → WASM synthetic stubs
- Everything else → Synthetic responses or 404

**Note:** The SW path does NOT use the CloudAdapter at all. It independently reimplements the same routing logic.

### Integration Layer 3: Server-Side Stubs (foundry repo)

| Component | File | Status |
|-----------|------|--------|
| webui_compat handlers | `internal/api/webui_compat.go` | ✅ ~450 lines, 80+ routes |
| LLM proxy | `internal/api/proxy.go` | ✅ ~350 lines |
| Workspace proxy | `internal/api/workspace_proxy.go` | ✅ ~150 lines |

When Foundry serves the sprout webui from its Go server (workspace mode), the `webui_compat.go` handlers return synthetic empty responses for endpoints that don't apply in cloud mode. The one real handler translates chat requests through the LLM router.

### How Foundry Launches Sprout

Foundry's Docker entrypoint (`docker/entrypoint.sh`) supports two modes:

**Task mode** (`SPROUT_MODE=agent`):
```bash
sprout agent --no-web-ui --output-json [--provider X] [--model X] "$SPROUT_PROMPT"
```
Env vars: `REPO_URL`, `SPROUT_PROMPT`, `SPROUT_TASK_ID`, `SPROUT_USER_ID`, `CI=true`

**Workspace mode** (`SPROUT_MODE=daemon`):
```bash
sprout agent -d --port 56000 --bind 0.0.0.0
```
Foundry reverse-proxies to the container's port 56000, including WebSocket upgrades.

### How the Runner Invokes Sprout

`sprout-runner` (`cmd/sprout-runner/main.go`) polls Foundry for tasks and runs them in Docker containers via `internal/runner/executor.go`. The contract is env-var-based (not CLI flags) — the entrypoint.sh translates env vars into CLI flags.

### Test Coverage (partial)

| Test File | Lines | Status |
|-----------|-------|--------|
| `cloudAdapter.test.ts` | ~250 | ✅ Unit tests for adapter behavior |
| `cloudAdapter.integration.test.ts` | ~1,300 | ✅ Integration tests across all CLOUD_ENDPOINTS |
| `mode.test.ts` | ~300 | ✅ Feature flag tests (local, cloud, with adapter) |

## Remaining Work

None in this repo. All R1–R7 items are shipped — see Status snapshot above.
For cross-repo evolution (foundry ↔ sprout API contract changes), follow up
in the sister repo: [`../sprout-foundry/AGENTS.md`](../sprout-foundry/AGENTS.md).

## API Route Classification

| Category | Routes | CloudAdapter | Service Worker |
|----------|--------|-------------|----------------|
| **WASM-local** (15) | `/api/files`, `/api/file`, `/api/create`, `/api/delete`, `/api/rename`, `/api/browse`, `/api/search`, etc. | ✅ Intercepted via `handleWasmLocal(shell, ...)` with server-safety-net fallback | N/A (see "What this spec doesn't cover anymore") |
| **Chat** | `/api/query`, `/api/query/steer`, `/api/query/stop` | ✅ Body translation + proxy | N/A |
| **Git** (~20) | `/api/git/*` | ✅ URL rewrite | N/A |
| **Settings** | `/api/settings/*` | ✅ URL rewrite | N/A |
| **Stats** | `/api/stats` | ✅ URL rewrite | N/A |
| **Synthetic** (~14) | `/api/instances`, `/api/instances/ssh-*`, `/api/onboarding/*`, etc. | ✅ Static JSON via `getSyntheticResponse()` | N/A |
| **Sessions/history** | `/api/sessions`, `/api/chat-sessions/*`, `/api/history/*` | ✅ Proxy | N/A |
| **Terminal** | `/api/terminal/*` | ✅ WASM stubs | N/A |
| **No-op** | `/api/open-in-file-browser` | ✅ Silent success | N/A |

## Files Reference

### Sprout repo (this repo)
| File | Action |
|------|--------|
| `webui/src/services/cloudAdapter.ts` | Modify: add WASM interception (R1), env var fix (R2) |
| `webui/src/services/cloudEndpointRegistry/` | Maintain: keep in sync with Foundry |
| `webui/src/services/clientSession.ts` | Review: adapter delegation |
| `webui/src/config/mode.ts` | Review: flag consistency (R2) |
| `webui/src/bootstrapAdapter.ts` | Modify: env var name if needed (R2) |
| `scripts/build-webui-dist.mjs` | Modify: env var consistency (R2) |
| `webui/src/components/Sidebar.tsx` | Audit: feature flag gating (R3) |
| `webui/src/components/LocationSwitcher.tsx` | Audit: feature flag gating (R3) |

### Foundry repo (external)
| File | What it does |
|------|-------------|
| `internal/api/webui_compat.go` | 80+ synthetic stub handlers |
| `internal/api/proxy.go` | LLM proxy with SSE streaming |
| `internal/api/workspace_proxy.go` | Reverse proxy to sprout containers |
| `docker/entrypoint.sh` | Launches sprout in task/daemon mode |
| `cmd/sprout-runner/main.go` | Task polling, Docker management |
| `internal/runner/executor.go` | Runs tasks in Docker containers |
| `internal/runner/workspace_tunnel.go` | WebSocket tunneling |
| `browser-ide/src/sprout-sw.ts` | Service Worker API interception |
| `browser-ide/src/chat-bridge.ts` | Chat body translation |
| `browser-ide/src/sw-vfs.ts` | VFS bridge for WASM |
| `browser-ide/src/editor.ts` | Loads webui bundle + WASM |

## Open Questions

1. **Should both paths continue to exist?** The CloudAdapter (sprout repo) and Service Worker (foundry repo) independently handle the same routing. Should one replace the other?
2. **Shared endpoint registry?** Could both repos import a shared endpoint classification to prevent drift?
3. **Chat translation dedup?** Both `CloudAdapter.translateRequestBody()` and `chat-bridge.ts` do the same translation. Should this be shared?
4. **Dist bundle format?** What's the canonical output structure for Foundry consumption?
