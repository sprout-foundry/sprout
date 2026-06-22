# SP-060: Desktop App — Per-Workspace Server Mode

**Status:** ✅ Implemented (Phase A + Phase B shipped and verified)  
**Depends on:** SP-001 (Agent Core Architecture), SP-003 (WebUI & Frontend Architecture)  
**Priority:** High  
**Effort Estimate:** ~1 week (Phase B focused)

## Problem

The desktop app currently spawns `sprout --isolated-config agent --daemon --web-port PORT` for each workspace. This is wrong for several reasons:

1. **Wrong abstraction** — `--daemon` mode is designed for long-running system services (launchd/systemd). It sets `SPROUT_DAEMON=1`, activates log rotation, runs a supervisor, and expects to be a persistent background process. The desktop app needs none of this — it needs a per-workspace HTTP server tied to the Electron window lifecycle.

2. **Config coupling** — `--isolated-config` creates a `.sprout/` config directory in the workspace root, bootstrapping a full agent config clone. The desktop app just needs enough config to serve the web UI and accept chat requests — it doesn't need the full agent initialization ceremony.

3. **No clean lifecycle** — The daemon process is fire-and-forget from Electron's perspective (`child.unref()`). When the user closes the workspace window, there's no graceful shutdown of the backend. The process lingers until it times out or the OS reclaims it.

4. **Single-purpose** — A daemon is a general-purpose agent server. The desktop app needs a single-purpose server: serve the web UI for one workspace, handle chat requests for that workspace, and shut down when the window closes.

## Security Problem

The current architecture has fundamental security issues:

1. The backend is a **general-purpose daemon** (`--daemon` mode = persistent agent service), not a focused workspace server.
2. The HTTP server is **accessible to any local process** (no auth on localhost binds). Any process on your machine can connect to that port — there's no auth token for localhost binds.
3. The desktop app is just a **thin browser wrapper** around the web UI — it doesn't control the backend lifecycle properly.

```
Current (broken):
┌──────────────────────────────┐    ┌──────────────────────────────┐
│  Electron Process            │    │  Any local process           │
│  BrowserWindow               │    │  (malware, other apps, etc.) │
│  loads http://127.0.0.1:PORT │    │  can also connect            │
└──────────────┬───────────────┘    └──────────────┬───────────────┘
               │                                   │
               └───────────────┬───────────────────┘
                               │
                    TCP port (unauthenticated)
                               │
                 ┌─────────────▼──────────────┐
                 │ sprout agent --daemon       │
                 │ --web-port PORT             │
                 └────────────────────────────┘
```

## Three Options

### Option A: Embed the Go Server In-Process via CGo/WASM (Ideal, High Effort)

**Concept**: Compile the Go backend into a library the Electron process calls directly — no separate process, no network port.

- **How**: Use `go build -buildmode=c-shared` to produce a `.dylib`/`.so`, call it from Node via `ffi-napi` or a native Node addon. Or compile the agent to WASM and run it in a Worker thread.
- **Security**: ✅ No open port at all. The backend is a function call inside Electron's process.
- **Pros**: Perfect security isolation; no port scanning possible; single process; fast IPC.
- **Cons**: **Massive engineering effort**. CGo + Electron packaging is painful cross-platform. WASM can't do file I/O directly. Would need to redesign the Go agent's architecture.

### Option B: Keep Child Process, Bind to Unix Domain Socket (Recommended Target)

**Concept**: Add a `--bind-socket <path>` flag to the Go agent. The desktop app spawns the backend process but it listens on a **Unix domain socket** (e.g. `/tmp/sprout-desktop-<random>.sock`) instead of a TCP port.

**Architecture**:
```
┌─────────────────────────────────────────────┐
│  Electron Process                           │
│  ┌──────────┐    ┌──────────────────────┐   │
│  │ BrowserWindow │ ←→ │ Internal HTTP Proxy  │   │
│  │ (loads localhost:RANDOM)                  │   │
│  └──────────┘    └──────────┬───────────┘   │
│                             │               │
│                   Unix socket IPC           │
│                             │               │
│              ┌──────────────▼──────────┐    │
│              │ sprout agent process    │    │
│              │ --bind-socket /tmp/xxx  │    │
│              └─────────────────────────┘    │
└─────────────────────────────────────────────┘
```

- **Security**: ✅ The socket file has filesystem permissions (owner-only by default). No TCP port to scan. The internal HTTP proxy is bound to `127.0.0.1:RANDOM` inside Electron but **only accepts connections from the renderer process**.
- **Auth**: Add a `--secret <random>` flag. Electron generates a 256-bit secret, passes it to the Go process. Every request must include it as a header. The proxy injects it automatically.
- **Pros**: Good security; relatively straightforward; Go gets a `--bind-socket` flag (useful beyond desktop); no CGo/WASM complexity.
- **Cons**: More work than Option C; needs HTTP-over-socket proxy in Electron.

### Option C: Keep TCP, Add Auth Token + Random Port (Quick Fix, Low Effort)

**Concept**: Minimal change. Keep spawning `sprout agent --daemon --web-port 0` (OS picks random port). Generate a random auth token and pass it via `SPROUT_AUTH_TOKEN`. The Electron `BrowserWindow` sets the header on every request.

**Architecture**: Same as today, but:
1. Port is random (not 56000) — harder to discover
2. Every request needs `Authorization: Bearer <token>` — blocks unauthorized access
3. Electron's `session.webRequest` API injects the header on every request from the renderer

- **Security**: ⚠️ Moderate. Port is random but discoverable by `lsof`/`netstat`. Auth token blocks casual access but not a determined local attacker who can read `/proc/<pid>/environ`.
- **Auth**: Electron generates secret → passes as `SPROUT_AUTH_TOKEN` env var to child process → injects in renderer requests via `session.webRequest`.
- **Pros**: Minimal changes; works today; adds real auth.
- **Cons**: Still TCP-visible; secret visible in `/proc`; security-by-obscurity for the port.

## Current State

### Already Implemented (Go Backend)

- **Auth middleware** (`pkg/webui/auth_middleware.go`): Fully implemented and wired into `server_lifecycle.go`. Checks `Authorization: Bearer <token>` on write methods (POST/PUT/PATCH/DELETE) to `/api/*` paths. Exempts GET, HEAD, OPTIONS, WebSocket upgrades (`/ws`, `/terminal`, `/api/lsp/ws`), and non-API paths. Uses `crypto/subtle` constant-time comparison. 19 unit tests in `auth_middleware_test.go`.
- **Auth token from env** (`pkg/webui/server.go`): Reads `SPROUT_AUTH_TOKEN` env var at startup. Logs "Auth token configured: write endpoints require authentication" (never logs the token value).
- **Non-localhost protection** (`pkg/webui/server.go`): Refuses to start if `SPROUT_BIND_ADDR` is a non-localhost address without `SPROUT_AUTH_TOKEN` set. Returns clear error: "Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set."
- **Random port** (`desktop/backend.js`): Already uses `findFreePort()` (Node `net.createServer().listen(0)`) to allocate a random port, then passes `--web-port PORT` to the Go process. The Go side also captures OS-assigned port when `--web-port 0` is used.
- **Nil-agent daemon mode** (`cmd/agent_modes.go`): The daemon can start the web UI without a provider configured. `createChatAgent()` returns `(nil, nil)` in daemon mode when provider is missing. All `chatAgent` dereferences in `RunAgent` are nil-guarded. When agent is nil and daemon mode, it prints a web UI URL and waits for Ctrl+C. Sync endpoints return 503 when agent is nil.

### What Electron Is Missing (Phase A completion)

- **Secret generation**: `desktop/backend.js` does NOT yet generate a 256-bit secret or pass `SPROUT_AUTH_TOKEN` to the child process.
- **Auth header injection**: `desktop/backend.js` does NOT yet use `session.webRequest.onBeforeSendHeaders` to inject the `Authorization` header on renderer requests.

These are the last two pieces needed for Phase A.

## Remaining Work: Phase B — Unix Domain Socket

**Goal**: Replace TCP binding with Unix domain socket for desktop mode. This is the main remaining work.

### Go Backend Changes

- **`--bind-socket <path>` flag**: Add to the agent command. When set, listen on `net.Listen("unix", path)` instead of TCP.
- **`--secret <token>` flag**: Same auth token behavior as `SPROUT_AUTH_TOKEN` env var, but as an explicit CLI flag. Useful for Electron to pass directly instead of via environment.
- **Socket file permissions**: `0600` (owner read/write only) via `os.Chmod()` after creation.
- **Stale socket cleanup**: Remove existing socket file on startup if present (left by a crashed process).
- **Signal handler cleanup**: On SIGTERM/SIGINT, remove the socket file before exiting.
- **Integration**: The auth middleware already works with any `net.Listener` — no changes needed there.
- **Unit tests**: Bind to socket, connect via `net.Dial("unix", path)`, verify permissions, verify cleanup.

### Electron Changes

- **Internal HTTP proxy**: Create in `desktop/backend.js` using `http.createServer()`. Listens on `127.0.0.1:RANDOM` (random TCP port). Forwards requests to the Unix socket via `http.request()` with `socketPath` option. Injects `Authorization: Bearer <token>` on forwarding.
- **Socket path generation**: `os.tmpdir()` + random suffix (e.g. `/tmp/sprout-desktop-<hex>.sock`).
- **Spawn command**: `sprout agent --daemon --bind-socket /tmp/sprout-desktop-<random>.sock --secret <token> --web-port 0` (web-port unused when socket is set, but kept for backward compat).
- **BrowserWindow**: Loads `http://127.0.0.1:RANDOM` (the Electron proxy port, not the Go process).
- **Windows fallback**: Windows doesn't have Unix sockets. Fall back to localhost+auth (Option C behavior) — random port with auth token via `SPROUT_AUTH_TOKEN`.

### Windows Fallback

Windows requires a different IPC strategy. Despite Windows 10+ supporting AF_UNIX at the OS level, Node.js does **not** support Unix domain socket file paths — it routes them through named pipes internally, and `http.request({socketPath})` fails with `ENOTSOCK`. This is blocked on [libuv#2537](https://github.com/libuv/libuv/issues/2537) with no resolution timeline.

**On Windows, the Electron proxy falls back to TCP + auth (Option C behavior):**
- Go process listens on `127.0.0.1:RANDOM` (no `--bind-socket`)
- Electron generates a random auth token and passes it via `SPROUT_AUTH_TOKEN`
- Electron injects `Authorization: Bearer <token>` via `session.webRequest`
- This is the same security level as the current architecture but with auth added

**Implementation**: `desktop/backend.js` detects `process.platform === 'win32'` and takes the TCP+auth code path instead of the socket+proxy code path. The Go `--bind-socket` flag is simply not passed on Windows.

### Key Design Decisions

- Unix socket is the primary transport on macOS and Linux — no TCP port exposed by the Go process.
- Windows uses TCP+auth (Option C). This is a conscious tradeoff: the security is weaker than Unix sockets (TCP port is discoverable, token visible in `/proc` equivalent), but it's a significant improvement over the current unauthenticated TCP on a fixed port.
- The Electron proxy on `127.0.0.1:RANDOM` is an implementation detail — not externally discoverable because the port is random and changes every launch.
- `--bind-socket` is a general-purpose flag usable beyond the desktop app (any CLI scenario wanting socket-based access).
- Auth middleware (`pkg/webui/auth_middleware.go`) already works for both TCP and socket transports — no changes needed.

## Deferred: Phase C — `desktop-serve` Command

**Status**: Deferred to future work.

A dedicated `sprout desktop-serve` command (separate from `sprout agent --daemon`) was originally planned as a third phase. However, the nil-agent daemon mode already provides the core functionality needed by the desktop app: the web UI starts without a provider, the user configures via the UI, and chat works. A dedicated command can be extracted later when the team has capacity and can clearly articulate the additional value over the existing daemon mode.

## Open Questions

1. **Should `--bind-socket` be added to the standard `agent --daemon` command?** — Likely yes. It's a general-purpose flag useful for non-desktop scenarios where you want socket-based access.
2. **Should we keep `SPROUT_DESKTOP=1` env var?** — Potentially useful for telemetry/debugging to distinguish desktop-launched vs CLI-launched servers, but should not gate any behavior.
3. **Should the desktop app pass `--no-project-skills`?** — Project skills are useful in the desktop context, so probably not.
4. **Windows: TCP+auth is the permanent fallback.** Named pipes were considered but add complexity for marginal security benefit over TCP+auth. If libuv adds AF_UNIX support for Windows in the future, the socket code path can be enabled for Windows too.
5. **Should `SPROUT_AUTH_TOKEN` also protect GET endpoints in desktop mode?** — Current implementation only protects write methods. This is intentional — GETs are read-only and the main risk is unauthorized writes. But worth revisiting if a stricter model is needed.