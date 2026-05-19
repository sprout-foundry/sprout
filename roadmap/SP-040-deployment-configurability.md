# SP-040: Deployment Configurability — Untangling Hardcoded Ports and Hosts

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** MEDIUM (blocks third-party deployment, complicates Foundry/local mode parity)
**Depends on:** None
**Related:** SP-015 (Cloud Platform Integration), SP-032 (Daemon Mode Hardening — `SPROUT_BIND_ADDR` / `SPROUT_AUTH_TOKEN`), SP-034 (WebUI ↔ Backend Workflow Hardening)

## Problem

The frontend assumes it is being served by a specific backend at a specific port. Multiple values are hardcoded across the build, runtime, and bootstrap layers.

### Concrete failure sites

1. **`webui/package.json:101`** — `"proxy": "http://localhost:56000"`. The Vite dev server proxies API and WS traffic to this URL. Anyone running the backend on a different port during development must edit `package.json` (and risks committing the change).

2. **`webui/src/bootstrapAdapter.ts`** (29 lines) — computes the backend URL at runtime. Uses `window.location` heuristics. If the embedded asset is served from a non-default origin, the heuristics may be wrong. There is no environment-variable escape hatch.

3. **No build-time configuration.** The webui has no `.env`, `.env.local`, or `import.meta.env.VITE_*` consumption for API URL, WS URL, or auth flow. Every consumer gets the same baked-in assumptions.

4. **No frontend auth UI.** Verified by `grep`: `webui/src/services/fileAccess.ts` deals with file-access consent tokens, but there is no login screen, no bearer-token capture, no token persistence to `localStorage`/`sessionStorage`. The backend (`pkg/webui/auth_middleware.go:23`) validates `Authorization: Bearer <token>` for write endpoints, but the frontend never sends one in standalone deployment scenarios. The implicit assumption is "same-origin localhost with no auth needed" — fine for default `127.0.0.1:56000`, broken for any other deployment topology.

5. **Foundry / local-mode split is opaque.** `cloudAdapter` (per SP-015) routes WASM-local endpoints separately from network ones. There is no documented invariant about what env state is required for each mode; readers must trace the adapter to find out.

### Why it matters

- Anyone outside the author wanting to run sprout on a remote VM or a non-default port has to fork the repo and recompile.
- `SPROUT_BIND_ADDR` (SP-032 B1) lets the *backend* listen on non-localhost, but the *frontend* shipped in the same binary still assumes localhost. If you serve the embedded UI from a remote host, the frontend's API calls go to the wrong place.
- A login UI would also let the frontend recover from auth failures gracefully (current behavior: confusing 401s with no remediation in the UI).

## Goals / Non-Goals

**Goals**
- All deployment-relevant URLs and ports are configurable via environment variables at build time (`VITE_*`) **and** discoverable at runtime (`/api/bootstrap` endpoint).
- A minimal Login UI captures and persists a bearer token. The frontend sends `Authorization: Bearer <token>` on every API and WS request.
- A `LogOut` flow clears the token and returns to the login screen.
- 401 responses route the user to the login screen rather than silently failing.
- One documented deployment recipe per scenario: localhost default, custom port, remote VM behind reverse proxy, Foundry cloud.

**Non-Goals**
- Multi-tenant auth or user accounts (single bearer token is sufficient).
- OAuth/OIDC integration.
- HTTPS termination — assumed handled by reverse proxy or the embedded server's existing TLS support (if any).
- Anything Foundry-specific that overlaps SP-015.

## Current State

| Concern | File:Line | Issue |
|---------|-----------|-------|
| Dev-server proxy hardcoded | `webui/package.json:101` | `"proxy": "http://localhost:56000"` |
| Bootstrap URL inference | `webui/src/bootstrapAdapter.ts` | No env override |
| Backend auth contract | `pkg/webui/auth_middleware.go:23-78` | Bearer-token gate on write endpoints; GET is open |
| `SPROUT_AUTH_TOKEN` source | `pkg/webui/server.go:69` | Read from env on startup |
| `SPROUT_BIND_ADDR` | (per SP-032 B1) | Backend bind; frontend unaware |
| Frontend auth UI | (does not exist) | No login screen, no `Authorization` header anywhere |
| Bootstrap endpoint | (does not exist) | Frontend cannot ask backend "what's your auth model?" |

## Proposed Solution

### Track A — Build-time and runtime configuration

A1. **Define the configuration surface.** Single `RuntimeConfig` interface used by the frontend:
```ts
type RuntimeConfig = {
  apiBaseURL: string;          // e.g. "/api" or "https://sprout.example.com/api"
  wsURL: string;               // e.g. "/api/ws" or "wss://sprout.example.com/api/ws"
  authMode: "none" | "bearer"; // backend tells the frontend if a token is required
  appMode: "local" | "cloud";  // surfaces in UI (debug banner, feature flags)
  buildVersion: string;
}
```

A2. **`VITE_*` env vars** consumed by Vite at build time:
  - `VITE_API_BASE_URL`
  - `VITE_WS_URL`
  - `VITE_AUTH_MODE` (default `"none"` for backward compat)
  - `VITE_APP_MODE` (default `"local"`)

A3. **Replace `webui/package.json:101` proxy hardcode.** Read from a Vite plugin config that consults `process.env.SPROUT_DEV_BACKEND_URL` with fallback to `http://localhost:56000` for default localhost dev.

A4. **`/api/bootstrap` endpoint** on the backend that returns the `RuntimeConfig` JSON (suitable for unauthenticated GET — it must work before login). The frontend fetches this on app start and uses it to populate the runtime config object. Build-time `VITE_*` vars override only when the runtime fetch fails (defensive default).

A5. **`bootstrapAdapter.ts` rewrite.** Replace the `window.location` heuristics with: (1) try fetching `/api/bootstrap`; (2) fall back to `import.meta.env.VITE_*` if the fetch fails; (3) fall back to localhost defaults if neither is available. Log a one-line warning to the console for each fallback step taken.

### Track B — Minimal Login UI

B1. **New `LoginPage` component** in `webui/src/components/LoginPage.tsx`. Single input field (token), Submit button. On submit: store token in `sessionStorage` under key `sprout_auth_token`; redirect to the app.

B2. **`AuthContext`** wrapping the app. Reads token from `sessionStorage` on mount; exposes `{token, setToken, clearToken, isAuthenticated}` via React context.

B3. **`apiClient` / `apiAdapter` injection.** Every API call adds `Authorization: Bearer <token>` if the auth context has a token. Apply identically to WebSocket connections (via subprotocol or first message after open).

B4. **401 handling.** A response interceptor: on 401, clear the stored token and route to `/login`. Surface a toast: "Your session expired. Please sign in again."

B5. **`LogOut` button** in the existing user menu (or settings panel). Clears the token, routes to `/login`.

B6. **Skip the LoginPage when `authMode === "none"`.** If the bootstrap call reports no auth, the LoginPage is bypassed and the app renders directly. This preserves the current localhost UX.

### Track C — Deployment documentation

C1. **`docs/DEPLOYMENT.md`** — one section per scenario:
  - **Default localhost** — `./sprout`; no env required; auth disabled by default.
  - **Custom port** — `./sprout --web-port=8000`; frontend auto-discovers via `/api/bootstrap`.
  - **Remote VM behind reverse proxy** — `SPROUT_BIND_ADDR=0.0.0.0`, `SPROUT_AUTH_TOKEN=<secret>`; nginx config example with WS upgrade; `Authorization` header passthrough.
  - **Foundry cloud mode** — pointer to SP-015 docs.
  - **Docker** — example `docker run` invocation with env vars and port mapping.

C2. **`docs/DEPLOYMENT.md` security section** — refresh the README disclaimer: bearer token requirement for non-localhost binds (already enforced by SP-032 B1 if landed), HTTPS-via-reverse-proxy guidance, no built-in TLS.

### Track D — Tests

D1. **Bootstrap endpoint test** — `pkg/webui/api_bootstrap_test.go`: GET `/api/bootstrap` returns the expected JSON with correct `authMode` based on whether `SPROUT_AUTH_TOKEN` is set.

D2. **AuthContext test** — `webui/src/contexts/AuthContext.test.tsx`: token persists across re-mounts, clearToken clears storage, 401 interceptor fires `clearToken`.

D3. **End-to-end auth flow test** — manual playbook in `docs/DEPLOYMENT.md` or automated via existing Playwright config (`playwright.config.js` exists at repo root): start backend with `SPROUT_AUTH_TOKEN`, open browser, expect LoginPage, enter token, expect app loads, click LogOut, expect LoginPage.

D4. **Bootstrap fallback test** — `bootstrapAdapter.test.ts`: when `/api/bootstrap` 404s, the adapter falls back to `VITE_*` env vars; when those are missing, falls back to localhost defaults.

## Implementation Phases

### Phase 1: Bootstrap endpoint + adapter rewrite
[ ] SP-040-1a: Define `RuntimeConfig` type in `pkg/webui/api_bootstrap.go` and `webui/src/types/runtimeConfig.ts`.
[ ] SP-040-1b: Implement `GET /api/bootstrap` returning `RuntimeConfig` (unauthenticated; `authMode` set based on `SPROUT_AUTH_TOKEN` env).
[ ] SP-040-1c: Rewrite `webui/src/bootstrapAdapter.ts` to fetch bootstrap → fall back to `VITE_*` → fall back to localhost defaults. Log each fallback step.
[ ] SP-040-1d: Update `bootstrapAdapter.test.ts` with all three fallback paths.

### Phase 2: Build-time configurability
[ ] SP-040-2a: Define `VITE_API_BASE_URL`, `VITE_WS_URL`, `VITE_AUTH_MODE`, `VITE_APP_MODE` in `webui/vite.config.ts` (or equivalent) with defaults.
[ ] SP-040-2b: Replace `webui/package.json:101` hardcoded proxy with a Vite plugin that consumes `process.env.SPROUT_DEV_BACKEND_URL`.
[ ] SP-040-2c: Add `.env.example` under `webui/` documenting every supported `VITE_*` var.

### Phase 3: Auth context + LoginPage
[ ] SP-040-3a: Create `webui/src/contexts/AuthContext.tsx` with `{token, setToken, clearToken, isAuthenticated}`.
[ ] SP-040-3b: Create `webui/src/components/LoginPage.tsx` with a single token-input form.
[ ] SP-040-3c: Wrap the app root in `AuthContext`; route to LoginPage when `authMode === "bearer"` and no token is stored.
[ ] SP-040-3d: Add `Authorization: Bearer <token>` injection to `webui/src/services/apiAdapter.ts` (or whichever module owns API calls).
[ ] SP-040-3e: Add subprotocol or first-message auth to WebSocket connections in `webui/src/services/websocket*.ts`.
[ ] SP-040-3f: Add 401 interceptor: clear token, route to LoginPage, show toast.
[ ] SP-040-3g: Add a Log Out menu item that calls `clearToken()`.

### Phase 4: Tests
[ ] SP-040-4a: `pkg/webui/api_bootstrap_test.go` — bootstrap endpoint shape + `authMode` toggling on `SPROUT_AUTH_TOKEN`.
[ ] SP-040-4b: `webui/src/contexts/AuthContext.test.tsx` — token lifecycle.
[ ] SP-040-4c: Playwright scenario: bearer-mode login flow (or a manual playbook if Playwright is not wired in).

### Phase 5: Documentation
[ ] SP-040-5a: Write `docs/DEPLOYMENT.md` with the four scenario sections.
[ ] SP-040-5b: Update `README.md` to point at `docs/DEPLOYMENT.md`; refresh the "use at your own risk" disclaimer with the bearer-token requirement for non-localhost binds.
[ ] SP-040-5c: Add a "Deployment configurability" note to `docs/WEB_UI.md`.

## Success Criteria

| Metric | Target |
|--------|--------|
| Frontend bundle assumes `localhost:56000` | No (configurable via bootstrap + env) |
| Login UI when `SPROUT_AUTH_TOKEN` is set | Visible and functional |
| 401 routes to login | Yes |
| `docs/DEPLOYMENT.md` covers 4 scenarios | Yes |
| Frontend works with `--web-port=8000` without code edits | Yes |

## Files Reference

| File | Action |
|------|--------|
| `pkg/webui/api_bootstrap.go` | Create: `/api/bootstrap` handler |
| `pkg/webui/api_bootstrap_test.go` | Create |
| `webui/src/bootstrapAdapter.ts` | Modify: fetch + fallback logic |
| `webui/src/contexts/AuthContext.tsx` | Create |
| `webui/src/components/LoginPage.tsx` | Create |
| `webui/src/services/apiAdapter.ts` | Modify: inject `Authorization` header; 401 interceptor |
| `webui/src/services/websocket*.ts` | Modify: WS auth |
| `webui/package.json` | Modify: remove hardcoded proxy; use env-aware Vite plugin |
| `webui/vite.config.ts` | Modify: define `VITE_*` defaults |
| `webui/.env.example` | Create |
| `docs/DEPLOYMENT.md` | Create |
| `docs/WEB_UI.md` | Modify: add configurability note |
| `README.md` | Modify: link to DEPLOYMENT; refresh disclaimer |

## Risks

- **`/api/bootstrap` being unauthenticated leaks information.** Mitigation: it returns only public metadata (URLs, auth mode, build version), never secrets or user data.
- **Token in `sessionStorage` is vulnerable to XSS.** Mitigation: sprout-served content is fully under our control; CSP via the backend (`pkg/webui/`) can be tightened. Document the threat model in `docs/DEPLOYMENT.md`.
- **WS auth via subprotocol is non-standard.** Mitigation: use first-message auth instead; reject connection if first message is not a valid auth payload within 5s.
- **Bootstrap-then-render adds a roundtrip to first paint.** Mitigation: ship a `VITE_*`-baked default for the dominant case (localhost) so the bootstrap fetch is best-effort, not a blocker.
- **Existing localhost users see a Login page they didn't before.** Mitigation: `authMode: "none"` is the default; LoginPage is skipped. Only deployments that opt into `SPROUT_AUTH_TOKEN` see the new UI.
