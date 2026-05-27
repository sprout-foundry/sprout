# SP-060: Desktop App — Per-Workspace Server Mode

**Status:** 📋 Proposed  
**Depends on:** SP-001 (Agent Core Architecture), SP-003 (WebUI & Frontend Architecture)  
**Priority:** High  
**Effort Estimate:** ~1-2 weeks (3 phases)

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

## Recommended Approach

**Start with Option C** (can be done in an hour or two), **then move to Option B** when you have time for a proper solution. Option A is deferred indefinitely — the engineering cost doesn't justify the marginal security improvement over Option B.

The phases below implement this recommendation: Phase A is Option C, Phase B is Option B, Phase C is the `desktop-serve` command cleanup.

## Implementation Phases

### Phase A: Auth Token + Random Port (Option C — Quick Security Fix)

**Goal**: Make the current daemon-approach secure enough for immediate use.

**Go backend changes:**
- `pkg/webui/server.go` — Support `SPROUT_AUTH_TOKEN` env var. If set, require `Authorization: Bearer <token>` on every request (except `/health`). Return 401 for missing/invalid tokens.
- `cmd/agent.go` or daemon startup — When `SPROUT_AUTH_TOKEN` is set, log "auth token enabled" (don't log the token itself).

**Electron changes:**
- `desktop/backend.js` — Generate 256-bit random secret via `crypto.randomBytes(32).toString('hex')`. Pass it as `SPROUT_AUTH_TOKEN` env var when spawning the backend.
- `desktop/backend.js` — Use `session.webRequest.onBeforeSendHeaders` to inject `Authorization: Bearer <token>` on every request from the renderer.
- `desktop/backend.js` — Change `--web-port 0` (or let the OS pick a random port) instead of using a fixed port.
- Read the actual assigned port from backend stdout/health response.

**Key design decisions:**
- `/health` endpoint remains unauthenticated (needed for readiness probe before renderer loads)
- Token is generated fresh each app launch (no persistence)
- If `SPROUT_AUTH_TOKEN` is not set, server works as before (backward compatible for CLI users)

### Phase B: Unix Domain Socket (Option B — Proper Security)

**Goal**: Replace TCP binding with Unix domain socket for desktop mode.

**Go backend changes:**
- New flag: `--bind-socket <path>` — Listen on Unix domain socket instead of TCP.
- `pkg/webui/server.go` — Support `http.Server` listening on `net.Listener` from `net.Listen("unix", path)`.
- Socket file permissions: `0600` (owner read/write only).
- Socket cleanup: Remove stale socket file on startup, install signal handler to clean up on exit.
- `--secret <token>` flag — Require this token as `Authorization: Bearer` on every request (same as Phase A but as explicit flag).

**Electron changes:**
- `desktop/backend.js` — Spawn with `--bind-socket /tmp/sprout-desktop-<random>.sock --secret <token>`.
- Internal HTTP proxy in Electron main process:
  - Listen on `127.0.0.1:RANDOM` (random TCP port, only accessible within Electron).
  - `BrowserWindow` loads `http://127.0.0.1:RANDOM`.
  - Proxy forwards requests to the Unix socket, injecting the `Authorization` header.
- Use Node.js `http.createServer()` + `http.request()` with `socketPath` option for proxying.
- Socket path uses `os.tmpdir()` + random suffix for cross-platform compatibility.

**Key design decisions:**
- Unix socket is the primary transport — no TCP port exposed by the Go process.
- The Electron proxy on `127.0.0.1:RANDOM` is an implementation detail — it's not externally discoverable because the port is random and changes every launch.
- `--bind-socket` is a general-purpose flag usable beyond the desktop app.
- On Windows (no Unix sockets), fall back to named pipes or localhost+auth (Option C behavior).

### Phase C: `desktop-serve` Command (Architecture Cleanup)

**Goal**: Replace the daemon-mode spawn with a purpose-built command.

**New Go command:**
- `cmd/desktop_serve.go` — Cobra command: `sprout desktop-serve --port PORT --workspace /path`
- `cmd/desktop_serve_modes.go` — Server lifecycle: start, health check, graceful shutdown
- Register in `cmd/root.go`

**Command flags:**
- `--port` — TCP port for Electron proxy to connect to (Phase B: unused, proxy connects via socket)
- `--workspace` — Workspace directory (defaults to cwd)
- `--bind-socket <path>` — Unix socket path (Phase B)
- `--secret <token>` — Auth token (Phase A/B)

**Key design decisions:**
- **No `SPROUT_DAEMON` env** — Not a daemon. No log rotation, no supervisor, no port competition logic.
- **Workspace-scoped config** — Use `--isolated-config` under the hood (workspace-scoped `.sprout/` dir), but without daemon-mode env vars.
- **Lazy agent initialization** — Create agent on first chat request, not at startup. Matches daemon behavior where `chatAgent` can be nil initially.
- **Graceful shutdown** — SIGTERM → cancel context → stop HTTP server → exit 0. 10-second force-kill timeout.
- **No stdin** — Runs with stdin disabled (already the case with `stdio: ['ignore']`).

**Electron migration:**
- `desktop/backend.js` — Change spawn command from `sprout --isolated-config agent --daemon --web-port PORT` to `sprout desktop-serve --port PORT --workspace /path`
- Remove `--daemon` flag, `--isolated-config` flag, `SPROUT_DAEMON=1` env
- Update WSL spawn path similarly

**Cleanup:**
- Remove desktop-specific workarounds in daemon mode code (e.g., `SPROUT_DESKTOP` env checks)
- Keep `SPROUT_DESKTOP=1` as optional telemetry/debugging hint, but gate no behavior on it

## TODO Checklist

### Phase A: Auth Token + Random Port (Option C)
- [ ] **Go**: Add `SPROUT_AUTH_TOKEN` support to `pkg/webui/server.go` — middleware that checks `Authorization: Bearer <token>` on all routes except `/health`
- [ ] **Go**: Log "auth token enabled" on startup when env var is set (don't log token value)
- [ ] **Go**: Unit tests for auth middleware (valid token, invalid token, missing token, health exempt)
- [ ] **Electron**: Generate 256-bit random secret in `desktop/backend.js` on launch
- [ ] **Electron**: Pass secret as `SPROUT_AUTH_TOKEN` env var when spawning backend
- [ ] **Electron**: Use `session.webRequest.onBeforeSendHeaders` to inject auth header on all renderer requests
- [ ] **Electron**: Use random port (`--web-port 0` or OS-assigned) instead of fixed port
- [ ] **Electron**: Read assigned port from backend startup output or health response
- [ ] **Verify**: Backend rejects unauthenticated requests when token is set
- [ ] **Verify**: Backend accepts requests with valid token
- [ ] **Verify**: `/health` works without token
- [ ] **Verify**: Desktop app loads and chats successfully
- [ ] **Verify**: `make build-all` passes

### Phase B: Unix Domain Socket (Option B)
- [ ] **Go**: Add `--bind-socket <path>` flag to agent command
- [ ] **Go**: Implement Unix socket listener in `pkg/webui/server.go` with `net.Listen("unix", path)`
- [ ] **Go**: Set socket file permissions to `0600` (owner-only)
- [ ] **Go**: Clean up stale socket file on startup
- [ ] **Go**: Install signal handler to remove socket on exit
- [ ] **Go**: Add `--secret <token>` flag for auth token (same behavior as env var, but explicit)
- [ ] **Go**: Unit tests for socket listener (bind, connect, permissions, cleanup)
- [ ] **Electron**: Create internal HTTP proxy in main process (`http.createServer` → forward to Unix socket)
- [ ] **Electron**: Generate random socket path (`os.tmpdir()` + random suffix)
- [ ] **Electron**: Spawn backend with `--bind-socket <path> --secret <token>`
- [ ] **Electron**: BrowserWindow loads `http://127.0.0.1:RANDOM` (Electron proxy port)
- [ ] **Electron**: Proxy injects `Authorization: Bearer <token>` header on forwarding
- [ ] **Verify**: Backend socket is not TCP-accessible (only via Unix socket)
- [ ] **Verify**: Socket file has correct permissions
- [ ] **Verify**: Desktop app loads and chats through proxy
- [ ] **Verify**: Stale socket cleaned up on crash/restart
- [ ] **Verify**: Windows fallback (named pipes or localhost+auth) works if applicable
- [ ] **Verify**: `make build-all` passes

### Phase C: `desktop-serve` Command
- [ ] **Go**: Create `cmd/desktop_serve.go` with Cobra command definition
- [ ] **Go**: Create `cmd/desktop_serve_modes.go` with server lifecycle logic (start, health, shutdown)
- [ ] **Go**: Implement workspace-scoped config (reuse isolated-config logic without daemon env)
- [ ] **Go**: Implement lazy agent initialization (on first chat request)
- [ ] **Go**: Implement graceful shutdown (SIGTERM → cancel → stop → exit, 10s force-kill)
- [ ] **Go**: Register `desktopServeCmd` in `cmd/root.go`
- [ ] **Go**: Verify `--port`, `--workspace`, `--bind-socket`, `--secret` flags
- [ ] **Go**: Verify `/health` endpoint responds
- [ ] **Go**: Unit tests for command flags and server lifecycle
- [ ] **Electron**: Update `desktop/backend.js` — change spawn to `sprout desktop-serve`
- [ ] **Electron**: Remove `--daemon`, `--isolated-config` flags and `SPROUT_DAEMON=1` env
- [ ] **Electron**: Update WSL spawn path similarly
- [ ] **Electron**: Verify health polling and crash detection still work
- [ ] **Cleanup**: Review daemon mode for desktop-specific workarounds to remove
- [ ] **Cleanup**: Decide on `SPROUT_DESKTOP` env var retention (keep as telemetry hint only)
- [ ] **Test**: Workspace open → chat → close window → backend exits
- [ ] **Test**: Workspace without provider → configure via web UI → chat
- [ ] **Test**: Two simultaneous workspaces (separate ports/processes)
- [ ] **Test**: Backend crash → Electron shows error page
- [ ] **Test**: WSL workspace (if applicable)
- [ ] **Verify**: `make build-all` passes
- [ ] **Verify**: CI passes

## Open Questions

1. **Should `desktop-serve` reuse `--isolated-config` logic?** — Likely yes. The workspace-scoped config is the right behavior. We just don't need the daemon baggage.
2. **Should we keep `SPROUT_DESKTOP=1` env var?** — Potentially useful for telemetry/debugging to distinguish desktop-launched vs CLI-launched servers, but should not gate any behavior.
3. **Should the desktop app pass `--no-project-skills`?** — Project skills are useful in the desktop context, so probably not. But worth confirming.
4. **Windows named pipes vs localhost+auth?** — Windows doesn't have Unix domain sockets (it has named pipes). Need to decide: use named pipes (requires different Go code path) or fall back to localhost+auth (Option C behavior) on Windows.
5. **Should `--bind-socket` be added to the standard `agent --daemon` command too?** — Potentially useful for non-desktop scenarios where you want socket-based access. Consider making it a general-purpose flag.
