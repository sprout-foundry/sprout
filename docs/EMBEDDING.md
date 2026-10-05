# Embedding the sprout WASM components in a webpage

Sprout's browser runtime ships as two small, dependency-free HTML pages
that are built alongside the main webui and are designed to be dropped
into an `<iframe>` on any product's page:

- **`terminal.html`** — xterm.js wired to the WASM POSIX shell. Your users
  can run real shell commands (ls, cat, grep — the file/text toolset)
  entirely in their browser. No server, no login.
- **`editor.html`** — CodeMirror 6 wired to the same WASM file layer.
  Opens, edits, and persists files in the browser's IndexedDB store.

Everything a host page needs is `window.postMessage`. Both pages announce
`{ source: 'sprout-terminal' | 'sprout-editor', type: 'ready' }` when
booted, and every subsequent interaction is a message exchange described
below (and implemented in
`webui/src/components/standalone/terminalMain.ts` and
`editorMain.ts` — those files are the protocol's source of truth).

**A runnable demo lives at `examples/embed/`** — a single host page that
embeds both components and drives the full protocol. The rest of this
document explains each step the demo takes.

---

## 1. Build the pieces

```bash
# The WASM runtime (sprout.wasm + wasm_exec.js) → webui/public/wasm/
make build-wasm

# The webui bundle, including terminal.html + editor.html + assets
# → webui/dist/
cd webui && npm install && npm run build && cd ..
```

`webui/dist/` is now self-contained:

```
dist/
├── index.html            ← the full webui app (not needed for embedding)
├── terminal.html         ← embeddable terminal page
├── editor.html           ← embeddable editor page
├── assets/…              ← JS/CSS chunks (both pages' bundles live here)
└── wasm/sprout.wasm      ← the WASM runtime (+ wasm_exec.js)
```

The two pages load everything from their own origin — no cross-origin
asset fetches, no service worker required for the embed path.

## 2. Serve it from one origin (the simple case)

Both iframes and your page must share an origin for the simplest setup
(see §5 for the cross-origin case). Any static file server pointed at
`webui/dist/` works:

```bash
cd webui/dist && python3 -m http.server 8788
```

Then, on your product's page:

```html
<iframe src="http://localhost:8788/terminal.html" width="600" height="400"></iframe>
```

That alone renders a working shell. The `postMessage` wiring below is
what makes it a *part of your product* instead of a stranded widget.

Or run the bundled demo, which serves the dist and the demo page on one
origin for you:

```bash
./examples/embed/serve.py --port 8788
# open http://localhost:8788/embed-demo.html
```

## 3. The host protocol

All messages are `window.postMessage` objects tagged with a `source`
field. The host sends `source: 'sprout-host'`; the components answer with
`source: 'sprout-terminal'` or `source: 'sprout-editor'`. Always pass the
embed's exact origin as `postMessage`'s target origin, and check
`event.origin` on the way in.

### Terminal (`terminal.html`)

| Direction | Message | Meaning |
|---|---|---|
| page → host | `{ type: 'ready' }` | The page is listening. (Both pages post `ready` even if WASM init failed — commands then answer "wasm shell not ready". Treat `ready` as "speak now", not "store healthy".) |
| host → page | `{ type: 'run', command }` | Execute one shell line in the WASM shell |
| host → page | `{ type: 'put', path, content }` | Write a file into the WASM filesystem |
| page → host | `{ type: 'command', command, exitCode }` | A command finished (user-typed or host-sent) |

Example — drop a file in and read it back:

```js
const frame = document.querySelector('iframe#terminal');
const origin = 'http://localhost:8788';
const send = (msg) => frame.contentWindow.postMessage({ source: 'sprout-host', ...msg }, origin);

window.addEventListener('message', (event) => {
  if (event.origin !== origin) return;
  const d = event.data || {};
  if (d.source === 'sprout-terminal' && d.type === 'ready') {
    send({ type: 'put', path: 'notes.txt', content: 'from your product\n' });
    send({ type: 'run', command: 'cat notes.txt' });   // output appears in the embedded terminal
  }
});
```

### Editor (`editor.html`)

| Direction | Message | Meaning |
|---|---|---|
| page → host | `{ type: 'ready' }` | Editor mounted (same caveat as above) |
| host → page | `{ type: 'open', path, content? }` | Open a file (content provided = seed; omitted = read from the WASM store) |
| host → page | `{ type: 'setDoc', content }` | Replace the visible document |
| host → page | `{ type: 'save' }` | Ask the editor to save |
| host → page | `{ type: 'theme', bg?, fg? }` | Match your product's colors (**editor only** — the terminal page has no theme handler) |
| page → host | `{ type: 'dirty', value }` | Edit state changed |
| page → host | `{ type: 'save', path, content }` | The save payload — **your page persists it** |

The editor already writes `content` to the browser store before posting
`save` — the message hands you `{ path, content }` so your page can also
send it to your backend (POST to your API, download…). Acknowledge with
`{ source: 'sprout-host', type: 'saved', path }` to clear its dirty flag.

### Shared filesystem

The terminal and the editor (and the full webui, if you also embed or
link it) share one browser-local store (`sprout-wasm-fs`, IndexedDB)
**per origin**. A file your product writes into the terminal via `put`
shows up in `ls` and opens in the editor at that path. Files live in the
user's browser — nothing leaves the machine unless your page sends it
somewhere.

## 4. Allowing the iframe: `SPROUT_FRAME_ANCESTORS`

When the pages are served by the sprout daemon (or any deployment of the
webui dist behind it), the default security posture forbids framing:
`X-Frame-Options: DENY` plus `Content-Security-Policy: frame-ancestors
'none'`. One environment variable opens framing to exactly the origins
you name:

```bash
# space- or comma-separated CSP source expressions
SPROUT_FRAME_ANCESTORS='https://admin.acme.com https://*.acme.dev' sprout agent --daemon
```

(The WebUI has no standalone subcommand — it auto-starts with the agent in
interactive or `--daemon` mode; `--daemon` keeps it serving without a
prompt.)

Setting it drops `X-Frame-Options` (it cannot express an allowlist) and
sets `frame-ancestors` to your list. Keep it narrow — it is the only CSP
directive that is configurable, by design.

If you serve `webui/dist/` from your own static server (§2), you own the
headers: the default here is *no* frame headers at all, so embedding
works out of the box — add your own `frame-ancestors` if the dist sits
behind a server that sets restrictive ones for you.

## 5. Cross-origin embedding

The demo is same-origin because that is the least to explain. The
protocol is origin-aware already; for a cross-origin setup:

- Your page lives at `https://app.acme.com`; the dist (or the daemon)
  lives at, say, `https://sprout.acme.com`.
- Iframe `https://sprout.acme.com/terminal.html` and set
  `postMessage`'s target origin to `'https://sprout.acme.com'`; check
  `event.origin === 'https://sprout.acme.com'` on inbound messages.
- If a daemon serves the dist, set `SPROUT_FRAME_ANCESTORS=https://app.acme.com`
  on it (§4). A static server needs no extra headers unless it adds some.
- The WASM filesystem is scoped to `sprout.acme.com` — that is a feature
  (your product's storage is separate), but it means the editor's files
  and the terminal's files live under the embed's origin, not yours.

`examples/embed/embed-demo.html` notes the single constant to change
(`EMBED_ORIGIN`).

## 5b. Embedding the cloud bundle (chat + in-browser agent)

`terminal.html`/`editor.html` are the lightweight embeds. The **full
cloud webui** — the React app with chat, the in-browser WASM agent loop,
the GitHub picker — is also embeddable (`index.html` of the cloud build,
normally served at `/webui/` by the platform). Several self-hosted
gateways already serve it at their own path (e.g. `/code`). This is a
bigger integration: the bundle expects a backend that speaks the hosted
platform's contract. What that contract actually is:

**Bootstrap (runtime config).** The host injects a config object the app
reads at boot. The fields that matter to a non-platform host:

- `appMode: 'cloud'` — selects the CloudAdapter.
- `navItems` — **send `[]`.** The fallback list
  (`CLOUD_NAV_ITEMS` in `webui/src/bootstrapAdapter.ts`) is the hosted
  platform's menu (Dashboard/Tasks/Billing/Team/Runners/Workspaces/Admin)
  and every entry is a dead end on a self-hosted origin.
- `platformURL` — the absolute base of the platform web UI used by every
  account-surface exit (`platformHref()` in
  `webui/src/utils/platformUrl.ts`). When absent, those links degrade to
  *relative* paths — i.e. they land on your origin's root. Set it to your
  real platform, or accept that account surfaces point at your own app.
- plus `apiBaseURL`, `wsURL`, `buildVersion`, `user` (`{id,email,tier}`).

**Backend endpoint set** (what the CloudAdapter fetches; grouped per
`webui/src/services/cloudEndpointRegistry/` — that registry plus
`cloudProxyRoutes.ts` is the machine-readable truth):

- **Chat**: `POST /proxy/chat` (the platform's LLM proxy; the webui's
  `{query, chat_id}` body is translated to `{provider, model, messages,
  stream}` — see `CHAT_ENDPOINT_MAP`/`TRANSLATE_BODY_PATHS` in
  `cloudProxyRoutes.ts`) and `GET /proxy/chat/status`. The in-browser
  agent loop itself runs locally in WASM (`/api/query*` never leaves the
  page except for status).
- **Git**: `GET|POST /api/git/*` (status, stage/commit, diff, log, push,
  pull, worktrees, …) — a large, stable surface listed in
  `cloudEndpointRegistry/endpoints/foundry-backend-git.ts`. These hit the
  git-proxy transport (`/git-proxy`) for remote operations.
- **GitHub account + picker**: `GET /user/me` (→ `{github_connected:
  boolean}`) and `GET /user/me/repos` (repo list). In cloud mode
  `usesPlatformGitHub()` is always true (it is just
  `gitCorsProxy() !== undefined`), so the repo picker is
  platform-hardwired: it reads those two endpoints, and its "Connect
  GitHub" CTA links to `platformHref('/?from=editor#/settings')` — the
  hosted platform's SPA settings. On a self-hosted origin without
  `platformURL` that resolves to your root (login/chat): a dead end. A
  self-host backend must implement both endpoints and either catch that
  redirect or set `platformURL` to where its own account settings live.
  (Host-driven picker configuration — pointing the CTA at a
  host-chosen route — is a requested follow-up.)
- **Tasks**: `GET|POST /api/tasks`, `GET /api/tasks/{id}`.
- **Escalation**: `GET|POST /workspace/fly` + the txn lifecycle
  (`/workspace/fly/{id}/txn/...`) — the "Run in cloud container" path.
  A self-host backend can implement it against any container host.
- **Settings/BYOK**: `GET|POST /api/settings`, `/api/settings/providers`,
  `/api/settings/credentials` (+ `/…/{id}`), `GET /api/providers`.
- **Misc**: `GET /api/stats`, `GET /api/terminal/agent-sessions*`,
  `GET /api/query/status`.

Auth on all of it is cookie-session (`credentials: 'include'`), with 401
→ `sprout:session-expired` event → redirect to
`/login?return_to=<here>`.

A reference self-hosted backend implementation of this surface exists in
production (an LLM gateway serving the bundle at `/code` — the
`/user/me*` GitHub account surface, BYOK settings stubs, `/api/tasks`,
`POST /api/repo/import`, git-proxy); a sketched version of it is the
natural companion follow-up to this document.

## 6. Sizing, theming, lifecycle

- **Sizing**: both pages fill their viewport (`html, body { height: 100% }`).
  Size the iframe; the terminal refits via the xterm FitAddon on window
  resize.
- **Theming**: the terminal's palette and the editor's colors are set in
  their `standalone/*Main.ts` entry files. The editor accepts a
  `{ type: 'theme', bg, fg }` message; the terminal accepts
  `{ type: 'theme', ... }` for the same purpose. Pass your product's
  colors after `ready`.
- **Lifecycle**: `ready` is your start signal — send commands only after
  it. On iframe reload the whole WASM runtime reboots and re-announces
  `ready`; re-seed any files your flow depends on at that point. There is
  no unload message; treat `put`/`save` as immediately durable in the
  browser store.
- **Browser support**: anything with WebAssembly + IndexedDB (all evergreen
  browsers). The store is per-origin and per-browser-profile; there is no
  cross-device sync unless your host implements it over the protocol.

## 7. What the embed cannot do (yet)

Standalone pages: no chat, no agent loop, no auth, no git bridge (the
browser-git dispatch needs the full webui's bridge, read-only when
present). Cloud bundle: needs a backend for the §5b surface, and the
GitHub picker is platform-hardwired today (host-driven picker
configuration is the requested fix).

The WASM shell covers the POSIX-ish core (file tools, text processing).
It cannot reach your network, spawn real processes, or run native
toolchains — a `go build` in the embedded terminal runs nothing. `git`
answers only after the full webui app has registered its browser-git
bridge (the standalone embed pages don't ship it), and read-only at that
(status/log/diff/show — see `pkg/wasmshell/commands_git.go`). For full
builds, the hosted path (platform workspaces, SP-BUILDER-5) remains the
answer; the browser shell is for light in-page work: editing,
inspection, format/transform tools, teaching, support consoles.

WASM feature parity is tracked in `docs/WASM_API.md` (its "Build state"
note).

## 8. Demo checklist

```
make build-wasm                     # or ./scripts/build-wasm.sh
cd webui && npm run build && cd ..
./examples/embed/serve.py           # serves dist + demo on :8788
open http://127.0.0.1:8788/embed-demo.html
```

Expect: both panels reach `ready`; the terminal prints `hello from the
host page` (the host `put` + `cat` round-trip); Open → edit → Save on the
editor logs the save payload and clears the dirty flag.
