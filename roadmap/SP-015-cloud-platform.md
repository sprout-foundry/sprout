# SP-015: Cloud Platform Integration

**Status:** 📋 Proposed (partially implemented)
**Depends on:** SP-003 (WebUI), SP-014 (Terminal Sessions)
**Priority:** High
**Effort Estimate:** ~2-3 weeks (polish existing infrastructure, add missing components)

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
| CloudEndpointRegistry | `webui/src/services/cloudEndpointRegistry.ts` | ✅ Complete |
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

### R1: WASM Interception in CloudAdapter

**Problem:** The CloudAdapter classifies 17 endpoints as `wasm-local` but does NOT intercept them. These fall through to `fetch()` → Foundry server → 404 or stub response. File ops don't work through the CloudAdapter path.

**Action:** Add WASM interception in `CloudAdapter.fetch()` — check `isWasmLocal()` and route to WASM shell methods instead of `fetch()`. The Service Worker path already does this correctly via `MessageChannel`.

### R2: Cloud Build Flag Consistency

**Problem:** `build-webui-dist.mjs` sets `VITE_SPROUT_MODE=cloud` but `mode.ts` reads `process.env.REACT_APP_SPROUT_MODE`. These may not match depending on build tooling.

**Action:** Audit env var names across build scripts and source files. Ensure consistency.

### R3: Component-Level Feature Flag Adoption

**Problem:** Feature flags exist in `mode.ts` but need consistent adoption across components that render local-only features.

**Action:** Audit components referencing SSH, instances, local terminal, or settings. Ensure they use `supports*` flags from `mode.ts`.

### R4: Endpoint Registry Synchronization

**Problem:** The sprout webui's `CloudEndpointRegistry` and Foundry's Service Worker have **independent route tables** that can drift. Adding an endpoint in one repo doesn't update the other.

**Action:** 
- Add a test/lint in sprout that catches unclassified API paths
- Document the sync requirement between repos
- Consider a shared endpoint classification that both repos consume

### R5: WebSocket Routing

**Problem:** Three different WebSocket patterns exist:
1. Transparent reverse proxy (workspace mode — Foundry proxies to sprout container)
2. JSON-over-WebSocket tunnel (remote runners)
3. No WebSocket at all (browser IDE — uses SSE + MessageChannel)

The CloudAdapter's `getWebSocketURL()` only addresses pattern 1.

**Action:** Verify the webui's WebSocket client correctly handles all three patterns. The browser IDE path needs SSE-based event delivery, not WebSocket.

### R6: Dist Bundle Structure

**Problem:** The browser IDE expects the bundle at `browser-ide/dist/sprout-webui/` with specific structure (index.html, static/js/, wasm/). The `build-webui-dist.mjs` output must match this.

**Action:** Define canonical dist bundle layout. Ensure build scripts produce matching output.

### R7: Chat Translation Robustness

**Problem:** Both the CloudAdapter and the Service Worker's `chat-bridge.ts` translate sprout's `{query, chat_id}` to Foundry's `{messages, stream, provider, model}`. These two translations can drift.

**Action:**
- Add edge case tests: empty query, missing chat_id, steer, stop signals
- Document the Foundry chat contract (expected fields, optional fields)
- Consider extracting translation logic into a shared module

## API Route Classification

| Category | Routes | CloudAdapter | Service Worker |
|----------|--------|-------------|----------------|
| **WASM-local** (15) | `/api/files`, `/api/file`, `/api/create`, `/api/delete`, `/api/rename`, `/api/browse`, `/api/search`, etc. | ❌ Not intercepted (R1) | ✅ WASM via MessageChannel |
| **Chat** | `/api/query`, `/api/query/steer`, `/api/query/stop` | ✅ Body translation + proxy | ✅ chat-bridge.ts → Foundry |
| **Git** (~20) | `/api/git/*` | ✅ URL rewrite | ✅ Foundry git proxy |
| **Settings** | `/api/settings/*` | ✅ URL rewrite | ✅ Foundry API |
| **Stats** | `/api/stats` | ✅ URL rewrite | ✅ Foundry API |
| **Synthetic** (~14) | `/api/instances`, `/api/instances/ssh-*`, `/api/onboarding/*`, etc. | ✅ Static JSON | ✅ Static JSON |
| **Sessions/history** | `/api/sessions`, `/api/chat-sessions/*`, `/api/history/*` | ✅ Proxy | ✅ Foundry API |
| **Terminal** | `/api/terminal/*` | ✅ WASM stubs | ✅ WASM stubs |
| **No-op** | `/api/open-in-file-browser` | ✅ Silent success | ✅ Silent success |

## Files Reference

### Sprout repo (this repo)
| File | Action |
|------|--------|
| `webui/src/services/cloudAdapter.ts` | Modify: add WASM interception (R1), env var fix (R2) |
| `webui/src/services/cloudEndpointRegistry.ts` | Maintain: keep in sync with Foundry |
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
