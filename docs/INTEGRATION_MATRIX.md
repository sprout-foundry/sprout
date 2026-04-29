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

The `APIAdapter` / `CloudAdapter` / `cloudEndpointRegistry.ts` pattern in the sprout
webui already captures this matrix in code:

```
services/apiAdapter.ts          — Interface definition (APIAdapter)
services/cloudAdapter.ts        — Cloud implementation (CloudAdapter)
services/cloudEndpointRegistry.ts — Maps every /api/* endpoint to a category:
                                    wasm-local | foundry-backend | synthetic | no-op
config/mode.ts                  — Feature flags derived from adapter capabilities
bootstrapAdapter.ts             — Installs CloudAdapter when REACT_APP_SPROUT_MODE=cloud
```

**Endpoint categories in cloudEndpointRegistry.ts:**
- `wasm-local` (17 endpoints) → Interface 1 + 2, handled by WASM in browser
- `foundry-backend` (44 endpoints) → Interface 3 + 4, proxied to Foundry Go server
- `synthetic` (13 endpoints) → Feature-gated (onboarding, instances, SSH, etc.)
- `no-op` → Reserved for future use

---

## What Still Needs Work

### Environment B (Cloud Web App — current focus)

1. **WASM file ops aren't wired into the adapter yet.** The registry classifies 17
   endpoints as `wasm-local`, but the CloudAdapter doesn't intercept them — it only
   rewrites URLs and returns synthetic responses. The `wasm-local` endpoints still
   fall through to `fetch()` which hits the Foundry server. Need to either:
   - Have the CloudAdapter return WASM-generated responses for these endpoints
   - Or add a WASM middleware that intercepts before the adapter

2. **Terminal uses WASM directly (already works).** `TerminalPane.tsx` calls
   `initWasmShell()` and runs commands through it. This is the one interface
   that already works correctly in cloud mode.

3. **Git operations need a real backend.** All 20 git endpoints route to
   `foundry-backend`, but the Foundry server needs real git implementations
   (currently stubs return empty data).

4. **Agent/chat needs Foundry WebSocket.** The CloudAdapter returns `wsUrl`, but
   the Foundry WebSocket bridge needs to relay agent events.

### Environment C (Cloud + Docker — future)

Not started. Will need:
- Docker API client in Foundry backend
- A `DockerAdapter` or extended CloudAdapter that routes file/shell/git ops
  through Docker container APIs instead of WASM

### Architecture Improvements (Option A — shared component library)

The current approach (Option B: adapter pattern) uses `clientFetch()` as the
interception point. This works but means the adapter is a network-level shim.
Option A would make interfaces explicit components:

```
<FileSystemProvider>   — injects readFile/writeFile/listDir
<TerminalProvider>     — injects executeCommand/streamOutput
<AgentProvider>        — injects sendQuery/streamResponse
<GitProvider>          — injects gitStatus/gitCommit/gitDiff
```

Each provider would have environment-specific implementations. But this is a
larger refactor and the current adapter pattern is sufficient for launch.
