# Cloud backend contract — self-hosting the cloud webui bundle

The cloud webui bundle (chat, in-browser WASM agent, GitHub picker, editor)
is a static frontend that expects an HTTP backend speaking the hosted
platform's contract. That backend lives in a **private repo**, so this
document catalogs the contract from the public side — every integration
point the bundle relies on, with shapes — so a self-hosted product can
implement its own backend without reverse-engineering anything.

Scope: this is the **embed direction with chat and the agent** (the full
`index.html` bundle). The lightweight standalone embeds (`terminal.html`,
`editor.html`) need none of this — see `docs/EMBEDDING.md`. Companion
reading: `docs/WASM_API.md` (the in-browser `SproutWasm` API) and
`docs/txn-protocol.md` (the escalation daemon contract).

Code sources of truth (all public, all in `webui/src/`):

| Area | File |
|---|---|
| Endpoint classification (this doc's tables) | `services/cloudEndpointRegistry/` |
| Request proxying + chat body translation | `services/cloudProxyRoutes.ts`, `services/cloudAdapter.ts` |
| GitHub account + repo picker | `services/platformGitHub.ts` |
| Editor model (BYOK) | `services/editorModel.ts` |
| Tasks | `services/cloudTasks.ts` |
| Platform links/notifications | `utils/platformUrl.ts`, `services/platformNotifications.ts`, `services/agentErrorMessage.ts` |
| Bootstrap config | `bootstrapAdapter.ts`, `types/runtimeConfig.ts` |

## 1. Bootstrap — `GET /api/bootstrap`

The first request the bundle makes; the JSON response configures the
entire app. All fields optional (absent → defaults):

```jsonc
{
  "appMode": "cloud",              // REQUIRED for the embed path: selects the CloudAdapter
  "apiBaseURL": "",                // same-origin default when empty/absent
  "wsURL": "wss://…/ws",           // websocket base (same-origin derived when absent)
  "authMode": "none",              // "none" | "bearer"
  "buildVersion": "1.2.3",
  "user": { "id": "…", "email": "a@b.c", "tier": "pro", "admin": false },
  "navItems": [],                  // REQUIRED [] for a non-platform host — see below
  "platformURL": "https://…",      // absolute platform web-UI base — see below
  "pluginScripts": [],             // IIFE bundle URLs loaded at boot
  "sync": null,                    // optional git snapshot (ETH-1)
  "update": null,                  // optional { current, latest }
  "sharedMode": false
}
```

Two fields carry the platform assumptions that bite self-hosters:

- **`navItems`** — send `[]`. The fallback when absent
  (`CLOUD_NAV_ITEMS`, `bootstrapAdapter.ts`) is the hosted platform's
  menu — Dashboard, Tasks, Billing, Team, Runners, Workspaces, Admin —
  and every entry is a dead end on your origin.
- **`platformURL`** — the absolute base for every "account surface" link
  (`platformHref()` in `utils/platformUrl.ts`). When absent those links
  degrade to **relative** paths, landing on your origin's root (login or
  chat). Set it to wherever your product's account/settings UI lives, or
  implement the routes the links name (§3).

## 2. Session model

All backend requests are same-origin fetches with
`credentials: 'include'` and the `X-Sprout-Client-ID` header (an opaque
per-browser id the host may echo back). A `401` from any proxied request
dispatches a `sprout:session-expired` CustomEvent and redirects the
browser to `/login?return_to=<current URL>` (same tab). A self-host
backend implements that login route (or accepts the redirect as its own
sign-in) and establishes whatever session cookie it wants; the bundle
never inspects it.

## 3. Platform (account) endpoints

These use `platformHref()` — `platformURL` + path, falling back to a
relative path — and are about the **user's account**, not the editor
session:

| Endpoint | Methods | Shape | Used by |
|---|---|---|---|
| `/user/me` | GET | `{ github_connected: boolean, … }` | GitHub picker gate (`platformGitHub.ts`) |
| `/user/me/repos?…` | GET | repo list (`{ id, name, host?, … }`) | GitHub picker (`platformGitHub.ts`) |
| `/user/me/repos` | POST | repo create (picker "new repo") | GitHub picker |
| `/user/me/editor-model` | GET/PUT | `{ provider, model, key_providers, recommend_byok }` | editor model (BYOK) (`editorModel.ts`) |
| `/user/me/editor-model/models?provider=…` | GET | `{ models: [{ id, context_length? }] }` | editor model picker |
| `/notifications` | GET | notifications payload | `platformNotifications.ts` |
| `/?from=editor#/settings` | — | **Link target** (not fetched) | "Connect GitHub" CTA, billing links (`agentErrorMessage.ts` → `/account/billing`) |

**The GitHub picker is platform-hardwired today.** In cloud mode
`usesPlatformGitHub()` is always true (it is literally
`gitCorsProxy() !== undefined`), so the picker reads `/user/me` +
`/user/me/repos` and its "Connect GitHub" CTA links to the settings
route above. A self-host backend therefore:

1. implements `/user/me` (report `github_connected`) and
   `GET/POST /user/me/repos` (your own GitHub OAuth app's repo list /
   repo create), and
2. either serves a settings page at the CTA's path or sets `platformURL`
   so the CTA points at your real account UI.

*Known gap:* the picker cannot be pointed at a host-chosen settings route
today — host-driven picker configuration is the requested follow-up.

## 4. Editor-session endpoints (the backend contract proper)

Everything in the generated tables below is requested from the bundle's
own origin with the session from §2. Grouped by what a self-host must do
(the generated table covers only registry-classified endpoints — the
chat proxy, `/api/git/*`, `/api/repo/import`, and the `/user/me*`
account surface bypass the registry and are documented by hand here and
in §3):

- **Chat** — `POST /proxy/chat` (streaming SSE; the webui's
  `{query, chat_id}` body is translated to Foundry's
  `{provider?, model?, messages, stream, …}` — see
  `translateRequestBody` in `cloudProxyRoutes.ts`; empty `provider`
  becomes `platform`) and `GET /proxy/chat/status`. The agent loop runs
  **in the browser** (WASM); only these two leave the page.
- **Git plumbing** — `/api/git/*` (status, stage, commit, diff, log,
  push, pull, worktrees, commit-message, …). File content itself lives in
  the browser (WASM VFS); these operate on the session.
- **Tasks** — `GET|POST /api/tasks`, `GET /api/tasks/{id}`.
- **Repo import** — `POST /api/repo/import` with `{url}` → clones
  server-side and returns the file tree as JSON, which the bundle writes
  into its browser VFS.
- **Escalation** — `GET|POST /workspace/fly` and the txn lifecycle under
  `/workspace/fly/{id}/txn/…` (open/status/push/run/pull/finish): the
  "Run in cloud container" action. See `docs/txn-protocol.md` for the
  daemon-side contract these proxy to. A self-host can implement them
  against any container host it runs.
- **Settings / BYOK** — `GET|POST /api/settings`,
  `GET|POST /api/settings/providers` (+ `/{id}`),
  `GET|POST /api/settings/credentials` (+ `/{id}`), `GET /api/providers`.
- **Misc** — `GET /api/stats`, `GET /api/terminal/agent-sessions*`,
  `GET /api/query/status`.

## 5. Browser-local state (no backend involvement)

- **Files**: WASM VFS in IndexedDB (`sprout-wasm-fs`, per origin).
- **Chat transcripts / sessions**: browser-local in cloud mode
  (localStorage + in-page agents).
- **App state** (layout, per-repo editor state): localStorage, keyed by
  instance + repo scope (`services/appStatePersistence.ts`).

Nothing here needs syncing to work; a self-host can ignore all of it.

<!-- endpoints:generated -->

### Backend endpoints a self-host server MUST implement

Every request below is sent by the CloudAdapter to the origin serving the bundle, with `credentials: 'include'` and the `X-Sprout-Client-ID` header. A 401 anywhere triggers the session-expired flow (`/login?return_to=…`).

| Endpoint | Methods | Purpose |
|---|---|---|
| `/api/providers` | GET | List available providers |
| `/api/query/status` | GET | Query execution status |
| `/api/settings` | GET, PUT | User settings (Foundry manages) |
| `/api/settings/credentials` | GET | Get credentials (Foundry manages) |
| `/api/settings/credentials//…` | GET, PUT, DELETE, POST | Credential CRUD (includes pool and test sub-paths) |
| `/api/settings/providers` | GET, PUT | Provider settings |
| `/api/settings/providers//…` | GET, PUT, DELETE | Provider CRUD |
| `/api/stats` | GET | Execution stats |
| `/api/tasks` | GET, POST | List/create user tasks (webui compatibility) |
| `/api/tasks//…` | GET | Get task status/details by id |
| `/api/terminal/agent-sessions` | GET | List background agent terminal sessions (needs backend) |
| `/api/terminal/agent-sessions//…` | GET, POST | Agent session actions (output, attach, kill) — needs backend |
| `/workspace/fly` | GET, POST | List/create Fly workspaces (escalation + txn workspace resolve) |
| `/workspace/fly//…` | POST, GET | Fly workspace txn lifecycle (open/status/push/run/pull/finish) |

### Endpoints handled inside the browser (WASM)

Never leave the page — a self-host backend does not need to implement these. Listed so the contract is complete.

| Endpoint | Methods | Purpose |
|---|---|---|
| `/api/ask-user/response` | POST | Deliver ask_user response to the WASM agent (unblocks pending ask_user request) |
| `/api/browse` | GET | Directory browsing (WASM handles locally) |
| `/api/create` | POST | File creation (WASM handles locally) |
| `/api/delete` | DELETE, POST | File deletion (WASM handles locally) |
| `/api/file` | GET, POST | Read/write file content (WASM handles locally) |
| `/api/file/check-modified` | GET, POST | Check if file modified (WASM handles locally) |
| `/api/file/consent` | POST | File consent (no security prompts in cloud mode) |
| `/api/files` | GET | File listing (WASM handles locally) |
| `/api/files/prettier-config` | GET | Prettier config for a file (WASM filesystem) |
| `/api/query` | POST | Agent query (WASM shell runs the full agent loop in-browser) |
| `/api/query/steer` | POST | Steer agent mid-conversation (WASM shell injects into steering channel) |
| `/api/query/stop` | POST | Stop agent execution (WASM shell interrupts in-browser agent) |
| `/api/rename` | POST | File rename (WASM handles locally) |
| `/api/search` | GET | Search files (WASM handles locally) |
| `/api/search/replace` | POST | Search and replace in files (WASM handles) |
| `/api/terminal/history` | GET, POST | Terminal history (WASM terminal) |
| `/api/terminal/sessions` | GET | Terminal sessions (WASM terminal manages) |
| `/api/terminal/shells` | GET | Available shells (WASM terminal) |
| `/api/workspace/browse` | GET | Workspace directory browsing (WASM filesystem) |

### Endpoints answered with synthetic safe-defaults

The adapter answers these itself with safe defaults; a backend may ignore them.

| Endpoint | Methods | Purpose |
|---|---|---|
| `/api/chat-session//…` | GET, POST | Chat session worktree sub-endpoints (not available in browser mode) |
| `/api/chat-sessions/compact` | POST | Compact chat session (not available in browser mode) |
| `/api/chat-sessions/create-in-worktree` | POST | Create session in worktree (not available in browser mode) |
| `/api/chat-sessions/worktree-mappings` | GET | Worktree mappings (not available in browser mode) |
| `/api/config` | GET | App config (empty in cloud mode) |
| `/api/diagnostics` | POST | Language diagnostics (not available in browser mode) |
| `/api/hotkeys` | GET, PUT | Built-in hotkeys (custom bindings need the desktop app) |
| `/api/hotkeys/preset` | POST | Hotkey preset (not available in browser mode) |
| `/api/hotkeys/validate` | POST | Hotkey validation (not available in browser mode) |
| `/api/instances` | GET | List instances (no local instances in cloud mode) |
| `/api/instances/select` | POST | Select instance (not available in cloud mode) |
| `/api/instances/ssh-browse` | POST | Browse SSH directory (not available in cloud mode) |
| `/api/instances/ssh-close` | POST | Close SSH session |
| `/api/instances/ssh-hosts` | GET | List SSH hosts (not available in cloud mode) |
| `/api/instances/ssh-launch-status` | GET | SSH launch status |
| `/api/instances/ssh-open` | POST | Open SSH workspace (not available in cloud mode) |
| `/api/instances/ssh-sessions` | GET | List SSH sessions (not available in cloud mode) |
| `/api/lsp/status` | GET | LSP server status (none in browser mode) |
| `/api/lsp/ws` | GET | LSP WebSocket bridge (not available in browser mode) |
| `/api/onboarding/complete` | POST | Complete onboarding |
| `/api/onboarding/skip` | POST | Skip onboarding |
| `/api/onboarding/status` | GET | Onboarding status (cloud is pre-configured) |
| `/api/providers/models` | GET | Provider model listing (not available in browser mode) |
| `/api/semantic` | POST | Semantic operations — go-to-def, hover etc (not available in browser mode) |
| `/api/sessions/search` | GET | Session search (not available in browser mode) |
| `/api/settings/mcp` | GET, PUT | MCP settings (not available in browser mode) |
| `/api/settings/mcp/servers//…` | GET, POST, PUT, DELETE | MCP server CRUD (not available in browser mode) |
| `/api/settings/skills` | GET, PUT | Skill settings (not available in browser mode) |
| `/api/settings/subagent-types` | GET | Subagent type configs (not available in browser mode) |
| `/api/settings/subagent-types//…` | GET | Subagent type read-only (not available in browser mode) |
| `/api/support-bundle` | GET | Support bundle (not available in cloud mode) |
| `/api/upload/image` | POST | Image upload for vision (not available in browser mode) |
| `/api/workspace` | GET, POST | Workspace info (cloud mode: WASM shell owns workspace, virtual FS root) |
| `/api/workspace/symbols` | GET | Workspace symbols via LSP (not available in browser mode) |

### No-op endpoints

Accepted and discarded.

| Endpoint | Methods | Purpose |
|---|---|---|
| `/api/chat-sessions/pin` | POST | Pin chat session (no-op in cloud mode) |
| `/api/chat-sessions/unpin` | POST | Unpin chat session (no-op in cloud mode) |
| `/api/open-in-file-browser` | POST | Open in OS file browser (not applicable in cloud mode) |

<!-- /endpoints:generated -->

## Regenerating the tables

The tables above are generated from `cloudEndpointRegistry` (the code's
own classification), so they cannot silently drift:

```bash
npx tsx scripts/gen-cloud-endpoints.mjs           # regenerate
npx tsx scripts/gen-cloud-endpoints.mjs --check   # CI gate: exit 1 if stale
```

Run the generator whenever `cloudEndpointRegistry` changes.
