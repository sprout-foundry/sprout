# Integration Matrix — Sprout WebUI Backend Interfaces

The sprout webui has **4 backend interfaces** that behave differently depending on
the runtime environment. Every API call, WebSocket, and file operation flows through
one of these interfaces. This document is the single source of truth for which
interface handles what, in each execution environment.

---

## The 4 Interfaces

| # | Interface | Current Implementation | What it provides |
|---|-----------|----------------------|------------------|
| **1** | **File System** | `clientFetch` → Go HTTP server, or WASM shell | Read/write/list/browse files |
| **2** | **Shell / Terminal** | WebSocket → Go PTY, or WASM shell | Execute commands, get output |
| **3** | **Agent / Chat** | `clientFetch` → Go HTTP + WebSocket | Chat, streaming responses, tool execution |
| **4** | **Git / VCS** | `clientFetch` → Go HTTP server | Status, diff, commit, branch operations |

Everything else in the webui (notifications, onboarding, instances, settings,
SSH hosts, etc.) is either gated behind feature flags or returns synthetic responses.

---

## The 3 Execution Environments

| Environment | Served from | File System | Shell | Agent | Git |
|---|---|---|---|---|---|
| **A. Local Desktop** | `localhost` Go server | Go server (`/api/file`, `/api/files`) | Go PTY via WebSocket (`/terminal`) | Go server (`/api/query`) | Go server (`/api/git/*`) |
| **B. Cloud Web App** | Foundry / Cloudflare Pages | WASM shell (browser IndexedDB) | WASM shell (browser) | Foundry API (`/api/query` → Go server) | Foundry API (`/api/git/*` → Go server) |
| **C. Cloud + Docker** | Foundry / Cloudflare Pages | Docker container filesystem | Docker container shell | Foundry API (`/api/query` → Go server) | Docker container git |

---

## Detailed Interface Matrix

### Interface 1: File System

| Operation | A. Local Desktop | B. Cloud Web App | C. Cloud + Docker |
|---|---|---|---|
| List directory (`GET /api/files`) | Go server reads local FS | **WASM shell** `listDir()` | Docker API proxy |
| Read file (`GET /api/file`) | Go server reads local FS | **WASM shell** `readFile()` | Docker API proxy |
| Write file (`POST /api/file`) | Go server writes local FS | **WASM shell** `writeFile()` | Docker API proxy |
| Create file/dir (`POST /api/create`) | Go server | **WASM shell** `executeCommand('touch …')` | Docker API proxy |
| Delete (`POST /api/delete`) | Go server | **WASM shell** `executeCommand('rm …')` | Docker API proxy |
| Rename (`POST /api/rename`) | Go server | **WASM shell** `executeCommand('mv …')` | Docker API proxy |
| Browse (`GET /api/browse`) | Go server | **WASM shell** `listDir()` | Docker API proxy |
| Search (`GET /api/search`) | Go server | **WASM shell** `executeCommand('grep …')` | Docker API proxy |
| Prettier config | Go server | WASM (not yet implemented) | Docker API proxy |

### Interface 2: Shell / Terminal

| Operation | A. Local Desktop | B. Cloud Web App | C. Cloud + Docker |
|---|---|---|---|
| Execute command | Go PTY via WebSocket | **WASM shell** `executeCommand()` | Docker exec API |
| Tab completion | Go PTY | **WASM shell** `autoComplete()` | Docker exec API |
| CWD tracking | Go PTY | **WASM shell** `getCwd()` | Docker exec API |
| Terminal I/O stream | WebSocket `/terminal` | **In-browser** (xterm.js → WASM) | WebSocket → Docker |

### Interface 3: Agent / Chat

| Operation | A. Local Desktop | B. Cloud Web App | C. Cloud + Docker |
|---|---|---|---|
| Send message (`POST /api/query`) | Go server → LLM | **Foundry API** → Go server → LLM | Foundry API → Go server → LLM |
| Stream response | WebSocket `/ws` | **Foundry WebSocket** `/ws` | Foundry WebSocket `/ws` |
| Stop query | Go server | Foundry API | Foundry API |
| Chat sessions CRUD | Go server | Foundry API | Foundry API |
| Provider/model selection | Go server | Foundry API | Foundry API |
| Image upload | Go server | Foundry API | Foundry API |

### Interface 4: Git / VCS

| Operation | A. Local Desktop | B. Cloud Web App | C. Cloud + Docker |
|---|---|---|---|
| Status, diff, log | Go server (local git) | **Foundry API** → Go server | Docker git via API |
| Stage/unstage/commit | Go server | **Foundry API** → Go server | Docker git via API |
| Branch operations | Go server | **Foundry API** → Go server | Docker git via API |
| Push/pull | Go server | **Foundry API** → Go server | Docker git via API |
| Deep review | Go server | Foundry API | Foundry API |

---

## What's Already Implemented

The integration uses **two independent paths** (both must stay in sync):

**Path 1: CloudAdapter** (sprout repo) — intercepts `clientFetch()` calls:
```
services/apiAdapter.ts          — Interface definition (APIAdapter)
services/cloudAdapter.ts        — Cloud implementation (CloudAdapter)
services/cloudEndpointRegistry/ — Maps every /api/* endpoint to a category
config/mode.ts                  — Feature flags derived from adapter capabilities
bootstrapAdapter.ts             — Installs CloudAdapter when VITE_SPROUT_MODE=cloud
```

**Path 2: Service Worker** (foundry repo) — intercepts browser fetch requests:
```
browser-ide/src/sprout-sw.ts    — Intercepts /api/* requests from webui
browser-ide/src/chat-bridge.ts  — Chat body translation to Foundry format
browser-ide/src/sw-vfs.ts       — VFS bridge between SW and WASM shell
browser-ide/src/editor.ts       — Loads webui static bundle + WASM
```

**Path 3: Server-side stubs** (foundry repo) — returns synthetic responses:
```
internal/api/webui_compat.go    — 80+ stub handlers for cloud-incompatible endpoints
internal/api/proxy.go           — LLM proxy with SSE streaming
internal/api/workspace_proxy.go — Reverse proxy to sprout containers (incl. WebSocket)
```

**Endpoint categories** (shared across all paths):
- `wasm-local` (17 endpoints) → File/shell ops, handled by WASM in browser
- `foundry-backend` (44 endpoints) → Chat/git/settings, proxied to Foundry
- `synthetic` (13 endpoints) → Feature-gated (onboarding, instances, SSH)
- `no-op` (1 endpoint) → OS file browser open

---

## What Still Needs Work

### Critical: Dual-path drift risk

The CloudAdapter (sprout repo) and Service Worker (foundry repo) independently
implement the same routing logic. When endpoints are added/changed in one repo,
the other must be updated manually. There is no automated sync or shared
classification between the two.

### Environment B (Cloud Static / Browser IDE — current focus)

1. **WASM file ops not wired into CloudAdapter.** The registry classifies 17
   endpoints as `wasm-local`, but the CloudAdapter doesn't intercept them — they
   fall through to `fetch()` → Foundry server → 404 or stub response.
   The Service Worker path handles these correctly via `MessageChannel`.
   Need: Add WASM interception in `CloudAdapter.fetch()`.

2. **Chat translation duplicated.** `CloudAdapter.translateRequestBody()` and
   Foundry's `chat-bridge.ts` both translate `{query}` → `{messages, stream}`.
   These can drift. Need: shared translation logic or explicit contract docs.

3. **WebSocket vs SSE mismatch.** The webui expects a WebSocket connection.
   In browser IDE mode, there is no WebSocket — Foundry uses SSE for streaming.
   Need: Verify the webui can work without WebSocket in cloud mode.

### Environment C (Cloud + Docker)

1. **Workspace proxy works.** Foundry reverse-proxies to the sprout container
   on port 56000, including WebSocket upgrades. This is the most functional path.

2. **Remote runner tunnels.** `workspace_tunnel.go` provides JSON-over-WebSocket
   tunneling for runners that can't expose ports. Works but needs resilience
   testing (reconnection, timeout handling).

### Cross-cutting concerns

1. **Build flag consistency.** `VITE_SPROUT_MODE` is now the standard env var name.
   Audit complete — all references updated.

2. **Feature flag adoption.** Components must consistently use `supports*`
   flags from `mode.ts` to avoid rendering local-only UI in cloud mode.

3. **Endpoint registry sync.** Adding a new API endpoint in sprout must be
   reflected in both `cloudEndpointRegistry/` AND `sprout-sw.ts`. There is
   no automated check for this.
