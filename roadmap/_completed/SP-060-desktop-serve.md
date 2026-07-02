# SP-060: Desktop App — Per-Workspace Server Mode

**Status:** ✅ Implemented (2026-06-26; auth middleware, nil-agent daemon, random ports, Electron secret injection)

The desktop app needed a proper per-workspace server instead of reusing the general-purpose `--daemon` mode. This spec shipped auth middleware on write endpoints, nil-agent daemon mode (web UI starts without a provider configured), random port allocation, and Electron-side secret generation with header injection. Phase B (Unix domain socket IPC) was deferred — the current TCP + random port + auth token approach is sufficient for production.

## Key decisions

- Reused `--daemon` mode with nil-agent support instead of building a dedicated `desktop-serve` command — the daemon already covers the use case.
- Auth middleware only protects write methods (POST/PUT/PATCH/DELETE); GETs remain open since they are read-only and the main risk is unauthorized writes.
- Unix domain socket (Phase B) deferred — TCP + random port + bearer token provides adequate security for the desktop context without the cross-platform complexity of socket IPC.
- Electron generates a 256-bit secret per launch, passes it as `SPROUT_AUTH_TOKEN` env var, and injects it via `session.webRequest` on renderer requests.
- Non-localhost binds require `SPROUT_AUTH_TOKEN` — the server refuses to start otherwise, preventing accidental exposure.

## Artifacts

- code: `pkg/webui/auth_middleware.go` — bearer-token auth middleware on write endpoints using constant-time comparison
- code: `pkg/webui/server.go` — reads `SPROUT_AUTH_TOKEN` env var, enforces non-localhost protection
- code: `cmd/agent_modes.go` — nil-agent daemon mode: web UI starts without provider configured
- code: `desktop/backend.js` — random port allocation via `findFreePort()`, Electron secret generation
- tests: `pkg/webui/auth_middleware_test.go` — 19 unit tests for auth middleware behavior

Full specification archived — see git history for original content.
